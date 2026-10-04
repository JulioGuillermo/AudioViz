// Package studio is the AudioViz graphical interface: a local web server (embedded in the
// binary) with a timeline editor, GPU-rendered preview and export.
package studio

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"audioviz/internal/config"
	"audioviz/internal/engine"
	"audioviz/internal/gpu"
	"audioviz/internal/lyrics"
	"audioviz/internal/lyricsgen"
	"audioviz/internal/progress"
	"audioviz/internal/video"
)

//go:embed web
var webFS embed.FS

type Server struct {
	mu       sync.Mutex
	projPath string
	cfg      *config.ProjectConfig
	sess     *engine.Session
	renderMu sync.Mutex
	rend     *engine.Renderer
	hub      *hub
	job      *job
	startDir string
	// Folder holding the projects (New creates them there, Import unpacks there)
	workspace string
	version   string
	logs      []string
}

type Options struct {
	Addr      string
	Project   string // project.yaml or folder to open at startup
	Workspace string // folder holding the projects (default ~/AudioViz)
	Version   string
}

// Run starts the studio and blocks until the server is closed
func Run(opt Options, ready func(url string)) error {
	s := &Server{hub: newHub(), version: opt.Version}
	s.workspace = opt.Workspace
	if s.workspace == "" {
		s.workspace = DefaultWorkspace()
	}
	s.workspace, _ = filepath.Abs(s.workspace)
	if err := os.MkdirAll(s.workspace, 0o755); err != nil {
		return fmt.Errorf("workspace %s: %w", s.workspace, err)
	}
	s.startDir = s.workspace
	if opt.Project == "" {
		if _, err := os.Stat("project.yaml"); err == nil {
			opt.Project = "project.yaml"
		}
	}
	if opt.Project != "" {
		path := opt.Project
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			path = filepath.Join(path, "project.yaml")
		}
		if _, err := os.Stat(path); err == nil {
			if err := s.open(path); err != nil {
				return err
			}
		}
	}

	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/state", s.hState)
	mux.HandleFunc("POST /api/project/new", s.hNew)
	mux.HandleFunc("POST /api/project/open", s.hOpen)
	mux.HandleFunc("GET /api/project", s.hGetProject)
	mux.HandleFunc("PUT /api/project", s.hPutProject)
	mux.HandleFunc("GET /api/analysis", s.hAnalysis)
	mux.HandleFunc("GET /api/lyrics", s.hGetLyrics)
	mux.HandleFunc("PUT /api/lyrics", s.hPutLyrics)
	mux.HandleFunc("GET /api/frame", s.hFrame)
	mux.HandleFunc("GET /api/fs", s.hFS)
	mux.HandleFunc("POST /api/upload", s.hUpload)
	mux.HandleFunc("GET /api/media", s.hMedia)
	mux.HandleFunc("GET /api/thumb", s.hThumb)
	mux.HandleFunc("GET /api/workspace", s.hWorkspace)
	mux.HandleFunc("GET /api/project/package", s.hPackage)
	mux.HandleFunc("POST /api/project/collect", s.hCollect)
	mux.HandleFunc("POST /api/project/import", s.hImport)
	mux.HandleFunc("GET /api/exports", s.hExports)
	mux.HandleFunc("DELETE /api/exports", s.hDeleteExport)
	mux.HandleFunc("GET /api/probe", s.hProbe)
	mux.HandleFunc("POST /api/export", s.hExport)
	mux.HandleFunc("POST /api/lyrics/generate", s.hGenLyrics)
	mux.HandleFunc("POST /api/lyrics/resync", s.hResync)
	mux.HandleFunc("GET /api/images", s.hImages)
	mux.HandleFunc("POST /api/images/generate", s.hGenImage)
	mux.HandleFunc("GET /api/images/preview", s.hImagePreview)
	mux.HandleFunc("DELETE /api/images", s.hDeleteImage)
	mux.HandleFunc("POST /api/job/cancel", s.hCancel)
	mux.HandleFunc("GET /api/events", s.hub.serve)

	ln, err := net.Listen("tcp", opt.Addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	if strings.HasPrefix(opt.Addr, ":") || strings.HasPrefix(opt.Addr, "0.0.0.0") {
		url = "http://localhost:" + strings.TrimPrefix(strings.TrimPrefix(opt.Addr, "0.0.0.0"), ":")
	}
	if ready != nil {
		ready(url)
	}
	return http.Serve(ln, mux)
}

