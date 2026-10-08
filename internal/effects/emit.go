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
	EmitText // on the letters of a fixed text (EmitParams.Pts)
)

// MaxEmitPts: points on a text's letters an emitter is born on; MaxPathSamples: points along the
// motion path its particles travel (both travel to the GPU with the emitter)
const (
	MaxEmitPts     = 4096
	MaxPathSamples = 256
)

// EmitFixed: fixed-point scale of the accumulation (1.0 = EmitFixed)
const EmitFixed = 1024

// Birth is one entry of the table of births: at time T, N particles had been born in all (it
// grows with the rate; two entries at the same time = a burst), the emitter was at X, Y (px)
// turned Rot (rad), and the music pushed the speed and the size by Speed, Size (factors).
type Birth struct {
	T, N, X, Y, Rot, Speed, Size float64
	// the shape's scale (a text's size then) and where on its motion path it was (0..1)
	Scale, PathP float64
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
	// Flame look (config.FlameLook): colors cool, mid, hot; heat; noise (px), its scale (px),
	// speed (px/s) and how much it eats the edges
	Flame                               bool
	FlameCol                            [3][3]float64
	Heat, Noise, NoiseScale, NoiseSpeed float64
	Edge                                float64
	// Glow: strength, reach (px), its own color (GlowOwn) or the particles' (the flame's middle)
	GlowI, GlowR float64
	GlowCol      [3]float64
	GlowOwn      bool
	// Light on what is behind: on, reach (px), color × strength × flicker now, add (or soft)
	LightOn  bool
	LightR   float64
	LightEnd float64 // px where it is gone (fades out from 60 % of it)
	// 2.5D light (see LightField): on, where it is (px), its depth, fade by depth, shading
	LightD3                  bool
	LightCX, LightCY, LightZ float64
	LightDepthK, LightShade  float64
	LightFrom, LightTo       float64 // only lit between these depths (soft by LightSoft)
	LightSoft                float64
	// rim light: on, from (px), soft (px), width (px), gain, edge (depth step)
	LightRim                                                bool
	LightRimFrom, LightRimSoft, LightRimW, LightRimGain, LightRimEdge float64
	LightCol                 [3]float64
	LightAdd                 bool
	// Text shape: points on its letters (x, y pairs, px around its anchor, before its rotation)
	Pts []float32
	// Along the motion path: the path sampled (x, y, length so far: px), its length, closed
	Follow     bool
	Path       []float32
	PathLen    float32
	PathClosed bool
	// A floor (y px) they bounce on: how much speed they keep (bounce) and sideways (friction)
	FloorOn                 bool
	Floor, Bounce, Friction float64
	// Smoke look: soft matter torn into wisps by noise: how much, its scale (px), how fast it
	// rises (px/s), how far it moves the smoke (px)
	Smoke                                     bool
	SmokeWisp, SmokeScale, SmokeSpeed, SmokeWarp float64
}

