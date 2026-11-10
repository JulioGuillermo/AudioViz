package engine

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"strings"
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

// gasSub: simulation steps per frame (about 30 per second of the gas's own time: semi-Lagrangian
// advection stays stable with long steps)
func gasSub(fps int, speed float32) int {
	return max(1, int(math.Ceil(30*float64(speed)/float64(max(fps, 1)))))
}

// gasSpeed: how fast the gas of emitter e lives (GasLook.Speed; 0 = 1)
func gasSpeed(e *config.EmitterConfig) float32 {
	if e.Gas.Speed <= 0 {
		return 1
	}
	return float32(math.Min(math.Max(e.Gas.Speed, 0.05), 8))
}

type gasRun struct {
	grid  string   // the grid's size: a new one starts over
	tab   string   // the clip as last fingerprinted (emitTable.key)
	f0    int      // the clip's first frame
	fp    []uint64 // fingerprints of the frames f0, f0+1… (checked against the clip as it is)
	sim   effects.GasSimulator
	live  int            // the frame the simulation is at (f0-1: before the first one)
	ck    map[int][]byte // the state after frame k
	every int            // frames between checkpoints
	// the particles of the frames just fingerprinted, for their steps (within one call)
	pre map[int]gasPre
	// the last frame drawn (its frame and its light)
	outK   int
	outKey effects.GasLight
	out    *effects.GasFrame
}

var gasMu sync.Mutex

// gasPre: an emitter clip's particles at a frame and its settings then
type gasPre struct {
	ef    *effects.EmitFrame
	e     *config.EmitterConfig
	solid []uint8            // the cells the gas goes round then (nil: none)
	surf  []float32          // 3D: the scene's surface (the liquid: depth px; the gas: depth 0..1)
	free  []uint8            // 3D gas: the cells free around the emitter
	z0    float32            // 3D gas: the emitter's depth
	scene *effects.Scene     // (and the scene it comes from)
	c     *config.ClipConfig // the clip then (its keys applied)
}

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
		Spread: f(math.Max(g.Spread, 0)) * sc32 * sc32, Expand: f(math.Max(g.Expand, 0)),
		SootCol: soot, Seed: seed, TimeScale: gasSpeed(e),
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
func gasFingerprint(p *effects.EmitParams, ph any, births []effects.Birth, solid []uint8) uint64 {
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
	w(ph)
	for _, b := range births {
		w(b)
	}
	w(int64(len(solid)))
	h.Write(solid)
	return h.Sum64()
}

// gasFrame: the gas of emitter clip (li, ci) at frame idx (ef: its particles then), simulated up
// to there
func (s *Session) gasFrame(li, ci int, c0 *config.ClipConfig, idx int, feats []audio.FrameFeatures, ef *effects.EmitFrame, e *config.EmitterConfig) *effects.GasFrame {
	gasMu.Lock()
	defer gasMu.Unlock()
	g, err := s.gasFrameAt(li, ci, c0, idx, feats, e)
	if err != nil {
		// the GPU failed: everything again on the CPU
		s.Logf("⚠️  Gas simulation on the GPU failed (%v), using the CPU", err)
		s.gasCPU = true
		s.closeGas()
		g, _ = s.gasFrameAt(li, ci, c0, idx, feats, e)
	}
	return g
}

// simOnGPU: whether emitter e's gas or liquid is simulated on the GPU: as its clip says (Sim),
// else (auto) the gas on the GPU unless rendering on the CPU or AUDIOVIZ_GAS_CPU=1, and the liquid
// on the CPU (it works only where the liquid is: as fast as the GPU on an iGPU, often faster);
// never once the GPU failed
func (s *Session) simOnGPU(e *config.EmitterConfig) bool {
	if s.gasCPU {
		return false
	}
	switch strings.ToLower(e.Sim) {
	case "cpu":
		return false
	case "gpu":
		return true
	}
	if e.Look == "liquid" || (e.Look == "gas" && e.Gas.D3) {
		return false // (the gas in 3D runs on the CPU)
	}
	return !strings.EqualFold(s.Cfg.Output.Renderer, "cpu") && os.Getenv("AUDIOVIZ_GAS_CPU") == ""
}

