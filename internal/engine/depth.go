package engine

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"audioviz/internal/config"
	"audioviz/internal/depth"
	"audioviz/internal/effects"
	"audioviz/internal/video"
)

// Depth maps of the background segments (config.DepthConfig): made once (depth.Make keeps them in
// the project's cache folder; the AI is run the first time if needed) and fitted to the frame as
// their image is (fit mode), as 16-bit nearness. The renderer samples them with
// the background's own motion, zoom, shake and transition (render.comp sceneDepth,
// effects.Scene).

type depthEntry struct {
	img  []uint16
	norm []uint16 // its surface normals (effects.NormalMap), for the 2.5D lights
	ver  string
	err  error
}

var depthSeq int

// depthFor: the fitted depth map of a background segment and its normals (nil if it has none or
// it failed); tv: the time in its video (s since the segment starts), for a video's depth on every
// frame (DepthConfig.EveryFrame: the map of the frame shown then)
func (s *Session) depthFor(bg *config.BackgroundConfig, tv float64) ([]uint16, []uint16, string) {
	d := bg.Depth
	if bg.Path == "" || d.Source == "" || d.Source == "none" {
		return nil, nil, ""
	}
	req := depth.Request{Image: bg.Path, Source: d.Source, Model: d.Model, Detail: d.Detail, File: d.Path}
	if d.Source == depth.SourceComfy {
		req.Model, req.File = d.Flow, depth.FlowFile(s.Cfg.Dir, d.Flow)
	}
	cache := filepath.Join(s.Cfg.Dir, "cache")
	if d.EveryFrame && d.Source != depth.SourceFile && video.IsVideo(bg.Path) {
		all := req
		all.All = true
		if img, norm, ver, ok := s.depthFrame(bg, all, cache, tv); ok {
			return img, norm, ver
		}
		return nil, nil, "" // (the preview: none until its job has made it; an export made it above)
	}
	if _, ok := depth.Cached(cache, req); !ok && s.Live {
		// the studio's preview never waits for a depth to be worked out (its job does it, with
		// progress, and can be stopped): none until then
		return nil, nil, ""
	}
	cp := depth.CachePath(cache, req)
	key := fmt.Sprintf("%s|%dx%d|%s|%g", cp, s.W, s.H, bg.FitMode, d.Relief)
	if st, err := os.Stat(cp); err == nil {
		key += fmt.Sprint("|", st.ModTime().UnixNano()) // (worked out again: the new one)
	}
	if e, ok := s.depths[key]; ok {
		return e.img, e.norm, e.ver
	}
	if s.depths == nil || len(s.depths) > 16 {
		s.depths = map[string]*depthEntry{}
	}
	e := &depthEntry{}
	s.depths[key] = e
	m, ok := depth.Cached(cache, req)
	if !ok {
		t0 := time.Now()
		var err error
		m, err = depth.Make(context.Background(), cache, req, func(string, float64) {})
		if err != nil {
			e.err = err
			s.Logf("⚠️  Depth of %s: %v", filepath.Base(bg.Path), err)
			return nil, nil, ""
		}
		s.Logf("🗺️  Depth of %s worked out in %.1fs", filepath.Base(bg.Path), time.Since(t0).Seconds())
	}
	e.img = fitDepth(m, s.W, s.H, bg.FitMode)
	e.norm = effects.NormalMap(e.img, s.W, s.H, d.Relief)
	depthSeq++
	e.ver = fmt.Sprintf("depth%d", depthSeq)
	return e.img, e.norm, e.ver
}

