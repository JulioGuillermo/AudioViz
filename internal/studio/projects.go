package studio

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/depth"
	"audioviz/internal/video"
)

// Projects as a whole: the workspace (folder holding projects), packaging a project into a zip
// with everything it uses, collecting external files into the project, importing a package,
// and the exports/ folder where rendered videos go.

const exportsDir = "exports"

// DefaultWorkspace is where projects live when --workspace is not given
func DefaultWorkspace() string { return filepath.Join(userHome(), "AudioViz") }

// ---------------------------------------------------------------- workspace

type wsProject struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"` // project.yaml
	Modified time.Time `json:"modified"`
	Thumb    string    `json:"thumb"` // background of the first segment, for the thumbnail
	Exports  int       `json:"exports"`
}

// hWorkspace lists the projects of the workspace (folders with a project.yaml)
func (s *Server) hWorkspace(w http.ResponseWriter, r *http.Request) {
	ents, _ := os.ReadDir(s.workspace)
	out := []wsProject{}
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(s.workspace, e.Name(), "project.yaml")
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		wp := wsProject{Name: e.Name(), Path: p, Modified: st.ModTime()}
		if cfg, err := config.LoadConfig(p); err == nil {
			wp.Thumb = cfg.Background.Path
		}
		wp.Exports = len(listExports(filepath.Join(s.workspace, e.Name())))
		out = append(out, wp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	writeJSON(w, map[string]any{"workspace": s.workspace, "projects": out})
}

// ---------------------------------------------------------------- relocation

// plan decides where each file used by the project goes inside its folder: files already inside
// keep their place; external ones go to assets/<name>, with a suffix if that name is taken by a
// different file. It returns source (absolute) -> destination (relative to the project folder).
func plan(cfg *config.ProjectConfig) map[string]string {
	dir := cfg.Dir
	out := map[string]string{}
	taken := map[string]string{} // relative destination -> source
	files := cfg.Files()
	// First the files that are already inside: their places are fixed
	for _, f := range files {
		if rel, err := filepath.Rel(dir, f); err == nil && !strings.HasPrefix(rel, "..") {
			rel = filepath.ToSlash(rel)
			out[f], taken[rel] = rel, f
		}
	}
	for _, f := range files {
		if _, ok := out[f]; ok {
			continue
		}
		base := filepath.Base(f)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		for n := 1; ; n++ {
			name := base
			if n > 1 {
				name = fmt.Sprintf("%s-%d%s", stem, n, ext)
			}
			rel := "assets/" + name
			src, used := taken[rel]
			if !used {
				// On disk there may already be a file with that name (from an earlier collect)
				if st, err := os.Stat(filepath.Join(dir, rel)); err == nil && !sameFile(st, f) {
					continue
				}
			} else if src != f {
				continue
			}
			out[f], taken[rel] = rel, f
			break
		}
	}
	return out
}

// sameFile: same size and modification time (a copy made by collect keeps both)
func sameFile(st os.FileInfo, p string) bool {
	o, err := os.Stat(p)
	return err == nil && o.Size() == st.Size() && o.ModTime().Equal(st.ModTime())
}

// relocated returns a copy of the project whose paths point to their planned places
func relocated(cfg *config.ProjectConfig, places map[string]string) (*config.ProjectConfig, error) {
	data, err := cfg.Marshal()
	if err != nil {
		return nil, err
	}
	cp, err := config.ParseConfig(data, cfg.Dir)
	if err != nil {
		return nil, err
	}
	cp.Markers = cfg.Markers
	cp.RewritePaths(func(p string) string {
		if rel, ok := places[p]; ok {
			return filepath.Join(cfg.Dir, filepath.FromSlash(rel))
		}
		return p
	})
	return cp, nil
}

// ---------------------------------------------------------------- package (zip)

// Already compressed media gain nothing from deflate: they are stored as they are
var storedExt = map[string]bool{".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".avi": true, ".m4v": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".flac": true, ".mp3": true,
	".ogg": true, ".m4a": true, ".opus": true, ".zip": true}

// hPackage streams the open project as a zip: project.yaml (paths relative), every file it uses
// (external ones moved to assets/), the rest of assets/, and optionally the exported videos.
// level: 0 = store, 1..9 = deflate level (media are always stored).
func (s *Server) hPackage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg, projPath := s.cfg, s.projPath
	s.mu.Unlock()
	if cfg == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	level = min(max(level, 0), 9)
	withVideos := r.URL.Query().Get("videos") == "1"
	withDepth := r.URL.Query().Get("depth") == "1"
	withStems := r.URL.Query().Get("stems") == "1"
	name := filepath.Base(filepath.Dir(projPath))

	places := plan(cfg)
	cp, err := relocated(cfg, places)
	if err != nil {
		fail(w, 500, err)
		return
	}
	cp.Output.Path = filepath.Join(cfg.Dir, exportsDir, name+".mp4")
	data, err := cp.Marshal()
	if err != nil {
		fail(w, 500, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, level)
	})
	added := map[string]bool{}
	add := func(rel string, open func() (io.ReadCloser, os.FileInfo, error)) error {
		if added[rel] {
			return nil
		}
		added[rel] = true
		rc, st, err := open()
		if err != nil {
			return err
		}
		defer rc.Close()
		h := &zip.FileHeader{Name: name + "/" + rel, Method: zip.Deflate}
		if level == 0 || storedExt[strings.ToLower(path.Ext(rel))] {
			h.Method = zip.Store
		}
		h.Modified = time.Now() // project.yaml is generated now
		if st != nil {
			h.Modified = st.ModTime()
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, rc)
		return err
	}
	file := func(p string) func() (io.ReadCloser, os.FileInfo, error) {
		return func() (io.ReadCloser, os.FileInfo, error) {
			f, err := os.Open(p)
			if err != nil {
				return nil, nil, err
			}
			st, _ := f.Stat()
			return f, st, nil
		}
	}
	err = add("project.yaml", func() (io.ReadCloser, os.FileInfo, error) {
		return io.NopCloser(strings.NewReader(string(data))), nil, nil
	})
	for src, rel := range places {
		if err == nil {
			err = add(rel, file(src))
		}
	}
	// The rest of assets/ (uploads not used right now are part of the project too)
	if err == nil {
		err = walkFiles(filepath.Join(cfg.Dir, "assets"), func(p string) error { return add(relTo(cfg.Dir, p), file(p)) })
	}
	// its chat (messages and what was shared)
	if err == nil {
		err = walkFiles(filepath.Join(cfg.Dir, "chat"), func(p string) error { return add(relTo(cfg.Dir, p), file(p)) })
	}
	// the project's own folders: its ComfyUI flows, the images generated (with how they were
	// made) and what the flows made
	for _, d := range []string{"comfy", imagesDir, "videos", "audio", "masks"} {
		if err == nil {
			err = walkFiles(filepath.Join(cfg.Dir, d), func(p string) error {
				if strings.HasSuffix(p, ".tmp") {
					return nil
				}
				return add(relTo(cfg.Dir, p), file(p))
			})
		}
	}
	// the song separated into its instruments (stems=1: no need to separate it again)
	if err == nil && withStems && cfg.Audio.Path != "" {
		err = walkFiles(audio.StemsDir(filepath.Join(cfg.Dir, "cache"), cfg.Audio.Path), func(p string) error {
			if strings.HasSuffix(p, ".tmp") {
				return nil
			}
			return add(relTo(cfg.Dir, p), file(p))
		})
	}
	// the depth maps edited by hand (the user's work: always); with depth=1 also the worked out
	// depth maps the backgrounds use now (with a video's every frame: can be large). Their names
	// come from the files' content, so the imported project finds them
	if err == nil {
		err = walkFiles(filepath.Join(cfg.Dir, "depth-edits"), func(p string) error { return add(relTo(cfg.Dir, p), file(p)) })
	}
	if err == nil && withDepth {
		for _, cp := range s.depthMapsInUse(cfg) {
			if _, e := os.Stat(cp); e == nil {
				if err = add(relTo(cfg.Dir, cp), file(cp)); err != nil {
					break
				}
			}
			seq := strings.TrimSuffix(cp, ".png") + ".seq"
			if err = walkFiles(seq, func(p string) error { return add(relTo(cfg.Dir, p), file(p)) }); err != nil {
				break
			}
		}
	}
	if err == nil && withVideos {
		for _, v := range listExports(cfg.Dir) {
			if err = add(exportsDir+"/"+v.Name, file(v.Path)); err != nil {
				break
			}
		}
	}
	if err != nil {
		s.logf("⚠️  Project package: %v", err) // the response is already streaming
	}
	_ = zw.Close()
}

