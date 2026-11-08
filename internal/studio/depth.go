package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"path/filepath"
	"time"

	"audioviz/internal/depth"
	"audioviz/internal/effects"
	"audioviz/internal/video"
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
	Flow   string `json:"flow"`  // comfy: the project's flow
	All    bool   `json:"all"`   // a video: every frame (DepthConfig.EveryFrame)
	Force  bool   `json:"force"` // worked out again (it is in the cache)
}

func (s *Server) depthRequest(r depthReq) depth.Request {
	q := depth.Request{Image: r.Image, Source: r.Source, Model: r.Model, Detail: r.Detail, File: r.File, Force: r.Force,
		All: r.All && r.Source != depth.SourceFile && video.IsVideo(r.Image)}
	if r.Source == depth.SourceComfy {
		s.mu.Lock()
		if s.cfg != nil {
			q.Model, q.File = r.Flow, depth.FlowFile(s.cfg.Dir, r.Flow)
		}
		s.mu.Unlock()
	}
	return q
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
		"refine": "Following the image's edges", "brightness": "Depth from the brightness", "file": "Reading the depth map",
		"comfy": "Working out the depth in ComfyUI", "frames": "Working out the depth of every frame"}
	if req.Source == depth.SourceComfy && req.Flow == "" {
		fail(w, 400, errors.New("choose the ComfyUI flow that makes the depth map"))
		return
	}
	dreq := s.depthRequest(req)
	j, err := s.startJob("depth", func(ctx context.Context, j *job) error {
		t0 := time.Now()
		_, err := depth.Make(ctx, dir, dreq, func(st string, p float64) {
			if ctx.Err() != nil { // (stopped: late reports of the frames being finished change nothing)
				return
			}
			j.mu.Lock()
			if dreq.All { // every frame: the total and this frame's progress
				switch st {
				case "count":
					j.Frames = &frameProgress{Total: int(p)}
				case "frames", "frame", "done":
					if f := j.Frames; f != nil {
						switch st {
						case "frames": // (the whole, counting the frames being worked out)
							j.Progress = p
							f.Done = min(f.Total, int(math.Floor(p*float64(f.Total)+1e-9)))
						case "frame":
							f.Frame = p
						default:
							f.Done, f.Frame, j.Progress = f.Total, 1, 1
						}
						j.Message = fmt.Sprintf("%s · %d / %d", labels["frames"], min(f.Done+1, f.Total), f.Total)
					}
				case "download":
					j.Message = fmt.Sprintf("%s · %d %%", labels[st], int(p*100))
				}
				j.mu.Unlock()
				s.hub.send("job", j.snapshot())
				return
			}
			if l, ok := labels[st]; ok {
				j.Message = l
				if st == "download" {
					j.Message = fmt.Sprintf("%s · %d %%", l, int(p*100))
				}
			}
			j.Progress = map[string]float64{"download": 0.5 * p, "estimate": 0.5 + 0.4*p, "refine": 0.92, "brightness": 0.5, "file": 0.5, "comfy": 0.95 * p, "done": 1}[st]
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		})
		if err != nil {
			return err
		}
		j.mu.Lock()
		j.Output = depth.CachePath(dir, dreq)
		j.Message = fmt.Sprintf("Depth ready in %s", fmtSecs(time.Since(t0).Seconds()))
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	s.tagJob(j, r, req)
	writeJSON(w, j.snapshot())
}

// GET /api/depth/partial?image=&source=&model=&detail=&flow=&all=1: the frames a stopped run of
// a video's every frame left ({frames}; 0 for none)
func (s *Server) hDepthPartial(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := depthReq{Image: q.Get("image"), Source: q.Get("source"), Model: q.Get("model"), Detail: q.Get("detail"),
		File: q.Get("file"), Flow: q.Get("flow"), All: q.Get("all") == "1"}
	n := 0
	if dir := s.cacheDir(); dir != "" && req.All {
		n = depth.Partial(dir, s.depthRequest(req))
	}
	writeJSON(w, map[string]int{"frames": n})
}

// POST /api/depth/discard (as /api/depth): throw away the frames a stopped run left
func (s *Server) hDepthDiscard(w http.ResponseWriter, r *http.Request) {
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
	s.mu.Lock()
	running := s.job != nil && s.job.Kind == "depth" && s.job.snapshot()["status"] == "running"
	s.mu.Unlock()
	if running {
		fail(w, 409, errors.New("the depth is being worked out: cancel it first"))
		return
	}
	if err := depth.DiscardPartial(dir, s.depthRequest(req)); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// GET /api/depth/view?image=&source=&model=&file=: the depth map (if worked out) as a grey PNG
// (white = near), at most 640 px wide; 404 if not in the cache yet
func (s *Server) hDepthView(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := depthReq{Image: q.Get("image"), Source: q.Get("source"), Model: q.Get("model"), Detail: q.Get("detail"), File: q.Get("file"), Flow: q.Get("flow"),
		All: q.Get("all") == "1"}
	dir := s.cacheDir()
	m, ok := depth.Cached(dir, s.depthRequest(req))
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
// sol (on, depth, band, x, y, free radius): what an emitter goes round, cyan (as viewColor)
func depthView(sc *effects.Scene, w, h, scale int, rng *[3]float32, obj *[2]float32, sol *[6]float32) *image.RGBA {
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
			if sol != nil {
				fx, fy := float32(x*scale), float32(y*scale)
				if dz := z - sol[1]; dz < sol[2] && dz > -sol[2] && (fx-sol[3])*(fx-sol[3])+(fy-sol[4])*(fy-sol[4]) >= sol[5]*sol[5] {
					r, g, b = r+(0.2*255-r)*0.6, g+(0.85*255-g)*0.6, b+(255-b)*0.6
				}
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
