package effects

import (
	"image"
	"math"
	"sync"
	"sync/atomic"
)

// Particle emitters (config.EmitterConfig). Stateless: a particle's state is a closed formula of
// its birth (from the table of births: time, origin, the music's push) and its age, so any frame
// is computed directly (seeking costs nothing, the result is always the same). Each particle is
// splatted into a frame-sized accumulation of fixed-point sums (premultiplied color and alpha):
// integer sums do not depend on the order, so the GPU (emit.comp, one thread per particle, atomic
// adds) and the CPU (DrawEmitter) give the same image, which is then composited as a clip.

// Emitter shapes
const (
	EmitPoint = iota
	EmitCircle
	EmitRing
	EmitLine
	EmitBox
)

// EmitFixed: fixed-point scale of the accumulation (1.0 = EmitFixed)
const EmitFixed = 1024

// Birth is one entry of the table of births: at time T, N particles had been born in all (it
// grows with the rate; two entries at the same time = a burst), the emitter was at X, Y (px)
// turned Rot (rad), and the music pushed the speed and the size by Speed, Size (factors).
type Birth struct {
	T, N, X, Y, Rot, Speed, Size float64
}

// EmitParams: an emitter's settings now, in px of this frame
type EmitParams struct {
	Shape                  int
	W, H                   float64
	Dir, Spread            float64 // rad (0 = up)
	Speed, SpeedVar        float64
	Gravity, Wind, Drag    float64
	Turb, TurbFreq         float64
	Life, LifeVar          float64
	Size, SizeEnd, SizeVar float64
	Col                    [3][4]float64 // birth, middle, end (straight rgb, alpha)
	FadeIn, FadeOut        float64
	Soft                   bool // "soft" mode (matter) instead of "add" (light)
	Streak, Softness       float64
	Seed                   uint32
	Div                    int // accumulation cells of Div×Div pixels (big soft particles: 2 or 4)
}

// EmitFrame is one emitter on screen at time T: its settings, the births that can still be
// alive and the particles they make, [I0, I1)
type EmitFrame struct {
	P      EmitParams
	Births []Birth
	I0, I1 int
	T      float64
}

// pcg hash (same as emit.comp)
func pcg(v uint32) uint32 {
	s := v*747796405 + 2891336453
	w := ((s >> ((s >> 28) + 4)) ^ s) * 277803737
	return (w >> 22) ^ w
}

// rnd: the j-th random number (0..1) of particle i
func rnd(seed uint32, i int, j uint32) float32 {
	return float32(pcg(seed^pcg(uint32(i)*16+j))) * (1.0 / 4294967296.0)
}

// Particle is the state of one particle now
type Particle struct {
	X, Y, VX, VY float32
	Size         float32 // diameter, px
	R, G, B, A   float32 // straight color and alpha
}

// birthOf: the moment particle i is born (it is born when the count passes i + 0.5); ok = false
// if it is out of the table
func (e *EmitFrame) birthOf(i int) (Birth, bool) {
	b := e.Births
	n := len(b)
	if n == 0 {
		return Birth{}, false
	}
	x := float32(i) + 0.5
	if x <= float32(b[0].N) || x > float32(b[n-1].N) {
		return Birth{}, false
	}
	// first entry with N >= x
	lo, hi := 0, n-1
	for lo < hi {
		m := (lo + hi) / 2
		if float32(b[m].N) >= x {
			hi = m
		} else {
			lo = m + 1
		}
	}
	a, c := b[lo-1], b[lo]
	f := float32(0)
	if d := float32(c.N) - float32(a.N); d > 0 {
		f = (x - float32(a.N)) / d
	}
	l := func(p, q float64) float64 { return float64(float32(p) + (float32(q)-float32(p))*f) }
	return Birth{l(a.T, c.T), x64(x), l(a.X, c.X), l(a.Y, c.Y), l(a.Rot, c.Rot), l(a.Speed, c.Speed), l(a.Size, c.Size)}, true
}

func x64(v float32) float64 { return float64(v) }

