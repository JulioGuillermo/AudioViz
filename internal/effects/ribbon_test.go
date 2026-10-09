package effects

import (
	"math"
	"testing"

	"audioviz/internal/config"
)

func TestRibbonStraight(t *testing.T) {
	// going right: facing, no fold: each height is seen where it is
	sh := &config.MaskShape{Points: [][]float64{{0.1, 0.5, 1}, {0.9, 0.5, 1}}, Open: true}
	pts, L := SamplePath(sh, 1000, 1000)
	r := NewRibbon(pts, L, false)
	for _, ly := range []float32{-8, -3, 0, 4, 9} {
		v, ok := r.V(400, ly)
		if !ok || math.Abs(float64(v-ly)) > 0.05 {
			t.Errorf("ly %v: v %v ok %v", ly, v, ok)
		}
	}
	k, o := r.At(400)
	t.Logf("k %v o %v", k, o)
}

func TestRibbonBack(t *testing.T) {
	// a U-turn: well past it (going left) every height is seen, mirrored, around the path
	sh := &config.MaskShape{Points: [][]float64{{0.1, 0.3, 1}, {0.8, 0.3}, {0.9, 0.4}, {0.8, 0.5}, {0.1, 0.5, 1}}, Open: true}
	pts, L := SamplePath(sh, 1000, 1000)
	r := NewRibbon(pts, L, false)
	sp := L - 200
	k, o := r.At(sp)
	for _, ly := range []float32{-8, 0, 8} {
		v, ok := r.V(sp, ly)
		if !ok || math.Abs(float64(v+ly)) > 0.5 {
			t.Errorf("ly %v: v %v ok %v (k %v o %v)", ly, v, ok, k, o)
		}
	}
	// in the twist the text is still there (folded), not gone
	seen := 0
	for s := float32(0); s < L; s += 10 {
		if _, ok := r.V(s, 0); ok {
			seen++
		}
	}
	t.Logf("k %v o %v; seen at %d of %d points", k, o, seen, int(L/10))
}
