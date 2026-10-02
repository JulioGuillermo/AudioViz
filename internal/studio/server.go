// Package studio es la interfaz gráfica de AudioViz: un servidor web local (embebido en el
// binario) con editor de línea de tiempo, preview renderizado por la GPU y exportación.
package studio

import (
	"bufio"
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
	"os/exec"
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
	toolsDir string
	version  string
	logs     []string
}

type Options struct {
	Addr     string
	Project  string // project.yaml o carpeta a abrir al arrancar
	ToolsDir string
	Version  string
}

// Run arranca el estudio y bloquea hasta que se cierra el servidor
func Run(opt Options, ready func(url string)) error {
	s := &Server{hub: newHub(), version: opt.Version}
	s.startDir, _ = os.Getwd()
	s.toolsDir = findTools(opt.ToolsDir)
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
	mux.HandleFunc("GET /api/probe", s.hProbe)
	mux.HandleFunc("POST /api/export", s.hExport)
	mux.HandleFunc("POST /api/lyrics/generate", s.hGenLyrics)
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

func findTools(explicit string) string {
	cands := []string{explicit, os.Getenv("AUDIOVIZ_TOOLS")}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "tools"))
	}
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands, filepath.Join(wd, "tools"))
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "lyrics", "run.sh")); err == nil {
			return c
		}
	}
	return ""
}

// ---------------------------------------------------------------- utilidades HTTP

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

// ---------------------------------------------------------------- proyecto

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
	s.logf("📂 Proyecto abierto: %s", abs)
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
	st := map[string]any{"version": s.version, "project": s.meta(), "startDir": s.startDir,
		"lyricsTool": s.toolsDir != "", "logs": s.logs, "gpu": gpu.ListDevices(), "home": os.Getenv("HOME")}
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
		fail(w, 400, errors.New("indica una carpeta"))
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		fail(w, 500, err)
		return
	}
	path := filepath.Join(dir, "project.yaml")
	if _, err := os.Stat(path); err == nil {
		fail(w, 409, fmt.Errorf("ya existe un proyecto en %s", dir))
		return
	}
	cfg := config.DefaultConfig()
	cfg.Dir, _ = filepath.Abs(dir)
	cfg.Output.Path = filepath.Join(cfg.Dir, "output.mp4")
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
		fail(w, 404, errors.New("no hay proyecto abierto"))
		return
	}
	writeJSON(w, map[string]any{"meta": s.meta(), "project": ProjectToJSON(s.cfg)})
}

