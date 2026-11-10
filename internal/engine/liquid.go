package engine

import (
	"fmt"
	"math"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
)

// Emitter clips with the liquid look (config.LiquidLook, effects.LiquidSim): their particles are
// born as drops of a liquid simulated from the clip's start. Kept as the gas (gas.go): the
// simulation goes on from frame to frame, its state every second, a fingerprint of what fed
// each frame so that a change of the clip keeps it up to the first frame it changes.

type liquidRun struct {
	grid  string
	tab   string
	f0    int
	fp    []uint64
	sim   *effects.LiquidSim
	live  int
	ck    map[int][]byte
	every int
	outK  int
	out   []effects.Particle
	pre   map[int]gasPre
}

// liquidCell: the simulation's cell (px of this frame)
func (s *Session) liquidCell(e *config.EmitterConfig) float64 {
	d := e.Liquid.Detail
	if d <= 0 {
		d = 6
	}
	return math.Min(math.Max(d, 2), 32) * s.emitScale()
}

// liquidSpeed: how fast the liquid of emitter e lives
func liquidSpeed(e *config.EmitterConfig) float32 {
	if e.Liquid.Speed <= 0 {
		return 1
	}
	return float32(math.Min(math.Max(e.Liquid.Speed, 0.05), 8))
}

// liquidPhys: how the liquid of frame ef moves (its floor: the emitter's, or the scene's under it)
func (s *Session) liquidPhys(ef *effects.EmitFrame, e *config.EmitterConfig, t float64) effects.LiquidPhys {
	P := &ef.P
	ph := effects.LiquidPhys{GravX: float32(P.Wind), GravY: float32(P.Gravity),
		Flip: float32(math.Min(math.Max(e.Liquid.Splash, 0), 1)), FloorOn: P.FloorOn, Floor: float32(P.Floor)}
	if e.FloorDepth {
		// the scene's floor below the emitter (as layersAt finds it for the particles)
		if scene := s.simScene(t); scene.On() && len(ef.Births) > 0 {
			b := ef.Births[len(ef.Births)-1]
			zf := float64(scene.Ground(float32(b.X), float32(b.Y)))
			for y := b.Y + 2; y < float64(s.H); y += 2 {
				if float64(scene.Ground(float32(b.X), float32(y))) >= zf-0.01 {
					ph.FloorOn, ph.Floor = true, float32(y+math.Max(e.Floor, 0)*s.emitScale())
					break
				}
			}
		}
	}
	return ph
}

// liquidFrame: the drops of emitter clip (li, ci) at frame idx, simulated up to there
func (s *Session) liquidFrame(li, ci int, c0 *config.ClipConfig, idx int, feats []audio.FrameFeatures, e *config.EmitterConfig) []effects.Particle {
	gasMu.Lock()
	defer gasMu.Unlock()
	tb := s.emitTableFor(li, ci, c0, feats)
	id := fmt.Sprintf("%d:%d", li, ci)
	cs := s.liquidCell(e)
	grid := fmt.Sprintf("%dx%d@%d|%g|%d", s.W, s.H, s.FPS, cs, tb.f0)
	run := s.liquidRuns[id]
	if run == nil || run.grid != grid {
		if s.liquidRuns == nil || len(s.liquidRuns) > 16 {
			s.liquidRuns = map[string]*liquidRun{}
		}
		run = &liquidRun{grid: grid, f0: tb.f0, live: tb.f0 - 1, ck: map[int][]byte{}, every: max(1, s.FPS), outK: -1}
		run.sim = effects.NewLiquidSim(s.W, s.H, cs)
		run.ck[run.f0-1] = run.sim.Save()
		s.liquidRuns[id] = run
	}
	n := idx - run.f0 + 1
	if n <= 0 {
		return nil
	}
	if run.tab != tb.key {
		valid := 0
		for k := 0; k < min(n, len(run.fp)); k++ {
			if s.liquidPrint(s.liquidClip(li, ci, c0, run.f0+k, feats), tb, run.f0+k) != run.fp[k] {
				break
			}
			valid++
		}
		run.fp, run.tab = run.fp[:valid], tb.key
		for f := range run.ck {
			if f >= run.f0+valid {
				delete(run.ck, f)
			}
		}
		if run.live >= run.f0+valid {
			run.live = -1 << 30
		}
	}
	run.pre = map[int]gasPre{}
	defer func() { run.pre = nil }()
	for k := len(run.fp); k < n; k++ {
		g := s.liquidClip(li, ci, c0, run.f0+k, feats)
		run.fp = append(run.fp, s.liquidPrint(g, tb, run.f0+k))
		if len(run.pre) < 4096 {
			run.pre[run.f0+k] = g
		}
	}
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
		run.sim.Load(run.ck[best])
		run.live, run.outK = best, -1
	}
	for run.live < idx {
		k := run.live + 1
		g, ok := run.pre[k]
		if !ok {
			g = s.liquidClip(li, ci, c0, k, feats)
		}
		s.liquidStep(g, k, run)
		run.live, run.outK = k, -1
		if (k-run.f0+1)%run.every == 0 {
			if _, ok := run.ck[k]; !ok {
				run.ck[k] = run.sim.Save()
			}
		}
	}
	if run.outK != idx {
		run.out, run.outK = run.sim.Particles(), idx
	}
	return run.out
}

