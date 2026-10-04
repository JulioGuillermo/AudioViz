package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"audioviz/internal/imagegen"
	"audioviz/internal/progress"
)

// Image generation in the studio: the images go to the project's images/ folder, each PNG with
// a .json next to it (prompt, model, seed…) so it can be regenerated or varied. While generating,
// the latest preview (the image forming, step by step) is served at /api/images/preview.

const imagesDir = "images"

var imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".bmp": true}

// genImage: a generated image and how it was made (its .json)
type genImage struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Prompt  string    `json:"prompt"`
	Model   string    `json:"model"`
	Seed    int64     `json:"seed"`
	Steps   int       `json:"steps"`
	Width   int       `json:"width"`
	Height  int       `json:"height"`
	Refs    []string  `json:"refs,omitempty"`
	Device  string    `json:"device,omitempty"`
	Elapsed float64   `json:"elapsed,omitempty"` // s
	Created time.Time `json:"created"`
}

var (
	previewMu  sync.Mutex
	previewJPG []byte
	// The image model stays loaded after generating (the next image starts faster) and is freed
	// after a while without use
	unloadTimer *time.Timer
)

const unloadAfter = 10 * time.Minute

func listGenImages(projDir string) []genImage {
	dir := filepath.Join(projDir, imagesDir)
	ents, _ := os.ReadDir(dir)
	out := []genImage{}
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		g := genImage{}
		if b, err := os.ReadFile(strings.TrimSuffix(p, filepath.Ext(p)) + ".json"); err == nil {
			_ = json.Unmarshal(b, &g)
		}
		if g.Created.IsZero() {
			if st, err := e.Info(); err == nil {
				g.Created = st.ModTime()
			}
		}
		g.Name, g.Path = e.Name(), p
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// projectImages: the images the project uses or holds (backgrounds, files in assets/), to pick
// as references
func projectImages(s *Server) []string {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if cfg == nil {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	add := func(p string) {
		if p == "" || seen[p] || !imageExt[strings.ToLower(filepath.Ext(p))] {
			return
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(cfg.Background.Path)
	for _, b := range cfg.Timeline.Background {
		add(b.Cfg.Path)
	}
	ents, _ := os.ReadDir(filepath.Join(cfg.Dir, "assets"))
	for _, e := range ents {
		add(filepath.Join(cfg.Dir, "assets", e.Name()))
	}
	return out
}

func (s *Server) hImages(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if cfg == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	writeJSON(w, map[string]any{"models": imagegen.Models(), "images": listGenImages(cfg.Dir),
		"assets": projectImages(s), "dir": filepath.Join(cfg.Dir, imagesDir)})
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// imageName: date and the first words of the prompt ("20261004-153012-lone-guitarist-cliff.png")
func imageName(prompt string, t time.Time) string {
	words := strings.Fields(slugRe.ReplaceAllString(strings.ToLower(prompt), " "))
	if len(words) > 4 {
		words = words[:4]
	}
	n := t.Format("20060102-150405")
	if len(words) > 0 {
		n += "-" + strings.Join(words, "-")
	}
	return n + ".png"
}

func (s *Server) hGenImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model  string   `json:"model"`
		Prompt string   `json:"prompt"`
		Refs   []string `json:"refs"`
		Width  int      `json:"width"`
		Height int      `json:"height"`
		Steps  int      `json:"steps"`
		Seed   int64    `json:"seed"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if cfg == nil {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		fail(w, 400, errors.New("write a prompt: what the image shows"))
		return
	}
	f := imagegen.Get(req.Model)
	if f == nil {
		fail(w, 400, fmt.Errorf("unknown model %q", req.Model))
		return
	}
	if len(req.Refs) > f.Refs {
		fail(w, 400, fmt.Errorf("%s takes up to %d reference images", f.Name, f.Refs))
		return
	}
	var refs []image.Image
	for _, p := range req.Refs {
		img, err := decodeImage(p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		refs = append(refs, img)
	}
	dir := filepath.Join(cfg.Dir, imagesDir)
	j, err := s.startJob("image", func(ctx context.Context, j *job) error {
		if unloadTimer != nil {
			unloadTimer.Stop()
		}
		previewMu.Lock()
		previewJPG = nil
		previewMu.Unlock()
		t0 := time.Now()
		res, err := imagegen.Generate(ctx, imagegen.Request{Model: req.Model, Prompt: req.Prompt, Width: req.Width, Height: req.Height,
			Steps: req.Steps, Seed: req.Seed, Refs: refs}, func(st progress.Status) {
			j.mu.Lock()
			st.Preview = j.preview()
			j.Gen = &st
			j.Progress = st.Progress
			cur := st.Steps[st.Current]
			j.Message = cur.Label
			if cur.Detail != "" {
				j.Message += " · " + cur.Detail
			}
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		}, func(img *image.NRGBA) {
			var b bytes.Buffer
			if jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}) != nil {
				return
			}
			previewMu.Lock()
			previewJPG = b.Bytes()
			previewMu.Unlock()
			j.mu.Lock()
			j.Previews++
			if j.Gen != nil {
				j.Gen.Preview = j.preview()
			}
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		})
		unloadTimer = time.AfterFunc(unloadAfter, imagegen.Unload)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		out := filepath.Join(dir, imageName(req.Prompt, time.Now()))
		var b bytes.Buffer
		if err := png.Encode(&b, res.Image); err != nil {
			return err
		}
		if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
			return err
		}
		meta := genImage{Prompt: req.Prompt, Model: res.Model, Seed: res.Seed, Steps: res.Steps, Width: res.Image.Rect.Dx(),
			Height: res.Image.Rect.Dy(), Refs: req.Refs, Device: res.Device, Elapsed: time.Since(t0).Seconds(), Created: time.Now()}
		if mb, err := json.MarshalIndent(meta, "", "  "); err == nil {
			_ = os.WriteFile(strings.TrimSuffix(out, ".png")+".json", mb, 0o644)
		}
		j.mu.Lock()
		j.Output = out
		j.Message = fmt.Sprintf("Image generated in %s", fmtSecs(time.Since(t0).Seconds()))
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}

// preview: URL of the latest preview ("" if none yet); j.mu held
func (j *job) preview() string {
	if j.Previews == 0 {
		return ""
	}
	return fmt.Sprintf("/api/images/preview?n=%d", j.Previews)
}

func (s *Server) hImagePreview(w http.ResponseWriter, r *http.Request) {
	previewMu.Lock()
	b := previewJPG
	previewMu.Unlock()
	if b == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

// hDeleteImage deletes a generated image (by name, only in images/) and its .json
func (s *Server) hDeleteImage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	name := r.URL.Query().Get("name")
	if cfg == nil || name == "" || name != filepath.Base(name) || !strings.EqualFold(filepath.Ext(name), ".png") {
		fail(w, 400, errors.New("invalid image"))
		return
	}
	p := filepath.Join(cfg.Dir, imagesDir, name)
	if err := os.Remove(p); err != nil {
		fail(w, 400, err)
		return
	}
	_ = os.Remove(strings.TrimSuffix(p, filepath.Ext(p)) + ".json")
	writeJSON(w, map[string]any{"ok": true})
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return img, nil
}

func fmtSecs(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%.0f s", s)
	}
	return fmt.Sprintf("%d min %02d s", int(s)/60, int(s)%60)
}