// pathPoint: the point at length s along a sampled path (x, y, length so far), and its unit
// tangent; past its ends it goes on straight (round, when closed). As in emit.comp.
func pathPoint(pp []float32, L float32, closed bool, s float32) (x, y, tx, ty float32) {
	n := len(pp) / 3
	if closed && L > 0 {
		s -= L * float32(math.Floor(float64(s/L)))
	}
	lo, hi := 1, n-1
	for lo < hi {
		m := (lo + hi) / 2
		if pp[m*3+2] >= s {
			hi = m
		} else {
			lo = m + 1
		}
	}
	a, b := (lo-1)*3, lo*3
	dl := max(pp[b+2]-pp[a+2], 1e-6)
	f := (s - pp[a+2]) / dl
	x, y = pp[a]+(pp[b]-pp[a])*f, pp[a+1]+(pp[b+1]-pp[a+1])*f
	return x, y, (pp[b] - pp[a]) / dl, (pp[b+1] - pp[a+1]) / dl
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
	return Birth{l(a.T, c.T), x64(x), l(a.X, c.X), l(a.Y, c.Y), l(a.Rot, c.Rot), l(a.Speed, c.Speed), l(a.Size, c.Size),
		l(a.Scale, c.Scale), l(a.PathP, c.PathP)}, true
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
	case EmitText:
		// a point on its letters (a pixel aside), at the text's size then
		if n := len(p.Pts) / 2; n > 0 {
			k := min(int(r(1)*float32(n)), n-1)
			sc := float32(bt.Scale)
			ox, oy = (p.Pts[2*k]+(r(2)-0.5)*1.5)*sc, (p.Pts[2*k+1]+(r(10)-0.5)*1.5)*sc
		}
	}
	x0 := float32(bt.X) + ox*rc - oy*rs
	y0 := float32(bt.Y) + ox*rs + oy*rc
	// launch: direction in the cone (0 = up, clockwise; turned with the emitter, not with a
	// text it is born on), speed
	rot := float32(bt.Rot)
	if p.Shape == EmitText {
		rot = 0
	}
	a := float32(p.Dir) + rot + (r(3)-0.5)*float32(p.Spread)
	sp := float32(p.Speed) * (1 + float32(p.SpeedVar)*(2*r(4)-1)) * float32(bt.Speed)
	vx0, vy0 := sp*float32(math.Sin(float64(a))), -sp*float32(math.Cos(float64(a)))
	ax, ay := float32(p.Wind), float32(p.Gravity)
	k := float32(p.Drag)
	// the launch and the forces at time tt: displacement and velocity (closed form, with drag)
	motion := func(tt float32) (dx, dy, vx, vy float32) {
		if k > 1e-3 {
			ek := float32(math.Exp(float64(-k * tt)))
			g := (1 - ek) / k
			return vx0*g + ax*(tt-g)/k, vy0*g + ay*(tt-g)/k, vx0*ek + ax*g, vy0*ek + ay*g
		}
		return vx0*tt + 0.5*ax*tt*tt, vy0*tt + 0.5*ay*tt*tt, vx0 + ax*tt, vy0 + ay*tt
	}
	dx, dy, vx, vy := motion(age)
	if p.Follow && len(p.Path) >= 6 {
		// along the path: as far as its speed takes it, the cross offset of its shape, the forces
		along, sv := sp*age, sp
		if k > 1e-3 {
			ek := float32(math.Exp(float64(-k * age)))
			along, sv = sp*(1-ek)/k, sp*ek
		}
		px, py, tx, ty := pathPoint(p.Path, p.PathLen, p.PathClosed, float32(bt.PathP)*p.PathLen+along)
		lat := ox
		fx, fy, fvx, fvy := float32(0), float32(0), float32(0), float32(0)
		if k > 1e-3 {
			ek := float32(math.Exp(float64(-k * age)))
			g := (1 - ek) / k
			fx, fy, fvx, fvy = ax*(age-g)/k, ay*(age-g)/k, ax*g, ay*g
		} else {
			fx, fy, fvx, fvy = 0.5*ax*age*age, 0.5*ay*age*age, ax*age, ay*age
		}
		x0, y0 = px-ty*lat, py+tx*lat
		dx, dy, vx, vy = fx, fy, tx*sv+fvx, ty*sv+fvy
	} else if p.FloorOn && ay > 0 && y0 < float32(p.Floor) && y0+dy > float32(p.Floor) {
		// it reached the floor: the moment (halving), then bounces losing speed
		fl := float32(p.Floor)
		lo, hi := float32(0), age
		for it := 0; it < 14; it++ {
			m := 0.5 * (lo + hi)
			if _, my, _, _ := motion(m); y0+my > fl {
				hi = m
			} else {
				lo = m
			}
		}
		t1 := hi
		x1, _, vx1, vy1 := motion(t1)
		fr, bo := float32(p.Friction), float32(p.Bounce)
		tau, v := age-t1, bo*max(vy1, 0)
		ry, rvy := float32(0), float32(0) // height above the floor, its speed (up)
		for b := 0; b < 16; b++ {
			flight := 2 * v / ay
			if v < 1 {
				break
			}
			if tau < flight {
				ry, rvy = v*tau-0.5*ay*tau*tau, v-ay*tau
				break
			}
			tau -= flight
			v *= bo
		}
		dx, dy = x1+vx1*fr*(age-t1), fl-y0-ry
		vx, vy = vx1*fr, -rvy
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

// EmitGlowDiv: the glow's accumulation has a cell per EmitGlowDiv×EmitGlowDiv pixels (it is blurry)
const EmitGlowDiv = 8

// EmitAccLayout: the words of one emitter's accumulations for W×H on the GPU (emit.comp): its
// particles (4 per pixel), its glow (4 per glow cell), its light sources and its light field (1
// per light cell); the offsets of the glow, the sources and the field, and the whole stride
func EmitAccLayout(W, H int) (glow, src, field, stride int) {
	gw, gh := emitGrid(W, H, EmitGlowDiv)
	xw, xh := emitGrid(W, H, EmitLightDiv)
	glow = W * H * 4
	src = glow + gw*gh*4
	field = src + xw*xh
	return glow, src, field, field + xw*xh
}

// EmitLightGrid: the size of the light grid for W×H
func EmitLightGrid(W, H int) (int, int) { return emitGrid(W, H, EmitLightDiv) }

// EmitLightDiv: the light is worked out on cells of EmitLightDiv×EmitLightDiv pixels (it is smooth)
const EmitLightDiv = 32

// EmitLightGain: how much particle (sum of alpha) makes how much light: 1 − e^(−sum·gain)
const EmitLightGain = 1.0 / 40

// LightField is an emitter's light over the frame: its strength 0..1 at the centers of the light
// cells, its color (× strength × flicker) and how it lights (Add, or soft)
type LightField struct {
	GW, GH int
	Div    int // its cells' side (px; 0 = EmitLightDiv)
	V      []float32
	Col    [3]float32
	Add    bool
	// 2.5D (a background with depth): where the light is (px) and its depth, how fast it fades
	// with the difference of depth, how much the surfaces are shaded by where they face
	D3            bool
	CX, CY, LZ    float32
	DepthK, Shade float32
	From, To      float32 // lit between these depths, soft edge Soft
	Soft          float32
	// rim light: farther than RimFrom (soft RimSoft) only the edges that face it, RimW px wide
	Rim                                     bool
	RimFrom, RimSoft, RimW, RimGain, RimEdge float32
}

// LightRange: how much a point at depth z is inside the lit range [to, from] (soft edges), as
// lightRange in render.comp
func LightRange(z, from, to, soft float32) float32 {
	e := max(soft, 1e-4)
	return smooth32(to-e, to+e, z) * (1 - smooth32(from-e, from+e, z))
}

// DepthZ: depth units → px, to compare depths with distances on the frame (the scene is about
// this deep)
const DepthPx = 800

// DepthFactor: how much of the light reaches frame pixel (x, y) given the scene's depth there
// (as lightDepth in render.comp): less with the difference of depth, and the surface's side that
// faces the light gets more (its normal from the depth's slope)
func (l *LightField) DepthFactor(s *Scene, x, y int) float32 {
	qx, qy := float32(x), float32(y)
	// the background point seen there; its surroundings around that point
	hx, hy, zp := s.Hit(qx, qy)
	dz := (zp - l.LZ) * l.DepthK
	f := float32(math.Exp(float64(-dz*dz))) * LightRange(zp, l.From, l.To, l.Soft)
	if l.Shade > 0 {
		gx := (s.Near(hx+2, hy) - s.Near(hx-2, hy)) / 4 * DepthPx
		gy := (s.Near(hx, hy+2) - s.Near(hx, hy-2)) / 4 * DepthPx
		nx, ny, nz := -gx, -gy, float32(1)
		nl := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
		lx, ly, lz := l.CX-qx, l.CY-qy, (l.LZ-zp)*DepthPx
		ll := float32(math.Sqrt(float64(lx*lx+ly*ly+lz*lz))) + 1e-3
		lam := max(0, (nx*lx+ny*ly+nz*lz)/(nl*ll))
		f *= 1 - l.Shade + l.Shade*(0.3+1.1*lam)
	}
	if l.Rim {
		// far away: only the edges that face it (the scene drops behind them towards the light)
		dx, dy := l.CX-qx, l.CY-qy
		d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if t := smooth32(l.RimFrom-l.RimSoft, l.RimFrom+l.RimSoft+1e-3, d); t > 0 {
			f = f*(1-t) + t*l.RimGain*l.rimAt(s, hx, hy, zp, dx/max(d, 1e-3), dy/max(d, 1e-3))*LightRange(zp, l.From, l.To, l.Soft)
		}
	}
	return f
}

// rimAt: how much background point h is on an edge facing the light (direction ux, uy): the
// scene farther than it within RimW px towards the light, brighter right at the edge (as in
// lightDepth in render.comp)
func (l *LightField) rimAt(s *Scene, hx, hy, zp, ux, uy float32) float32 {
	var r float32
	for k := 1; k <= 4; k++ {
		st := l.RimW * float32(k) / 4
		dz := zp - s.Near(hx+ux*st, hy+uy*st)
		r = max(r, smooth32(l.RimEdge*0.35, l.RimEdge, dz)*(1-0.55*float32(k-1)/3))
	}
	return r
}

// lightFalloff: how much of a source's light reaches a distance d (px) for a reach R, gone at E
func lightFalloff(d, R, E float32) float32 {
	k := d / R
	return 1 / (1 + 4*k*k) * (1 - smooth32(0.6*E, E, d))
}

// lightFrom: the light at each cell from the particles summed in src (as the light phase of emit.comp)
func lightFrom(src []uint32, gw, gh int, R, E float32) []float32 {
	v := make([]float32, gw*gh)
	d := float32(EmitLightDiv)
	parallelFor(gh, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < gw; x++ {
				var raw float32
				for c := range src {
					if src[c] == 0 {
						continue
					}
					cx, cy := c%gw, c/gw
					dx, dy := float32(cx-x)*d, float32(cy-y)*d
					raw += float32(src[c]) / EmitFixed * lightFalloff(float32(math.Sqrt(float64(dx*dx+dy*dy))), R, E)
				}
				v[y*gw+x] = 1 - float32(math.Exp(float64(-raw*EmitLightGain)))
			}
		}
	})
	return v
}

