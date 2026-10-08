package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"path/filepath"
	"time"

	"audioviz/internal/depth"
	"audioviz/internal/effects"
)

// Depth maps of the backgrounds (internal/depth): worked out as a job (the model is downloaded
// the first time) and kept in the project's cache folder.

// depthReq: which depth map (config.DepthConfig of a background, plus the background file)
type depthReq struct {
	Image  string `json:"image"`
	Source string `json:"source"`
	Model  string `json:"model"`
	Detail string `json:"detail"`
	File   string `json:"file"`
}

func (r depthReq) request() depth.Request {
	return depth.Request{Image: r.Image, Source: r.Source, Model: r.Model, Detail: r.Detail, File: r.File}
}

// cacheDir: the project's cache folder ("" without a project)
func (s *Server) cacheDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return ""
	}
	return filepath.Join(s.cfg.Dir, "cache")
}

// POST /api/depth: work out a depth map (a job)
func (s *Server) hDepth(w http.ResponseWriter, r *http.Request) {
	var req depthReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	dir := s.cacheDir()
	if dir == "" {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	if req.Image == "" {
		fail(w, 400, errors.New("the segment has no background image"))
		return
	}
	labels := map[string]string{"download": "Downloading the depth model", "estimate": "Working out the depth",
		"refine": "Following the image's edges", "brightness": "Depth from the brightness", "file": "Reading the depth map"}
	j, err := s.startJob("depth", func(ctx context.Context, j *job) error {
		t0 := time.Now()
		_, err := depth.Make(ctx, dir, req.request(), func(st string, p float64) {
			j.mu.Lock()
			if l, ok := labels[st]; ok {
				j.Message = l
				if st == "download" {
					j.Message = fmt.Sprintf("%s · %d %%", l, int(p*100))
				}
			}
			j.Progress = map[string]float64{"download": 0.5 * p, "estimate": 0.5 + 0.4*p, "refine": 0.92, "brightness": 0.5, "file": 0.5, "done": 1}[st]
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		})
		if err != nil {
			return err
		}
		j.mu.Lock()
		j.Output = depth.CachePath(dir, req.request())
		j.Message = fmt.Sprintf("Depth ready in %s", fmtSecs(time.Since(t0).Seconds()))
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	writeJSON(w, j.snapshot())
}

// GET /api/depth/view?image=&source=&model=&file=: the depth map (if worked out) as a grey PNG
// (white = near), at most 640 px wide; 404 if not in the cache yet
func (s *Server) hDepthView(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := depthReq{Image: q.Get("image"), Source: q.Get("source"), Model: q.Get("model"), Detail: q.Get("detail"), File: q.Get("file")}
	dir := s.cacheDir()
	m, ok := depth.Cached(dir, req.request())
	if dir == "" || !ok {
		http.Error(w, "not worked out yet", 404)
		return
	}
	k := max(1, (m.W+639)/640)
	img := image.NewGray(image.Rect(0, 0, m.W/k, m.H/k))
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			img.Pix[y*img.Stride+x] = uint8(m.At(x*k, y*k)*255 + 0.5)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b.Bytes())
}

// depthView: the scene's depth as an image (white = near; a dark red grid when there is none);
// rng (from, to, soft): only what a light lights, black elsewhere (as effects.LightRange);
// obj (z, soft): where an object is in it, what is behind it half red (as the occlusion's edge)
func depthView(sc *effects.Scene, w, h, scale int, rng *[3]float32, obj *[2]float32) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := y*img.Stride + x*4
			if !sc.On() {
				v := uint8(20)
				if (x/16+y/16)%2 == 0 {
					v = 34
				}
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = v+10, v, v, 255
				continue
			}
			z := sc.At(float32(x*scale), float32(y*scale))
			if rng != nil {
				z *= effects.LightRange(z, rng[0], rng[1], rng[2])
			}
			d := z * 255
			r, g, b := d, d, d
			if obj != nil {
				// behind: the scene is farther than the object
				k := 0.55 * (1 - smooth(obj[0]-obj[1], obj[0]+obj[1]+1e-4, z))
				r, g, b = d+(255-d)*k*0.75, d*(1-k), d*(1-k)
			}
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = uint8(r+0.5), uint8(g+0.5), uint8(b+0.5), 255
		}
	}
	return img
}

// smooth: smoothstep
func smooth(a, b, x float32) float32 {
	t := max(0, min(1, (x-a)/(b-a)))
	return t * t * (3 - 2*t)
}
