package engine

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sync"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/visualizer"
)

// Emitter clips with the gas look (config.GasLook, effects.GasSim): their particles feed a gas
// simulated from the clip's start. The state after a frame depends on every frame before it, so
// each clip keeps its simulation going (the next frame is one step more), its state every second
// (checkpoints: seeking goes on from the nearest one before) and a fingerprint of what fed each
// frame (its particles and the gas's settings): when the clip changes, the simulation is kept up
// to the first frame whose fingerprint changed (changing only how it looks keeps all of it).

// gasSub: simulation steps per frame (about 60 per second)
func gasSub(fps int) int { return max(1, int(math.Ceil(60/float64(max(fps, 1))))) }

type gasRun struct {
	grid  string   // the grid's size: a new one starts over
	tab   string   // the clip as last fingerprinted (emitTable.key)
	f0    int      // the clip's first frame
	fp    []uint64 // fingerprints of the frames f0, f0+1… (checked against the clip as it is)
	sim   *effects.GasSim
	live  int            // the frame the simulation is at (f0-1: before the first one)
	ck    map[int][]byte // the state after frame k
	every int            // frames between checkpoints
	// the last frame drawn (its frame and its light)
	outK   int
	outKey effects.GasLight
	out    *effects.GasFrame
}

var gasMu sync.Mutex

// gasPhys: how the gas of emitter e moves, at sc px per px of a 1080-line frame
func gasPhys(e *config.EmitterConfig, sc float64, seed uint32) effects.GasPhys {
	g := e.Gas
	f := func(v float64) float32 { return float32(v) }
	sc32 := f(sc)
	soot := [3]float32{0.15, 0.14, 0.13}
	if c, err := visualizer.ParseHexColor(g.SootColor); err == nil {
		soot = [3]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}
	}
	return effects.GasPhys{
		Density: f(math.Max(g.Smoke, 0)), Heat: f(math.Max(g.Heat, 0)), Fuel: f(math.Max(g.Fuel, 0)), Push: f(math.Min(math.Max(g.Push, 0), 1)),
		Buoyancy: f(g.Rise) * sc32, Weight: f(g.Weight) * sc32, Swirl: f(math.Max(g.Swirl, 0)),
		WindX: f(g.Wind) * sc32, Turb: f(math.Max(g.Turbulence, 0)) * sc32, TurbScale: f(math.Max(g.TurbScale, 1)) * sc32,
		Fade: f(math.Max(g.Fade, 0)), Cool: f(math.Max(g.Cool, 0)), Burn: f(math.Max(g.Burn, 0)), Soot: f(math.Max(g.Soot, 0)),
		Spread:  f(math.Max(g.Spread, 0)) * sc32 * sc32,
		SootCol: soot, Seed: seed,
	}
}

// gasCell: the simulation's cell (px of this frame)
func (s *Session) gasCell(e *config.EmitterConfig) float64 {
	d := e.Gas.Detail
	if d <= 0 {
		d = 5
	}
	return math.Min(math.Max(d, 2), 32) * s.emitScale()
}

// gasFingerprint: what feeds the gas at a frame: its particles' settings, the births of that
// frame and how the gas moves
func gasFingerprint(p *effects.EmitParams, ph *effects.GasPhys, births []effects.Birth) uint64 {
	h := fnv.New64a()
	w := func(v ...any) {
		for _, x := range v {
			binary.Write(h, binary.LittleEndian, x)
		}
	}
	w(int64(p.Shape), p.W, p.H, p.Dir, p.Spread, p.Speed, p.SpeedVar, p.Gravity, p.Wind, p.Drag, p.Turb, p.TurbFreq,
		p.Life, p.LifeVar, p.Size, p.SizeEnd, p.SizeVar, p.Col, p.FadeIn, p.FadeOut, p.Seed,
		p.Follow, p.Route, p.RouteFixed, p.RouteK, p.PathClosed, p.PathLen, p.FloorOn, p.Floor, p.Bounce, p.Friction)
	w(int64(len(p.Path)), int64(len(p.Pts)))
	for _, v := range p.Path {
		w(v)
	}
	for _, v := range p.Pts {
		w(v)
	}
	if len(p.Exprs) > 0 {
		fmt.Fprintf(h, "%v", p.Exprs)
	}
	w(*ph)
	for _, b := range births {
		w(b)
	}
	return h.Sum64()
}

