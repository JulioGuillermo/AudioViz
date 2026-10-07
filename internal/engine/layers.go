package engine

import (
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"sort"

	"gopkg.in/yaml.v3"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/gpu"
	"audioviz/internal/video"
)

// MaxLayers: overlay clips drawn at once (the GPU's limit; the CPU draws them all the same way)
const MaxLayers = 16

// maxOwnGlitch: bands of a clip's own glitch
const maxOwnGlitch = 8

// layerMedia: the reader of a clip's file and the file's own size
type layerMedia struct {
	r      video.BackgroundReader
	nw, nh int
}

// layerMediaFor opens the file of clip ci of layer li once. It is kept at its own size, up to
// what covers the whole frame (enough for any placement up to covering it).
func (s *Session) layerMediaFor(li, ci int, c *config.ClipConfig) (*layerMedia, error) {
	key := fmt.Sprintf("layer|%d|%d|%s|%v", li, ci, c.Path, c.Video.Loop)
	if m, ok := s.layerMedia[key]; ok {
		return m, nil
	}
	pr, err := video.ProbeMedia(c.Path)
	if err != nil {
		return nil, err
	}
	k := math.Max(float64(s.W)/float64(max(pr.Width, 1)), float64(s.H)/float64(max(pr.Height, 1)))
	r, err := video.NewLayerReader(c.Path, max(s.W, int(math.Ceil(float64(pr.Width)*k))), max(s.H, int(math.Ceil(float64(pr.Height)*k))), c.Video.Loop)
	if err != nil {
		return nil, err
	}
	m := &layerMedia{r: r, nw: max(pr.Width, 1), nh: max(pr.Height, 1)}
	s.layerMedia[key] = m
	return m, nil
}

// contrastFactor: the contrast boost as the factor the renderers use (as fillParams)
func contrastFactor(contrast float64) float64 {
	c := contrast * 128.0
	return (259.0 * (c + 255.0)) / (255.0 * (259.0 - c))
}

// layersAt resolves the overlay clips visible at t, bottom to top, and the stack position of each
// (its layer's index + 1), to place the visualizer, particles and texts among them
func (s *Session) layersAt(t float64, idx int, feat *audio.FrameFeatures, fx frameFX, glitchAmt float64) ([]effects.LayerFrame, []int, error) {
	var out []effects.LayerFrame
	var stack []int
	W, H := float64(s.W), float64(s.H)
	minDim := math.Max(math.Min(W, H), 64)
	// The background's effects, for the clips that follow them
	bg := effects.Warp{Zoom: fx.zoom, RippleAmp: fx.rippleAmp, RippleFreq: fx.rippleFreq, RippleTm: fx.rTim,
		ShakeX: fx.shakeX, ShakeY: fx.shakeY, Chroma: fx.chromaShift, ChromaMode: fx.chromaMode,
		FlashCF: contrastFactor(fx.contrast), FlashBright: fx.bright}
	if fx.glitch {
		bg.Glitch = effects.GlitchSlices(s.W, s.H, glitchAmt, int64(idx))
	}
	for li := range s.Cfg.Layers {
		layer := &s.Cfg.Layers[li]
		for ci := range layer.Clips {
			c0 := &layer.Clips[ci]
			if t < c0.Start || t >= c0.End || c0.Path == "" {
				continue
			}
			if len(out) == MaxLayers {
				break
			}
			c := c0
			if len(c0.Keys) > 0 {
				c = config.ApplyKeys(c0, c0.Keys, t-c0.Start)
			}
			m, err := s.layerMediaFor(li, ci, c0)
			if err != nil {
				return nil, nil, err
			}
			speed := c.Video.Speed
			if speed <= 0 {
				speed = 1
			}
			img, ver := m.r.FrameAt(c.Video.Trim + (t-c.Start)*speed)
			tw, th := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
			// Base size on screen
			bw, bh := float64(m.nw), float64(m.nh)
			switch c.Fit {
			case "contain", "":
				k := math.Min(W/bw, H/bh)
				bw, bh = bw*k, bh*k
			case "cover":
				k := math.Max(W/bw, H/bh)
				bw, bh = bw*k, bh*k
			case "stretch":
				bw, bh = W, H
			}
			band := feat.Band(c.React.ReactTo, "bass")
			sc := c.Scale * (1 + c.React.Scale*band)
			sx, sy := bw*sc/tw, bh*sc/th
			if c.FlipX {
				sx = -sx
			}
			if c.FlipY {
				sy = -sy
			}
			op := c.Opacity * (1 - c.React.Opacity + c.React.Opacity*band)
			if c.FadeIn > 0 {
				op *= math.Min(1, (t-c.Start)/c.FadeIn)
			}
			if c.FadeOut > 0 {
				op *= math.Min(1, (c.End-t)/c.FadeOut)
			}
			lf := effects.LayerFrame{Img: img, Ver: fmt.Sprintf("%p:%d", m.r, ver),
				CX: c.X * W, CY: c.Y * H, SX: sx, SY: sy, Rot: c.Rotation * math.Pi / 180,
				Opacity: math.Max(0, math.Min(1, op)), Blend: c.BlendIndex(),
				Brightness: c.Brightness, Contrast: c.Contrast, Saturate: c.Saturation, Hue: c.Hue * math.Pi / 180, Blur: c.Blur,
				Key: c.Chroma.Enabled, KeyTol: c.Chroma.Tolerance, KeySoft: c.Chroma.Softness}
			if col, ok := hexRGB(c.Chroma.Color); ok {
				lf.KeyColor = col
			} else {
				lf.KeyColor = [3]float64{0, 1, 0}
			}
			lf.LayerID, lf.Clip = layer.ID, ci
			lf.W = s.clipWarp(&c.Effects, bg, t, idx, li, feat, minDim)
			if len(c.Masks) > 0 {
				lf.Mask, lf.MaskVer = s.maskFor(c.Masks, int(tw), int(th))
			}
			out = append(out, lf)
			stack = append(stack, li+1)
		}
	}
	return out, stack, nil
}

