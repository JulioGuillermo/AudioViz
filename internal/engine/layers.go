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
func (s *Session) layersAt(t float64, idx int, feats []audio.FrameFeatures, fx frameFX, glitchAmt float64, scene *effects.Scene) ([]effects.LayerFrame, []int, error) {
	feat := &feats[idx]
	nEmit := 0
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
			emit := layer.IsEmitter()
			if t < c0.Start || t >= c0.End || (c0.Path == "" && !emit) {
				continue
			}
			if len(out) == MaxLayers || (emit && nEmit == MaxEmitters) {
				break
			}
			c := c0
			if len(c0.Keys) > 0 {
				c = config.ApplyKeys(c0, c0.Keys, t-c0.Start)
			}
			if emit {
				// particles: a frame-sized image (made by the GPU pass or DrawEmitter) shown as is
				ef := s.emitFrame(li, ci, c0, c, idx, feats)
				if ef == nil {
					continue
				}
				nEmit++
				op := c.Opacity
				if c.FadeIn > 0 {
					op *= math.Min(1, (t-c.Start)/c.FadeIn)
				}
				if c.FadeOut > 0 {
					op *= math.Min(1, (c.End-t)/c.FadeOut)
				}
				// its light fades with it
				for j := range ef.P.LightCol {
					ef.P.LightCol[j] *= math.Max(0, math.Min(1, op))
				}
				lf := effects.LayerFrame{Emit: ef, Ver: "emit", CX: W / 2, CY: H / 2, SX: 1, SY: 1,
					Opacity: math.Max(0, math.Min(1, op)), Blend: c.BlendIndex(),
					Brightness: c.Brightness, Contrast: c.Contrast, Saturate: c.Saturation, Hue: c.Hue * math.Pi / 180, Blur: c.Blur}
				lf.LayerID, lf.Clip = layer.ID, ci
				lf.W = s.clipWarp(&c.Effects, bg, t, idx, li, feat, minDim)
				if len(c.Masks) > 0 {
					lf.Mask, lf.MaskVer = s.maskFor(c.Masks, s.W, s.H)
				}
				// where it is in the scene's depth: its origin now
				ox, oy := W/2, H/2
				if n := len(ef.Births); n > 0 {
					ox, oy = ef.Births[n-1].X, ef.Births[n-1].Y
				}
				z, ok := clipDepth(&c.Depth, scene, ox, oy)
				s.placeInDepth(&lf, &c.Depth, scene, z, ox, oy, ok)
				if e := c.Emitter; e != nil && e.FloorDepth && scene.On() {
					// the scene's floor: below the emitter, where the depth comes up to its own
					zf := z
					if !ok {
						zf = float64(scene.Ground(float32(ox), float32(oy)))
					}
					for y := oy + 2; y < H; y += 2 {
						if float64(scene.Ground(float32(ox), float32(y))) >= zf-0.01 {
							ef.P.FloorOn, ef.P.Floor = true, y+math.Max(e.Floor, 0)*s.emitScale()
							if ef.P.Bounce == 0 && ef.P.Friction == 0 {
								ef.P.Bounce, ef.P.Friction = math.Min(math.Max(e.Bounce, 0), 0.95), math.Min(math.Max(e.Friction, 0), 1)
							}
							break
						}
					}
				}
				if P := &ef.P; P.LightOn && scene.On() {
					// its light in 2.5D: from its origin (as moved), at its depth (the scene's there
					// when it has none)
					if !ok {
						z = float64(scene.Ground(float32(ox), float32(oy)))
					}
					e := c.Emitter
					P.LightD3, P.LightCX, P.LightCY, P.LightZ = true, ox+lf.W.ShakeX, oy+lf.W.ShakeY, z
					if e != nil {
						P.LightDepthK, P.LightShade = e.Light.Depth, e.Light.Shade
						P.LightFrom, P.LightTo, P.LightSoft = e.Light.From, e.Light.To, e.Light.RangeSoft
					}
				}
				out = append(out, lf)
				stack = append(stack, li+1)
				continue
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
			cx, cy, rot := c.X*W, c.Y*H, c.Rotation*math.Pi/180
			if c.Motion.On() {
				var a float64
				cx, cy, a = effects.PathAt(c.Motion.Path, W, H, c.Motion.Progress)
				if c.Motion.Orient {
					rot += a
				}
			}
			lf := effects.LayerFrame{Img: img, Ver: fmt.Sprintf("%p:%d", m.r, ver),
				CX: cx, CY: cy, SX: sx, SY: sy, Rot: rot,
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
			z, ok := clipDepth(&c.Depth, scene, cx, cy)
			s.placeInDepth(&lf, &c.Depth, scene, z, cx, cy, ok)
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
			if c.End <= c.Start {
				continue
			}
			if s.Cfg.Layers[li].IsEmitter() {
				// its image (and mask) are frame-sized; at most MaxEmitters at once (counted as always on)
				continue
			}
			if c.Path == "" {
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
	return best + s.textMaskWords() + s.emitterWords() + s.depthWords()
}

// depthWords: room for the depth of the backgrounds A and B (16 bits per pixel) when some
// segment has a depth map
func (s *Session) depthWords() int {
	for _, sg := range s.Cfg.Timeline.Background {
		if d := sg.Cfg.Depth.Source; d != "" && d != "none" {
			return 2 * ((s.W*s.H + 1) / 2)
		}
	}
	return 0
}

// emitterWords: room for the images of the emitters (one frame each, and its mask)
func (s *Session) emitterWords() int {
	n, masks := 0, 0
	for li := range s.Cfg.Layers {
		if !s.Cfg.Layers[li].IsEmitter() {
			continue
		}
		for _, c := range s.Cfg.Layers[li].Clips {
			if c.End > c.Start {
				n++
				if len(c.Masks) > 0 {
					masks++
				}
			}
		}
	}
	n, masks = min(n, MaxEmitters), min(masks, MaxEmitters)
	mw, mh := effects.MaskSize(s.W, s.H)
	return n*s.W*s.H + masks*((mw*mh+3)/4)
}

// EmitterCount: emitter clips that can be on screen at once (the GPU's accumulation buffers)
func (s *Session) EmitterCount() int {
	n := 0
	for li := range s.Cfg.Layers {
		if s.Cfg.Layers[li].IsEmitter() {
			n += len(s.Cfg.Layers[li].Clips)
		}
	}
	return min(n, MaxEmitters)
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

// clipDepth: the depth of a clip at (x, y) of the frame (ok = false if it has none)
func clipDepth(d *config.ClipDepth, scene *effects.Scene, x, y float64) (float64, bool) {
	switch d.Mode {
	case "fixed":
		return d.Z, true
	case "auto":
		if scene.On() {
			return float64(scene.Ground(float32(x), float32(y))) + d.Z, true
		}
	}
	return 0, false
}

// placeInDepth: a clip at depth z is hidden where the scene is nearer (if it asks to)
func (s *Session) placeInDepth(lf *effects.LayerFrame, d *config.ClipDepth, scene *effects.Scene, z, x, y float64, ok bool) {
	lf.Scene = scene
	if ok && scene.On() {
		lf.InDepth, lf.DepthZ, lf.DepthSoft = true, float32(z), float32(math.Max(d.Soft, 0.005))
		lf.Behind = d.Behind
	}
	if ok && scene.On() && scene.DoF.On() {
		// out of focus as its depth is
		lf.Blur += float64(scene.DoF.CoC(float32(z)))
	}
	if ok && scene.On() && scene.Par.On() {
		// the 2.5D camera moves it as its depth
		dx, dy := scene.Par.Disp(z, x, y, s.W, s.H)
		lf.W.ShakeX += dx
		lf.W.ShakeY += dy
	}
}

// ObjectDepth: the depth (and its soft edge) of a clip (layerID, clip) or a text (isText: text,
// -1 = the lyrics) in this frame: where it is drawn, or when it isn't on screen now (entering,
// out of its time, hidden) from its settings at this time, at its place. false: it has no depth
// (or the frame none)
func (s *Session) ObjectDepth(fs *FrameState, isText bool, text int, layerID string, clip int) (float64, float64, bool) {
	sc := fs.Scene
	if !sc.On() {
		return 0, 0, false
	}
	soft := func(d *config.ClipDepth) float64 { return math.Max(d.Soft, 0.005) }
	W, H := float64(s.W), float64(s.H)
	s.mu.Lock()
	defer s.mu.Unlock()
	if isText {
		for _, st := range fs.TextStyles {
			if st.Text == text && st.InDepth {
				return st.DepthZ, st.DepthSoft, true
			}
		}
		var d config.ClipDepth
		x, y := W/2, H/2
		if text >= 0 {
			if text >= len(s.Cfg.Texts) {
				return 0, 0, false
			}
			tc := &s.Cfg.Texts[text]
			if len(tc.Keys) > 0 {
				tc = config.ApplyKeys(tc, tc.Keys, fs.T-tc.Start)
			}
			d, x, y = tc.Depth, tc.PositionX*W, tc.PositionY*H
			if mo := tc.Motion; mo.On() {
				x, y, _ = effects.PathAt(mo.Path, W, H, mo.Progress)
			}
		} else if ly := s.Cfg.Timeline.Lyrics; len(ly) > 0 {
			c := ly[config.Active(ly, fs.T)].CfgAt(fs.T)
			d, y = c.Depth, c.PositionY*H
		}
		z, ok := clipDepth(&d, sc, x, y)
		return z, soft(&d), ok
	}
	for _, l := range fs.Layers {
		if l.LayerID == layerID && l.Clip == clip && l.InDepth {
			return float64(l.DepthZ), float64(l.DepthSoft), true
		}
	}
	for li := range s.Cfg.Layers {
		ly := &s.Cfg.Layers[li]
		if ly.ID != layerID || clip < 0 || clip >= len(ly.Clips) {
			continue
		}
		c := &ly.Clips[clip]
		if len(c.Keys) > 0 {
			c = config.ApplyKeys(c, c.Keys, fs.T-c.Start)
		}
		z, ok := clipDepth(&c.Depth, sc, c.X*W, c.Y*H)
		return z, soft(&c.Depth), ok
	}
	return 0, 0, false
}