// At is particle i now (ok = false if it is not alive). float32 all along, as on the GPU.
func (e *EmitFrame) At(i int) (Particle, bool) {
	p := &e.P
	bt, ok := e.birthOf(i)
	if !ok {
		return Particle{}, false
	}
	r := func(j uint32) float32 { return rnd(p.Seed, i, j) }
	life := float32(p.Life) * (1 + float32(p.LifeVar)*(2*r(0)-1))
	if life < 0.05 {
		life = 0.05
	}
	age := float32(e.T) - float32(bt.T)
	if age < 0 || age >= life {
		return Particle{}, false
	}
	u := age / life
	rc, rs := float32(math.Cos(bt.Rot)), float32(math.Sin(bt.Rot))
	// where in the shape (emitter space, turned with it)
	var ox, oy float32
	w, h := float32(p.W), float32(p.H)
	switch p.Shape {
	case EmitCircle, EmitRing:
		rad := w * 0.5
		if p.Shape == EmitCircle {
			rad *= float32(math.Sqrt(float64(r(1))))
		}
		a := 2 * math.Pi * r(2)
		ox, oy = rad*float32(math.Cos(float64(a))), rad*float32(math.Sin(float64(a)))
	case EmitLine:
		ox = (r(1) - 0.5) * w
	case EmitBox:
		ox, oy = (r(1)-0.5)*w, (r(2)-0.5)*h
	}
	x0 := float32(bt.X) + ox*rc - oy*rs
	y0 := float32(bt.Y) + ox*rs + oy*rc
	// launch: direction in the cone (0 = up, clockwise), speed
	a := float32(p.Dir) + float32(bt.Rot) + (r(3)-0.5)*float32(p.Spread)
	sp := float32(p.Speed) * (1 + float32(p.SpeedVar)*(2*r(4)-1)) * float32(bt.Speed)
	vx0, vy0 := sp*float32(math.Sin(float64(a))), -sp*float32(math.Cos(float64(a)))
	ax, ay := float32(p.Wind), float32(p.Gravity)
	k := float32(p.Drag)
	var dx, dy, vx, vy float32
	if k > 1e-3 {
		ek := float32(math.Exp(float64(-k * age)))
		g := (1 - ek) / k
		dx = vx0*g + ax*(age-g)/k
		dy = vy0*g + ay*(age-g)/k
		vx, vy = vx0*ek+ax*g, vy0*ek+ay*g
	} else {
		dx, dy = vx0*age+0.5*ax*age*age, vy0*age+0.5*ay*age*age
		vx, vy = vx0+ax*age, vy0+ay*age
	}
	// wandering: two waves per axis with random phases, zero at birth
	if p.Turb > 0 {
		wv := 2 * math.Pi * float32(p.TurbFreq)
		wave := func(j uint32) float32 {
			p1, p2 := 2*math.Pi*r(j), 2*math.Pi*r(j+1)
			s := float32(math.Sin(float64(wv*age+p1))) - float32(math.Sin(float64(p1)))
			s += 0.5 * (float32(math.Sin(float64(2.13*wv*age+p2))) - float32(math.Sin(float64(p2))))
			return s / 1.5
		}
		dx += float32(p.Turb) * wave(5)
		dy += float32(p.Turb) * wave(7)
	}
	size := (float32(p.Size) + (float32(p.SizeEnd)-float32(p.Size))*u) * (1 + float32(p.SizeVar)*(2*r(9)-1)) * float32(bt.Size)
	// color over the life, faded in and out
	var c [4]float32
	c0, c1 := p.Col[0], p.Col[1]
	f := u * 2
	if u >= 0.5 {
		c0, c1, f = p.Col[1], p.Col[2], (u-0.5)*2
	}
	for j := range c {
		c[j] = float32(c0[j]) + (float32(c1[j])-float32(c0[j]))*f
	}
	if p.FadeIn > 0 {
		c[3] *= min(1, u/float32(p.FadeIn))
	}
	if p.FadeOut > 0 {
		c[3] *= min(1, (1-u)/float32(p.FadeOut))
	}
	return Particle{x0 + dx, y0 + dy, vx, vy, max(size, 0), c[0], c[1], c[2], c[3]}, true
}

// footprint: the box of pixels a particle touches, and its radius and streak
func (pt *Particle) footprint(streak float32) (r, sx, sy float32, x0, y0, x1, y1 int) {
	r = pt.Size * 0.5
	if r < 0.7 {
		r = 0.7
	}
	sx, sy = pt.VX*streak, pt.VY*streak
	x0 = int(math.Floor(float64(min(pt.X, pt.X-sx) - r - 1)))
	x1 = int(math.Ceil(float64(max(pt.X, pt.X-sx) + r + 1)))
	y0 = int(math.Floor(float64(min(pt.Y, pt.Y-sy) - r - 1)))
	y1 = int(math.Ceil(float64(max(pt.Y, pt.Y-sy) + r + 1)))
	return
}

// weight of a particle at a pixel center (px, py): its soft disc, or a soft stroke when streaked
func (pt *Particle) weight(px, py, r, sx, sy, soft float32) float32 {
	dx, dy := px-pt.X, py-pt.Y
	if sx != 0 || sy != 0 {
		// distance to the segment from the particle back along its motion
		l2 := sx*sx + sy*sy
		t := -(dx*sx + dy*sy) / l2
		t = max(0, min(1, t))
		dx, dy = dx+sx*t, dy+sy*t
	}
	d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	e := max(r*soft, 0.5)
	return 1 - smooth32(r-e, r, d)
}

