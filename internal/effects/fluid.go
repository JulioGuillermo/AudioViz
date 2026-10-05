package effects

import (
	"image"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"
)

// Fluid transition: the old image flows into the new one like a liquid.
//
//  1. Matching. The old image is cut into particles (one per small block, its color) and the
//     new one into as many slots. Every particle gets a slot of similar color and not too far
//     (sliced optimal transport in color + position space), every slot gets a particle.
//  2. Simulation. The particles push an incompressible fluid (a velocity grid) toward their
//     slots; pressure makes them go around each other, vorticity confinement and a stir at the
//     start turn that into eddies. It is computed once for the whole transition with a fixed
//     step and seed (deterministic) and the positions are stored.
//  3. Rendering. Each frame splats the particles at their stored positions, their color going
//     from the old pixel to the new one; it starts exactly as the old image and ends exactly as
//     the new one.

// FluidOpts: 0..1 knobs (0 = default)
type FluidOpts struct {
	Turbulence  float64 // eddies (vorticity confinement + stir)
	Viscosity   float64 // how thick the fluid is (damps the motion)
	ColorWeight float64 // 0: particles stay near, 1: they go to the most similar color anywhere
	Seed        uint32
}

// Fluid is a simulated transition, ready to render any instant of it
type Fluid struct {
	W, H       int     // frame size
	gx, gy     int     // particle grid (and fluid grid) size
	cell       float64 // particle spacing in pixels
	n          int
	snaps      int
	pos        []float32 // snaps × n × 2, in pixels
	colA, colB []uint32  // per particle: its color and the color of its slot (RGBA)
	tx, ty     []float32 // slot position (pixels)
	accum      sync.Pool
}

func px(img *image.RGBA, x, y int) uint32 {
	o := y*img.Stride + x*4
	p := img.Pix[o : o+4 : o+4]
	return uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
}

func ch(c uint32, i int) float64 { return float64((c >> (8 * i)) & 255) }

// NewFluid matches and simulates a transition from a to b (both w×h) lasting `frames` frames
func NewFluid(a, b *image.RGBA, w, h, frames int, o FluidOpts) *Fluid {
	if o.Turbulence <= 0 {
		o.Turbulence = 0.5
	}
	if o.Viscosity <= 0 {
		o.Viscosity = 0.3
	}
	if o.ColorWeight <= 0 {
		o.ColorWeight = 0.6
	}
	f := &Fluid{W: w, H: h}
	// Particles every ~H/180 pixels (6 px at 1080p): ~57,000 at 16:9
	f.cell = math.Max(2, float64(h)/180)
	f.gx, f.gy = max(8, int(float64(w)/f.cell)), max(8, int(float64(h)/f.cell))
	f.n = f.gx * f.gy
	sx, sy := float64(w)/float64(f.gx), float64(h)/float64(f.gy)
	// Each particle / slot takes the average color of its block
	block := func(img *image.RGBA, i int) uint32 {
		cx, cy := i%f.gx, i/f.gx
		x0, y0 := int(float64(cx)*sx), int(float64(cy)*sy)
		x1, y1 := min(w, int(float64(cx+1)*sx)), min(h, int(float64(cy+1)*sy))
		var r, g, bb, nn float64
		for y := y0; y < max(y1, y0+1); y++ {
			for x := x0; x < max(x1, x0+1); x++ {
				c := px(img, min(x, w-1), min(y, h-1))
				r, g, bb, nn = r+ch(c, 0), g+ch(c, 1), bb+ch(c, 2), nn+1
			}
		}
		return uint32(r/nn) | uint32(g/nn)<<8 | uint32(bb/nn)<<16
	}
	ca, cb := make([]uint32, f.n), make([]uint32, f.n)
	parallelFor(f.gy, func(y0, y1 int) {
		for i := y0 * f.gx; i < y1*f.gx; i++ {
			ca[i], cb[i] = block(a, i), block(b, i)
		}
	})
	match := sliceMatch(ca, cb, f.gx, f.gy, o)
	f.colA, f.colB = ca, make([]uint32, f.n)
	f.tx, f.ty = make([]float32, f.n), make([]float32, f.n)
	for i, j := range match {
		f.colB[i] = cb[j]
		f.tx[i], f.ty[i] = float32((float64(j%f.gx)+0.5)*sx), float32((float64(j/f.gx)+0.5)*sy)
	}
	f.simulate(frames, o, sx, sy)
	return f
}