// fitDepth places a depth map on the frame as its image is placed (fit mode, as video.Fit), as
// 16-bit nearness (bilinear; 0 = far outside the image)
func fitDepth(m *depth.Map, W, H int, mode string) []uint16 {
	ow, oh := m.W, m.H
	x0, y0, dw, dh := 0, 0, W, H
	if mode != "stretch" {
		sx, sy := float64(W)/float64(ow), float64(H)/float64(oh)
		k := sx
		if (mode == "contain") == (sy < sx) {
			k = sy
		}
		dw, dh = int(float64(ow)*k+0.5), int(float64(oh)*k+0.5)
		x0, y0 = (W-dw)/2, (H-dh)/2
	}
	out := make([]uint16, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			u := (float64(x-x0)+0.5)*float64(ow)/float64(dw) - 0.5
			v := (float64(y-y0)+0.5)*float64(oh)/float64(dh) - 0.5
			if u < -0.5 || v < -0.5 || u > float64(ow)-0.5 || v > float64(oh)-0.5 {
				continue
			}
			ix, iy := int(math.Floor(u)), int(math.Floor(v))
			fx, fy := float32(u-float64(ix)), float32(v-float64(iy))
			a := m.At(ix, iy) + (m.At(ix+1, iy)-m.At(ix, iy))*fx
			b := m.At(ix, iy+1) + (m.At(ix+1, iy+1)-m.At(ix, iy+1))*fx
			out[y*W+x] = uint16(max(0, min(1, a+(b-a)*fy))*65535 + 0.5)
		}
	}
	return out
}

// depthFrame: a video's depth on the frame shown at tv (its sequence worked out first if it is not
// in the cache yet: the studio does it as a job, with its progress); the last frames used are kept
// fitted, with their normals
func (s *Session) depthFrame(bg *config.BackgroundConfig, req depth.Request, cache string, tv float64) ([]uint16, []uint16, string, bool) {
	sk := depth.SeqDir(cache, req)
	sq, ok := s.depthSeqs[sk]
	if !ok && s.Live { // (the preview: looked for again until the studio's job has made it)
		var found bool
		if sq, found = depth.OpenSeq(cache, req); !found {
			return nil, nil, "", false
		}
		if s.depthSeqs == nil || len(s.depthSeqs) > 16 {
			s.depthSeqs = map[string]*depth.Seq{}
		}
		s.depthSeqs[sk] = sq
		ok = true
	}
	if !ok {
		var found bool
		if sq, found = depth.OpenSeq(cache, req); !found {
			t0 := time.Now()
			s.Logf("🗺️  Working out the depth of every frame of %s…", filepath.Base(bg.Path))
			if _, err := depth.Make(context.Background(), cache, req, func(string, float64) {}); err != nil {
				s.Logf("⚠️  Depth of %s: %v", filepath.Base(bg.Path), err)
			} else {
				s.Logf("🗺️  Depth of every frame of %s worked out in %.1fs", filepath.Base(bg.Path), time.Since(t0).Seconds())
			}
			sq, _ = depth.OpenSeq(cache, req)
		}
		if s.depthSeqs == nil || len(s.depthSeqs) > 16 {
			s.depthSeqs = map[string]*depth.Seq{}
		}
		s.depthSeqs[sk] = sq // (nil: failed; not tried again for every frame)
	}
	if sq == nil {
		return nil, nil, "", false
	}
	return s.seqFrame(bg, sq, tv)
}

// seqFrame: the fitted map (and normals) of sequence sq on the frame shown at tv
func (s *Session) seqFrame(bg *config.BackgroundConfig, sq *depth.Seq, tv float64) ([]uint16, []uint16, string, bool) {
	i := sq.Index(tv, bg.LoopVideo)
	key := fmt.Sprintf("%s|%d|%dx%d|%s|%g", sq.Dir, i, s.W, s.H, bg.FitMode, bg.Depth.Relief)
	if e, ok := s.depths[key]; ok {
		return e.img, e.norm, e.ver, e.img != nil
	}
	if s.depths == nil || len(s.depths) > 16 {
		s.depths = map[string]*depthEntry{}
	}
	e := &depthEntry{}
	s.depths[key] = e
	m, err := sq.Frame(i)
	if err != nil {
		e.err = err
		return nil, nil, "", false
	}
	e.img = fitDepth(m, s.W, s.H, bg.FitMode)
	e.norm = effects.NormalMap(e.img, s.W, s.H, bg.Depth.Relief)
	depthSeq++
	e.ver = fmt.Sprintf("depth%d", depthSeq)
	return e.img, e.norm, e.ver, true
}