// gasFrame: the gas of emitter clip (li, ci) at frame idx (ef: its particles then), simulated up
// to there
func (s *Session) gasFrame(li, ci int, c0 *config.ClipConfig, idx int, feats []audio.FrameFeatures, ef *effects.EmitFrame, e *config.EmitterConfig) *effects.GasFrame {
	gasMu.Lock()
	defer gasMu.Unlock()
	tb := s.emitTableFor(li, ci, c0, feats)
	id := fmt.Sprintf("%d:%d", li, ci)
	cs := s.gasCell(e)
	grid := fmt.Sprintf("%dx%d@%d|%g|%d", s.W, s.H, s.FPS, cs, tb.f0)
	run := s.gasRuns[id]
	if run == nil || run.grid != grid {
		if s.gasRuns == nil || len(s.gasRuns) > 16 {
			s.gasRuns = map[string]*gasRun{}
		}
		run = &gasRun{grid: grid, f0: tb.f0, live: tb.f0 - 1, ck: map[int][]byte{}, every: max(1, s.FPS), outK: -1}
		run.sim = effects.NewGasSim(s.W, s.H, cs)
		s.gasRuns[id] = run
	}
	// what fed each frame up to this one, as the clip is now: kept up to the first change
	n := idx - run.f0 + 1
	if n <= 0 {
		return nil
	}
	if run.tab != tb.key {
		valid := 0
		for k := 0; k < min(n, len(run.fp)); k++ {
			if s.gasPrint(li, ci, c0, run.f0+k, feats, tb) != run.fp[k] {
				break
			}
			valid++
		}
		run.fp = run.fp[:valid]
		run.tab = tb.key
		run.drop(run.f0 + valid)
	}
	for k := len(run.fp); k < n; k++ {
		run.fp = append(run.fp, s.gasPrint(li, ci, c0, run.f0+k, feats, tb))
	}
	// from where it is, or the nearest checkpoint before
	if run.live > idx {
		run.live = -1 << 30
	}
	best := run.f0 - 1
	for k := range run.ck {
		if k <= idx && k > best {
			best = k
		}
	}
	if run.live < best {
		if best == run.f0-1 {
			run.sim = effects.NewGasSim(s.W, s.H, cs)
		} else {
			run.sim.Load(run.ck[best])
		}
		run.live = best
		run.outK = -1
	}
	for run.live < idx {
		k := run.live + 1
		s.gasStep(li, ci, c0, k, feats, run)
		run.live = k
		if (k-run.f0+1)%run.every == 0 {
			if _, ok := run.ck[k]; !ok {
				run.ck[k] = run.sim.Save()
			}
		}
		run.outK = -1
	}
	sc := s.emitScale()
	l := effects.GasLight{Angle: float32(e.Gas.LightAngle * math.Pi / 180), Shadow: float32(math.Max(e.Gas.Shadow, 0)),
		Opacity: float32(math.Max(e.Gas.Opacity, 0)), HeatK: float32(math.Max(e.Flame.Heat, 0.01)),
		Reach: float32(320 * sc), FireReach: float32(140 * sc), Depth: float32(40 * sc)}
	if run.outK == idx && run.outKey == l {
		return run.out
	}
	run.out, run.outK, run.outKey = run.sim.Frame(l), idx, l
	return run.out
}

// drop forgets the simulation from frame k on
func (r *gasRun) drop(k int) {
	for f := range r.ck {
		if f >= k {
			delete(r.ck, f)
		}
	}
	if r.live >= k {
		r.live = -1 << 30
	}
}

// gasClip: the emitter clip at frame k (its keys and expressions applied) and its particles
func (s *Session) gasClip(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures) (*effects.EmitFrame, *config.EmitterConfig) {
	t := float64(k) / float64(s.FPS)
	c := c0
	if len(c0.Keys) > 0 || len(c0.Exprs) > 0 {
		c = config.Animate(c0, c0.Keys, s.clipFrameExprs(c0, true), s, config.ExprCtx{T: t, U: t - c0.Start, Dur: c0.End - c0.Start, ID: clipID(s.Cfg.Layers[li].ID, ci)})
	}
	e := c.Emitter
	if e == nil {
		d := config.DefaultEmitter()
		e = &d
	}
	return s.emitParticles(li, ci, c0, c, k, feats), e
}

// gasPrint: the fingerprint of frame k
func (s *Session) gasPrint(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures, tb *emitTable) uint64 {
	ef, e := s.gasClip(li, ci, c0, k, feats)
	if ef == nil {
		return 0
	}
	ph := gasPhys(e, s.emitScale(), ef.P.Seed)
	j := k - tb.f0
	lo := 0
	if j > 0 {
		lo = tb.at[j-1] + 1
	}
	return gasFingerprint(&ef.P, &ph, tb.births[lo:tb.at[j]+1])
}

// gasStep: the simulation through frame k (from the moment of frame k - 1): its steps, each fed
// by the particles alive then
func (s *Session) gasStep(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures, run *gasRun) {
	ef, e := s.gasClip(li, ci, c0, k, feats)
	if ef == nil {
		return
	}
	ph := gasPhys(e, s.emitScale(), ef.P.Seed)
	sub := gasSub(s.FPS)
	dt := float32(1 / float64(s.FPS*sub))
	src := make([]effects.GasSource, 0, ef.I1-ef.I0)
	for j := 0; j < sub; j++ {
		ef.T = (float64(k-1) + float64(j+1)/float64(sub)) / float64(s.FPS)
		src = src[:0]
		parts := make([]effects.Particle, ef.I1-ef.I0)
		alive := make([]bool, len(parts))
		effects.ParallelRange(len(parts), func(a, b int) {
			for i := a; i < b; i++ {
				parts[i], alive[i] = ef.At(ef.I0 + i)
			}
		})
		for i, pt := range parts {
			if alive[i] && pt.A > 0 {
				src = append(src, effects.GasSource{X: pt.X, Y: pt.Y, R: pt.Size * 0.5, A: pt.A, VX: pt.VX, VY: pt.VY,
					Col: [3]float32{pt.R, pt.G, pt.B}})
			}
		}
		run.sim.Feed(src, &ph, dt)
		run.sim.Step(&ph, dt)
	}
}