// sliceMatch: sliced optimal transport between the particles (old colors at their places) and
// the slots (new colors at theirs) in a 5D space (r, g, b, λx, λy); returns the slot of each particle
func sliceMatch(ca, cb []uint32, gx, gy int, o FluidOpts) []int {
	n := len(ca)
	// λ: weight of position against color. ColorWeight 0 → positions dominate, 1 → colors
	lam := 2.5 * math.Pow(0.06, o.ColorWeight) // 2.5 .. 0.15 (in units of the color range per frame height)
	feat := func(c []uint32, i int) [5]float64 {
		return [5]float64{ch(c[i], 0) / 255, ch(c[i], 1) / 255, ch(c[i], 2) / 255,
			lam * float64(i%gx) / float64(gy), lam * float64(i/gx) / float64(gy)}
	}
	A, B := make([][5]float64, n), make([][5]float64, n)
	for i := range n {
		A[i], B[i] = feat(ca, i), feat(cb, i)
	}
	rng := rand.New(rand.NewPCG(uint64(o.Seed)+1, 0x9e3779b97f4a7c15))
	ia, ib := make([]int, n), make([]int, n)
	pa, pb := make([]float64, n), make([]float64, n)
	for i := range n {
		ia[i], ib[i] = i, i
	}
	const iters = 48
	for it := 0; it < iters; it++ {
		var d [5]float64
		var l float64
		for k := range d {
			d[k] = rng.NormFloat64()
			l += d[k] * d[k]
		}
		l = math.Sqrt(l)
		for k := range d {
			d[k] /= l
		}
		for i := range n {
			pa[i], pb[i] = dot5(A[i], d), dot5(B[i], d)
		}
		sort.Slice(ia, func(x, y int) bool { return pa[ia[x]] < pa[ia[y]] })
		sort.Slice(ib, func(x, y int) bool { return pb[ib[x]] < pb[ib[y]] })
		if it == iters-1 {
			break
		}
		for r := range n {
			delta := pb[ib[r]] - pa[ia[r]]
			for k := range d {
				A[ia[r]][k] += delta * d[k]
			}
		}
	}
	out := make([]int, n)
	for r := range n {
		out[ia[r]] = ib[r]
	}
	return out
}

func dot5(a, b [5]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3] + a[4]*b[4] }

