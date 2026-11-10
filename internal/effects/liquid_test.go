package effects

import (
	"testing"
	"time"
)

// water poured onto a floor piles up into a pool: it stays in the frame, above the floor, and
// does not blow up
func TestLiquidPool(t *testing.T) {
	s := NewLiquidSim(640, 360, 6)
	ph := LiquidPhys{GravY: 900, Flip: 0.95, FloorOn: true, Floor: 300}
	// a basin: walls at x 200 and 440
	s.Solid = make([]uint8, s.GW*s.GH)
	for j := 0; j < s.GH; j++ {
		for i := 0; i < s.GW; i++ {
			x := (float32(i) + 0.5) * s.CS
			if (x > 194 && x < 206) || (x > 434 && x < 446) {
				if (float32(j)+0.5)*s.CS > 150 {
					s.Solid[j*s.GW+i] = 1
				}
			}
		}
	}
	t0 := time.Now()
	for f := 0; f < 120; f++ {
		var bs []LiquidBirth
		if f < 60 {
			for k := 0; k < 30; k++ {
				bs = append(bs, LiquidBirth{X: 320 + float32(k%6)*3, Y: 60 + float32(k/6)*3, VY: 300, Size: 10, Col: [4]float32{0.3, 0.6, 1, 1}})
			}
		}
		s.Add(bs, 10000)
		dt := float32(1.0 / 30)
		n := s.Substeps(dt, &ph)
		for i := 0; i < n; i++ {
			s.Step(&ph, dt/float32(n))
		}
		if f%30 == 29 {
			var ymin, ymax, vmax float32 = 1e9, -1e9, 0
			for q := range s.X {
				ymin, ymax = min(ymin, s.Y[q]), max(ymax, s.Y[q])
				vmax = max(vmax, max(abs32(s.VX[q]), abs32(s.VY[q])))
			}
			t.Logf("frame %d: %d drops, y %.0f..%.0f, |v| max %.0f, substeps %d", f, s.Count(), ymin, ymax, vmax, n)
		}
	}
	t.Logf("%.1f ms per frame", float64(time.Since(t0).Milliseconds())/120)
	if s.Count() < 1500 {
		t.Fatalf("liquid lost: %d drops", s.Count())
	}
	for q := range s.Y {
		if s.Y[q] > 300.5 {
			t.Fatalf("a drop below the floor: %v", s.Y[q])
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// a jet onto a floor at 1080p: some thousands of drops, a pool
func BenchmarkLiquidStep(b *testing.B) {
	s := NewLiquidSim(1920, 1080, 6)
	ph := LiquidPhys{GravY: 1500, Flip: 0.95, FloorOn: true, Floor: 1000}
	add := func() {
		var bs []LiquidBirth
		for k := 0; k < 9; k++ {
			bs = append(bs, LiquidBirth{X: 960 + float32(k%3)*4, Y: 300 + float32(k/3)*4, VX: float32(k%3-1) * 80, VY: -600, Size: 30})
		}
		s.Add(bs, 9000)
	}
	for f := 0; f < 300; f++ {
		add()
		s.Step(&ph, 1.0/60)
		s.Step(&ph, 1.0/60)
	}
	b.Logf("%d drops", s.Count())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Step(&ph, 1.0/60)
	}
}
