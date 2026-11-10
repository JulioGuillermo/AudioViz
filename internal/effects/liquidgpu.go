package effects

import (
	"math"

	"audioviz/internal/gpu"
)

// LiquidSimulator: a liquid simulation, on the CPU (LiquidSim) or on the GPU (LiquidGPU); both
// keep the drops on the CPU between frames (their state, checkpoints of one fit the other)
type LiquidSimulator interface {
	// Add: newborn drops (past max the oldest go)
	Add(bs []LiquidBirth, max int)
	// Advance: a frame of dt in steps (Substeps), the solid cells then (nil: none)
	Advance(ph *LiquidPhys, dt float32, steps int, solid []uint8) error
	Substeps(dt float32, ph *LiquidPhys) int
	Particles() []Particle
	Save() []byte
	Load(b []byte) bool
	NextBirth() *int32
	Close()
}

func (s *LiquidSim) Advance(ph *LiquidPhys, dt float32, steps int, solid []uint8) error {
	s.Solid = solid
	for j := 0; j < steps; j++ {
		s.Step(ph, dt/float32(steps))
	}
	return nil
}
func (s *LiquidSim) NextBirth() *int32 { return &s.Next }
func (s *LiquidSim) Close()            {}

// LiquidGPU: the liquid simulated on the GPU (gpu.LiquidJob, liquid.comp): the steps of a frame
// there, the drops (LiquidSim's state) uploaded before and read back after
type LiquidGPU struct {
	*LiquidSim
	job       *gpu.LiquidJob
	clearP    bool // the pressure to clear before the next steps (after a checkpoint)
	lastSolid bool
}

// NewLiquidGPU: no liquid yet over a W×H frame, cells of about cs px, on GPU deviceIndex
func NewLiquidGPU(deviceIndex, W, H int, cs float64) (*LiquidGPU, error) {
	s := NewLiquidSim(W, H, cs)
	job, err := gpu.NewLiquidJob(deviceIndex, s.GW, s.GH)
	if err != nil {
		return nil, err
	}
	return &LiquidGPU{LiquidSim: s, job: job}, nil
}

func (g *LiquidGPU) Close() { g.job.Close() }

func (g *LiquidGPU) Save() []byte {
	g.clearP = true
	return g.LiquidSim.Save()
}

func (g *LiquidGPU) Load(b []byte) bool {
	g.clearP = true
	return g.LiquidSim.Load(b)
}

func (g *LiquidGPU) Advance(ph *LiquidPhys, dt float32, steps int, solid []uint8) error {
	s := g.LiquidSim
	n := min(len(s.X), gpu.LiquidCap)
	ib := func(v int32) float32 { return math.Float32frombits(uint32(v)) }
	on := int32(0)
	if ph.FloorOn {
		on = 1
	}
	copy(g.job.Cfg(), []float32{ib(int32(s.GW)), ib(int32(s.GH)), ib(int32(n)), ib(on),
		s.CS, s.W, s.H, ph.Floor, ph.GravX, ph.GravY, ph.Flip, 0})
	if solid != nil || g.lastSolid {
		sb := g.job.Solid()
		clear(sb)
		for i, v := range solid {
			if v != 0 {
				sb[i] = 1
			}
		}
		g.lastSolid = solid != nil
	}
	d := g.job.Drops()
	c := gpu.LiquidCap
	copy(d[0:n], s.X[:n])
	copy(d[c:c+n], s.Y[:n])
	copy(d[2*c:2*c+n], s.VX[:n])
	copy(d[3*c:3*c+n], s.VY[:n])
	copy(d[4*c:4*c+n], s.Born[:n])
	if g.clearP {
		if err := g.job.ClearPressure(); err != nil {
			return err
		}
		g.clearP = false
	}
	h := dt / float32(steps)
	for j := 0; j < steps; j++ {
		if err := g.job.Step(n, float32(s.T)+float32(j)*h, h); err != nil {
			return err
		}
	}
	if err := g.job.Run(); err != nil {
		return err
	}
	copy(s.X[:n], d[0:n])
	copy(s.Y[:n], d[c:c+n])
	copy(s.VX[:n], d[2*c:2*c+n])
	copy(s.VY[:n], d[3*c:3*c+n])
	for i := range s.X[:n] {
		if math.IsNaN(float64(s.X[i])) || math.IsNaN(float64(s.Y[i])) {
			s.X[i], s.Y[i] = -1e9, -1e9 // (gone)
		}
	}
	// the drops gone out of the frame
	mg := 4 * s.CS
	s.drop(func(q int) bool {
		return s.X[q] < -mg || s.X[q] > s.W+mg || s.Y[q] > s.H+mg || s.Y[q] < -s.H
	})
	for j := 0; j < steps; j++ {
		s.T += float64(h)
	}
	return nil
}
