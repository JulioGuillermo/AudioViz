package engine

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"audioviz/internal/config"
	"audioviz/internal/depth"
)

// Depth maps of the background segments (config.DepthConfig): made once (depth.Make keeps them in
// the project's cache folder; the AI is run the first time if needed) and fitted to the frame as
// their image is (fit mode), as 16-bit nearness. The renderer samples them with
// the background's own motion, zoom, shake and transition (render.comp sceneDepth,
// effects.Scene).

type depthEntry struct {
	img []uint16
	ver string
	err error
}

var depthSeq int

// depthFor: the fitted depth map of a background segment (nil if it has none or it failed)
func (s *Session) depthFor(bg *config.BackgroundConfig) ([]uint16, string) {
	d := bg.Depth
	if bg.Path == "" || d.Source == "" || d.Source == "none" {
		return nil, ""
	}
	req := depth.Request{Image: bg.Path, Source: d.Source, Model: d.Model, Detail: d.Detail, File: d.Path}
	cache := filepath.Join(s.Cfg.Dir, "cache")
	key := fmt.Sprintf("%s|%dx%d|%s", depth.CachePath(cache, req), s.W, s.H, bg.FitMode)
	if e, ok := s.depths[key]; ok {
		return e.img, e.ver
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
			return nil, ""
		}
		s.Logf("🗺️  Depth of %s worked out in %.1fs", filepath.Base(bg.Path), time.Since(t0).Seconds())
	}
	e.img = fitDepth(m, s.W, s.H, bg.FitMode)
	depthSeq++
	e.ver = fmt.Sprintf("depth%d", depthSeq)
	return e.img, e.ver
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
