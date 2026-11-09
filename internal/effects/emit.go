package effects

import (
	"image"
	"math"
	"sync"
	"sync/atomic"

	"audioviz/internal/config"
	"audioviz/internal/expr"
	"audioviz/internal/gpu"
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
	LightRim                                                          bool
	LightRimFrom, LightRimSoft, LightRimW, LightRimGain, LightRimEdge float64
	// with depth: specular shine (0..1) and how dark the shadows that nearer things cast (0..1)
	LightGloss, LightShadow float64
	LightCol                [3]float64
	LightAdd                bool
	// Text shape: points on its letters (x, y pairs, px around its anchor, before its rotation)
	Pts []float32
	// Along the motion path: the path sampled (x, y, length so far: px), its length, closed
	Follow     bool
	Path       []float32
	PathLen    float32
	PathClosed bool
	// their own route (Path): from its start (not the emitter's place on it), moved to where
	// they are born unless RouteFixed
	Route, RouteFixed bool
	RouteK            float64 // how much they follow their route (0..1): mixed with their free motion
	// expressions worked out per particle (EmitExprProps), specialized for this frame; Frame: the
	// frame (for random()), Count: how many particles (count)
	Exprs          []EmitExpr
	Frame, Count   int
	FrameW, FrameH int // the frame's size (path.x / .y are 0..1 of it)
	// the depth along the path they go along (their route, or the motion path): each one takes
	// the depth of its point on it (nil: the emitter's own)
	ZTrack *PathTrack
	// A floor (y px) they bounce on: how much speed they keep (bounce) and sideways (friction)
	FloorOn                 bool
	Floor, Bounce, Friction float64
	// only its light and glow: the particles themselves are not drawn
	Hidden bool
	// Smoke look: soft matter torn into wisps by noise: how much, its scale (px), how fast it
	// rises (px/s), how far it moves the smoke (px)
	Smoke                                        bool
	SmokeWisp, SmokeScale, SmokeSpeed, SmokeWarp float64
	// Water look: where the liquid starts (piled up weight), how clear, its shine, how rounded
	// (bump: px of tilt per unit of slope), ripples (px, their size px, speed px/s)
	Water                                      bool
	WaterTh, WaterClear, WaterShine, WaterBump float64
	WaterRipple, WaterScale, WaterSpeed        float64
	WaterLens                                  float64 // px: what is under it is seen bent through it
	// Gas look (gas.go): the gas their particles feed, this frame (nil: none yet); how thick it
	// looks per unit of smoke, its wisps (how much, their size px), the light's color and the
	// color in its shadow, how much its fire lights it (the flame's colors, heat and edge: Flame…)
	GasOn                  bool
	Gas                    *GasFrame
	GasOpacity, GasWisp    float64
	GasWispScale           float64
	GasLightCol, GasShadow [3]float64
	GasFireLight           float64
	// What they go round in the scene: a signed distance (px; < 0 inside) on a CGW×CGH grid of
	// CDiv px cells (nil: nothing)
	CSDF           []float32
	CGW, CGH, CDiv int
	CBounce        float64 // 0 slide off it; more: bounce off walls and ceilings
	// Each particle hidden where the scene is nearer than it (its own depth: ZTrack): the scene's
	// depth on screen (0…1, nearer is more) at the centers of OGW×OGH cells of ODiv px, the soft
	// edge (nil: not behind things, or one depth for all of it: the clip's own test)
	Occl           []float32
	OGW, OGH, ODiv int
	OSoft          float32
}

// occl: how much of a particle at (x, y) with depth z is seen (the scene nearer hides it), as
// occlAt in emit.comp
func (p *EmitParams) occl(x, y, z float32) float32 {
	if p.Occl == nil || z < 0 || p.OGW < 2 || p.OGH < 2 {
		return 1
	}
	d := float32(p.ODiv)
	qx, qy := max(0, min(float32(p.OGW-1), x/d-0.5)), max(0, min(float32(p.OGH-1), y/d-0.5))
	ix, iy := max(min(int(qx), p.OGW-2), 0), max(min(int(qy), p.OGH-2), 0)
	fx, fy := qx-float32(ix), qy-float32(iy)
	at := func(cx, cy int) float32 { return p.Occl[min(cy, p.OGH-1)*p.OGW+min(cx, p.OGW-1)] }
	s := (at(ix, iy)*(1-fx)+at(ix+1, iy)*fx)*(1-fy) + (at(ix, iy+1)*(1-fx)+at(ix+1, iy+1)*fx)*fy
	return 1 - smooth32(z-p.OSoft, z+p.OSoft+1e-4, s)
}

// MaxCollideCells: cells of an emitter's collision grid (at most 240 a side)
const MaxCollideCells = 240 * 240

