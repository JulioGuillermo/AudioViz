package effects

import (
	"math"

	"audioviz/internal/expr"
)

// A text's color worked out per letter or per pixel (an expression, specialized for the frame):
// what is left reads the letter (index, count, rand()), where the pixel is (text_x …), the color
// it would have (value, value.r …) and its path's values where it is. The same program runs in
// render.comp (per pixel) and here (per letter; per pixel on the CPU).

// Each: how a text's color expression is worked out
const (
	EachText   = "text"   // once for all of it
	EachLetter = "letter" // per letter
	EachPixel  = "pixel"  // per pixel
)

// PixExpr: an expression ready to run per letter / pixel
type PixExpr struct {
	Prog *expr.Program
	// its path's values (one per Prog.Keys: Path), the path sampled for its own (x, y …)
	Keys   []EmitExprKey
	Pts    []float32
	L      float32
	Closed bool
	Seed   uint32
	Frame  int
	W, H   int
	// worked out per letter: read where the letter's middle is (else per pixel)
	Letter bool
}

// PixIn: where it is worked out: the letter (index, how many), how far along the path (px), in
// the text's box, in the letter, on the screen (0…1)
type PixIn struct {
	Index, Count           int
	S                      float32
	TX, TY, LX, LY, SX, SY float64
}

// PixKeep: the variables a PixExpr keeps (read per letter / pixel)
func PixKeep(v int) bool {
	return v == expr.VarIndex || v == expr.VarCount || v == expr.VarValue || v == expr.VarChannel ||
		(v >= expr.VarValueR && v <= expr.VarScreenY)
}

// Color: the color at in, from the color it would have there (base), channel by channel (0…1)
func (x *PixExpr) Color(in PixIn, base [4]float64) [4]float64 {
	var out [4]float64
	env := pixEnv{x: x, in: &in, base: &base}
	for ch := 0; ch < 4; ch++ {
		env.ch = ch
		r := x.Prog.Run(&env)
		if !r.Nil {
			out[ch] = math.Max(0, math.Min(1, r.V))
		}
	}
	return out
}

// Scalar: a number at in, from the one it would have there (v; nil gives 0; not clamped)
func (x *PixExpr) Scalar(in PixIn, v float64) float64 {
	base := [4]float64{v, v, v, 1}
	r := x.Prog.Run(&pixEnv{x: x, in: &in, base: &base})
	if r.Nil {
		return 0
	}
	return r.V
}

type pixEnv struct {
	x    *PixExpr
	in   *PixIn
	base *[4]float64
	ch   int
}

func (e *pixEnv) Var(v int) float64 {
	in := e.in
	switch v {
	case expr.VarIndex:
		return float64(in.Index)
	case expr.VarCount:
		return float64(max(in.Count, 1))
	case expr.VarValue:
		return e.base[e.ch]
	case expr.VarChannel:
		return float64(e.ch)
	case expr.VarValueR, expr.VarValueG, expr.VarValueB, expr.VarValueA:
		return e.base[v-expr.VarValueR]
	case expr.VarTextX:
		return in.TX
	case expr.VarTextY:
		return in.TY
	case expr.VarLetterX:
		return in.LX
	case expr.VarLetterY:
		return in.LY
	case expr.VarScreenX:
		return in.SX
	case expr.VarScreenY:
		return in.SY
	}
	return 0
}
func (e *pixEnv) Band(int, float64) float64 { return 0 }
func (e *pixEnv) Lag(int, float64) float64  { return 0 }
func (e *pixEnv) Peak(int, float64) float64 { return 0 }
func (e *pixEnv) Key(kind int, name string) (float64, bool) {
	x := e.x
	if kind != 0 {
		return 0, false
	}
	for k, n := range x.Prog.Keys {
		if n != name || k >= len(x.Keys) {
			continue
		}
		if b := x.Keys[k].Builtin; b > 0 && len(x.Pts) >= 6 {
			return PathBuiltinAt(b, x.Pts, x.L, x.Closed, e.in.S, 0, 0, x.W, x.H), true
		}
		if t := x.Keys[k].Path; t != nil {
			return float64(t.At(e.in.S)), true
		}
	}
	return 0, false
}
func (e *pixEnv) Rand(perFrame bool) float64 {
	return expr.ItemRand(e.x.Seed, e.in.Index, perFrame, e.x.Frame)
}

// PixExprWords: the words PackPixExpr writes for x
func PixExprWords(x *PixExpr) int {
	n := 8 + len(x.Prog.Code) + len(x.Prog.Consts) + 2*len(x.Keys)
	for _, k := range x.Keys {
		if k.Builtin == 0 && k.Path != nil {
			n += 1 + 3*len(k.Path.S)
		}
	}
	if x.uses() {
		n += len(x.Pts)
	}
	return n
}

// uses: whether it reads its path's own values (its samples are packed then)
func (x *PixExpr) uses() bool {
	for _, k := range x.Keys {
		if k.Builtin > 0 {
			return len(x.Pts) >= 6
		}
	}
	return false
}

// PackPixExpr writes x at word off of buf (as render.comp's textExpr reads it): code, its length,
// consts, the key table (2 words per key: -, path: 0 none, a track's offset + 1, or 0xFFFFFFF0 +
// built-in), the path's samples, how many, its length, closed; then all of them
func PackPixExpr(put func(i int, u uint32), off int, x *PixExpr) {
	putF := func(i int, f float32) { put(i, math.Float32bits(f)) }
	d := off + 8
	put(off, uint32(d))
	put(off+1, uint32(len(x.Prog.Code)))
	for _, c := range x.Prog.Code {
		put(d, uint32(c))
		d++
	}
	put(off+2, uint32(d))
	for _, c := range x.Prog.Consts {
		putF(d, float32(c))
		d++
	}
	put(off+3, uint32(d))
	tab := d
	d += 2 * len(x.Keys)
	for k, ky := range x.Keys {
		put(tab+2*k, 0)
		put(tab+2*k+1, 0)
		if ky.Builtin > 0 {
			put(tab+2*k+1, 0xFFFFFFF0+uint32(ky.Builtin))
			continue
		}
		t := ky.Path
		if t == nil {
			continue
		}
		put(tab+2*k+1, uint32(d+1))
		n := len(t.S)
		putF(d, float32(n))
		for m := 0; m < n; m++ {
			putF(d+1+m, t.S[m])
			putF(d+1+n+m, t.V[m])
			putF(d+1+2*n+m, float32(t.E[m]))
		}
		d += 1 + 3*n
	}
	put(off+4, 0)
	put(off+5, 0)
	if x.uses() {
		put(off+4, uint32(d))
		put(off+5, uint32(len(x.Pts)/3))
		for _, v := range x.Pts {
			putF(d, v)
			d++
		}
	}
	putF(off+6, x.L)
	putF(off+7, 0)
	if x.Closed {
		putF(off+7, 1)
	}
}