// newGasSim: an empty gas, where emitter e says (simOnGPU)
func (s *Session) newGasSim(cs float64, e *config.EmitterConfig) effects.GasSimulator {
	if s.simOnGPU(e) {
		g, err := effects.NewGasGPU(s.Cfg.Output.GPUDevice, s.W, s.H, cs)
		if err == nil {
			return g
		}
		s.Logf("⚠️  Gas simulation on the GPU not available (%v), using the CPU", err)
		s.gasCPU = true
	}
	return effects.NewGasSim(s.W, s.H, cs)
}

// closeGas forgets every gas simulation (freeing what they hold on the GPU)
func (s *Session) closeGas() {
	for _, r := range s.gasRuns {
		r.sim.Close()
	}
	s.gasRuns = nil
}

func (s *Session) gasFrameAt(li, ci int, c0 *config.ClipConfig, idx int, feats []audio.FrameFeatures, e *config.EmitterConfig) (*effects.GasFrame, error) {
	tb := s.emitTableFor(li, ci, c0, feats)
	id := fmt.Sprintf("%d:%d", li, ci)
	cs := s.gasCell(e)
	grid := fmt.Sprintf("%dx%d@%d|%g|%d|%v", s.W, s.H, s.FPS, cs, tb.f0, s.simOnGPU(e))
	run := s.gasRuns[id]
	if run == nil || run.grid != grid {
		if run != nil {
			run.sim.Close()
		}
		if s.gasRuns == nil || len(s.gasRuns) > 16 {
			s.closeGas()
			s.gasRuns = map[string]*gasRun{}
		}
		run = &gasRun{grid: grid, f0: tb.f0, live: tb.f0 - 1, ck: map[int][]byte{}, every: max(1, s.FPS), outK: -1}
		run.sim = s.newGasSim(cs, e)
		// the empty state, to start again from
		ck, err := run.sim.Checkpoint()
		if err != nil {
			run.sim.Close()
			return nil, err
		}
		run.ck[run.f0-1] = ck
		s.gasRuns[id] = run
	}
	// what fed each frame up to this one, as the clip is now: kept up to the first change
	n := idx - run.f0 + 1
	if n <= 0 {
		return nil, nil
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
	run.pre = map[int]gasPre{}
	defer func() { run.pre = nil }()
	for k := len(run.fp); k < n; k++ {
		g := s.gasClip(li, ci, c0, run.f0+k, feats)
		run.fp = append(run.fp, gasPrintOf(g, s.emitScale(), tb, run.f0+k))
		if len(run.pre) < 4096 {
			run.pre[run.f0+k] = g
		}
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
		run.sim.Restore(run.ck[best])
		run.live = best
		run.outK = -1
	}
	for run.live < idx {
		k := run.live + 1
		run.live = -1 << 30 // (until this step is done)
		if err := s.gasStep(li, ci, c0, k, feats, run); err != nil {
			return nil, err
		}
		run.live = k
		if (k-run.f0+1)%run.every == 0 {
			if _, ok := run.ck[k]; !ok {
				ck, err := run.sim.Checkpoint()
				if err != nil {
					return nil, err
				}
				run.ck[k] = ck
			}
		}
		run.outK = -1
	}
	sc := s.emitScale()
	l := effects.GasLight{Angle: float32(e.Gas.LightAngle * math.Pi / 180), Shadow: float32(math.Max(e.Gas.Shadow, 0)),
		Opacity: float32(math.Max(e.Gas.Opacity, 0)), HeatK: float32(math.Max(e.Flame.Heat, 0.01)),
		Reach: float32(320 * sc), FireReach: float32(140 * sc), Depth: float32(40 * sc)}
	if run.outK == idx && run.outKey == l {
		return run.out, nil
	}
	out, err := run.sim.Draw(l)
	if err != nil {
		return nil, err
	}
	// its flames as the light sources of the emitter's light (as much light as a flame of
	// ~250 px² of 1080-line frame per unit)
	sc32 := float32(sc)
	out.Light = out.LightSources(s.W, s.H, l.HeatK, 250*sc32*sc32)
	run.out, run.outK, run.outKey = out, idx, l
	return run.out, nil
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

// gasClip: the emitter clip at frame k (its keys and expressions applied): its particles, its
// settings and what its gas goes round
func (s *Session) gasClip(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures) gasPre {
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
	g := gasPre{ef: ef, e: e, c: c}
	if ef != nil {
		g.solid = s.simSolid(c, e, ef, t, s.gasCell(e))
		if e.Gas.D3 {
			s.gasScene3D(&g, c, e, t)
		}
	}
	return g
}

// simScene: the scene's depth at time t for the simulations (gas, liquid): the background segment
// on screen then, as it moves, without the camera's effects (nil: no depth)
func (s *Session) simScene(t float64) *effects.Scene {
	tl := &s.Cfg.Timeline
	bi := config.Active(tl.Background, t)
	if bi < 0 {
		return nil
	}
	_, cbg, _ := config.BlendEv(tl.Background, t, s.Duration(), s, "Background")
	dA, nA, _ := s.depthFor(cbg, t-tl.Background[bi].At)
	if dA == nil {
		return nil
	}
	return &effects.Scene{A: dA, NA: nA, LA: s.motionAt(bi, t), W: s.W, H: s.H}
}

// gasSolid: the cells of the gas grid it goes round at time t: where the background's depth (the
// segment on screen then, as it moves; without the camera's effects) is within the band of the
// emitter's depth, but not around where it is born (as the particles' collide in layersAt)
func (s *Session) gasSolid(c *config.ClipConfig, e *config.EmitterConfig, ef *effects.EmitFrame, t, cell float64) []uint8 {
	scene := s.simScene(t)
	if scene == nil {
		return nil
	}
	ox, oy := float64(s.W)/2, float64(s.H)/2
	if n := len(ef.Births); n > 0 {
		ox, oy = ef.Births[n-1].X, ef.Births[n-1].Y
	}
	zc, ok := clipDepth(&c.Depth, scene, ox, oy)
	if !ok {
		zc = float64(scene.Ground(float32(ox), float32(oy)))
	}
	band := e.CollideBand
	if band <= 0 {
		band = 0.06
	}
	sc := s.emitScale() * math.Max(c.Scale, 0.01)
	free := math.Max(e.Width, e.Height)*sc/2 + 24*s.emitScale()
	gw, gh, cs := effects.GasGrid(s.W, s.H, cell)
	solid := make([]uint8, gw*gh)
	found := false
	for cy := 0; cy < gh; cy++ {
		for cx := 0; cx < gw; cx++ {
			x, y := (float64(cx)+0.5)*float64(cs), (float64(cy)+0.5)*float64(cs)
			if math.Hypot(x-ox, y-oy) < free || x >= float64(s.W) || y >= float64(s.H) {
				continue
			}
			if math.Abs(float64(scene.At(float32(x), float32(y)))-zc) < band {
				solid[cy*gw+cx], found = 1, true
			}
		}
	}
	if !found {
		return nil
	}
	return solid
}

// simSolid: the cells of a simulation's grid (cells of about cell px) its gas or liquid goes
// round at time t: the scene's (collide) and the frame's edges (walls); nil: none
func (s *Session) simSolid(c *config.ClipConfig, e *config.EmitterConfig, ef *effects.EmitFrame, t, cell float64) []uint8 {
	var solid []uint8
	if e.Collide && !(e.Look == "gas" && e.Gas.D3) { // (the gas in 3D works its solids out itself)
		solid = s.gasSolid(c, e, ef, t, cell)
	}
	if e.Walls {
		gw, gh, _ := effects.GasGrid(s.W, s.H, cell)
		if solid == nil {
			solid = make([]uint8, gw*gh)
		}
		// (2: a wall, solid but casting no shadow on the gas's light)
		for x := 0; x < gw; x++ {
			solid[x], solid[(gh-1)*gw+x] = 2, 2
		}
		for y := 0; y < gh; y++ {
			solid[y*gw], solid[y*gw+gw-1] = 2, 2
		}
	}
	return solid
}

// gasPrint: the fingerprint of frame k
func (s *Session) gasPrint(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures, tb *emitTable) uint64 {
	return gasPrintOf(s.gasClip(li, ci, c0, k, feats), s.emitScale(), tb, k)
}

// gasPrintOf: the fingerprint of frame k with its particles and solid cells g
func gasPrintOf(g gasPre, sc float64, tb *emitTable, k int) uint64 {
	ef, e := g.ef, g.e
	if ef == nil {
		return 0
	}
	ph := gasPhys(e, sc, ef.P.Seed)
	j := k - tb.f0
	lo := 0
	if j > 0 {
		lo = tb.at[j-1] + 1
	}
	fp := gasFingerprint(&ef.P, &ph, tb.births[lo:tb.at[j]+1], g.solid)
	if ef.P.Route {
		fp ^= gasFingerprint(&ef.P, &[2]float64{e.RouteWidth, e.RoutePull}, nil, nil) * 41
	}
	if g.surf != nil {
		fp ^= gasFingerprint(&ef.P, &g.z0, nil, append(g.free, unsafeBytes(g.surf)...)) * 31
	}
	return fp
}

// gasScene3D: the 3D gas's scene at time t: the surface on the gas's cells (depth 0..1), the
// cells free around the emitter (as the particles' collide), the new gas's depth (its clip's,
// else just in front of what is under the emitter)
func (s *Session) gasScene3D(g *gasPre, c *config.ClipConfig, e *config.EmitterConfig, t float64) {
	scene := s.simScene(t)
	if !scene.On() {
		return
	}
	gw, gh, cs := effects.GasGrid(s.W, s.H, s.gasCell(e))
	ox, oy := float64(s.W)/2, float64(s.H)/2
	if n := len(g.ef.Births); n > 0 {
		ox, oy = g.ef.Births[n-1].X, g.ef.Births[n-1].Y
	}
	band := e.CollideBand
	if band <= 0 {
		band = 0.06
	}
	z, ok := clipDepth(&c.Depth, scene, ox, oy)
	if !ok {
		z = float64(scene.Ground(float32(ox), float32(oy))) + band*1.2
	}
	free := math.Max(e.Width, e.Height)*s.emitScale()*math.Max(c.Scale, 0.01)/2 + 24*s.emitScale()
	g.surf, g.free = make([]float32, gw*gh), make([]uint8, gw*gh)
	for j := 0; j < gh; j++ {
		for i := 0; i < gw; i++ {
			x, y := (float32(i)+0.5)*cs, (float32(j)+0.5)*cs
			g.surf[j*gw+i] = scene.At(x, y)
			if math.Hypot(float64(x)-ox, float64(y)-oy) < free {
				g.free[j*gw+i] = 1
			}
		}
	}
	g.z0 = float32(z)
}

// gasStep: the simulation through frame k (from the moment of frame k - 1): its steps, each fed
// by the particles alive then
func (s *Session) gasStep(li, ci int, c0 *config.ClipConfig, k int, feats []audio.FrameFeatures, run *gasRun) error {
	g, ok := run.pre[k]
	if !ok {
		g = s.gasClip(li, ci, c0, k, feats)
	}
	ef, e := g.ef, g.e
	if ef == nil {
		return nil
	}
	ph := gasPhys(e, s.emitScale(), ef.P.Seed)
	// (faster: more and longer steps, the particles still at their own moments)
	sub := gasSub(s.FPS, ph.TimeScale)
	dt := ph.TimeScale / float32(s.FPS*sub)
	srcs := make([][]effects.GasSource, sub)
	for j := 0; j < sub; j++ {
		ef.T = (float64(k-1) + float64(j+1)/float64(sub)) / float64(s.FPS)
		src := make([]effects.GasSource, 0, ef.I1-ef.I0)
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
		srcs[j] = src
	}
	band := e.CollideBand
	if band <= 0 {
		band = 0.06
	}
	run.sim.SetScene(g.surf, g.free, g.z0, float32(band), float32(-e.Gas.DriftZ/effects.DepthPx))
	gw, gh, cs := effects.GasGrid(s.W, s.H, s.gasCell(e))
	run.sim.SetGuide(s.simGuide(ef, e, gw, gh, cs))
	return run.sim.Advance(srcs, &ph, dt, g.solid)
}

// simGuide: the particles' route as a guide for the simulated flow (effects.SimGuide) on a
// gw×gh grid of cs px: where it is drawn, or from where the emitter is now; nil: no route
func (s *Session) simGuide(ef *effects.EmitFrame, e *config.EmitterConfig, gw, gh int, cs float32) []float32 {
	P := &ef.P
	if !P.Route || P.RouteK <= 0 || len(P.Path) < 6 {
		return nil
	}
	var sx, sy float32
	if n := len(ef.Births); !P.RouteFixed && n > 0 {
		sx, sy = float32(ef.Births[n-1].X)-P.Path[0], float32(ef.Births[n-1].Y)-P.Path[1]
	}
	w := e.RouteWidth
	if w <= 0 {
		w = 120
	}
	sc := s.emitScale()
	return effects.SimGuide(P.Path, P.PathClosed, sx, sy, gw, gh, cs, float32(w*sc), float32(P.Speed),
		float32(math.Min(math.Max(e.RoutePull, 0), 1)), float32(P.RouteK))
}
