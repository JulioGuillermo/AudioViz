package effects

import (
	"math"
	"testing"

	"audioviz/internal/config"
)

func TestPathAt(t *testing.T) {
	// an L: right 1000 px, then down 500 px (corners): half the length is at x = 750
	sh := &config.MaskShape{Points: [][]float64{{0, 0, 1}, {1, 0, 1}, {1, 1, 1}}, Open: true}
	x, y, a := PathAt(sh, 1000, 500, 0.5)
	if math.Abs(x-750) > 1 || math.Abs(y) > 1 || math.Abs(a) > 1e-6 {
		t.Fatalf("0.5: %v %v %v", x, y, a)
	}
	x, y, a = PathAt(sh, 1000, 500, 0.9)
	if math.Abs(x-1000) > 1 || math.Abs(y-350) > 1 || math.Abs(a-math.Pi/2) > 1e-6 {
		t.Fatalf("0.9: %v %v %v", x, y, a)
	}
	if x, _, _ = PathAt(sh, 1000, 500, 2); math.Abs(x-1000) > 1e-6 {
		t.Fatalf("an open path stops at its end: %v", x)
	}
	// closed: wraps around
	sh.Open = false
	x1, y1, _ := PathAt(sh, 1000, 500, 0.25)
	x2, y2, _ := PathAt(sh, 1000, 500, 1.25)
	if math.Abs(x1-x2) > 1e-9 || math.Abs(y1-y2) > 1e-9 {
		t.Fatalf("closed path does not loop")
	}
}
