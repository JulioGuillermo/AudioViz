package engine

import (
	"fmt"
	"math"

	"gopkg.in/yaml.v3"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
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

// emitTableFor builds (or reuses) the table of births of a clip
func (s *Session) emitTableFor(li, ci int, c0 *config.ClipConfig, feats []audio.FrameFeatures) *emitTable {
	b, _ := yaml.Marshal(c0)
	key := fmt.Sprintf("%d:%d:%dx%d@%d|%s", li, ci, s.W, s.H, s.FPS, b)
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
		bt := effects.Birth{T: t, N: n, X: x, Y: y, Rot: rot, Speed: 1 + e.React.Speed*band, Size: 1 + e.React.Size*band}
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