// clipWarp: the background effects a clip follows (inherit), its own, or none
func (s *Session) clipWarp(e *config.LayerEffects, bg effects.Warp, t float64, idx, li int, feat *audio.FrameFeatures, minDim float64) effects.Warp {
	w := effects.Warp{Zoom: 1, FlashCF: 1}
	zoom := 1.0
	switch config.EffectMode(e.Zoom.Mode) {
	case 0:
		zoom = bg.Zoom
	case 1:
		zoom = zoomOf(&e.Zoom.Own, feat)
	}
	switch config.EffectMode(e.Shake.Mode) {
	case 0:
		w.ShakeX, w.ShakeY = bg.ShakeX, bg.ShakeY
		if config.EffectMode(e.Zoom.Mode) != 0 {
			// the background's zoom includes the shake's edge zoom
			_, _, z := shakeOf(&s.Cfg.Timeline.Shake[config.Active(s.Cfg.Timeline.Shake, t)].Cfg, t, feat, minDim)
			zoom *= z
		}
	case 1:
		var z float64
		w.ShakeX, w.ShakeY, z = shakeOf(&e.Shake.Own, t, feat, minDim)
		zoom *= z
	}
	w.Zoom = zoom
	switch config.EffectMode(e.Ripple.Mode) {
	case 0:
		w.RippleAmp, w.RippleFreq, w.RippleTm = bg.RippleAmp, bg.RippleFreq, bg.RippleTm
	case 1:
		w.RippleAmp, w.RippleFreq, w.RippleTm = rippleOf(&e.Ripple.Own, idx, feat)
	}
	switch config.EffectMode(e.Chromatic.Mode) {
	case 0:
		w.Chroma, w.ChromaMode = bg.Chroma, bg.ChromaMode
	case 1:
		w.Chroma, w.ChromaMode = chromaOf(&e.Chromatic.Own, feat)
	}
	switch config.EffectMode(e.Glitch.Mode) {
	case 0:
		w.Glitch, w.GlitchBG = bg.Glitch, len(bg.Glitch) > 0
	case 1:
		if glitchOn(&e.Glitch.Own, feat) {
			w.Glitch = effects.GlitchSlices(s.W, s.H, e.Glitch.Own.Intensity, int64(idx)*31+int64(li)+7)
			if len(w.Glitch) > maxOwnGlitch {
				w.Glitch = w.Glitch[:maxOwnGlitch]
			}
		}
	}
	switch config.EffectMode(e.Flash.Mode) {
	case 0:
		w.FlashCF, w.FlashBright = bg.FlashCF, bg.FlashBright
	case 1:
		b, c := flashOf(&e.Flash.Own, feat)
		w.FlashCF, w.FlashBright = contrastFactor(c), b
	}
	return w
}