// CollideDiv: the collision grid's cell (px) for a W×H frame
func CollideDiv(W, H int) int { return max(8, (max(W, H)+239)/240) }

// sdfAt: the signed distance at (x, y), bilinear between the cells (as in emit.comp)
func (p *EmitParams) sdfAt(x, y float32) float32 {
	d := float32(p.CDiv)
	qx, qy := x/d-0.5, y/d-0.5
	ix, iy := int(math.Floor(float64(qx))), int(math.Floor(float64(qy)))
	fx, fy := qx-float32(ix), qy-float32(iy)
	at := func(cx, cy int) float32 {
		cx, cy = max(0, min(p.CGW-1, cx)), max(0, min(p.CGH-1, cy))
		return p.CSDF[cy*p.CGW+cx]
	}
	a := at(ix, iy) + (at(ix+1, iy)-at(ix, iy))*fx
	b := at(ix, iy+1) + (at(ix+1, iy+1)-at(ix, iy+1))*fx
	return a + (b-a)*fy
}

// SDFAt: sdfAt, for tests and tools
func (p *EmitParams) SDFAt(x, y float32) float32 { return p.sdfAt(x, y) }

// collide: a particle inside what it goes round comes out at its surface, losing the speed that
// went in (it slides off it)
func (p *EmitParams) collide(x, y, vx, vy float32) (float32, float32, float32, float32) {
	d := p.sdfAt(x, y)
	if d >= 0 {
		return x, y, vx, vy
	}
	h := float32(p.CDiv)
	gx := p.sdfAt(x+h, y) - p.sdfAt(x-h, y)
	gy := p.sdfAt(x, y+h) - p.sdfAt(x, y-h)
	l := float32(math.Sqrt(float64(gx*gx + gy*gy)))
	if l < 1e-6 {
		return x, y, vx, vy
	}
	nx, ny := gx/l, gy/l
	if e := float32(p.CBounce); e > 0 && ny > -0.5 {
		// off a wall or a ceiling: mirrored across it, keeping e of the speed that went in
		x, y = x+nx*(-d)*(1+e), y+ny*(-d)*(1+e)
		if vn := vx*nx + vy*ny; vn < 0 {
			vx, vy = vx-(1+e)*vn*nx, vy-(1+e)*vn*ny
		}
		return x, y, vx, vy
	}
	x, y = x+nx*(0.5-d), y+ny*(0.5-d)
	if vn := vx*nx + vy*ny; vn < 0 {
		vx, vy = vx-nx*vn, vy-ny*vn
	}
	return x, y, vx, vy
}

// CollideField: the signed distance (px, < 0 inside) to what is solid on a gw×gh grid of div px
// cells (solid: one per cell, true = solid): two chamfer passes each way, as distances to the
// nearest cell of the other kind
func CollideField(solid []bool, gw, gh, div int) []float32 {
	const big = 1e9
	dist := func(in bool) []float32 {
		d := make([]float32, gw*gh)
		for i := range d {
			if solid[i] == in {
				d[i] = big
			}
		}
		const a, b = 1, 1.4142135
		for y := 0; y < gh; y++ {
			for x := 0; x < gw; x++ {
				i := y*gw + x
				if x > 0 {
					d[i] = min(d[i], d[i-1]+a)
				}
				if y > 0 {
					d[i] = min(d[i], d[i-gw]+a)
					if x > 0 {
						d[i] = min(d[i], d[i-gw-1]+b)
					}
					if x < gw-1 {
						d[i] = min(d[i], d[i-gw+1]+b)
					}
				}
			}
		}
		for y := gh - 1; y >= 0; y-- {
			for x := gw - 1; x >= 0; x-- {
				i := y*gw + x
				if x < gw-1 {
					d[i] = min(d[i], d[i+1]+a)
				}
				if y < gh-1 {
					d[i] = min(d[i], d[i+gw]+a)
					if x < gw-1 {
						d[i] = min(d[i], d[i+gw+1]+b)
					}
					if x > 0 {
						d[i] = min(d[i], d[i+gw-1]+b)
					}
				}
			}
		}
		return d
	}
	out, in := dist(false), dist(true) // to the nearest solid cell; to the nearest free one
	sdf := make([]float32, gw*gh)
	for i := range sdf {
		if solid[i] {
			sdf[i] = -(in[i] - 0.5) * float32(div)
		} else {
			sdf[i] = (out[i] - 0.5) * float32(div)
		}
	}
	return sdf
}

// PathPoint: pathPoint, for others (a text bent along a path)
func PathPoint(pp []float32, L float32, closed bool, s float32) (x, y, tx, ty float32) {
	return pathPoint(pp, L, closed, s)
}

