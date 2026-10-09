package engine

import (
	"fmt"
	"math"

	"gopkg.in/yaml.v3"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/lyrics"
	"audioviz/internal/visualizer"
)

// Emitter clips (config.EmitterConfig): the table of births of the whole clip is computed once
// (its keys, its motion and the music decide, frame by frame, how many are born, where and how
// fast) and kept until the clip changes; each frame takes the part that can still be alive.

// MaxEmitters: emitter clips drawn at once (each needs a frame-sized accumulation on the GPU)
const MaxEmitters = 4

type emitTable struct {
	key    string
	f0     int             // global frame of the first entry
	births []effects.Birth // per frame (a burst adds a second entry at the same time)
	at     []int           // index in births of each frame's last entry
}

// emitShape: the shape names of config.EmitterConfig
func emitShape(s string) int {
	switch s {
	case "circle":
		return effects.EmitCircle
	case "ring":
		return effects.EmitRing
	case "line":
		return effects.EmitLine
	case "box":
		return effects.EmitBox
	case "text":
		return effects.EmitText
	}
	return effects.EmitPoint
}

// emitScale: px of this frame per px of a 1080-line frame
func (s *Session) emitScale() float64 { return float64(s.H) / 1080 }

// clipPlace: where the clip is at time t (px) and its rotation (rad), keys and motion applied
func (s *Session) clipPlace(c *config.ClipConfig) (x, y, rot float64) {
	W, H := float64(s.W), float64(s.H)
	x, y, rot = c.X*W, c.Y*H, c.Rotation*math.Pi/180
	if c.Motion.On() {
		var a float64
		x, y, a = effects.PathAt(c.Motion.Path, W, H, c.Motion.Progress)
		if c.Motion.Orient {
			rot += a
		}
	}
	return
}

// textRef: the moment a fixed text's letters are measured at (fully in)
func textRef(tc *config.TextConfig) float64 {
	return math.Min(tc.Start+tc.InDuration+0.05, (tc.Start+tc.End)/2)
}

// textAt: a fixed text's settings at time t (its keys applied)
func textAt(tc *config.TextConfig, t float64) *config.TextConfig {
	if len(tc.Keys) > 0 {
		return config.ApplyKeys(tc, tc.Keys, t-tc.Start)
	}
	return tc
}

// textPlace: where fixed text i is at time t (its anchor, px), its rotation (rad) and its size
// against the moment its letters were measured (textRef)
func (s *Session) textPlace(i int, t float64) (x, y, rot, scale float64, ok bool) {
	if i < 0 || i >= len(s.Cfg.Texts) {
		return 0, 0, 0, 1, false
	}
	tc := &s.Cfg.Texts[i]
	k := textAt(tc, t)
	W, H := float64(s.W), float64(s.H)
	x, y, rot, scale = k.PositionX*W, k.PositionY*H, k.Rotation*math.Pi/180, 1
	if mo := k.Motion; mo.On() {
		var a float64
		x, y, a = effects.PathAt(mo.Path, W, H, mo.Progress)
		if mo.Orient {
			rot += a
		}
	}
	if r := textAt(tc, textRef(tc)); r.Size > 0 && k.Size > 0 {
		scale = k.Size / r.Size
	}
	return x, y, rot, scale, true
}

// textPoints: points on the letters of fixed text i (its outline, or all of it), around its
// anchor before its rotation, as drawn at textRef; kept until the text changes
func (s *Session) textPoints(i int, fill bool) []float32 {
	if i < 0 || i >= len(s.Cfg.Texts) {
		return nil
	}
	tc := &s.Cfg.Texts[i]
	b, _ := yaml.Marshal(tc)
	key := fmt.Sprintf("%d|%v|%dx%d|%s", i, fill, s.W, s.H, b)
	if v, ok := s.textPts[key]; ok {
		return v
	}
	pts := lyrics.TextPointsOf(*tc, textRef(tc), fill, effects.MaxEmitPts, s.W, s.H)
	if s.textPts == nil || len(s.textPts) > 32 {
		s.textPts = map[string][]float32{}
	}
	s.textPts[key] = pts
	return pts
}