// ---------------------------------------------------------------- HTTP utilities

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(v)
}

func (s *Server) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s.mu.Lock()
	s.logs = append(s.logs, msg)
	if len(s.logs) > 200 {
		s.logs = s.logs[len(s.logs)-200:]
	}
	s.mu.Unlock()
	s.hub.send("log", msg)
}

// ---------------------------------------------------------------- project

func (s *Server) open(path string) error {
	abs, _ := filepath.Abs(path)
	cfg, err := config.LoadConfig(abs)
	if err != nil {
		return err
	}
	sess, err := engine.Open(cfg, s.logf)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.sess
	s.projPath, s.cfg, s.sess = abs, cfg, sess
	s.mu.Unlock()
	s.renderMu.Lock()
	if s.rend != nil {
		s.rend.Close()
		s.rend = nil
	}
	s.renderMu.Unlock()
	if old != nil {
		old.Close()
	}
	s.logf("📂 Project opened: %s", abs)
	s.hub.send("project", "open")
	return nil
}

type projectMeta struct {
	Path     string   `json:"path"`
	Dir      string   `json:"dir"`
	Name     string   `json:"name"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	FPS      int      `json:"fps"`
	Frames   int      `json:"frames"`
	Duration float64  `json:"duration"`
	HasAudio bool     `json:"hasAudio"`
	Tracks   []string `json:"trackNames"`
}

func (s *Server) meta() *projectMeta {
	if s.sess == nil {
		return nil
	}
	return &projectMeta{Path: s.projPath, Dir: filepath.Dir(s.projPath), Name: filepath.Base(filepath.Dir(s.projPath)),
		Width: s.sess.W, Height: s.sess.H, FPS: s.sess.FPS, Frames: s.sess.Frames, Duration: s.sess.Duration(),
		HasAudio: s.sess.Audio != nil, Tracks: config.TrackNames}
}

func (s *Server) hState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := map[string]any{"version": s.version, "project": s.meta(), "startDir": s.startDir, "workspace": s.workspace,
		"lyricsTool": true, "models": map[string]bool{"whisper": lyricsgen.WhisperModel.Present(), "demucs": lyricsgen.DemucsModel.Present()}, "logs": s.logs, "gpu": gpu.ListDevices(), "home": userHome()}
	if s.job != nil {
		st["job"] = s.job.snapshot()
	}
	writeJSON(w, st)
}

func (s *Server) hNew(w http.ResponseWriter, r *http.Request) {
	var req struct{ Dir, Name string }
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	dir := req.Dir
	if req.Name != "" {
		dir = filepath.Join(req.Dir, req.Name)
	}
	if dir == "" {
		fail(w, 400, errors.New("specify a folder"))
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		fail(w, 500, err)
		return
	}
	path := filepath.Join(dir, "project.yaml")
	if _, err := os.Stat(path); err == nil {
		fail(w, 409, fmt.Errorf("a project already exists in %s", dir))
		return
	}
	cfg := config.DefaultConfig()
	cfg.Dir, _ = filepath.Abs(dir)
	_ = os.MkdirAll(filepath.Join(cfg.Dir, exportsDir), 0o755)
	cfg.Output.Path = filepath.Join(cfg.Dir, exportsDir, filepath.Base(cfg.Dir)+".mp4")
	if err := cfg.BuildTimeline(); err != nil {
		fail(w, 500, err)
		return
	}
	if err := cfg.Save(path); err != nil {
		fail(w, 500, err)
		return
	}
	if err := s.open(path); err != nil {
		fail(w, 500, err)
		return
	}
	s.hState(w, r)
}

func (s *Server) hOpen(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	p := req.Path
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		p = filepath.Join(p, "project.yaml")
	}
	if err := s.open(p); err != nil {
		fail(w, 400, err)
		return
	}
	s.hState(w, r)
}

func (s *Server) hGetProject(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		fail(w, 404, errors.New("no project is open"))
		return
	}
	writeJSON(w, map[string]any{"meta": s.meta(), "project": ProjectToJSON(s.cfg)})
}

// hPutProject receives the edited project, applies it to the session and saves it
func (s *Server) hPutProject(w http.ResponseWriter, r *http.Request) {
	var p ProjectJSON
	if err := readJSON(r, &p); err != nil {
		fail(w, 400, err)
		return
	}
	// A body of another shape decodes into an empty project: never save that over the user's
	if len(p.Tracks) == 0 || p.Audio == nil || p.Output == nil {
		fail(w, 400, errors.New("not a project: tracks, audio and output are required"))
		return
	}
	s.mu.Lock()
	if s.sess == nil {
		s.mu.Unlock()
		fail(w, 404, errors.New("no project is open"))
		return
	}
	dir := filepath.Dir(s.projPath)
	path, sess := s.projPath, s.sess
	s.mu.Unlock()
	cfg, err := ProjectFromJSON(&p, dir)
	if err != nil {
		fail(w, 400, err)
		return
	}
	s.renderMu.Lock()
	err = sess.Update(cfg)
	if err == nil && s.rend != nil && !s.rend.Fits() {
		s.rend.Close()
		s.rend = nil
	}
	s.renderMu.Unlock()
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := cfg.Save(path); err != nil {
		fail(w, 500, err)
		return
	}
	s.mu.Lock()
	s.cfg = cfg
	m := s.meta()
	s.mu.Unlock()
	writeJSON(w, map[string]any{"meta": m, "saved": time.Now().Format("15:04:05")})
}

// ---------------------------------------------------------------- analysis and lyrics

func b64u8(vals []float64) string {
	b := make([]byte, len(vals))
	for i, v := range vals {
		b[i] = byte(math.Max(0, math.Min(255, math.Round(v*255))))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func (s *Server) hAnalysis(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		fail(w, 404, errors.New("no project is open"))
		return
	}
	feats := sess.Features()
	n := len(feats)
	bass, mid, tre, rms := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	beats := []int{}
	for i, f := range feats {
		bass[i], mid[i], tre[i], rms[i] = f.BassEnergy, f.MidEnergy, f.TrebleEnergy, math.Min(f.OverallRMS*3, 1)
		if f.IsBeat {
			beats = append(beats, i)
		}
	}
	// Waveform: peak per column (2 per frame second)
	wave := []float64{}
	if a := sess.Audio; a != nil {
		cols := n
		per := len(a.Samples) / 2 / max(cols, 1)
		wave = make([]float64, cols)
		for c := 0; c < cols; c++ {
			pk := 0.0
			for i := c * per; i < (c+1)*per && i*2+1 < len(a.Samples); i++ {
				pk = math.Max(pk, math.Abs(float64(a.Samples[i*2]))+math.Abs(float64(a.Samples[i*2+1])))
			}
			wave[c] = math.Min(pk/2, 1)
		}
	}
	writeJSON(w, map[string]any{"fps": sess.FPS, "frames": n, "bass": b64u8(bass), "mid": b64u8(mid),
		"treble": b64u8(tre), "rms": b64u8(rms), "wave": b64u8(wave), "beats": beats})
}

type wordJSON struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type lineJSON struct {
	Start float64    `json:"start"`
	End   float64    `json:"end"`
	Words []wordJSON `json:"words"`
}

func (s *Server) lyricsPath() (string, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil || len(s.cfg.Timeline.Lyrics) == 0 {
		return "", 0
	}
	l := s.cfg.Timeline.Lyrics[0].Cfg
	return l.Path, l.Offset
}

func (s *Server) hGetLyrics(w http.ResponseWriter, r *http.Request) {
	path, _ := s.lyricsPath()
	if path == "" {
		writeJSON(w, map[string]any{"path": "", "lines": []lineJSON{}})
		return
	}
	// Times are shown without the offset (the offset is applied when rendering)
	lines, err := lyrics.Load(path, 0)
	if err != nil {
		fail(w, 400, err)
		return
	}
	out := []lineJSON{}
	for _, l := range lines {
		lj := lineJSON{Start: l.Start, End: l.End}
		for _, wd := range l.Words {
			lj.Words = append(lj.Words, wordJSON{wd.Text, wd.Start, wd.End})
		}
		out = append(out, lj)
	}
	writeJSON(w, map[string]any{"path": path, "lines": out})
}

func lrcTime(t float64) string {
	t = math.Max(t, 0)
	m := int(t / 60)
	return fmt.Sprintf("%02d:%05.2f", m, t-float64(m)*60)
}

// hPutLyrics saves the edited lyrics as LRC with per-word timing
func (s *Server) hPutLyrics(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lines []lineJSON `json:"lines"`
		Path  string     `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	path, _ := s.lyricsPath()
	if req.Path != "" {
		path = req.Path
	}
	if path == "" {
		s.mu.Lock()
		path = filepath.Join(filepath.Dir(s.projPath), "lyrics.lrc")
		s.mu.Unlock()
	}
	if !strings.HasSuffix(strings.ToLower(path), ".lrc") {
		path = strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc"
	}
	sort.Slice(req.Lines, func(i, j int) bool { return req.Lines[i].Start < req.Lines[j].Start })
	var b bytes.Buffer
	b.WriteString("[re:audioviz studio]\n")
	for _, l := range req.Lines {
		if len(l.Words) == 0 {
			continue
		}
		fmt.Fprintf(&b, "[%s]", lrcTime(l.Words[0].Start))
		for i, wd := range l.Words {
			if i > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "<%s>%s", lrcTime(wd.Start), wd.Text)
			// A pause before the next word: an empty tag marks where this one ends (otherwise
			// a word lasts until the next one starts and the karaoke fill runs through the silence)
			if i+1 < len(l.Words) && l.Words[i+1].Start-wd.End >= lrcGapMin {
				fmt.Fprintf(&b, " <%s>", lrcTime(wd.End))
			}
		}
		fmt.Fprintf(&b, " <%s>\n", lrcTime(l.Words[len(l.Words)-1].End))
	}
	if err := os.WriteFile(path, b.Bytes(), 0644); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"path": path})
}