// LightAt: the light (0..1) at frame pixel (x, y), bilinear between the cells
func (l *LightField) LightAt(x, y float32) float32 {
	div := float32(EmitLightDiv)
	if l.Div > 0 {
		div = float32(l.Div)
	}
	qx := (x+0.5)/div - 0.5
	qy := (y+0.5)/div - 0.5
	ix, iy := int(math.Floor(float64(qx))), int(math.Floor(float64(qy)))
	fx, fy := qx-float32(ix), qy-float32(iy)
	at := func(cx, cy int) float32 {
		cx, cy = max(0, min(l.GW-1, cx)), max(0, min(l.GH-1, cy))
		return l.V[cy*l.GW+cx]
	}
	a := at(ix, iy) + (at(ix+1, iy)-at(ix, iy))*fx
	b := at(ix, iy+1) + (at(ix+1, iy+1)-at(ix, iy+1))*fx
	return a + (b-a)*fy
}

// Lit: color c (0..1) under this light of strength L (as applyLight in render.comp): soft lights
// and tints what is there (√c brings out what is in the dark, as a light on its surfaces), add
// adds the light's color
func (l *LightField) Lit(c [3]float64, L float32) [3]float64 {
	for j := 0; j < 3; j++ {
		lc := float64(l.Col[j] * L)
		if l.Add {
			c[j] += lc * 0.6
		} else {
			c[j] += lc * (math.Sqrt(c[j])*0.9 + 0.03)
		}
		c[j] = math.Min(1, c[j])
	}
	return c
}