func relTo(dir, p string) string {
	rel, _ := filepath.Rel(dir, p)
	return filepath.ToSlash(rel)
}

func walkFiles(dir string, fn func(p string) error) error {
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		return fn(p)
	})
}

// ---------------------------------------------------------------- collect

// hCollect copies the files the project uses from outside its folder into assets/ and points the
// project to the copies, so the folder is self-contained.
func (s *Server) hCollect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg, projPath := s.cfg, s.projPath
	s.mu.Unlock()
	if cfg == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	places := plan(cfg)
	var copied []string
	for src, rel := range places {
		dst := filepath.Join(cfg.Dir, filepath.FromSlash(rel))
		if src == dst {
			continue
		}
		if _, err := os.Stat(dst); err != nil {
			if err := copyFile(src, dst); err != nil {
				fail(w, 500, err)
				return
			}
		}
		copied = append(copied, rel)
	}
	sort.Strings(copied)
	if len(copied) > 0 {
		cp, err := relocated(cfg, places)
		if err == nil {
			err = cp.Save(projPath)
		}
		if err == nil {
			err = s.open(projPath)
		}
		if err != nil {
			fail(w, 500, err)
			return
		}
		s.logf("📦 Collected %d files into assets/", len(copied))
	}
	writeJSON(w, map[string]any{"copied": copied})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, _ := in.Stat()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Same modification time: recognized as the same file by a later collect / package
	return os.Chtimes(dst, st.ModTime(), st.ModTime())
}