// emitTableFor builds (or reuses) the table of births of a clip
func (s *Session) emitTableFor(li, ci int, c0 *config.ClipConfig, feats []audio.FrameFeatures) *emitTable {
	b, _ := yaml.Marshal(c0)
	key := fmt.Sprintf("%d:%d:%dx%d@%d|%s", li, ci, s.W, s.H, s.FPS, b)
	if e := c0.Emitter; e != nil && e.Shape == "text" && e.Text >= 0 && e.Text < len(s.Cfg.Texts) {
		tb, _ := yaml.Marshal(&s.Cfg.Texts[e.Text]) // born on a text: it follows the text
		key += "|" + string(tb)
	}
	id := fmt.Sprintf("%d:%d", li, ci)
	if tb, ok := s.emitTabs[id]; ok && tb.key == key {
		return tb
	}
	fps := float64(s.FPS)
	f0 := int(math.Ceil(c0.Start * fps))
	f1 := min(s.Frames-1, int(math.Ceil(c0.End*fps))-1)
	tb := &emitTable{key: key, f0: f0}
	n := 0.0
	for f := f0; f <= f1; f++ {
		t := float64(f) / fps
		c := c0
		if len(c0.Keys) > 0 {
			c = config.ApplyKeys(c0, c0.Keys, t-c0.Start)
		}
		e := c.Emitter
		if e == nil {
			d := config.DefaultEmitter()
			e = &d
		}
		band := 0.0
		beat := false
		if f < len(feats) {
			band = math.Min(feats[f].Band(e.React.ReactTo, "bass"), 1.5)
			beat = feats[f].IsBeat
		}
		x, y, rot := s.clipPlace(c)
		scale, pathP := 1.0, 0.0
		if e.Shape == "text" {
			if tx, ty, tr, ts, ok := s.textPlace(e.Text, t); ok {
				x, y, rot, scale = tx, ty, tr, ts
			}
		}
		if c.Motion.On() {
			pathP = c.Motion.Progress
		}
		bt := effects.Birth{T: t, N: n, X: x, Y: y, Rot: rot, Speed: 1 + e.React.Speed*band, Size: 1 + e.React.Size*band,
			Scale: scale, PathP: pathP}
		lifeMax := math.Max(0.05, e.Life*(1+math.Abs(e.LifeVar)))
		capN := float64(max(e.Max, 1))
		burst := 0
		if f == f0 {
			burst += e.Burst
		}
		if beat && e.React.Burst > 0 {
			burst += e.React.Burst
		}
		if burst > 0 {
			tb.births = append(tb.births, bt)
			n += math.Min(float64(burst), capN)
			bt.N = n
		}
		tb.births = append(tb.births, bt)
		tb.at = append(tb.at, len(tb.births)-1)
		// the rate until the next frame, held under the most alive at once
		rate := math.Max(0, e.Rate*(1+e.React.Rate*band))
		rate = math.Min(rate, capN/lifeMax)
		n += rate / fps
	}
	if s.emitTabs == nil || len(s.emitTabs) > 64 {
		s.emitTabs = map[string]*emitTable{}
	}
	s.emitTabs[id] = tb
	return tb
}