// DrawEmitter makes the emitter's image (W×H, premultiplied RGBA) on the CPU, exactly as the GPU,
// and its light (nil if it has none)
func DrawEmitter(e *EmitFrame, W, H int) (*image.RGBA, *LightField) {
	p := &e.P
	div := max(p.Div, 1)
	GW, GH := emitGrid(W, H, div)
	LW, LH := emitGrid(W, H, EmitGlowDiv)
	acc := make([]uint32, GW*GH*4)
	glow := make([]uint32, LW*LH*4)
	XW, XH := emitGrid(W, H, EmitLightDiv)
	var src []uint32
	if p.LightOn {
		src = make([]uint32, XW*XH)
	}
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
					splat(&pt, p.Streak, p.Softness, acc, GW, GH, div)
					if p.GlowI > 0 && p.GlowR > 0 {
						splatGlow(&pt, float32(p.GlowR), glow, LW, LH)
					}
					if src != nil {
						// the particle's light source: its cell
						cx, cy := int(math.Floor(float64(pt.X/EmitLightDiv))), int(math.Floor(float64(pt.Y/EmitLightDiv)))
						if q := uint32(pt.A*emitTiny(pt.Size)*EmitFixed + 0.5); q > 0 && cx >= 0 && cy >= 0 && cx < XW && cy < XH {
							atomic.AddUint32(&src[cy*XW+cx], q)
						}
					}
				}
			}(k)
		}
		wg.Wait()
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				c := resolvePixel(p, float32(e.T), acc, GW, GH, div, glow, LW, LH, x, y)
				copy(img.Pix[(y*W+x)*4:], c[:])
			}
		}
	})
	if src == nil {
		return img, nil
	}
	lf := &LightField{GW: XW, GH: XH, V: lightFrom(src, XW, XH, float32(max(p.LightR, 1)), float32(max(p.LightEnd, 1))), Add: p.LightAdd,
		Col: [3]float32{float32(p.LightCol[0]), float32(p.LightCol[1]), float32(p.LightCol[2])},
		D3:  p.LightD3, CX: float32(p.LightCX), CY: float32(p.LightCY), LZ: float32(p.LightZ),
		DepthK: float32(p.LightDepthK), Shade: float32(p.LightShade),
		From: float32(p.LightFrom), To: float32(p.LightTo), Soft: float32(p.LightSoft),
		Rim: p.LightRim, RimFrom: float32(p.LightRimFrom), RimSoft: float32(p.LightRimSoft), RimW: float32(p.LightRimW),
		RimGain: float32(p.LightRimGain), RimEdge: float32(p.LightRimEdge)}
	return img, lf
}

