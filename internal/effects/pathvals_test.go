package effects

import (
	"math"
	"testing"

	"audioviz/internal/config"
)

// A straight path of four nodes, 100 px apart (A, B, C, D)
func straight(vals []config.NodeVals) *config.MaskShape {
	return &config.MaskShape{Points: [][]float64{{0, 0, 1}, {0.1, 0, 1}, {0.2, 0, 1}, {0.3, 0, 1}}, Open: true, Vals: vals}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

func TestTrackOnlyDefiningNodes(t *testing.T) {
	// A = 1, C = 2: B takes no part, D holds C's value
	sh := straight([]config.NodeVals{{Keys: map[string]config.NodeKey{"x": {V: 1}}}, {}, {Keys: map[string]config.NodeKey{"x": {V: 2}}}, {}})
	tr := Track(sh, "x", 1000, 1000)
	if tr == nil {
		t.Fatal("no track")
	}
	for _, c := range []struct{ s, v float32 }{{0, 1}, {100, 1.5}, {150, 1.75}, {200, 2}, {300, 2}} {
		if got := tr.At(c.s); !near(got, c.v) {
			t.Errorf("x at %v = %v, want %v", c.s, got, c.v)
		}
	}
}

func TestTrackFromZero(t *testing.T) {
	// only B defines it: 0 at the start up to B
	sh := straight([]config.NodeVals{{}, {Keys: map[string]config.NodeKey{"x": {V: 4}}}})
	tr := Track(sh, "x", 1000, 1000)
	if got := tr.At(50); !near(got, 2) {
		t.Errorf("x at 50 = %v, want 2", got)
	}
	if got := tr.At(300); !near(got, 4) {
		t.Errorf("x at 300 = %v, want 4", got)
	}
	if Track(sh, "y", 1000, 1000) != nil {
		t.Error("a value no node defines has a track")
	}
}

func TestTrackDepthEase(t *testing.T) {
	z0, z1 := 0.2, 0.8
	sh := straight([]config.NodeVals{{Z: &z0, ZEase: "ease_in"}, {}, {}, {Z: &z1}})
	tr := Track(sh, "z", 1000, 1000)
	want := float32(0.2 + 0.6*config.Ease("ease_in", 0.5))
	if got := tr.At(150); !near(got, want) {
		t.Errorf("z at 150 = %v, want %v", got, want)
	}
}
