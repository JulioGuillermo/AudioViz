package engine

import (
	"fmt"
	"math"
	"strconv"

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

// textAt: a fixed text's settings at time t (its keys and expressions applied; i: its index)
func textAt(tc *config.TextConfig, t float64, ev config.Evaluator, i int) *config.TextConfig {
	if len(tc.Keys) > 0 || len(tc.Exprs) > 0 {
		return config.Animate(tc, tc.Keys, tc.Exprs, ev, config.ExprCtx{T: t, U: t - tc.Start, Dur: tc.End - tc.Start, ID: "text:" + strconv.Itoa(i)})
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
	k := textAt(tc, t, s, i)
	W, H := float64(s.W), float64(s.H)
	x, y, rot, scale = k.PositionX*W, k.PositionY*H, k.Rotation*math.Pi/180, 1
	if mo := k.Motion; mo.On() {
		var a float64
		x, y, a = effects.PathAt(mo.Path, W, H, mo.Progress)
		if mo.Orient {
			rot += a
		}
	}
	if r := textAt(tc, textRef(tc), s, i); r.Size > 0 && k.Size > 0 {
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
		if len(c0.Keys) > 0 || len(c0.Exprs) > 0 {
			c = config.Animate(c0, c0.Keys, c0.Exprs, s, config.ExprCtx{T: t, U: t - c0.Start, Dur: c0.End - c0.Start, ID: clipID(s.Cfg.Layers[li].ID, ci)})
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

// emitFrame: the emitter clip c0 (keys applied: c) at frame idx (with the gas look, its gas
// simulated up to then: gas.go)
func (s *Session) emitFrame(li, ci int, c0, c *config.ClipConfig, idx int, feats []audio.FrameFeatures) *effects.EmitFrame {
	ef := s.emitParticles(li, ci, c0, c, idx, feats)
	if ef == nil {
		return nil
	}
	if e := c.Emitter; e != nil && e.Look == "gas" {
		ef.P.Gas = s.gasFrame(li, ci, c0, idx, feats, ef, e)
	}
	if e := c.Emitter; e != nil && e.Look == "liquid" {
		ef.Sim = s.liquidFrame(li, ci, c0, idx, feats, e)
		if ef.Sim == nil {
			ef.Sim = []effects.Particle{} // (none yet: not the births' particles either)
		}
	}
	return ef
}

// emitParticles: the emitter clip c0 (keys applied: c) at frame idx: its settings and the
// particles alive
func (s *Session) emitParticles(li, ci int, c0, c *config.ClipConfig, idx int, feats []audio.FrameFeatures) *effects.EmitFrame {
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
	if e.Look == "gas" {
		// the gas look: the flame's colors for its fire, its light and wisps
		g, fl := e.Gas, e.Flame
		rgb := func(h, def string) [3]float64 { c := col(h, def); return [3]float64{c[0], c[1], c[2]} }
		p.GasOn = true
		p.FlameCol = [3][3]float64{rgb(fl.ColorCool, "#b8200a"), rgb(fl.ColorMid, "#ff8a1e"), rgb(fl.ColorHot, "#fff4d0")}
		p.Heat, p.Edge = math.Max(fl.Heat, 0.01), math.Min(math.Max(fl.Edge, 0), 1)
		p.GasOpacity, p.GasWisp, p.GasWispScale = math.Max(g.Opacity, 0), math.Min(math.Max(g.Wisps, 0), 1), math.Max(g.WispScale, 1)*sc
		p.GasLightCol, p.GasShadow, p.GasFireLight = rgb(g.LightColor, "#fff2e0"), rgb(g.ShadowColor, "#3a4250"), math.Max(g.FireLight, 0)
		p.GasHaze, p.GasHazeScale = math.Max(g.Haze, 0)*sc, math.Max(g.HazeScale, 4)*sc
	}
	if e.Look == "smoke" {
		sm := e.Smoke
		p.Smoke = true
		p.SmokeWisp, p.SmokeScale = math.Min(math.Max(sm.Wisp, 0), 1), math.Max(sm.Scale*sc, 1)
		p.SmokeSpeed, p.SmokeWarp = sm.Speed*sc, sm.Warp*sc
	}
	p.PerZ = (e.Look == "liquid" && e.Liquid.D3) || (e.Look == "gas" && e.Gas.D3)
	if e.Look == "clouds" {
		p.Clouds = s.cloudParams(e, sc, idx, feats)
	}
	if e.Look == "water" || e.Look == "liquid" {
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
	s.emitExprs(&p, c0, c, e, float64(idx)/float64(s.FPS), li, ci, sc)
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
		case p.Flame || p.GasOn:
			lc = p.FlameCol[1]
		default:
			lc = [3]float64{p.Col[1][0], p.Col[1][1], p.Col[1][2]}
		}
		// flicker: two quick waves, like a flame
		t := float64(idx) / float64(s.FPS)
		ph := float64(p.Seed%1000) / 159.0
		k := l.Intensity * math.Max(0, 1+l.Flicker*(0.55*math.Sin(t*11.3+ph)+0.45*math.Sin(t*29.7+2*ph)))
		if (l.React != 0 || l.Quiet != 0) && idx < len(feats) { // stronger / weaker with the music
			by := l.ReactTo
			if by == "" {
				by = e.React.ReactTo
			}
			k *= config.LightMusic(l.React, l.Quiet, math.Min(feats[idx].Band(by, "bass"), 1.5))
		}
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
	if p.GasOn {
		p.Div = 4 // not drawn (only their glow)
	} else if big := math.Max(p.Size, p.SizeEnd) * (1 + e.SizeVar) * (1 + e.React.Size); big >= 160 && p.Softness >= 0.5 {
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

// emitExprs: the clip's expressions that change per particle (index, rand(), route.name…),
// specialized for the frame at t for each particle to work out (EmitParams.Exprs)
func (s *Session) emitExprs(p *effects.EmitParams, c0, c *config.ClipConfig, e *config.EmitterConfig, t float64, li, ci int, sc float64) {
	p.Exprs, p.Frame, p.Count, p.FrameW, p.FrameH = nil, int(math.Round(t*float64(s.FPS))), e.Max, s.W, s.H
	if len(c0.Exprs) == 0 {
		return
	}
	W, H := float64(s.W), float64(s.H)
	var route, mpath *config.MaskShape
	if p.Route {
		route = e.Route
	} else if p.Follow {
		mpath = c.Motion.Path
	}
	ctx := &config.ExprCtx{T: t, U: t - c0.Start, Dur: c0.End - c0.Start, ID: clipID(s.Cfg.Layers[li].ID, ci)}
	if c.Motion.On() {
		ctx.Motion = &c.Motion
	}
	for prop, path := range effects.EmitExprProps {
		src, ok := c0.Exprs[path]
		if !ok {
			continue
		}
		prog, err := s.exprProgram(src)
		if err != nil || prog == nil || !prog.PerItem {
			continue // a mistake, or worked out for the whole emitter (Animate)
		}
		value, _, _ := config.NumberAt(c, nil, 0, path)
		color := prop >= effects.EmitColorProp
		keys := make([]effects.EmitExprKey, len(prog.Keys))
		for k, name := range prog.Keys {
			if b := effects.PathBuiltin(name); b > 0 {
				keys[k].Builtin = b // the route's / path's own: where each particle is on it
				continue
			}
			if route != nil {
				keys[k].Route = effects.Track(route, name, W, H)
			}
			if mpath != nil {
				keys[k].Path = effects.Track(mpath, name, W, H)
			}
		}
		perItem := func(kind int, name string) bool {
			for k, n := range prog.Keys {
				if n != name {
					continue
				}
				if keys[k].Builtin > 0 {
					return (kind == 1 && route != nil) || (kind == 0 && mpath != nil)
				}
				return (kind == 1 && keys[k].Route != nil) || (kind == 0 && keys[k].Path != nil)
			}
			return false
		}
		env := &exprEnv{s: s, ctx: ctx, value: value, count: float64(e.Max)}
		mul := sc
		switch prop {
		case 1: // life (s)
			mul = 1
		case 7, 8: // degrees
			mul = math.Pi / 180
		}
		q := prog.Specialize(env, perItem)
		if color {
			// a color: its value (the emitter's color) and its channel read per particle
			q, mul = prog.SpecializeKeep(env, perItem, effects.EmitColorKeep), 1
		}
		p.Exprs = append(p.Exprs, effects.EmitExpr{Prop: prop, Prog: q, Mul: mul, Keys: keys})
	}
	p.Exprs = effects.FitEmitExprs(p.Exprs)
}

// cloudParams: the procedural clouds look of emitter e at frame idx (sc: px per px of a
// 1080-line frame, the clip's scale in it); its lightning flashes on the beats, fading in 0.12 s
func (s *Session) cloudParams(e *config.EmitterConfig, sc float64, idx int, feats []audio.FrameFeatures) *effects.CloudParams {
	c := e.Clouds
	W, H := float64(s.W), float64(s.H)
	f := func(v float64) float32 { return float32(v) }
	rgb := func(h, def string) [3]float32 {
		v, err := visualizer.ParseHexColor(h)
		if err != nil {
			v, _ = visualizer.ParseHexColor(def)
		}
		return [3]float32{float32(v.R) / 255, float32(v.G) / 255, float32(v.B) / 255}
	}
	a := c.LightAngle * math.Pi / 180
	p := &effects.CloudParams{
		Cov: f(math.Min(math.Max(c.Coverage, 0), 1)), Dens: f(math.Max(c.Density, 0)), Scale: f(math.Max(c.Scale, 4) * sc),
		Detail: f(math.Min(math.Max(c.Detail, 0), 1)), Billow: f(math.Max(c.Billow, 0)), EdgeSoft: f(math.Min(math.Max(c.EdgeSoft, 0), 1)),
		WindX: f(c.WindX * sc), WindY: f(c.WindY * sc), Evolve: f(math.Max(c.Evolve, 0)),
		Top: f(c.Top * H), Bottom: f(c.Bottom * H), Soft: f(math.Max(c.Soft, 0.001) * H),
		Persp: c.Perspective, Horizon: f(c.Horizon * H),
		LX: f(math.Sin(a)), LY: f(-math.Cos(a)),
		LightCol: rgb(c.LightColor, "#ffffff"), ShadowCol: rgb(c.ShadowColor, "#8c98aa"), Shadow: f(math.Max(c.Shadow, 0)),
		DepthFog: f(math.Max(c.DepthFog, 0)), W: f(W), H: f(H),
	}
	if c.Lightning > 0 {
		env := 0.0
		for k := 0; k < s.FPS/2 && idx-k >= 0; k++ {
			if idx-k < len(feats) && feats[idx-k].IsBeat {
				env = math.Max(env, math.Exp(-float64(k)/float64(s.FPS)/0.12))
			}
		}
		p.Flash = f(c.Lightning * env)
	}
	return p
}