// cellAt: the sums of a cell (clamped to the grid)
func cellAt(acc []uint32, GW, GH, cx, cy, j int) float32 {
	cx, cy = max(0, min(GW-1, cx)), max(0, min(GH-1, cy))
	return float32(acc[(cy*GW+cx)*4+j]) / EmitFixed
}

// sumAt: the sums at a point (px), bilinear between the cells of div px
func sumAt(acc []uint32, GW, GH, div int, px, py float32) [4]float32 {
	qx, qy := px/float32(div)-0.5, py/float32(div)-0.5
	ix, iy := int(math.Floor(float64(qx))), int(math.Floor(float64(qy)))
	fx, fy := qx-float32(ix), qy-float32(iy)
	var o [4]float32
	for j := range o {
		a := cellAt(acc, GW, GH, ix, iy, j) + (cellAt(acc, GW, GH, ix+1, iy, j)-cellAt(acc, GW, GH, ix, iy, j))*fx
		b := cellAt(acc, GW, GH, ix, iy+1, j) + (cellAt(acc, GW, GH, ix+1, iy+1, j)-cellAt(acc, GW, GH, ix, iy+1, j))*fx
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
			addCell(acc, (y*GW+x)*4, pt, w)
		}
	}
}

func addCell(acc []uint32, o int, pt *Particle, w float32) {
	if w <= 0 {
		return
	}
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

// splatGlow: the particle's halo (its disc grown by R, fading out) in the coarse glow grid
func splatGlow(pt *Particle, R float32, glow []uint32, LW, LH int) {
	rg := pt.Size*0.5 + R
	d := float32(EmitGlowDiv)
	x0, x1 := max(int(math.Floor(float64((pt.X-rg)/d))), 0), min(int(math.Ceil(float64((pt.X+rg)/d))), LW-1)
	y0, y1 := max(int(math.Floor(float64((pt.Y-rg)/d))), 0), min(int(math.Ceil(float64((pt.Y+rg)/d))), LH-1)
	// the same light whatever the reach: spread over the halo's area (in cells)
	a := pt.A * emitTiny(pt.Size) * (d * d) / (0.25 * math.Pi * rg * rg)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			dx, dy := (float32(x)+0.5)*d-pt.X, (float32(y)+0.5)*d-pt.Y
			f := 1 - smooth32(0, rg, float32(math.Sqrt(float64(dx*dx+dy*dy))))
			addCell(glow, (y*LW+x)*4, pt, a*f*f)
		}
	}
}