// ---------------------------------------------------------------- preview

// hFrame renders a frame and returns it as JPEG (?f=frame&scale=1|2|3|4)
func (s *Server) hFrame(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.sess
	mode := "auto"
	if s.cfg != nil {
		mode = s.cfg.Output.Renderer
	}
	s.mu.Unlock()
	if sess == nil {
		fail(w, 404, errors.New("no project is open"))
		return
	}
	idx, _ := strconv.Atoi(r.URL.Query().Get("f"))
	scale, _ := strconv.Atoi(r.URL.Query().Get("scale"))
	scale = max(1, min(scale, 4))

	s.renderMu.Lock()
	if s.rend == nil {
		rend, err := engine.NewRenderer(sess, mode, 1)
		if err != nil {
			s.renderMu.Unlock()
			fail(w, 500, err)
			return
		}
		s.rend = rend
	}
	t0 := time.Now()
	fs, err := sess.Prepare(idx)
	var img *image.RGBA
	if err == nil {
		img, err = s.rend.Preview(fs, scale)
	}
	renderMs := time.Since(t0).Milliseconds()
	s.renderMu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Render-Ms", strconv.FormatInt(renderMs, 10))
	w.Header().Set("X-Total-Ms", strconv.FormatInt(time.Since(t0).Milliseconds(), 10))
	w.Write(buf.Bytes())
}