// ---------------------------------------------------------------- import

var unsafeName = regexp.MustCompile(`[^\pL\pN ._-]+`)

// hImport unpacks a project package into the workspace and opens it. The zip comes as an upload
// (field "file") or as the path of a zip on this machine (field "path").
func (s *Server) hImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail(w, 400, err)
		return
	}
	var (
		ra   io.ReaderAt
		size int64
		name string
	)
	if f, h, err := r.FormFile("file"); err == nil {
		defer f.Close()
		ra, size, name = f, h.Size, h.Filename
	} else if p := r.FormValue("path"); p != "" {
		f, err := os.Open(p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		ra, size, name = f, st.Size(), filepath.Base(p)
	} else {
		fail(w, 400, errors.New("no package given"))
		return
	}
	dir, err := unpack(ra, size, name, s.workspace)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.open(filepath.Join(dir, "project.yaml")); err != nil {
		fail(w, 400, err)
		return
	}
	s.logf("📥 Project imported: %s", dir)
	s.hState(w, r)
}

// unpack extracts a project package into a new folder of the workspace and returns it. The
// project.yaml may be at the root of the zip or inside one top folder (as hPackage writes it).
func unpack(ra io.ReaderAt, size int64, zipName, workspace string) (string, error) {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return "", fmt.Errorf("not a zip file: %w", err)
	}
	prefix := ""
	found := false
	for _, f := range zr.File {
		n := strings.TrimPrefix(f.Name, "./")
		if path.Base(n) == "project.yaml" && (!found || len(n) < len(prefix)+len("project.yaml")) {
			prefix, found = strings.TrimSuffix(n, "project.yaml"), true
		}
	}
	if !found {
		return "", errors.New("the zip has no project.yaml")
	}
	base := strings.TrimSuffix(prefix, "/")
	if base == "" {
		base = strings.TrimSuffix(filepath.Base(zipName), filepath.Ext(zipName))
	}
	base = strings.TrimSpace(unsafeName.ReplaceAllString(path.Base(base), "-"))
	if base == "" || base == "." || base == ".." {
		base = "project"
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return "", err
	}
	dir := filepath.Join(workspace, base)
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		dir = filepath.Join(workspace, fmt.Sprintf("%s-%d", base, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := extract(zr, prefix, dir); err != nil {
		_ = os.RemoveAll(dir) // a new folder: nothing of the user's is lost
		return "", err
	}
	_ = os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, exportsDir), 0o755)
	return dir, nil
}