func smooth01(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// simulate runs the fluid and stores the particle positions at `snaps` instants
func (f *Fluid) simulate(frames int, o FluidOpts, sx, sy float64) {
	f.snaps = max(2, min(frames, 240))
	n := f.n
	f.pos = make([]float32, f.snaps*n*2)
	// Fluid grid: a quarter of the particle grid (each cell ~4 particles wide)
	G := 4
	nx, ny := max(8, f.gx/G), max(8, f.gy/G)
	cw := float64(f.W) / float64(nx) // pixels per fluid cell
	u, v := make([]float64, nx*ny), make([]float64, nx*ny)
	wt := make([]float64, nx*ny)
	px0, py0 := make([]float64, n), make([]float64, n)
	vx, vy := make([]float64, n), make([]float64, n)
	sx0, sy0 := make([]float64, n), make([]float64, n)
	for i := range n {
		px0[i], py0[i] = (float64(i%f.gx)+0.5)*sx, (float64(i/f.gx)+0.5)*sy
		sx0[i], sy0[i] = px0[i], py0[i]
	}
	store := func(s int) {
		base := s * n * 2
		for i := range n {
			f.pos[base+2*i], f.pos[base+2*i+1] = float32(px0[i]), float32(py0[i])
		}
	}
	store(0)
	sub := 3
	steps := (f.snaps - 1) * sub
	dt := 1.0 / float64(steps)
	idx := func(x, y int) int { return min(max(y, 0), ny-1)*nx + min(max(x, 0), nx-1) }
	// bilinear in fluid-grid coordinates (cell centers at +0.5)
	sample := func(g []float64, x, y float64) float64 {
		x, y = x/cw-0.5, y/cw-0.5
		x0, y0 := math.Floor(x), math.Floor(y)
		fx, fy := x-x0, y-y0
		i0, j0 := int(x0), int(y0)
		a := g[idx(i0, j0)] + (g[idx(i0+1, j0)]-g[idx(i0, j0)])*fx
		b := g[idx(i0, j0+1)] + (g[idx(i0+1, j0+1)]-g[idx(i0, j0+1)])*fx
		return a + (b-a)*fy
	}
	p := make([]float64, nx*ny)
	div := make([]float64, nx*ny)
	curl := make([]float64, nx*ny)
	turb := o.Turbulence
	damp := 0.5 + 6*o.Viscosity // per unit time
	diag := math.Hypot(float64(f.W), float64(f.H))
	for st := 1; st <= steps; st++ {
		t := float64(st) * dt
		s := smooth01(t)
		ds := 6 * t * (1 - t) // d(smoothstep)/dt
		// 1. Particles → grid: the velocity they want (follow the path to their slot)
		for k := range u {
			u[k], v[k], wt[k] = 0, 0, 0
		}
		const kp, kv = 40.0, 6.0
		for i := range n {
			dxp := sx0[i] + (float64(f.tx[i])-sx0[i])*s
			dyp := sy0[i] + (float64(f.ty[i])-sy0[i])*s
			dvx := (float64(f.tx[i]) - sx0[i]) * ds
			dvy := (float64(f.ty[i]) - sy0[i]) * ds
			ax := kp*(dxp-px0[i]) + kv*(dvx-vx[i])
			ay := kp*(dyp-py0[i]) + kv*(dvy-vy[i])
			wx, wy := vx[i]+ax*dt, vy[i]+ay*dt
			gxf, gyf := px0[i]/cw-0.5, py0[i]/cw-0.5
			x0, y0 := int(math.Floor(gxf)), int(math.Floor(gyf))
			fx, fy := gxf-float64(x0), gyf-float64(y0)
			for _, c := range [4][3]float64{{0, 0, (1 - fx) * (1 - fy)}, {1, 0, fx * (1 - fy)}, {0, 1, (1 - fx) * fy}, {1, 1, fx * fy}} {
				k := idx(x0+int(c[0]), y0+int(c[1]))
				u[k] += wx * c[2]
				v[k] += wy * c[2]
				wt[k] += c[2]
			}
		}
		for k := range u {
			if wt[k] > 1e-6 {
				u[k] /= wt[k]
				v[k] /= wt[k]
			}
		}
		// 2. Stir (curl noise, strongest in the middle) and vorticity confinement: eddies
		stir := turb * diag * 1.2 * math.Sin(math.Pi*t)
		for y := range ny {
			for x := range nx {
				k := y*nx + x
				fxn, fyn := float64(x)*0.22, float64(y)*0.22
				e := 0.5
				psi := func(a, b float64) float64 { return vnoise(a+t*1.5, b, o.Seed+77) }
				dpdy := (psi(fxn, fyn+e) - psi(fxn, fyn-e)) / (2 * e)
				dpdx := (psi(fxn+e, fyn) - psi(fxn-e, fyn)) / (2 * e)
				u[k] += stir * dpdy * dt
				v[k] += -stir * dpdx * dt
				curl[k] = (v[idx(x+1, y)] - v[idx(x-1, y)] - u[idx(x, y+1)] + u[idx(x, y-1)]) * 0.5
			}
		}
		eps := turb * 3.0
		for y := 1; y < ny-1; y++ {
			for x := 1; x < nx-1; x++ {
				k := y*nx + x
				gxw := (math.Abs(curl[idx(x+1, y)]) - math.Abs(curl[idx(x-1, y)])) * 0.5
				gyw := (math.Abs(curl[idx(x, y+1)]) - math.Abs(curl[idx(x, y-1)])) * 0.5
				l := math.Hypot(gxw, gyw) + 1e-9
				u[k] += eps * (gyw / l) * curl[k] * dt * 60
				v[k] += eps * (-gxw / l) * curl[k] * dt * 60
			}
		}
		// 3. Viscosity: damping
		fd := math.Exp(-damp * dt)
		for k := range u {
			u[k] *= fd
			v[k] *= fd
		}
		// 4. Incompressible: remove the divergence (Jacobi pressure solve), walls at the edges
		for y := range ny {
			for x := range nx {
				div[y*nx+x] = (u[idx(x+1, y)] - u[idx(x-1, y)] + v[idx(x, y+1)] - v[idx(x, y-1)]) * 0.5
			}
		}
		for k := range p {
			p[k] = 0
		}
		for range 30 {
			for y := range ny {
				for x := range nx {
					p[y*nx+x] = (p[idx(x+1, y)] + p[idx(x-1, y)] + p[idx(x, y+1)] + p[idx(x, y-1)] - div[y*nx+x]) * 0.25
				}
			}
		}
		for y := range ny {
			for x := range nx {
				k := y*nx + x
				u[k] -= (p[idx(x+1, y)] - p[idx(x-1, y)]) * 0.5
				v[k] -= (p[idx(x, y+1)] - p[idx(x, y-1)]) * 0.5
				if x == 0 || x == nx-1 {
					u[k] = 0
				}
				if y == 0 || y == ny-1 {
					v[k] = 0
				}
			}
		}
		// 5. Grid → particles, move
		for i := range n {
			vx[i], vy[i] = sample(u, px0[i], py0[i]), sample(v, px0[i], py0[i])
			px0[i] = math.Max(0, math.Min(float64(f.W-1), px0[i]+vx[i]*dt))
			py0[i] = math.Max(0, math.Min(float64(f.H-1), py0[i]+vy[i]*dt))
		}
		if st%sub == 0 {
			store(st / sub)
		}
	}
}

type fluidAccum struct{ ax, ay, bx, by, w []float32 }

// Render draws instant k (0..1) of the transition into dst (w×h). sa / sb are the two images as
// they are shown now (with their motion).
//
// The particles are material points of the fluid. Each one knows where it started and where it
// will be at the end, so splatting those coordinates at its current position gives, for every
// pixel, where the old image comes from (carried by the flow so far) and where the new one is
// going (carried by the rest of the flow). Both images are drawn warped by the same flow and
// crossfaded: exactly the old image at k = 0, exactly the new one at k = 1, sharp in between.
func (f *Fluid) Render(dst *image.RGBA, k float64, sa, sb *sampler) {
	k = math.Max(0, math.Min(1, k))
	ft := k * float64(f.snaps-1)
	s0 := min(int(ft), f.snaps-2)
	fr := ft - float64(s0)
	last := (f.snaps - 1) * f.n * 2
	hw, hh := (f.W+1)/2, (f.H+1)/2
	workers := runtime.NumCPU()
	accs := make([]*fluidAccum, workers)
	var wg sync.WaitGroup
	rad := f.cell * 0.5 * 1.6 // half-res pixels
	sig2 := 2 * (f.cell * 0.5 * 0.75) * (f.cell * 0.5 * 0.75)
	R := int(math.Ceil(rad))
	for w := range workers {
		acc, _ := f.accum.Get().(*fluidAccum)
		if acc == nil || len(acc.w) != hw*hh {
			mk := func() []float32 { return make([]float32, hw*hh) }
			acc = &fluidAccum{mk(), mk(), mk(), mk(), mk()}
		} else {
			clear(acc.ax)
			clear(acc.ay)
			clear(acc.bx)
			clear(acc.by)
			clear(acc.w)
		}
		accs[w] = acc
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; i < f.n; i += workers {
				b0, b1 := s0*f.n*2+2*i, (s0+1)*f.n*2+2*i
				x := float64(f.pos[b0]) + float64(f.pos[b1]-f.pos[b0])*fr
				y := float64(f.pos[b0+1]) + float64(f.pos[b1+1]-f.pos[b0+1])*fr
				ax, ay := f.pos[2*i], f.pos[2*i+1]           // where it started
				bx, by := f.pos[last+2*i], f.pos[last+2*i+1] // where it ends
				hx, hy := x*0.5, y*0.5
				ix, iy := int(hx), int(hy)
				for yy := iy - R; yy <= iy+R; yy++ {
					if yy < 0 || yy >= hh {
						continue
					}
					dy := float64(yy) + 0.5 - hy
					for xx := ix - R; xx <= ix+R; xx++ {
						if xx < 0 || xx >= hw {
							continue
						}
						dx := float64(xx) + 0.5 - hx
						d2 := dx*dx + dy*dy
						if d2 > rad*rad {
							continue
						}
						wgt := float32(math.Exp(-d2 / sig2))
						o := yy*hw + xx
						acc.ax[o] += ax * wgt
						acc.ay[o] += ay * wgt
						acc.bx[o] += bx * wgt
						acc.by[o] += by * wgt
						acc.w[o] += wgt
					}
				}
			}
		}(w)
	}
	wg.Wait()
	sum := accs[0]
	for _, a := range accs[1:] {
		for o := range sum.w {
			sum.ax[o] += a.ax[o]
			sum.ay[o] += a.ay[o]
			sum.bx[o] += a.bx[o]
			sum.by[o] += a.by[o]
			sum.w[o] += a.w[o]
		}
		f.accum.Put(a)
	}
	// Normalize; where no particle covers, keep the pixel itself
	for y := 0; y < hh; y++ {
		for x := 0; x < hw; x++ {
			o := y*hw + x
			if w := sum.w[o]; w > 1e-3 {
				sum.ax[o] /= w
				sum.ay[o] /= w
				sum.bx[o] /= w
				sum.by[o] /= w
			} else {
				px, py := float32(x*2+1), float32(y*2+1)
				sum.ax[o], sum.ay[o], sum.bx[o], sum.by[o] = px, py, px, py
			}
		}
	}
	// The new image takes over in the second half, while both are still swirling
	mixB := smooth01((k - 0.3) / 0.55)
	parallelFor(f.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			hy := (float64(y)+0.5)*0.5 - 0.5
			j0 := min(max(int(math.Floor(hy)), 0), hh-1)
			j1 := min(j0+1, hh-1)
			fy := float32(math.Max(0, math.Min(1, hy-float64(j0))))
			for x := 0; x < f.W; x++ {
				hx := (float64(x)+0.5)*0.5 - 0.5
				i0 := min(max(int(math.Floor(hx)), 0), hw-1)
				i1 := min(i0+1, hw-1)
				fx := float32(math.Max(0, math.Min(1, hx-float64(i0))))
				bil := func(g []float32) float64 {
					a := g[j0*hw+i0] + (g[j0*hw+i1]-g[j0*hw+i0])*fx
					b := g[j1*hw+i0] + (g[j1*hw+i1]-g[j1*hw+i0])*fx
					return float64(a + (b-a)*fy)
				}
				var A, B rgb
				if mixB < 1 {
					A = sa.at(bil(sum.ax), bil(sum.ay))
				}
				if mixB > 0 {
					B = sb.at(bil(sum.bx), bil(sum.by))
				}
				c := mixc(A, B, mixB)
				o := y*dst.Stride + x*4
				dst.Pix[o] = uint8(math.Max(0, math.Min(255, c[0]+0.5)))
				dst.Pix[o+1] = uint8(math.Max(0, math.Min(255, c[1]+0.5)))
				dst.Pix[o+2] = uint8(math.Max(0, math.Min(255, c[2]+0.5)))
				dst.Pix[o+3] = 255
			}
		}
	})
	f.accum.Put(sum)
}

// NewSampler exposes the layer sampler to the engine (the fluid renders with the images' motion)
func NewSampler(img *image.RGBA, l Layer, w, h int) *sampler {
	s := newSampler(img, l, w, h)
	return &s
}