// ---------------------------------------------------------------- files

var kindExts = map[string][]string{
	"audio":   {".flac", ".wav", ".mp3", ".ogg"},
	"image":   {".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif"},
	"bg":      {".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v"},
	"lyrics":  {".lrc", ".srt"},
	"txt":     {".txt"},
	"font":    {".ttf", ".otf"},
	"project": {".yaml", ".yml"},
	"zip":     {".zip"},
}

func (s *Server) hFS(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	kind := r.URL.Query().Get("kind")
	if dir == "" {
		s.mu.Lock()
		dir = s.startDir
		if s.projPath != "" {
			dir = filepath.Dir(s.projPath)
		}
		s.mu.Unlock()
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, dir[1:])
	}
	type ent struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
	}
	// Windows: the list of drives sits above the root of each drive
	drives := fsDrives()
	if dir == drivesPath && len(drives) > 0 {
		var out []ent
		for _, d := range drives {
			out = append(out, ent{d, d, true, 0})
		}
		writeJSON(w, map[string]any{"path": drivesPath, "parent": drivesPath, "entries": out})
		return
	}
	dir, _ = filepath.Abs(dir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var out []ent
	exts := kindExts[kind]
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		isDir := e.IsDir()
		if !isDir && len(exts) > 0 {
			ok := false
			ext := strings.ToLower(filepath.Ext(e.Name()))
			for _, x := range exts {
				ok = ok || x == ext
			}
			// Images without an extension (like the example background) count as backgrounds
			if !ok && kind == "bg" && ext == "" {
				if _, err := video.ProbeMedia(filepath.Join(dir, e.Name())); err == nil {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, ent{e.Name(), filepath.Join(dir, e.Name()), isDir, info.Size()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	parent := filepath.Dir(dir)
	if parent == dir && len(drives) > 0 {
		parent = drivesPath
	}
	writeJSON(w, map[string]any{"path": dir, "parent": parent, "entries": out})
}

// hUpload saves an uploaded file to <project>/assets
func (s *Server) hUpload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	proj := s.projPath
	s.mu.Unlock()
	if proj == "" {
		fail(w, 400, errors.New("open or create a project first"))
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer file.Close()
	dir := filepath.Join(filepath.Dir(proj), "assets")
	os.MkdirAll(dir, 0755)
	name := filepath.Base(hdr.Filename)
	dst := filepath.Join(dir, name)
	f, err := os.Create(dst)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer f.Close()
	if _, err := io.Copy(f, file); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"path": dst})
}

func (s *Server) hMedia(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filepath.Base(p), `"`, "")))
	}
	http.ServeFile(w, r, p)
}