// liquidClip: the emitter clip at frame k: its particles, settings and what its liquid goes round
func (s *Session) liquidClip(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures) gasPre {
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
	ef := s.emitParticles(li, ci, c0, c, k, feats)
	g := gasPre{ef: ef, e: e}
	if ef != nil && e.Collide {
		g.solid = s.gasSolid(c, e, ef, t, s.liquidCell(e))
	}
	return g
}

// liquidPrint: what fed frame k (its particles' settings and births, the liquid's physics,
// the solid cells)
func (s *Session) liquidPrint(g gasPre, tb *emitTable, k int) uint64 {
	ef, e := g.ef, g.e
	if ef == nil {
		return 0
	}
	ph := s.liquidPhys(ef, e, float64(k)/float64(s.FPS))
	extra := struct {
		Ph    effects.LiquidPhys
		Speed float32
		Max   int64
	}{ph, liquidSpeed(e), int64(e.Max)}
	j := k - tb.f0
	lo := 0
	if j > 0 {
		lo = tb.at[j-1] + 1
	}
	return gasFingerprint(&ef.P, &extra, tb.births[lo:tb.at[j]+1], g.solid)
}

// liquidStep: the simulation through frame k: its steps, then the drops born since frame k - 1
// where their particles are at frame k (each at its own age: a jet stays a jet)
func (s *Session) liquidStep(g gasPre, k int, run *liquidRun) {
	ef, e := g.ef, g.e
	if ef == nil {
		return
	}
	t := float64(k) / float64(s.FPS)
	var bs []effects.LiquidBirth
	col := ef.P.Col[1]
	for {
		i := int(run.sim.Next)
		bt, ok := ef.BirthOf(i)
		if !ok || bt > t {
			// (born before this frame's window: gone; after now: later)
			if ok || i >= ef.I1 {
				break
			}
			run.sim.Next++
			continue
		}
		if pt, ok := ef.At(i); ok { // (at frame k's moment)
			bs = append(bs, effects.LiquidBirth{X: pt.X, Y: pt.Y, VX: pt.VX, VY: pt.VY, Size: pt.Size,
				Col: [4]float32{float32(col[0]), float32(col[1]), float32(col[2]), float32(col[3])}, Idx: int32(i)})
		}
		run.sim.Next++
	}
	run.sim.Solid = g.solid
	ph := s.liquidPhys(ef, e, t)
	dt := liquidSpeed(e) / float32(s.FPS)
	steps := run.sim.Substeps(dt, &ph)
	for j := 0; j < steps; j++ {
		run.sim.Step(&ph, dt/float32(steps))
	}
	run.sim.Add(bs, max(e.Max, 1))
}