// Value noise of the flame (same as emit.comp): a hash per lattice point, smooth between them
func hash2(ix, iy int32, seed uint32) float32 {
	return float32(pcg(uint32(ix)*1597334677^uint32(iy)*3812015801^seed)) * (1.0 / 4294967296.0)
}

func evnoise(x, y float32, seed uint32) float32 {
	fx, fy := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
	ix, iy := int32(fx), int32(fy)
	ux, uy := x-fx, y-fy
	ux, uy = ux*ux*(3-2*ux), uy*uy*(3-2*uy)
	a, b := hash2(ix, iy, seed), hash2(ix+1, iy, seed)
	c, d := hash2(ix, iy+1, seed), hash2(ix+1, iy+1, seed)
	return a + (b-a)*ux + (c-a)*uy + (a-b-c+d)*ux*uy
}

func efbm(x, y float32, seed uint32) float32 {
	v := 0.5 * evnoise(x, y, seed)
	v += 0.25 * evnoise(2*x+5.2, 2*y+1.3, seed)
	v += 0.125 * evnoise(4*x+1.7, 4*y+9.2, seed)
	return v / 0.875
}

func mix3f(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

// flameColor: the flame's color and alpha for a heat 0..1 (cool → mid → hot)
func flameColor(p *EmitParams, h float32) ([3]float32, float32) {
	f := func(c [3]float64) [3]float32 { return [3]float32{float32(c[0]), float32(c[1]), float32(c[2])} }
	cool, mid, hot := f(p.FlameCol[0]), f(p.FlameCol[1]), f(p.FlameCol[2])
	var c [3]float32
	switch {
	case h < 0.35:
		c = cool
	case h < 0.7:
		c = mix3f(cool, mid, (h-0.35)/0.35)
	default:
		c = mix3f(mid, hot, (h-0.7)/0.3)
	}
	return c, smooth32(0.02, 0.3, h)
}

// resolvePixel: pixel (x, y) of the emitter's image (premultiplied RGBA8), as resolveOne in emit.comp
func resolvePixel(p *EmitParams, T float32, acc []uint32, GW, GH, div int, glow []uint32, LW, LH, x, y int) [4]uint8 {
	px, py := float32(x)+0.5, float32(y)+0.5
	var o [4]float32
	if p.Flame {
		// the flame: its heat taken a little aside by rising noise, its edges eaten by more noise
		// the whirls are twice as tall as wide: long tongues
		sc := max(float32(p.NoiseScale), 1)
		sy := sc * 2
		tt := T * float32(p.NoiseSpeed) / sy
		nx := efbm(px/sc, py/sy+tt, p.Seed)
		ny := efbm(px/sc+7.3, py/sy+tt+3.1, p.Seed)
		nz := float32(p.Noise)
		s := sumAt(acc, GW, GH, div, px+nz*(nx-0.5)*2, py+nz*(ny-0.5)*2)
		h := 1 - float32(math.Exp(float64(-s[3]*float32(p.Heat))))
		if p.Edge > 0 {
			k := float32(p.Edge) * efbm(px/(sc*0.5), py/sc+tt*1.7, p.Seed+11) * 0.6
			h = max(0, min(1, (h-k)/(1-k)))
		}
		c, a := flameColor(p, h)
		o = [4]float32{c[0] * a, c[1] * a, c[2] * a, a}
	} else if p.Smoke {
		// smoke: soft matter taken aside by rising noise and torn into wisps by finer noise
		sc := max(float32(p.SmokeScale), 1)
		tt := T * float32(p.SmokeSpeed) / sc
		nx := efbm(px/sc, py/sc+tt, p.Seed+17)
		ny := efbm(px/sc+4.1, py/sc+tt+8.7, p.Seed+17)
		wp := float32(p.SmokeWarp)
		s := sumAt(acc, GW, GH, div, px+wp*(nx-0.5)*2, py+wp*(ny-0.5)*2)
		if s[3] > 0 {
			al := 1 - float32(math.Exp(float64(-s[3])))
			m := efbm(px/(sc*0.5), py/(sc*0.5)+tt*1.3, p.Seed+23)
			al *= 1 - float32(p.SmokeWisp)*(1-smooth32(0.35, 0.75, m))
			o = [4]float32{s[0] / s[3] * al, s[1] / s[3] * al, s[2] / s[3] * al, al}
		}
	} else {
		var s [4]float32
		if div == 1 {
			s = [4]float32{cellAt(acc, GW, GH, x, y, 0), cellAt(acc, GW, GH, x, y, 1), cellAt(acc, GW, GH, x, y, 2), cellAt(acc, GW, GH, x, y, 3)}
		} else {
			s = sumAt(acc, GW, GH, div, px, py)
		}
		R, G, B, A := s[0], s[1], s[2], s[3]
		if p.Soft {
			if A > 0 {
				al := 1 - float32(math.Exp(float64(-A)))
				o = [4]float32{R / A * al, G / A * al, B / A * al, al}
			}
		} else {
			al := min(1, max(A, max(R, max(G, B))))
			o = [4]float32{min(R, al), min(G, al), min(B, al), al}
		}
	}
	if p.GlowI > 0 {
		g := sumAt(glow, LW, LH, EmitGlowDiv, px, py)
		var gc [3]float32
		switch {
		case p.GlowOwn:
			gc = [3]float32{float32(p.GlowCol[0]) * g[3], float32(p.GlowCol[1]) * g[3], float32(p.GlowCol[2]) * g[3]}
		case p.Flame:
			gc = [3]float32{float32(p.FlameCol[1][0]) * g[3], float32(p.FlameCol[1][1]) * g[3], float32(p.FlameCol[1][2]) * g[3]}
		default:
			gc = [3]float32{g[0], g[1], g[2]}
		}
		// added like light, softly saturating
		for j := 0; j < 3; j++ {
			v := 1 - float32(math.Exp(float64(-gc[j]*float32(p.GlowI))))
			o[j] = 1 - (1-o[j])*(1-v)
		}
		o[3] = max(o[3], max(o[0], max(o[1], o[2])))
	}
	var c [4]uint8
	for j := range c {
		c[j] = uint8(max(0, min(1, o[j]))*255 + 0.5)
	}
	return c
}

// TextLightDiv: the cell (px) of a text's light grid for a W×H frame: 32, larger on big frames
// (at most 60 cells a side: the grid travels with each frame's params)
func TextLightDiv(W, H int) int { return max(EmitLightDiv, (max(W, H)+59)/60) }

// TextLightArea: px² of letters that make a light of 1 − 1/e near them
const TextLightArea = 1500

// TextLight: the light field (cells of div px) of a text whose letters cover src (px² per cell),
// reach R (px), gone at E (as lightFrom: the same falloff as the emitters' light)
func TextLight(src []float32, gw, gh, div int, R, E float32) []float32 {
	v := make([]float32, gw*gh)
	d := float32(div)
	parallelFor(gh, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < gw; x++ {
				var raw float32
				for c, s := range src {
					if s == 0 {
						continue
					}
					dx, dy := float32(c%gw-x)*d, float32(c/gw-y)*d
					raw += s * lightFalloff(float32(math.Sqrt(float64(dx*dx+dy*dy))), R, E)
				}
				v[y*gw+x] = 1 - float32(math.Exp(float64(-raw/TextLightArea)))
			}
		}
	})
	return v
}

// LightOnto: a light field on what is already in dst (with the scene's depth, if it has one):
// as applyLight in render.comp
func LightOnto(dst *image.RGBA, l *LightField, s *Scene) {
	W, H := dst.Bounds().Dx(), dst.Bounds().Dy()
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				L := l.LightAt(float32(x), float32(y))
				if L > 1e-4 && l.D3 && s.On() {
					L *= l.DepthFactor(s, x, y)
				}
				if L <= 1e-4 {
					continue
				}
				o := y*dst.Stride + x*4
				c := l.Lit([3]float64{float64(dst.Pix[o]) / 255, float64(dst.Pix[o+1]) / 255, float64(dst.Pix[o+2]) / 255}, L)
				for i := 0; i < 3; i++ {
					dst.Pix[o+i] = uint8(c[i]*255 + 0.5)
				}
			}
		}
	})
}
