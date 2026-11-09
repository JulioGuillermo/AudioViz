package effects

import (
	"math"

	"audioviz/internal/gpu"
)

// GasGPU: the gas simulated on the GPU (gpu.GasJob, gas.comp): the same steps as GasSim, its
// fields kept on the GPU between frames
type GasGPU struct {
	job *gpu.GasJob
	GW  int
	GH  int
	CS  float32
	T   float64
}

// NewGasGPU: an empty gas over a W×H frame, cells of about cs px, on GPU deviceIndex
func NewGasGPU(deviceIndex, W, H int, cs float64) (*GasGPU, error) {
	gw, gh, c := GasGrid(W, H, cs)
	job, err := gpu.NewGasJob(deviceIndex, gw, gh)
	if err != nil {
		return nil, err
	}
	return &GasGPU{job: job, GW: gw, GH: gh, CS: c}, nil
}

func (g *GasGPU) Close() { g.job.Close() }

// gasGPUState: gas.comp's fields of the state, in the order of GasSim.fields (VX VY D Heat Fuel
// Fire R G B C1x C1y C2x C2y)
var gasGPUState = []int{0, 1, 3, 7, 8, 9, 4, 5, 6, 10, 11, 12, 13}

// setCfg: the settings words of the gas.comp's Cfg (the physics; the light in Draw)
func (g *GasGPU) setCfg(ph *GasPhys) {
	c := g.job.Cfg()
	ib := func(v int32) float32 { return math.Float32frombits(uint32(v)) }
	tw := (g.GW + gpu.GasTile - 1) / gpu.GasTile
	copy(c[0:24], []float32{
		ib(int32(g.GW)), ib(int32(g.GH)), ib(int32(ph.Seed)), ib(int32(tw)),
		g.CS, ph.Density, ph.Heat, ph.Fuel,
		ph.Push, ph.Buoyancy, ph.Weight, ph.Swirl,
		ph.WindX, ph.WindY, ph.Turb, ph.TurbScale,
		ph.Spread, ph.Soot, 0, 0,
		ph.SootCol[0], ph.SootCol[1], ph.SootCol[2], 0,
	})
}

func (g *GasGPU) Advance(srcs [][]GasSource, ph *GasPhys, dt float32) error {
	g.setCfg(ph)
	g.job.Begin()
	buf := make([]float32, 0, 1024)
	for _, src := range srcs {
		buf = buf[:0]
		for _, p := range src {
			if p.A <= 0 || p.R <= 0 {
				continue
			}
			buf = append(buf, p.X, p.Y, p.R, p.A, p.VX, p.VY, p.Col[0], p.Col[1], p.Col[2], 0)
		}
		if len(buf) > gpu.GasMaxSrc*gpu.GasSrcFloats {
			buf = buf[:gpu.GasMaxSrc*gpu.GasSrcFloats]
		}
		wk, diff, fade, cool, warm, burn := gasStepFactors(ph, g.CS, dt)
		k1, k2 := gasRestarts(g.T, dt)
		st := gpu.GasStep{T: float32(g.T), Dt: dt, Wk: wk, Diff: diff, Fade: fade, Cool: cool, Warm: warm, Burn: burn}
		if k1 {
			st.Restart |= 1
		}
		if k2 {
			st.Restart |= 2
		}
		ok, err := g.job.Step(buf, st)
		if err != nil {
			return err
		}
		if !ok {
			// no room left for this step's sources: run what is recorded first
			if err := g.job.Run(); err != nil {
				return err
			}
			if ok, err = g.job.Step(buf, st); err != nil {
				return err
			}
		}
		g.T += float64(dt)
	}
	return g.job.Run()
}

func (g *GasGPU) Draw(l GasLight) (*GasFrame, error) {
	c := g.job.Cfg()
	dx, dy := float32(math.Sin(float64(l.Angle))), -float32(math.Cos(float64(l.Angle)))
	ib := func(v int32) float32 { return math.Float32frombits(uint32(v)) }
	rad, fr := gasBlurRadii(l, g.CS)
	copy(c[24:36], []float32{
		dx, dy, l.Opacity * l.Shadow, max(l.Reach, g.CS) / 24,
		l.Depth, l.Opacity, l.HeatK, 0,
		ib(int32(rad)), ib(int32(fr)), 0, 0,
	})
	if err := g.job.Light(rad, fr); err != nil {
		return nil, err
	}
	out := &GasFrame{GW: g.GW, GH: g.GH, CS: g.CS, W1: gasW1(g.T), Cells: make([]uint32, g.GW*g.GH*GasCellWords)}
	copy(out.Cells, g.job.Out())
	return out, nil
}

func (g *GasGPU) Checkpoint() ([]byte, error) {
	if err := g.job.Quantize(); err != nil {
		return nil, err
	}
	// (the mapped fields are slow to read one by one: copied at once first)
	n := g.GW * g.GH
	buf := make([]float32, gpu.GasStateCount*n)
	copy(buf, g.job.Fields()[:gpu.GasStateCount*n])
	var fs [][]float32
	for _, k := range gasGPUState {
		fs = append(fs, buf[k*n:(k+1)*n])
	}
	return encodeGas(g.T, fs, false), nil
}

func (g *GasGPU) Restore(b []byte) bool {
	n := g.GW * g.GH
	buf := make([]float32, gpu.GasStateCount*n) // (the pressure: none)
	var fs [][]float32
	for _, k := range gasGPUState {
		fs = append(fs, buf[k*n:(k+1)*n])
	}
	t, ok := decodeGas(b, fs)
	if !ok {
		return false
	}
	g.T = t
	copy(g.job.Fields(), buf)
	return true
}
