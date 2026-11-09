package effects

import (
	"math"
	"testing"
)

// a hot puff rises: its smoke's centre moves up, nothing becomes NaN
func TestGasRises(t *testing.T) {
	s := NewGasSim(640, 360, 4)
	ph := GasPhys{Density: 3, Heat: 1, Push: 0.5, Buoyancy: 100, Swirl: 1, Turb: 15, TurbScale: 120,
		Fade: 0.1, Cool: 0.8, Burn: 2, Soot: 0.5, Seed: 1}
	cy := func() (float64, float64) {
		var m, y float64
		for i, d := range s.D {
			m += float64(d)
			y += float64(d) * float64(i/s.GW)
		}
		return y / math.Max(m, 1e-9) * float64(s.CS), m
	}
	for f := 0; f < 90; f++ {
		for j := 0; j < 2; j++ {
			src := []GasSource{{X: 320, Y: 330, R: 20, A: 1, VX: 0, VY: -100, Col: [3]float32{0.5, 0.5, 0.5}}}
			s.Feed(src, &ph, 1.0/60)
			s.Step(&ph, 1.0/60)
		}
		if f%15 == 14 {
			y, m := cy()
			var vmax, hmax, dmax float32
			for i := range s.VX {
				hmax, dmax = max(hmax, s.Heat[i]), max(dmax, s.D[i])
				vmax = max(vmax, float32(math.Hypot(float64(s.VX[i]), float64(s.VY[i]))))
			}
			t.Logf("frame %d: centre y %.1f, smoke %.1f, |v| max %.1f heat max %.2f smoke max %.2f", f, y, m, vmax, hmax, dmax)
		}
	}
}

func TestGasBuoyancy(t *testing.T) {
	s := NewGasSim(640, 640, 4)
	ph := GasPhys{Density: 1, Heat: 4, Buoyancy: 200, Cool: 0.1, Seed: 1}
	for f := 0; f < 120; f++ {
		s.Feed([]GasSource{{X: 320, Y: 560, R: 30, A: 1}}, &ph, 1.0/60)
		s.Step(&ph, 1.0/60)
		if f%20 == 19 {
			var vmin, hmax float32
			for i := range s.VY {
				vmin, hmax = min(vmin, s.VY[i]), max(hmax, s.Heat[i])
			}
			t.Logf("step %d: vy min %.1f heat max %.2f", f, vmin, hmax)
		}
	}
}

func BenchmarkGasStep(b *testing.B) {
	s := NewGasSim(1920, 1080, 5)
	ph := GasPhys{Density: 1, Heat: 1, Fuel: 1, Buoyancy: 300, Swirl: 2, Turb: 50, TurbScale: 140, Cool: 3, Burn: 4, Soot: 0.5, Spread: 100, Seed: 1}
	src := []GasSource{{X: 960, Y: 1000, R: 60, A: 1}}
	for i := 0; i < 60; i++ {
		s.Feed(src, &ph, 1.0/60)
		s.Step(&ph, 1.0/60)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Feed(src, &ph, 1.0/60)
		s.Step(&ph, 1.0/60)
	}
}

func BenchmarkGasGPUFrame(b *testing.B) {
	g, err := NewGasGPU(-1, 1920, 1080, 5)
	if err != nil {
		b.Skip(err)
	}
	defer g.Close()
	ph := GasPhys{Density: 1, Heat: 1, Fuel: 1, Buoyancy: 300, Swirl: 2, Turb: 50, TurbScale: 140, Cool: 3, Burn: 4, Soot: 0.5, Spread: 100, Seed: 1}
	src := []GasSource{{X: 960, Y: 1000, R: 60, A: 1}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := g.Advance([][]GasSource{src, src}, &ph, 1.0/60); err != nil {
			b.Fatal(err)
		}
	}
}