// hPutProject recibe el proyecto editado, lo aplica a la sesión y lo guarda
func (s *Server) hPutProject(w http.ResponseWriter, r *http.Request) {
	var p ProjectJSON
	if err := readJSON(r, &p); err != nil {
		fail(w, 400, err)
		return
	}
	s.mu.Lock()
	if s.sess == nil {
		s.mu.Unlock()
		fail(w, 404, errors.New("no hay proyecto abierto"))
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

// ---------------------------------------------------------------- análisis y letra

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
		fail(w, 404, errors.New("no hay proyecto abierto"))
		return
	}
	feats := sess.Features()
	n := len(feats)
	bass, mid, tre, rms := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	var beats []int
	for i, f := range feats {
		bass[i], mid[i], tre[i], rms[i] = f.BassEnergy, f.MidEnergy, f.TrebleEnergy, math.Min(f.OverallRMS*3, 1)
		if f.IsBeat {
			beats = append(beats, i)
		}
	}
	// Forma de onda: pico por columna (2 por segundo de cuadro)
	var wave []float64
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
	// Los tiempos se muestran sin el desfase (el desfase se aplica al renderizar)
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

// hPutLyrics guarda la letra editada como LRC con tiempo por palabra
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
		path = filepath.Join(filepath.Dir(s.projPath), "letra.lrc")
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

// hFrame renderiza un cuadro y lo devuelve como JPEG (?f=cuadro&scale=1|2|3|4)
func (s *Server) hFrame(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.sess
	mode := "auto"
	if s.cfg != nil {
		mode = s.cfg.Output.Renderer
	}
	s.mu.Unlock()
	if sess == nil {
		fail(w, 404, errors.New("no hay proyecto abierto"))
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

// ---------------------------------------------------------------- archivos

var kindExts = map[string][]string{
	"audio":  {".flac", ".wav", ".mp3", ".ogg"},
	"bg":     {".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v"},
	"lyrics": {".lrc", ".srt"},
	"txt":    {".txt"},
	"font":   {".ttf", ".otf"},
	"project": {".yaml", ".yml"},
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
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	dir, _ = filepath.Abs(dir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		fail(w, 400, err)
		return
	}
	type ent struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
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
			// Imágenes sin extensión (como el fondo del ejemplo) cuentan como fondo
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
	writeJSON(w, map[string]any{"path": dir, "parent": filepath.Dir(dir), "entries": out})
}

// hUpload guarda un archivo subido en <proyecto>/assets
func (s *Server) hUpload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	proj := s.projPath
	s.mu.Unlock()
	if proj == "" {
		fail(w, 400, errors.New("abre o crea un proyecto primero"))
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

// ---------------------------------------------------------------- trabajos

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
	cancel   context.CancelFunc
}

func (j *job) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	log := j.Log
	if len(log) > 40 {
		log = log[len(log)-40:]
	}
	return map[string]any{"id": j.ID, "kind": j.Kind, "status": j.Status, "progress": j.Progress,
		"message": j.Message, "output": j.Output, "log": log, "elapsed": time.Since(j.Started).Seconds()}
}

func (s *Server) startJob(kind string, run func(ctx context.Context, j *job) error) (*job, error) {
	s.mu.Lock()
	if s.job != nil && s.job.snapshot()["status"] == "running" {
		s.mu.Unlock()
		return nil, errors.New("ya hay un trabajo en marcha")
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
			j.Status, j.Message = "canceled", "Cancelado"
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
		fail(w, 400, errors.New("no hay proyecto abierto"))
		return
	}
	out := req.Path
	if out == "" {
		out = cfg.Output.Path
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(cfg.Dir, out)
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
		err = fork.ExportRange(ctx, cfg.Output.Renderer, start, end, out, func(p engine.Progress) {
			j.mu.Lock()
			j.Progress = float64(p.Done) / float64(max(p.Total, 1))
			if p.Stage == "mux" {
				j.Message = "Escribiendo MP4..."
			} else {
				j.Message = fmt.Sprintf("Cuadro %d/%d · %.0f FPS · quedan %.0f s", p.Done, p.Total, p.FPS, p.ETA)
			}
			j.mu.Unlock()
			if time.Since(last) > 300*time.Millisecond || p.Stage == "mux" {
				last = time.Now()
				s.hub.send("job", j.snapshot())
			}
		})
		if err == nil {
			j.mu.Lock()
			j.Message = "Exportado en " + out
			j.mu.Unlock()
			s.logf("✨ Exportado: %s", out)
		}
		return err
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}

// hGenLyrics lanza tools/lyrics/run.sh (Demucs + Whisper) para generar el .lrc
func (s *Server) hGenLyrics(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Txt  string `json:"txt"`
		Lang string `json:"lang"`
	}
	if err := readJSON(r, &req); err != nil || req.Txt == "" {
		fail(w, 400, errors.New("indica el archivo .txt con la letra"))
		return
	}
	if s.toolsDir == "" {
		fail(w, 400, errors.New("no se encontró tools/lyrics (debe estar junto al binario)"))
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	proj := s.projPath
	s.mu.Unlock()
	if cfg == nil || cfg.Audio.Path == "" {
		fail(w, 400, errors.New("el proyecto no tiene audio"))
		return
	}
	out := filepath.Join(filepath.Dir(proj), "letra.lrc")
	lang := req.Lang
	if lang == "" {
		lang = "en"
	}
	j, err := s.startJob("lyrics", func(ctx context.Context, j *job) error {
		cmd := exec.CommandContext(ctx, filepath.Join(s.toolsDir, "lyrics", "run.sh"), cfg.Audio.Path, req.Txt, out, "--lang", lang)
		pr, pw := io.Pipe()
		cmd.Stdout, cmd.Stderr = pw, pw
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { cmd.Wait(); pw.Close() }()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		sc.Split(scanLinesCR)
		stages := []string{"📝", "🎙️", "💾", "🧠", "🔧", "✅"}
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			j.mu.Lock()
			if strings.Contains(line, "%|") {
				j.Message = line
			} else {
				j.Log = append(j.Log, line)
				j.Message = line
				for k, st := range stages {
					if strings.HasPrefix(line, st) {
						j.Progress = float64(k+1) / float64(len(stages))
					}
				}
			}
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
			return errors.New("la generación de la letra falló (mira el registro)")
		}
		j.mu.Lock()
		j.Output = out
		j.Message = "Letra generada: " + out
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}

// scanLinesCR separa por \n y por \r (las barras de progreso usan \r)
func scanLinesCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
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

// ---------------------------------------------------------------- eventos (SSE)

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
		http.Error(w, "streaming no soportado", 500)
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
	fmt.Fprint(w, ": hola\n\n")
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