func (s *Server) hProbe(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	res, err := video.ProbeMedia(p)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, res)
}

// ---------------------------------------------------------------- jobs

type job struct {
	mu       sync.Mutex
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Status   string    `json:"status"` // running, done, error, canceled
	Progress float64   `json:"progress"`
	Message  string    `json:"message"`
	Output   string    `json:"output"`
	Log      []string  `json:"log"`
	Started  time.Time `json:"started"`
	Gen      *progress.Status
	Export   *exportStatus
	Previews int // image generation: previews received so far
	cancel   context.CancelFunc
}

// exportStatus: structured progress of an export, formatted by the UI
type exportStatus struct {
	Stage   string  `json:"stage"` // render, mux
	Done    int     `json:"done"`  // frames
	Total   int     `json:"total"`
	FPS     float64 `json:"fps"`     // render speed
	ETA     float64 `json:"eta"`     // s left
	Elapsed float64 `json:"elapsed"` // s since the start
	Seconds float64 `json:"seconds"` // length of the video being exported
}

func (j *job) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	log := j.Log
	if len(log) > 40 {
		log = log[len(log)-40:]
	}
	var exp *exportStatus
	if j.Export != nil {
		e := *j.Export
		exp = &e
	}
	return map[string]any{"id": j.ID, "kind": j.Kind, "status": j.Status, "progress": j.Progress,
		"message": j.Message, "output": j.Output, "log": log, "elapsed": time.Since(j.Started).Seconds(), "gen": j.Gen,
		"export": exp}
}

