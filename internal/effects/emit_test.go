package effects

import (
	"bytes"
	"testing"
)

// The emitter's image does not depend on the order the particles are added (integer sums)
func TestEmitterDeterministic(t *testing.T) {
	e := &EmitFrame{T: 2, I0: 0, I1: 400,
		P: EmitParams{Shape: EmitCircle, W: 40, Dir: 0, Spread: 1, Speed: 200, SpeedVar: 0.4, Gravity: -50, Drag: 0.5,
			Turb: 10, TurbFreq: 1, Life: 2, LifeVar: 0.3, Size: 20, SizeEnd: 5, SizeVar: 0.3, Softness: 0.8,
			Col: [3][4]float64{{1, 1, 0.5, 1}, {1, 0.5, 0, 0.8}, {0.5, 0, 0, 0}}, FadeOut: 0.5, Seed: 7, Div: 1}}
	e.Births = []Birth{{T: 0, N: 0, X: 160, Y: 200, Speed: 1, Size: 1}, {T: 2, N: 400, X: 160, Y: 200, Speed: 1, Size: 1}}
	a := DrawEmitter(e, 320, 240)
	for k := 0; k < 3; k++ {
		if b := DrawEmitter(e, 320, 240); !bytes.Equal(a.Pix, b.Pix) {
			t.Fatal("two draws differ")
		}
	}
	lit := 0
	for i := 3; i < len(a.Pix); i += 4 {
		if a.Pix[i] > 0 {
			lit++
		}
	}
	if lit < 500 {
		t.Fatalf("too few pixels drawn: %d", lit)
	}
	// a particle born after now, or dead, is not there
	if _, ok := e.At(399); !ok {
		t.Fatal("the last particle should be alive")
	}
}