// hexRGB parses #rrggbb into 0..1
func hexRGB(s string) ([3]float64, bool) {
	var r, g, b int
	if len(s) < 7 || s[0] != '#' {
		return [3]float64{}, false
	}
	if _, err := fmt.Sscanf(s[1:7], "%02x%02x%02x", &r, &g, &b); err != nil {
		return [3]float64{}, false
	}
	return [3]float64{float64(r) / 255, float64(g) / 255, float64(b) / 255}, true
}

// LayerPixels: the most image pixels of overlay clips on screen at the same time (to size the
// GPU buffer of their images)
func (s *Session) LayerPixels() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	type ev struct {
		t float64
		d int
	}
	var evs []ev
	for li := range s.Cfg.Layers {
		for ci := range s.Cfg.Layers[li].Clips {
			c := &s.Cfg.Layers[li].Clips[ci]
			if c.Path == "" || c.End <= c.Start {
				continue
			}
			m, err := s.layerMediaFor(li, ci, c)
			if err != nil {
				continue
			}
			n := m.r.Width() * m.r.Height()
			if len(c.Masks) > 0 {
				mw, mh := effects.MaskSize(m.r.Width(), m.r.Height())
				n += (mw*mh + 3) / 4 // its mask, one byte per px
			}
			evs = append(evs, ev{c.Start, n}, ev{c.End, -n})
		}
	}
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].t != evs[j].t {
			return evs[i].t < evs[j].t
		}
		return evs[i].d < evs[j].d // ends first
	})
	cur, best := 0, 0
	for _, e := range evs {
		cur += e.d
		best = max(best, cur)
	}
	return best + s.textMaskWords()
}

// textMaskWords: room for the screen masks of the lyrics and the fixed texts (one frame-sized
// mask, up to effects.MaskSize, per text that has masks; the lyrics count once)
func (s *Session) textMaskWords() int {
	n := 0
	for _, sg := range s.Cfg.Timeline.Lyrics {
		if len(sg.Cfg.Masks) > 0 {
			n = 1
			break
		}
	}
	for i := range s.Cfg.Texts {
		if len(s.Cfg.Texts[i].Masks) > 0 {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mw, mh := effects.MaskSize(s.W, s.H)
	return min(n, gpu.MaxTextStyles) * ((mw*mh + 3) / 4)
}

// maskFor: the combined mask of a clip whose image is w×h, computed again only when the masks
// (after their keys) or their painted files change
func (s *Session) maskFor(masks []config.MaskConfig, w, h int) (*image.Alpha, string) {
	b, _ := yaml.Marshal(masks)
	key := fmt.Sprintf("%dx%d|%s", w, h, b)
	for _, m := range masks {
		if m.Kind == "raster" && m.Path != "" {
			if st, err := os.Stat(m.Path); err == nil {
				key += fmt.Sprintf("|%d", st.ModTime().UnixNano())
			}
		}
	}
	if e, ok := s.masks[key]; ok {
		return e.img, e.ver
	}
	mw, mh := effects.MaskSize(w, h)
	img := effects.MaskImage(masks, mw, mh, float64(mw)/float64(w), loadMaskFile)
	if len(s.masks) >= 64 {
		s.masks = map[string]maskEntry{}
	}
	s.maskSeq++
	e := maskEntry{img, fmt.Sprintf("m%d", s.maskSeq)}
	s.masks[key] = e
	return e.img, e.ver
}

type maskEntry struct {
	img *image.Alpha
	ver string
}

func loadMaskFile(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}