func (s *Server) startJob(kind string, run func(ctx context.Context, j *job) error) (*job, error) {
	s.mu.Lock()
	if s.job != nil && s.job.snapshot()["status"] == "running" {
		s.mu.Unlock()
		return nil, errors.New("a job is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Kind: kind, Status: "running", Started: time.Now(), cancel: cancel}
	s.job = j
	s.mu.Unlock()
	go func() {
		err := run(ctx, j)
		j.mu.Lock()
		switch {
		case errors.Is(err, context.Canceled):
			j.Status, j.Message = "canceled", "Canceled"
		case err != nil:
			j.Status, j.Message = "error", err.Error()
		default:
			j.Status, j.Progress = "done", 1
		}
		j.mu.Unlock()
		s.hub.send("job", j.snapshot())
	}()
	return j, nil
}

func (s *Server) hExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string  `json:"path"`
		Start  float64 `json:"start"`
		EndSec float64 `json:"end"`
	}
	_ = readJSON(r, &req)
	s.mu.Lock()
	sess, cfg := s.sess, s.cfg
	s.mu.Unlock()
	if sess == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	out := req.Path
	if out == "" {
		out = cfg.Output.Path
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(cfg.Dir, out)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fail(w, 400, err)
		return
	}
	j, err := s.startJob("export", func(ctx context.Context, j *job) error {
		fork, err := sess.Fork()
		if err != nil {
			return err
		}
		defer fork.Close()
		start := int(req.Start * float64(fork.FPS))
		end := fork.Frames
		if req.EndSec > 0 {
			end = min(int(req.EndSec*float64(fork.FPS)), fork.Frames)
		}
		j.mu.Lock()
		j.Output = out
		j.mu.Unlock()
		last := time.Time{}
		t0 := time.Now()
		secs := float64(end-start) / float64(fork.FPS)
		err = fork.ExportRange(ctx, cfg.Output.Renderer, start, end, out, func(p engine.Progress) {
			j.mu.Lock()
			j.Progress = float64(p.Done) / float64(max(p.Total, 1))
			stage := "render"
			if p.Stage == "mux" {
				stage = "mux"
				j.Message = "Writing MP4..."
			} else {
				j.Message = fmt.Sprintf("Exporting · %.0f%%", j.Progress*100)
			}
			j.Export = &exportStatus{Stage: stage, Done: p.Done, Total: p.Total, FPS: p.FPS, ETA: p.ETA,
				Elapsed: time.Since(t0).Seconds(), Seconds: secs}
			j.mu.Unlock()
			if time.Since(last) > 300*time.Millisecond || p.Stage == "mux" {
				last = time.Now()
				s.hub.send("job", j.snapshot())
			}
		})
		if err == nil {
			j.mu.Lock()
			j.Message = "Exported to " + out
			j.mu.Unlock()
			s.logf("✨ Exported: %s", out)
		}
		return err
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}

// hResync re-aligns only some lines of the current lyric (the client sends its edited lines)
func (s *Server) hResync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lines   []lineJSON `json:"lines"`
		Targets []int      `json:"targets"`
		Lang    string     `json:"lang"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	s.mu.Lock()
	sess, cfg := s.sess, s.cfg
	s.mu.Unlock()
	if sess == nil || sess.Audio == nil {
		fail(w, 400, errors.New("the project has no audio"))
		return
	}
	d := sess.Audio
	stereo := make([]float32, len(d.PCM))
	sc := float32(1.0 / float64(int64(1)<<(d.BitsPerSample-1)))
	for i, v := range d.PCM {
		stereo[i] = float32(v) * sc
	}
	in := make([]lyricsgen.LLine, len(req.Lines))
	for i, l := range req.Lines {
		for _, wd := range l.Words {
			in[i].Words = append(in[i].Words, lyricsgen.LWord{Text: wd.Text, Line: i, Start: wd.Start, End: wd.End})
		}
	}
	out, source, err := lyricsgen.Resync(r.Context(), lyricsgen.ResyncOptions{Stereo: stereo, SampleRate: d.SampleRate,
		AudioKey: audioKey(cfg.Audio.Path), Lang: req.Lang, Lines: in, Targets: req.Targets})
	if err != nil {
		fail(w, 500, err)
		return
	}
	res := make([]lineJSON, len(out))
	for i, l := range out {
		for _, wd := range l.Words {
			res[i].Words = append(res[i].Words, wordJSON{wd.Text, wd.Start, wd.End})
		}
		if len(l.Words) > 0 {
			res[i].Start, res[i].End = l.Words[0].Start, l.Words[len(l.Words)-1].End
		}
	}
	writeJSON(w, map[string]any{"lines": res, "source": source})
}

// audioKey identifies an audio file for the separated-vocals cache (path, size, mtime)
func audioKey(path string) string {
	if st, err := os.Stat(path); err == nil {
		return fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().Unix())
	}
	return path
}

// hGenLyrics generates the synchronized lyrics inside the binary itself (Demucs + Whisper + alignment)
func (s *Server) hGenLyrics(w http.ResponseWriter, r *http.Request) {
	// The lyrics come from a .txt file (txt), as text (text: the studio's lyrics editor), or
	// not at all (transcribe: the recognized words become the lyrics)
	var req struct {
		Txt        string `json:"txt"`
		Text       string `json:"text"`
		Transcribe bool   `json:"transcribe"`
		Lang       string `json:"lang"`
		Fast       bool   `json:"fast"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	txt := []byte(req.Text)
	switch {
	case req.Transcribe:
		txt = nil
	case req.Txt != "":
		b, err := os.ReadFile(req.Txt)
		if err != nil {
			fail(w, 400, err)
			return
		}
		txt = b
	case strings.TrimSpace(req.Text) == "":
		fail(w, 400, errors.New("give the lyrics (.txt file or text), or choose to transcribe the audio"))
		return
	}
	s.mu.Lock()
	sess, cfg, proj := s.sess, s.cfg, s.projPath
	s.mu.Unlock()
	if sess == nil || sess.Audio == nil {
		fail(w, 400, errors.New("the project has no audio"))
		return
	}
	d := sess.Audio
	key := audioKey(cfg.Audio.Path)
	out := filepath.Join(filepath.Dir(proj), "lyrics.lrc")
	j, err := s.startJob("lyrics", func(ctx context.Context, j *job) error {
		stereo := make([]float32, len(d.PCM))
		sc := float32(1.0 / float64(int64(1)<<(d.BitsPerSample-1)))
		for i, v := range d.PCM {
			stereo[i] = float32(v) * sc
		}
		res, err := lyricsgen.Generate(ctx, lyricsgen.Options{Stereo: stereo, SampleRate: d.SampleRate, AudioKey: key,
			Lyrics: string(txt), Lang: req.Lang, Separate: !req.Fast}, func(st lyricsgen.Status) {
			j.mu.Lock()
			j.Gen = &st
			j.Progress = st.Progress
			cur := st.Steps[st.Current]
			j.Message = cur.Label
			if cur.Detail != "" {
				j.Message += " · " + cur.Detail
			}
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		})
		if err != nil {
			return err
		}
		// If lyrics already existed, keep a backup copy
		if _, err := os.Stat(out); err == nil {
			_ = os.Rename(out, strings.TrimSuffix(out, ".lrc")+time.Now().Format("-20060102-150405")+".lrc")
		}
		if err := os.WriteFile(out, []byte(res.LRC), 0644); err != nil {
			return err
		}
		j.mu.Lock()
		j.Output = out
		j.Message = fmt.Sprintf("Lyrics generated: %d lines", len(res.Lines))
		j.mu.Unlock()
		s.logf("🎤 Lyrics generated: %s", out)
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}

func (s *Server) hCancel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	j := s.job
	s.mu.Unlock()
	if j != nil && j.cancel != nil {
		j.cancel()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- events (SSE)

type hub struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
}

func newHub() *hub { return &hub{clients: map[chan []byte]bool{}} }

func (h *hub) send(kind string, data any) {
	b, _ := json.Marshal(map[string]any{"type": kind, "data": data})
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- b:
		default:
		}
	}
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	c := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	fmt.Fprint(w, ": hello\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-c:
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// lrcGapMin: shortest silence between two words written to the .lrc (LRC times have 10 ms steps)
const lrcGapMin = 0.05

// userHome is the user's home folder (HOME on Unix, the profile folder on Windows)
func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}