func smooth32(a, b, x float32) float32 {
	t := max(0, min(1, (x-a)/(b-a)))
	return t * t * (3 - 2*t)
}

// EmitScale: tiny particles (radius under 0.7 px) are drawn at 0.7 px with less alpha
func emitTiny(size float32) float32 {
	r := size * 0.5
	if r >= 0.7 {
		return 1
	}
	return (r * r) / (0.7 * 0.7)
}

// emitGrid: the accumulation's size for a W×H frame (one cell per Div×Div pixels)
func emitGrid(W, H, div int) (int, int) { return (W + div - 1) / div, (H + div - 1) / div }

// DrawEmitter makes the emitter's image (W×H, premultiplied RGBA) on the CPU, exactly as the GPU
func DrawEmitter(e *EmitFrame, W, H int) *image.RGBA {
	div := max(e.P.Div, 1)
	GW, GH := emitGrid(W, H, div)
	acc := make([]uint32, GW*GH*4)
	n := e.I1 - e.I0
	if n > 0 {
		nw := min(16, max(1, n/256))
		var wg sync.WaitGroup
		for k := 0; k < nw; k++ {
			wg.Add(1)
			go func(k int) {
				defer wg.Done()
				for i := e.I0 + k; i < e.I1; i += nw {
					pt, ok := e.At(i)
					if !ok || pt.A <= 0 {
						continue
					}
					splat(&pt, e.P.Streak, e.P.Softness, acc, GW, GH, div)
				}
			}(k)
		}
		wg.Wait()
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				s := emitSum(acc, GW, GH, div, x, y)
				c := resolveEmit(s, e.P.Soft)
				copy(img.Pix[(y*W+x)*4:], c[:])
			}
		}
	})
	return img
}

// emitSum: the sums at pixel (x, y), bilinear between the cells (as resolveOne in emit.comp)
func emitSum(acc []uint32, GW, GH, div, x, y int) [4]float32 {
	cell := func(cx, cy, j int) float32 {
		cx, cy = max(0, min(GW-1, cx)), max(0, min(GH-1, cy))
		return float32(acc[(cy*GW+cx)*4+j]) / EmitFixed
	}
	if div == 1 {
		return [4]float32{cell(x, y, 0), cell(x, y, 1), cell(x, y, 2), cell(x, y, 3)}
	}
	qx := (float32(x)+0.5)/float32(div) - 0.5
	qy := (float32(y)+0.5)/float32(div) - 0.5
	ix, iy := int(math.Floor(float64(qx))), int(math.Floor(float64(qy)))
	fx, fy := qx-float32(ix), qy-float32(iy)
	var o [4]float32
	for j := range o {
		a := cell(ix, iy, j) + (cell(ix+1, iy, j)-cell(ix, iy, j))*fx
		b := cell(ix, iy+1, j) + (cell(ix+1, iy+1, j)-cell(ix, iy+1, j))*fx
		o[j] = a + (b-a)*fy
	}
	return o
}

func splat(pt *Particle, streak, softness float64, acc []uint32, GW, GH, div int) {
	r, sx, sy, x0, y0, x1, y1 := pt.footprint(float32(streak))
	x0, y0 = max(x0/div, 0), max(y0/div, 0)
	x1, y1 = min(x1/div+1, GW-1), min(y1/div+1, GH-1)
	a := pt.A * emitTiny(pt.Size)
	d := float32(div)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			w := pt.weight((float32(x)+0.5)*d, (float32(y)+0.5)*d, r, sx, sy, float32(softness)) * a
			if w <= 0 {
				continue
			}
			o := (y*GW + x) * 4
			add := func(j int, v float32) {
				if q := uint32(v*EmitFixed + 0.5); q > 0 {
					atomic.AddUint32(&acc[o+j], q)
				}
			}
			add(0, pt.R*w)
			add(1, pt.G*w)
			add(2, pt.B*w)
			add(3, w)
		}
	}
}

// resolveEmit turns the sums of a pixel into premultiplied RGBA8 (same as emit.comp): "add" keeps
// the light (alpha = its strength), "soft" is the mean color with alpha 1 − e^(−coverage)
func resolveEmit(s [4]float32, soft bool) [4]uint8 {
	R, G, B, A := s[0], s[1], s[2], s[3]
	var o [4]float32
	if soft {
		if A > 0 {
			al := 1 - float32(math.Exp(float64(-A)))
			o = [4]float32{R / A * al, G / A * al, B / A * al, al}
		}
	} else {
		al := min(1, max(A, max(R, max(G, B))))
		o = [4]float32{min(R, al), min(G, al), min(B, al), al}
	}
	var c [4]uint8
	for j := range c {
		c[j] = uint8(max(0, min(1, o[j]))*255 + 0.5)
	}
	return c
}