// emitFrame: the emitter clip c0 (keys applied: c) at frame idx
func (s *Session) emitFrame(li, ci int, c0, c *config.ClipConfig, idx int, feats []audio.FrameFeatures) *effects.EmitFrame {
	tb := s.emitTableFor(li, ci, c0, feats)
	k := idx - tb.f0
	if k < 0 || k >= len(tb.at) {
		return nil
	}
	e := c.Emitter
	if e == nil {
		d := config.DefaultEmitter()
		e = &d
	}
	sc := s.emitScale() * math.Max(c.Scale, 0.01)
	col := func(h, def string) [4]float64 {
		v, err := visualizer.ParseHexColor(h)
		if err != nil {
			v, _ = visualizer.ParseHexColor(def)
		}
		return [4]float64{float64(v.R) / 255, float64(v.G) / 255, float64(v.B) / 255, float64(v.A) / 255}
	}
	p := effects.EmitParams{
		Shape: emitShape(e.Shape), W: e.Width * sc, H: e.Height * sc,
		Dir: e.Direction * math.Pi / 180, Spread: math.Min(math.Max(e.Spread, 0), 360) * math.Pi / 180,
		Speed: e.Speed * sc, SpeedVar: e.SpeedVar,
		Gravity: e.Gravity * sc, Wind: e.Wind * sc, Drag: math.Max(e.Drag, 0),
		Turb: e.Turbulence * sc, TurbFreq: e.TurbFreq,
		Life: e.Life, LifeVar: e.LifeVar,
		Size: e.Size * sc, SizeEnd: e.SizeEnd * sc, SizeVar: e.SizeVar,
		Col:    [3][4]float64{col(e.ColorStart, "#ffffffff"), col(e.ColorMid, "#ffffffff"), col(e.ColorEnd, "#ffffff00")},
		FadeIn: e.FadeIn, FadeOut: e.FadeOut, Soft: e.Mode == "soft",
		Streak: e.Streak, Softness: math.Min(math.Max(e.Softness, 0), 1), Seed: uint32(e.Seed)*2654435761 + uint32(li*131+ci),
	}
	if e.Look == "flame" {
		fl := e.Flame
		rgb := func(h, def string) [3]float64 { c := col(h, def); return [3]float64{c[0], c[1], c[2]} }
		p.Flame = true
		p.FlameCol = [3][3]float64{rgb(fl.ColorCool, "#b8200a"), rgb(fl.ColorMid, "#ff8a1e"), rgb(fl.ColorHot, "#fff4d0")}
		p.Heat, p.Noise, p.NoiseScale, p.NoiseSpeed, p.Edge = math.Max(fl.Heat, 0), fl.Noise*sc, math.Max(fl.NoiseScale*sc, 1), fl.NoiseSpeed*sc, math.Min(math.Max(fl.Edge, 0), 1)
	}
	p.Hidden = e.Hidden
	if e.Look == "smoke" {
		sm := e.Smoke
		p.Smoke = true
		p.SmokeWisp, p.SmokeScale = math.Min(math.Max(sm.Wisp, 0), 1), math.Max(sm.Scale*sc, 1)
		p.SmokeSpeed, p.SmokeWarp = sm.Speed*sc, sm.Warp*sc
	}
	if e.Look == "water" {
		w := e.Water
		p.Water = true
		p.WaterTh, p.WaterClear, p.WaterShine = math.Max(w.Threshold, 0.01), math.Min(math.Max(w.Clear, 0), 1), math.Max(w.Shine, 0)
		// the slope of a particle's edge is about 1 / its radius: a bump of 1 tilts it fully
		p.WaterBump = math.Max(w.Bump, 0) * math.Max(e.Size*sc, 2) * 0.5
		p.WaterRipple, p.WaterScale, p.WaterSpeed = w.Ripple*sc, math.Max(w.RippleScale*sc, 1), w.RippleSpeed*sc
		p.WaterLens = math.Max(w.Lens, 0) * sc
	}
	if p.Shape == effects.EmitText {
		p.Pts = s.textPoints(e.Text, e.TextPart == "fill")
	}
	if r := e.Route; r != nil && len(r.Points) >= 2 {
		// their own route
		p.Follow, p.Route, p.RouteFixed, p.RouteK = true, true, e.RouteFixed, math.Min(math.Max(e.RouteStrength, 0), 1)
		p.Path, p.PathLen = effects.SamplePath(r, float64(s.W), float64(s.H))
		p.PathClosed = !r.Open
		p.ZTrack = effects.Track(r, "z", float64(s.W), float64(s.H))
	} else if e.FollowPath && c.Motion.On() {
		p.Follow = true
		p.Path, p.PathLen = effects.SamplePath(c.Motion.Path, float64(s.W), float64(s.H))
		p.PathClosed = !c.Motion.Path.Open
		p.ZTrack = effects.Track(c.Motion.Path, "z", float64(s.W), float64(s.H))
	}
	if e.Floor > 0 || e.FloorDepth {
		// below the emitter now (with a depth map and FloorDepth, layersAt finds the scene's)
		_, y, _ := s.clipPlace(c)
		p.FloorOn, p.Floor = true, y+math.Max(e.Floor, 0)*sc
		p.Bounce, p.Friction = math.Min(math.Max(e.Bounce, 0), 0.95), math.Min(math.Max(e.Friction, 0), 1)
	}
	if e.Glow.Intensity > 0 && e.Glow.Radius > 0 {
		p.GlowI, p.GlowR = e.Glow.Intensity, e.Glow.Radius*sc
		if e.Glow.Color != "" {
			c := col(e.Glow.Color, "#ffffff")
			p.GlowCol, p.GlowOwn = [3]float64{c[0], c[1], c[2]}, true
		}
	}
	if l := e.Light; l.Enabled && l.Intensity > 0 && l.Reach > 0 {
		var lc [3]float64
		switch {
		case l.Color != "":
			c := col(l.Color, "#ffffff")
			lc = [3]float64{c[0], c[1], c[2]}
		case p.Flame:
			lc = p.FlameCol[1]
		default:
			lc = [3]float64{p.Col[1][0], p.Col[1][1], p.Col[1][2]}
		}
		// flicker: two quick waves, like a flame
		t := float64(idx) / float64(s.FPS)
		ph := float64(p.Seed%1000) / 159.0
		k := l.Intensity * math.Max(0, 1+l.Flicker*(0.55*math.Sin(t*11.3+ph)+0.45*math.Sin(t*29.7+2*ph)))
		end := l.End
		if end <= 0 {
			end = 2.5 * l.Reach
		}
		p.LightOn, p.LightR, p.LightEnd, p.LightAdd = true, l.Reach*sc, end*sc, l.Mode == "add"
		p.LightCol = [3]float64{lc[0] * k, lc[1] * k, lc[2] * k}
		if l.Rim {
			// rim light (used with depth): its sizes scale as the reach
			pos := func(v, def float64) float64 {
				if v > 0 {
					return v
				}
				return def
			}
			p.LightRim, p.LightRimFrom, p.LightRimSoft = true, l.RimFrom*sc, math.Max(l.RimSoft, 1)*sc
			p.LightRimW, p.LightRimGain, p.LightRimEdge = pos(l.RimWidth, 10)*sc, pos(l.RimGain, 2), pos(l.RimEdge, 0.04)
		}
	}
	// big particles are splatted on a coarser grid (they are soft: the same look, far fewer pixels)
	if big := math.Max(p.Size, p.SizeEnd) * (1 + e.SizeVar) * (1 + e.React.Size); big >= 160 && p.Softness >= 0.5 {
		p.Div = 4
	} else if big >= 64 && p.Softness >= 0.5 {
		p.Div = 2
	} else {
		p.Div = 1
	}
	// the births that can still be alive: from lifeMax ago to now
	t := float64(idx) / float64(s.FPS)
	lifeMax := math.Max(0.05, e.Life*(1+math.Abs(e.LifeVar)))
	first := max(0, k-int(math.Ceil(lifeMax*float64(s.FPS)))-1)
	lo, hi := 0, tb.at[k]
	if first > 0 {
		lo = tb.at[first-1] + 1 // with the burst entry of that frame
	}
	bs := tb.births[lo : hi+1]
	if len(bs) < 2 {
		return &effects.EmitFrame{P: p, T: t}
	}
	i0 := max(0, int(math.Floor(bs[0].N-0.5)))
	i1 := int(math.Floor(bs[len(bs)-1].N + 0.5))
	return &effects.EmitFrame{P: p, Births: bs, I0: i0, I1: i1, T: t}
}