// SamplePath: a motion path as MaxPathSamples points along it with the length so far (x, y, px),
// and its length
func SamplePath(sh *config.MaskShape, W, H float64) ([]float32, float32) {
	const n = MaxPathSamples
	out := make([]float32, 0, n*3)
	var L, px, py float64
	for i := 0; i < n; i++ {
		x, y, _ := PathAt(sh, W, H, float64(i)/float64(n-1))
		if i > 0 {
			L += math.Hypot(x-px, y-py)
		}
		px, py = x, y
		out = append(out, float32(x), float32(y), float32(L))
	}
	return out, float32(L)
}

// wrapLen: a length along a path of length L: round it when closed
func wrapLen(s, L float32, closed bool) float32 {
	if closed && L > 0 {
		s -= L * float32(math.Floor(float64(s/L)))
	}
	return s
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
	// the water's lens (DrawEmitter): where what is under each pixel is seen from, moved (x, y px; nil = none)
	Lens []float32
	// the depth of its particles at each pixel (DrawEmitter, with EmitParams.ZTrack; -1: none there)
	ZPlane []float32
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
	Z            float32 // its depth from the path it goes along (EmitParams.ZTrack; -1: none)
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
	// its own values (expressions per particle), else the emitter's
	pp := partParams{p.Speed, p.Life, p.Size, p.SizeEnd, p.Gravity, p.Wind, p.Turb, p.Dir, p.Spread, p.Col}
	if len(p.Exprs) > 0 {
		e.particleExprs(&pp, i, bt, r)
	}
	life := float32(pp.Life) * (1 + float32(p.LifeVar)*(2*r(0)-1))
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
	a := float32(pp.Dir) + rot + (r(3)-0.5)*float32(pp.Spread)
	sp := float32(pp.Speed) * (1 + float32(p.SpeedVar)*(2*r(4)-1)) * float32(bt.Speed)
	vx0, vy0 := sp*float32(math.Sin(float64(a))), -sp*float32(math.Cos(float64(a)))
	ax, ay := float32(pp.Wind), float32(pp.Gravity)
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
	pz := float32(-1) // its depth from its path (none)
	if p.Follow && !p.Route && len(p.Path) >= 6 {
		// along the path: as far as its speed takes it, the cross offset of its shape, the forces
		along, sv := sp*age, sp
		if k > 1e-3 {
			ek := float32(math.Exp(float64(-k * age)))
			along, sv = sp*(1-ek)/k, sp*ek
		}
		px, py, tx, ty := pathPoint(p.Path, p.PathLen, p.PathClosed, float32(bt.PathP)*p.PathLen+along)
		if p.ZTrack != nil {
			pz = p.ZTrack.At(wrapLen(float32(bt.PathP)*p.PathLen+along, p.PathLen, p.PathClosed))
		}
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
	if p.Route && p.RouteK > 0 && len(p.Path) >= 6 {
		// their own route, from its start (moved to where they are born unless fixed): as far
		// along it as they would have gone free (their launch and the forces push them along it,
		// the way they would have taken: Simpson over their speed), mixed with their free motion
		// by RouteK
		spd := func(t float32) float32 {
			_, _, ux, uy := motion(t)
			return float32(math.Hypot(float64(ux), float64(uy)))
		}
		hs := age / 8
		along := spd(0) + spd(age)
		for j := 1; j < 8; j++ {
			along += float32(2+2*(j&1)) * spd(float32(j)*hs)
		}
		along *= hs / 3
		sv := spd(age)
		px, py, tx, ty := pathPoint(p.Path, p.PathLen, p.PathClosed, along)
		if p.ZTrack != nil {
			pz = p.ZTrack.At(wrapLen(along, p.PathLen, p.PathClosed))
		}
		if !p.RouteFixed {
			px, py = px+float32(bt.X)-p.Path[0], py+float32(bt.Y)-p.Path[1]
		}
		px, py = px-ty*ox, py+tx*ox
		m := float32(min(p.RouteK, 1))
		dx += m * (px - (x0 + dx))
		dy += m * (py - (y0 + dy))
		vx += m * (tx*sv - vx)
		vy += m * (ty*sv - vy)
	}
	// wandering: two waves per axis with random phases, zero at birth
	if pp.Turb > 0 {
		wv := 2 * math.Pi * float32(p.TurbFreq)
		wave := func(j uint32) float32 {
			p1, p2 := 2*math.Pi*r(j), 2*math.Pi*r(j+1)
			s := float32(math.Sin(float64(wv*age+p1))) - float32(math.Sin(float64(p1)))
			s += 0.5 * (float32(math.Sin(float64(2.13*wv*age+p2))) - float32(math.Sin(float64(p2))))
			return s / 1.5
		}
		dx += float32(pp.Turb) * wave(5)
		dy += float32(pp.Turb) * wave(7)
	}
	size := (float32(pp.Size) + (float32(pp.SizeEnd)-float32(pp.Size))*u) * (1 + float32(p.SizeVar)*(2*r(9)-1)) * float32(bt.Size)
	// color over the life, faded in and out
	var c [4]float32
	c0, c1 := pp.Col[0], pp.Col[1]
	f := u * 2
	if u >= 0.5 {
		c0, c1, f = pp.Col[1], pp.Col[2], (u-0.5)*2
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
	px, py := x0+dx, y0+dy
	if p.CSDF != nil {
		// twice: out of one thing it may land in another close by
		px, py, vx, vy = p.collide(px, py, vx, vy)
		px, py, vx, vy = p.collide(px, py, vx, vy)
	}
	return Particle{px, py, vx, vy, max(size, 0), c[0], c[1], c[2], c[3], pz}, true
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

// EmitAccLayout: the words of the emitter pass's accumulations for W×H on the GPU (emit.comp),
// which goes in batches of emitters: per emitter of a batch its particles (4 per pixel), its glow
// (4 per glow cell), its light sources (1 per light cell) and its particles' depth (1 per pixel),
// stride words in all; after the batch's, every emitter's light field (field words each)
func EmitAccLayout(W, H int) (stride, field int) {
	gw, gh := emitGrid(W, H, EmitGlowDiv)
	xw, xh := emitGrid(W, H, EmitLightDiv)
	return W*H*4 + gw*gh*4 + xw*xh + W*H, xw * xh
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
	Rim                                      bool
	RimFrom, RimSoft, RimW, RimGain, RimEdge float32
	// specular shine (0..1) and cast shadows (0..1), with depth
	Gloss, Shadow float32
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
// (as depthLight in render.comp): less with the difference of depth, only in the lit range, and
// shaded by the surface there (its normal from the normal map, or the depth's slope without one):
// wrapped diffuse light, specular shine (Gloss), farther than RimFrom only the edges that turn
// towards the light (rim), and darker where nearer things stand between it and the light (Shadow)
func (l *LightField) DepthFactor(s *Scene, x, y int) float32 {
	qx, qy := float32(x), float32(y)
	// the background point seen there; its surroundings around that point
	hx, hy, zp, w := s.HitW(qx, qy)
	dz := (zp - l.LZ) * l.DepthK
	att := float32(math.Exp(float64(-dz * dz)))
	rng := LightRange(zp, l.From, l.To, l.Soft)
	f := att * rng
	nx, ny, nz, hasN := s.normalW(w, hx, hy)
	if !hasN {
		gx := (s.nearW(w, hx+2, hy) - s.nearW(w, hx-2, hy)) / 4 * DepthPx
		gy := (s.nearW(w, hx, hy+2) - s.nearW(w, hx, hy-2)) / 4 * DepthPx
		nl := float32(math.Sqrt(float64(gx*gx + gy*gy + 1)))
		nx, ny, nz = -gx/nl, -gy/nl, 1/nl
	}
	lx, ly, lz := l.CX-qx, l.CY-qy, (l.LZ-zp)*DepthPx
	ll := float32(math.Sqrt(float64(lx*lx+ly*ly+lz*lz))) + 1e-3
	lx, ly, lz = lx/ll, ly/ll, lz/ll
	ndl := nx*lx + ny*ly + nz*lz
	if l.Shade > 0 {
		diff := max(0, min(1, (ndl+0.2)/1.2))
		f *= 1 - l.Shade + l.Shade*(0.25+1.15*diff)
	}
	if l.Gloss > 0 {
		hx2, hy2, hz2 := lx, ly, lz+1
		hl := float32(math.Sqrt(float64(hx2*hx2+hy2*hy2+hz2*hz2))) + 1e-6
		sp := float32(math.Pow(float64(max(0, (nx*hx2+ny*hy2+nz*hz2)/hl)), float64(6+58*l.Gloss)))
		f += l.Gloss * 1.6 * sp * att * rng * smooth32(-0.05, 0.2, ndl)
	}
	if l.Rim {
		// far away: only the edges that face it
		dx, dy := l.CX-qx, l.CY-qy
		d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if t := smooth32(l.RimFrom-l.RimSoft, l.RimFrom+l.RimSoft+1e-3, d); t > 0 {
			ux, uy := dx/max(d, 1e-3), dy/max(d, 1e-3)
			var r float32
			if hasN {
				// a rim is back light: only as much as the light is behind the surface (in front
				// of it the surface keeps its light)
				r = l.rimNormal(nx, ny, nz, ux, uy)
				t *= smooth32(-0.05, 0.35, -lz)
			} else {
				r = l.rimAt(s, w, hx, hy, zp, ux, uy)
			}
			f = f*(1-t) + t*l.RimGain*r*rng
		}
	}
	if l.Shadow > 0 {
		f *= 1 - l.Shadow*l.shadowAt(s, w, qx, qy, hx, hy, zp)
	}
	return f
}

// rimNormal: how much a surface of normal n is an edge facing the light (direction ux, uy on the
// frame): turned away from the viewer (1 − nz) towards the light, from RimEdge×4 of turn (as
// rimNormal in render.comp)
func (l *LightField) rimNormal(nx, ny, nz, ux, uy float32) float32 {
	side := 1 - nz
	nl := float32(math.Sqrt(float64(nx*nx+ny*ny))) + 1e-5
	faces := (nx*ux + ny*uy) / nl
	e := max(0.02, min(0.6, l.RimEdge*4))
	return smooth32(0, e, side) * smooth32(-0.1, 0.4, faces)
}

// shadowAt: how much nearer things block the way from background point h (frame pixel q) to the
// light: 12 steps over up to 600 px towards it, each one in shadow where the scene there is nearer
// than the ray (as shadowAt in render.comp)
func (l *LightField) shadowAt(s *Scene, w int, qx, qy, hx, hy, zp float32) float32 {
	dx, dy := l.CX-qx, l.CY-qy
	d := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if d < 2 {
		return 0
	}
	span := min(d, 600)
	ux, uy := dx/d, dy/d
	var occ float32
	for k := 1; k <= 12; k++ {
		st := span * float32(k) / 12
		zr := zp + (l.LZ-zp)*(st/d)
		zs := s.nearW(w, hx+ux*st, hy+uy*st)
		occ = max(occ, smooth32(0.004, 0.03, zs-zr)*(1-0.5*float32(k-1)/11))
	}
	return occ
}

// rimAt: how much background point h is on an edge facing the light (direction ux, uy): the
// scene farther than it within RimW px towards the light, brighter right at the edge (as in
// lightDepth in render.comp)
func (l *LightField) rimAt(s *Scene, w int, hx, hy, zp, ux, uy float32) float32 {
	var r float32
	for k := 1; k <= 4; k++ {
		st := l.RimW * float32(k) / 4
		dz := zp - s.nearW(w, hx+ux*st, hy+uy*st)
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
	var zacc []uint32 // their depth (× their weight) when it comes from their path
	if p.ZTrack != nil {
		zacc = make([]uint32, GW*GH)
	}
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
					// hidden where the scene is nearer than it (its own depth)
					if pt.A *= p.occl(pt.X, pt.Y, pt.Z); pt.A <= 0 {
						continue
					}
					if !p.GasOn {
						splat(&pt, p.Streak, p.Softness, acc, GW, GH, div, zacc)
					}
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
	e.Lens = nil
	if p.Water && p.WaterLens > 0 {
		e.Lens = make([]float32, W*H*2)
	}
	e.ZPlane = nil
	if zacc != nil {
		e.ZPlane = make([]float32, W*H)
	}
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				var ln [2]float32
				c := resolvePixel(p, float32(e.T), acc, GW, GH, div, glow, LW, LH, x, y, &ln)
				copy(img.Pix[(y*W+x)*4:], c[:])
				if e.Lens != nil {
					e.Lens[(y*W+x)*2], e.Lens[(y*W+x)*2+1] = ln[0], ln[1]
				}
				if e.ZPlane != nil {
					e.ZPlane[y*W+x] = zPlaneAt(acc, zacc, GW, GH, div, x, y)
				}
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
		RimGain: float32(p.LightRimGain), RimEdge: float32(p.LightRimEdge),
		Gloss: float32(p.LightGloss), Shadow: float32(p.LightShadow)}
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

func splat(pt *Particle, streak, softness float64, acc []uint32, GW, GH, div int, zacc []uint32) {
	r, sx, sy, x0, y0, x1, y1 := pt.footprint(float32(streak))
	x0, y0 = max(x0/div, 0), max(y0/div, 0)
	x1, y1 = min(x1/div+1, GW-1), min(y1/div+1, GH-1)
	a := pt.A * emitTiny(pt.Size)
	d := float32(div)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			w := pt.weight((float32(x)+0.5)*d, (float32(y)+0.5)*d, r, sx, sy, float32(softness)) * a
			addCell(acc, (y*GW+x)*4, pt, w)
			if zacc != nil && pt.Z >= 0 && w > 0 {
				if q := uint32(pt.Z*w*EmitFixed + 0.5); q > 0 {
					atomic.AddUint32(&zacc[y*GW+x], q)
				}
			}
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

// waterPixel: the water look at (px, py), as in emit.comp: the particles' weight piled up is a
// height; past the threshold it is liquid, its slope a surface lit from the top left (shine), see
// through in its body and denser at its edges (Fresnel), moved by ripples
// (and the lens: where what is under it is seen from, moved px)
func waterPixel(p *EmitParams, T float32, acc []uint32, GW, GH, div int, px, py float32) ([4]float32, [2]float32) {
	sc := max(float32(p.WaterScale), 1)
	tt := T * float32(p.WaterSpeed) / sc
	rp := float32(p.WaterRipple)
	qx := px + rp*(efbm(px/sc+tt, py/sc, p.Seed+31)-0.5)*2
	qy := py + rp*(efbm(px/sc+3.7, py/sc-tt+5.3, p.Seed+31)-0.5)*2
	s := sumAt(acc, GW, GH, div, qx, qy)
	th := max(float32(p.WaterTh), 0.01)
	if s[3] < th*0.7 {
		return [4]float32{}, [2]float32{}
	}
	al := smooth32(th*0.8, th*1.15, s[3])
	e := float32(max(div, 2))
	gx := (sumAt(acc, GW, GH, div, qx+e, qy)[3] - sumAt(acc, GW, GH, div, qx-e, qy)[3]) / (2 * e)
	gy := (sumAt(acc, GW, GH, div, qx, qy+e)[3] - sumAt(acc, GW, GH, div, qx, qy-e)[3]) / (2 * e)
	k := float32(p.WaterBump)
	nx, ny, nz := -gx*k, -gy*k, float32(1)
	nl := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
	nx, ny, nz = nx/nl, ny/nl, nz/nl
	// the light from the top left, the eye in front
	const lx, ly, lz = -0.4319, -0.5758, 0.6943
	const hx, hy, hz = -0.2357, -0.3143, 0.9196 // halfway between the light and the eye
	ndl := max(0, nx*lx+ny*ly+nz*lz)
	spec := float32(math.Pow(float64(max(0, nx*hx+ny*hy+nz*hz)), 48)) * float32(p.WaterShine)
	fr := float32(math.Pow(float64(1-nz), 2))
	clear := float32(math.Min(math.Max(p.WaterClear, 0), 1))
	body := (1 - clear) + clear*min(1, fr*1.2)*0.6 // see through its middle, denser at its edges
	var o [4]float32
	for j := 0; j < 3; j++ {
		b := s[j] / s[3]
		c := b*(0.55+0.6*ndl)*body + (b*0.5+0.5)*fr*0.4*(1-0.85*clear) + spec
		o[j] = c * al
	}
	o[3] = max(al*body, max(o[0], max(o[1], o[2])))
	// a lens: seen from towards its middle (what is behind is magnified)
	lk := float32(p.WaterLens) * al
	return o, [2]float32{-nx * lk, -ny * lk}
}

// resolvePixel: pixel (x, y) of the emitter's image (premultiplied RGBA8), as resolveOne in emit.comp
func resolvePixel(p *EmitParams, T float32, acc []uint32, GW, GH, div int, glow []uint32, LW, LH, x, y int, lens *[2]float32) [4]uint8 {
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
	} else if p.Water {
		o, *lens = waterPixel(p, T, acc, GW, GH, div, px, py)
	} else if p.GasOn {
		if p.Gas != nil {
			o = gasPixel(p, p.Gas, px, py)
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
	if p.Hidden {
		o = [4]float32{} // a light source: only its glow shows
		*lens = [2]float32{}
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

// zPlaneAt: the depth of the particles at pixel (x, y): their depths weighted as their alpha, in
// the pixel's cell (-1 without any), as resolveOne in emit.comp. (Only for how the 2.5D camera
// moves them and the depth of field blurs them: what hides them is worked out per particle, occl.)
func zPlaneAt(acc, zacc []uint32, GW, GH, div, x, y int) float32 {
	cx, cy := min(x/div, GW-1), min(y/div, GH-1)
	a := acc[(cy*GW+cx)*4+3]
	if a == 0 {
		return -1
	}
	return float32(zacc[cy*GW+cx]) / float32(a)
}

// EmitExprProps: the emitter's properties an expression can give per particle (their paths in
// the config), in the order emit.comp numbers them
var EmitExprProps = []string{"emitter.speed", "emitter.life", "emitter.size", "emitter.size_end", "emitter.gravity",
	"emitter.wind", "emitter.turbulence", "emitter.direction", "emitter.spread",
	"emitter.color_start", "emitter.color_mid", "emitter.color_end"}

// EmitColorProp: the first of EmitExprProps that are colors (birth, middle, end: worked out channel
// by channel, from the emitter's)
const EmitColorProp = 9

// EmitColorKeep: the variables a particle's color expression reads per particle
func EmitColorKeep(v int) bool {
	return v == expr.VarIndex || v == expr.VarCount || v == expr.VarValue || v == expr.VarChannel ||
		(v >= expr.VarValueR && v <= expr.VarValueA)
}

// EmitExpr: an expression worked out per particle: its property (EmitExprProps), its program
// specialized for the frame, what turns its result into the params' units (the clip's scale,
// degrees → radians), and the tracks of the values it reads on the route / motion path
type EmitExpr struct {
	Prop int
	Prog *expr.Program
	Mul  float64
	Keys []EmitExprKey // one per Prog.Keys
}

// EmitExprKey: a named value along the route and along the motion path (nil: none)
type EmitExprKey struct {
	Route, Path *PathTrack
	Builtin     int // PathBuiltin's: x, y, progress or angle of the route / path where it is
}

// PathBuiltin: the values every path has (path.x, .y, .progress, .angle): 1…4; 0 if name is a
// value of its nodes
func PathBuiltin(name string) int {
	switch name {
	case "x":
		return 1
	case "y":
		return 2
	case "progress":
		return 3
	case "angle":
		return 4
	}
	return 0
}

// PathBuiltinAt: a path's own value b (PathBuiltin) at s px along it (pp, L, closed as
// SamplePath; dx, dy: how far it is moved, px), in a W×H frame: x, y 0..1 of the frame, its
// progress 0..1, its direction in degrees
func PathBuiltinAt(b int, pp []float32, L float32, closed bool, s, dx, dy float32, W, H int) float64 {
	s = wrapLen(s, L, closed)
	x, y, tx, ty := pathPoint(pp, L, closed, s)
	switch b {
	case 1:
		return float64((x + dx) / float32(W))
	case 2:
		return float64((y + dy) / float32(H))
	case 3:
		if L <= 0 {
			return 0
		}
		return float64(max(0, min(1, s/L)))
	case 4:
		return math.Atan2(float64(ty), float64(tx)) * 180 / math.Pi
	}
	return 0
}

// partParams: the params a particle can have of its own
type partParams struct {
	Speed, Life, Size, SizeEnd, Gravity, Wind, Turb, Dir, Spread float64
	Col                                                          [3][4]float64 // birth, middle, end
}

func (pp *partParams) set(prop int, v float64) {
	switch prop {
	case 0:
		pp.Speed = v
	case 1:
		pp.Life = v
	case 2:
		pp.Size = v
	case 3:
		pp.SizeEnd = v
	case 4:
		pp.Gravity = v
	case 5:
		pp.Wind = v
	case 6:
		pp.Turb = v
	case 7:
		pp.Dir = v
	case 8:
		pp.Spread = v
	}
}

// particleExprs works out particle i's expressions: they read its index, its random numbers and
// the values of the route / motion path where it is (as far along as the emitter's speed takes
// it, its age then). As exprItem in emit.comp.
func (e *EmitFrame) particleExprs(pp *partParams, i int, bt Birth, r func(uint32) float32) {
	p := &e.P
	age := float32(e.T) - float32(bt.T)
	sp := float32(p.Speed) * (1 + float32(p.SpeedVar)*(2*r(4)-1)) * float32(bt.Speed)
	along := sp * max(age, 0)
	if k := float32(p.Drag); k > 1e-3 {
		along = sp * (1 - float32(math.Exp(float64(-k*max(age, 0))))) / k
	}
	env := &particleEnv{p: p, i: i, sRoute: along, sPath: float32(bt.PathP)*p.PathLen + along, W: max(p.FrameW, 1), H: max(p.FrameH, 1)}
	if p.Route && !p.RouteFixed && len(p.Path) >= 2 {
		env.shiftX, env.shiftY = float32(bt.X)-p.Path[0], float32(bt.Y)-p.Path[1]
	}
	for k := range p.Exprs {
		x := &p.Exprs[k]
		env.keys, env.prog = x.Keys, x.Prog
		if c := x.Prop - EmitColorProp; c >= 0 && c < 3 {
			// a color: channel by channel, from the emitter's (as exprColor in emit.comp)
			base := p.Col[c]
			env.base = &base
			for ch := 0; ch < 4; ch++ {
				env.ch = ch
				res := x.Prog.Run(env)
				v := 0.0
				if !res.Nil {
					v = max(0, min(1, res.V))
				}
				pp.Col[c][ch] = v
			}
			env.base = nil
			continue
		}
		res := x.Prog.Run(env)
		v := res.V
		if res.Nil {
			v = 0
		}
		pp.set(x.Prop, v*x.Mul)
	}
}

// particleEnv: what a particle's expression reads (expr.Env, specialized: only index, random
// numbers and the values along its route / path are left)
type particleEnv struct {
	p              *EmitParams
	i              int
	sRoute, sPath  float32
	shiftX, shiftY float32 // its route moved to where it was born
	W, H           int
	keys           []EmitExprKey
	prog           *expr.Program
	base           *[4]float64 // a color worked out: the emitter's, the channel now
	ch             int
}

func (e *particleEnv) Var(v int) float64 {
	switch v {
	case expr.VarIndex:
		return float64(e.i)
	case expr.VarCount:
		return float64(max(e.p.Count, 1))
	}
	if e.base != nil {
		switch v {
		case expr.VarValue:
			return e.base[e.ch]
		case expr.VarChannel:
			return float64(e.ch)
		case expr.VarValueR, expr.VarValueG, expr.VarValueB, expr.VarValueA:
			return e.base[v-expr.VarValueR]
		}
	}
	return 0
}
func (e *particleEnv) Band(int, float64) float64 { return 0 }
func (e *particleEnv) Lag(int, float64) float64  { return 0 }
func (e *particleEnv) Peak(int, float64) float64 { return 0 }
func (e *particleEnv) Key(kind int, name string) (float64, bool) {
	for k, n := range e.prog.Keys {
		if n != name || k >= len(e.keys) {
			continue
		}
		if b := e.keys[k].Builtin; b > 0 && len(e.p.Path) >= 6 {
			// where it is on its route (moved to where it was born unless fixed) or path
			if kind == 1 && e.p.Route {
				return PathBuiltinAt(b, e.p.Path, e.p.PathLen, e.p.PathClosed, e.sRoute, e.shiftX, e.shiftY, e.W, e.H), true
			}
			if kind == 0 && e.p.Follow && !e.p.Route {
				return PathBuiltinAt(b, e.p.Path, e.p.PathLen, e.p.PathClosed, e.sPath, 0, 0, e.W, e.H), true
			}
		}
		if kind == 1 && e.keys[k].Route != nil {
			return float64(e.keys[k].Route.At(wrapLen(e.sRoute, e.p.PathLen, e.p.PathClosed))), true
		}
		if kind == 0 && e.keys[k].Path != nil {
			return float64(e.keys[k].Path.At(wrapLen(e.sPath, e.p.PathLen, e.p.PathClosed))), true
		}
	}
	return 0, false
}
func (e *particleEnv) Rand(perFrame bool) float64 {
	return expr.ItemRand(e.p.Seed, e.i, perFrame, e.p.Frame)
}

// exprWords: the words an expression takes in the emitter data (render.go packs it so)
func (x *EmitExpr) exprWords() int {
	n := 6 + len(x.Prog.Code) + len(x.Prog.Consts) + 2*len(x.Keys)
	for _, k := range x.Keys {
		for _, t := range []*PathTrack{k.Route, k.Path} {
			if t != nil {
				n += 1 + 3*len(t.S)
			}
		}
	}
	return n
}

// FitEmitExprs: the expressions that fit in the GPU's room (in order), the same for the CPU
func FitEmitExprs(xs []EmitExpr) []EmitExpr {
	var out []EmitExpr
	used := 1
	for _, x := range xs {
		w := x.exprWords()
		if x.Prog.Stack > gpu.EmitExprStack || x.Prog.Slots > gpu.EmitExprSlots || used+w > gpu.EmitExprWords {
			continue
		}
		used += w
		out = append(out, x)
	}
	return out
}

// PackEmitExprs writes the expressions (that fit: FitEmitExprs) at word off of the emitter data
// (u, f the same words as uint32 / float32), as exprRun in emit.comp reads them: n, then per
// expression 6 words (property, code, its length, consts, key table, multiplier), then the data
func PackEmitExprs(u []uint32, f []float32, off int, xs []EmitExpr) {
	u[off] = uint32(len(xs))
	d := off + 1 + 6*len(xs)
	for q, x := range xs {
		h := off + 1 + 6*q
		u[h] = uint32(x.Prop)
		u[h+1], u[h+2] = uint32(d), uint32(len(x.Prog.Code))
		for _, c := range x.Prog.Code {
			u[d] = uint32(c)
			d++
		}
		u[h+3] = uint32(d)
		for _, c := range x.Prog.Consts {
			f[d] = float32(c)
			d++
		}
		u[h+4] = uint32(d)
		tab := d
		d += 2 * len(x.Keys)
		for k, ky := range x.Keys {
			for j, t := range []*PathTrack{ky.Route, ky.Path} {
				u[tab+2*k+j] = 0
				if ky.Builtin > 0 {
					u[tab+2*k+j] = 0xFFFFFFF0 + uint32(ky.Builtin) // the path's own (exprRun works it out)
					continue
				}
				if t == nil {
					continue
				}
				u[tab+2*k+j] = uint32(d + 1)
				n := len(t.S)
				f[d] = float32(n)
				for m := 0; m < n; m++ {
					f[d+1+m], f[d+1+n+m], f[d+1+2*n+m] = t.S[m], t.V[m], float32(t.E[m])
				}
				d += 1 + 3*n
			}
		}
		f[h+5] = float32(x.Mul)
	}
}