// extract writes the entries under prefix into dir, refusing names that would leave it
func extract(zr *zip.Reader, prefix, dir string) error {
	for _, f := range zr.File {
		n := strings.TrimPrefix(f.Name, "./")
		if !strings.HasPrefix(n, prefix) || strings.HasSuffix(n, "/") {
			continue
		}
		rel := path.Clean(strings.TrimPrefix(n, prefix))
		// Never write outside the project folder (".." or absolute names in the zip)
		if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." || path.IsAbs(rel) || strings.Contains(rel, `\`) {
			return fmt.Errorf("unsafe path in the zip: %q", f.Name)
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err == nil {
			_, err = io.Copy(out, rc)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
		rc.Close()
		if err != nil {
			return err
		}
		// Keep the dates of the package (zip tools without dates write 1980: ignore those)
		if f.Modified.Year() > 1980 {
			_ = os.Chtimes(dst, f.Modified, f.Modified)
		}
	}
	return nil
}

// ---------------------------------------------------------------- exports

type exportFile struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Duration float64   `json:"duration"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
}

var videoExt = map[string]bool{".mp4": true, ".mov": true, ".mkv": true, ".webm": true}

func listExports(projDir string) []exportFile {
	dir := filepath.Join(projDir, exportsDir)
	ents, _ := os.ReadDir(dir)
	var out []exportFile
	for _, e := range ents {
		if e.IsDir() || !videoExt[strings.ToLower(filepath.Ext(e.Name()))] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, exportFile{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Size: st.Size(), Modified: st.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}

// hExports lists the videos in the project's exports/ folder (with duration and size)
func (s *Server) hExports(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if cfg == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	list := listExports(cfg.Dir)
	for i := range list {
		if pr, err := video.ProbeMedia(list[i].Path); err == nil {
			list[i].Duration, list[i].Width, list[i].Height = pr.Duration, pr.Width, pr.Height
		}
	}
	if list == nil {
		list = []exportFile{}
	}
	writeJSON(w, map[string]any{"dir": filepath.Join(cfg.Dir, exportsDir), "files": list})
}

// hDeleteExport deletes a video of the exports/ folder (by name only: nothing else can be removed)
func (s *Server) hDeleteExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	name := r.URL.Query().Get("name")
	if cfg == nil || name == "" || name != filepath.Base(name) || !videoExt[strings.ToLower(filepath.Ext(name))] {
		fail(w, 400, errors.New("invalid export"))
		return
	}
	if err := os.Remove(filepath.Join(cfg.Dir, exportsDir, name)); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// depthMapsInUse: where the depth maps of the project's backgrounds are kept (CachePath; for a
// video's every frame also its first frame's map, and its sequence next to it). Finding them also
// renames the ones kept under the old names (by the files' paths)
func (s *Server) depthMapsInUse(cfg *config.ProjectConfig) []string {
	cache := filepath.Join(cfg.Dir, "cache")
	var out []string
	for _, b := range cfg.Timeline.Background {
		d := b.Cfg.Depth
		if b.Cfg.Path == "" || d.Source == "" || d.Source == "none" {
			continue
		}
		model, detail := d.Model, d.Detail
		if model == "" {
			model = "small"
		}
		if detail == "" {
			detail = "high"
		}
		q := s.depthRequest(depthReq{Image: b.Cfg.Path, Source: d.Source, Model: model, Detail: detail, File: d.Path, Flow: d.Flow, All: d.EveryFrame})
		out = append(out, depth.CachePath(cache, q))
	}
	return out
}
