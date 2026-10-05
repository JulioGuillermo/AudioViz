package effects

import (
	"image"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"
)

// Fluid transition ("pixels that travel"): every small piece of the old image is a particle that
// travels on its own to a place of similar color in the new one.
//
//  1. Matching. The old image is cut into particles (one per ~3 px block at 1080p, with its
//     color) and the new one into as many slots. Each particle gets a slot of similar color, not
//     too far (sliced optimal transport in color + position space); every slot gets one.
//  2. Simulation (once per transition, fixed step and seed, positions stored). Each particle
//     heads for its slot on its own (leaving at its own moment), with inertia. Particles are
//     solid and cannot overlap: they push each other, get compressed, give way and then head
//     back to their slots, so the paths come out of the collisions, like in a fluid.
//  3. Rendering. Each frame draws the particles where they are, with a color going from the old
//     pixel to the new one as each one arrives. It starts as the old image and ends as the new.

// FluidOpts: 0..1 knobs (0 = default)
type FluidOpts struct {
	ColorWeight float64 // 0: particles stay near, 1: they go to the most similar color anywhere
	Interaction float64 // how solid the particles are: 1 = they cannot overlap at all
	Stagger     float64 // how spread out in time the departures are
	Seed        uint32
}

// Fluid is a simulated transition, ready to render any instant of it
type Fluid struct {
	W, H       int
	gx, gy     int     // particle grid
	cell       float64 // particle spacing in pixels
	n, snaps   int
	pos        []int16  // snaps × n × 2, in 1/8 pixel
	colA, colB []uint32 // per particle: its color and the color of its slot (RGB)
	sx, sy     []float32
	tx, ty     []float32
	acc        []float32 // per-frame splat buffer (r, g, b, w per pixel)
	mu         sync.Mutex
}

func px(img *image.RGBA, x, y int) uint32 {
	o := y*img.Stride + x*4
	p := img.Pix[o : o+4 : o+4]
	return uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
}

func ch(c uint32, i int) float64 { return float64((c >> (8 * i)) & 255) }

func smooth01(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// NewFluid matches and simulates a transition from a to b (both w×h) lasting `frames` frames
func NewFluid(a, b *image.RGBA, w, h, frames int, o FluidOpts) *Fluid {
	def := func(v *float64, d float64) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&o.ColorWeight, 0.6)
	def(&o.Interaction, 0.5)
	def(&o.Stagger, 0.5)
	f := &Fluid{W: w, H: h}
	f.cell = math.Max(2, float64(h)/270) // 4 px at 1080p: ~130,000 particles at 16:9
	f.gx, f.gy = max(8, int(float64(w)/f.cell)), max(8, int(float64(h)/f.cell))
	f.n = f.gx * f.gy
	bx, by := float64(w)/float64(f.gx), float64(h)/float64(f.gy)
	block := func(img *image.RGBA, i int) uint32 {
		cx, cy := i%f.gx, i/f.gx
		x0, y0 := int(float64(cx)*bx), int(float64(cy)*by)
		x1, y1 := max(x0+1, min(w, int(float64(cx+1)*bx))), max(y0+1, min(h, int(float64(cy+1)*by)))
		var r, g, bb, nn float64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
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
	f.sx, f.sy = make([]float32, f.n), make([]float32, f.n)
	f.tx, f.ty = make([]float32, f.n), make([]float32, f.n)
	for i, j := range match {
		f.colB[i] = cb[j]
		f.sx[i], f.sy[i] = float32((float64(i%f.gx)+0.5)*bx), float32((float64(i/f.gx)+0.5)*by)
		f.tx[i], f.ty[i] = float32((float64(j%f.gx)+0.5)*bx), float32((float64(j/f.gx)+0.5)*by)
	}
	f.simulate(frames, o)
	return f
}

// sliceMatch: sliced optimal transport between the particles (old colors at their places) and
// the slots (new colors at theirs) in a 5D space (r, g, b, λx, λy); returns the slot of each particle
func sliceMatch(ca, cb []uint32, gx, gy int, o FluidOpts) []int {
	n := len(ca)
	lam := 2.5 * math.Pow(0.06, o.ColorWeight) // 2.5 (position matters) .. 0.15 (color matters)
	feat := func(c []uint32, i int) [5]float32 {
		return [5]float32{float32(ch(c[i], 0) / 255), float32(ch(c[i], 1) / 255), float32(ch(c[i], 2) / 255),
			float32(lam * float64(i%gx) / float64(gy)), float32(lam * float64(i/gx) / float64(gy))}
	}
	A, B := make([][5]float32, n), make([][5]float32, n)
	for i := range n {
		A[i], B[i] = feat(ca, i), feat(cb, i)
	}
	rng := rand.New(rand.NewPCG(uint64(o.Seed)+1, 0x9e3779b97f4a7c15))
	ia, ib := make([]int32, n), make([]int32, n)
	pa, pb := make([]float32, n), make([]float32, n)
	for i := range n {
		ia[i], ib[i] = int32(i), int32(i)
	}
	const iters = 40
	for it := range iters {
		var d [5]float32
		var l float64
		for k := range d {
			x := rng.NormFloat64()
			d[k], l = float32(x), l+x*x
		}
		for k := range d {
			d[k] /= float32(math.Sqrt(l))
		}
		for i := range n {
			pa[i] = A[i][0]*d[0] + A[i][1]*d[1] + A[i][2]*d[2] + A[i][3]*d[3] + A[i][4]*d[4]
			pb[i] = B[i][0]*d[0] + B[i][1]*d[1] + B[i][2]*d[2] + B[i][3]*d[3] + B[i][4]*d[4]
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); slices.SortFunc(ia, func(x, y int32) int { return cmpF(pa[x], pa[y]) }) }()
		go func() { defer wg.Done(); slices.SortFunc(ib, func(x, y int32) int { return cmpF(pb[x], pb[y]) }) }()
		wg.Wait()
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
		out[ia[r]] = int(ib[r])
	}
	return out
}

func cmpF(a, b float32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// simulate moves the particles and stores their positions at `snaps` instants. Each particle
// only wants to reach its slot; there is no prescribed path. Particles are solid: two cannot
// overlap, so a moving particle pushes the ones in its way, which give way, get compressed and
// then head back to their own slots. The paths come out of those collisions.
func (f *Fluid) simulate(frames int, o FluidOpts) {
	f.snaps = max(2, min(frames, 120))
	n := f.n
	f.pos = make([]int16, f.snaps*n*2)
	px, py := make([]float32, n), make([]float32, n)
	vx, vy := make([]float32, n), make([]float32, n)
	qx, qy := make([]float32, n), make([]float32, n) // predicted positions
	dx, dy := make([]float32, n), make([]float32, n) // collision corrections
	t0, t1 := make([]float32, n), make([]float32, n)
	diag := math.Hypot(float64(f.W), float64(f.H))
	for i := range n {
		px[i], py[i] = f.sx[i], f.sy[i]
		h := hash3(uint32(i), 1, o.Seed)
		// Departure: spread over the first part (stagger); far travelers leave earlier
		dist := math.Hypot(float64(f.tx[i]-f.sx[i]), float64(f.ty[i]-f.sy[i])) / diag
		t0[i] = float32(o.Stagger * 0.35 * (0.6*h + 0.4*(1-math.Min(1, dist*2))))
		// Arrival time it aims for, also staggered
		t1[i] = float32(0.86 - o.Stagger*0.16*hash3(uint32(i), 2, o.Seed))
	}
	store := func(s int) {
		base := s * n * 2
		for i := range n {
			f.pos[base+2*i] = int16(math.Round(float64(px[i]) * 8))
			f.pos[base+2*i+1] = int16(math.Round(float64(py[i]) * 8))
		}
	}
	store(0)
	sub := 2
	steps := (f.snaps - 1) * sub
	dt := float32(1.0 / float64(steps))
	// Particle size: the spacing (full packing: they really have to push each other), a bit
	// smaller with low interaction (they slip past each other more easily)
	diam := float32(f.cell * (0.55 + 0.45*o.Interaction))
	// Neighbor grid: cells as big as a particle
	cs := float64(diam)
	nx, ny := int(float64(f.W)/cs)+1, int(float64(f.H)/cs)+1
	start := make([]int32, nx*ny+1)
	cellOf := make([]int32, n)
	order := make([]int32, n)
	vmax := float32(diag * 4)
	workers := runtime.NumCPU()
	par := func(fn func(lo, hi int)) {
		var wg sync.WaitGroup
		chunk := (n + workers - 1) / workers
		for w := range workers {
			lo, hi := w*chunk, min(n, (w+1)*chunk)
			if lo >= hi {
				continue
			}
			wg.Add(1)
			go func() { defer wg.Done(); fn(lo, hi) }()
		}
		wg.Wait()
	}
	for st := 1; st <= steps; st++ {
		t := float32(st) * dt
		// 1. Each particle wants to cover what is left of its way in the time it has left (so the
		// journey spreads over the whole transition), starting gently; predicts its position
		par(func(lo, hi int) {
			for i := lo; i < hi; i++ {
				var wx, wy float32
				if t >= t0[i] {
					ramp := float32(smooth01(float64((t - t0[i]) / 0.18)))
					gain := ramp / max(0.05, t1[i]-t)
					wx, wy = (f.tx[i]-px[i])*gain, (f.ty[i]-py[i])*gain
					if l := float32(math.Hypot(float64(wx), float64(wy))); l > vmax {
						wx, wy = wx*vmax/l, wy*vmax/l
					}
				}
				// Inertia: it turns toward the wanted velocity, not instantly
				vx[i] += (wx - vx[i]) * min(1, 18*dt)
				vy[i] += (wy - vy[i]) * min(1, 18*dt)
				qx[i] = float32(math.Max(0, math.Min(float64(f.W)-0.01, float64(px[i]+vx[i]*dt))))
				qy[i] = float32(math.Max(0, math.Min(float64(f.H)-0.01, float64(py[i]+vy[i]*dt))))
			}
		})
		// 2. Collisions: two particles cannot overlap. Each overlapping pair is pushed apart
		// (half each); two passes per step
		for range 2 {
			clear(start)
			for i := range n {
				c := int32(int(float64(qy[i])/cs)*nx + int(float64(qx[i])/cs))
				cellOf[i] = c
				start[c+1]++
			}
			for c := 1; c < len(start); c++ {
				start[c] += start[c-1]
			}
			fill := make([]int32, nx*ny)
			for i := range n {
				c := cellOf[i]
				order[start[c]+fill[c]] = int32(i)
				fill[c]++
			}
			par(func(lo, hi int) {
				for i := lo; i < hi; i++ {
					cx, cy := int(float64(qx[i])/cs), int(float64(qy[i])/cs)
					var sx, sy float32
					for yy := max(0, cy-1); yy <= min(ny-1, cy+1); yy++ {
						for xx := max(0, cx-1); xx <= min(nx-1, cx+1); xx++ {
							c := yy*nx + xx
							for _, j := range order[start[c]:start[c+1]] {
								if int(j) == i {
									continue
								}
								ex, ey := qx[i]-qx[j], qy[i]-qy[j]
								d2 := ex*ex + ey*ey
								if d2 >= diam*diam {
									continue
								}
								d := float32(math.Sqrt(float64(d2)))
								if d < 1e-4 { // exactly on top: separate along a fixed direction
									ex, ey, d = float32(hash3(uint32(i), uint32(j), 7)-0.5), float32(hash3(uint32(j), uint32(i), 9)-0.5), 1e-4
								}
								push := (diam - d) * 0.5 / max(d, 1e-4)
								sx += ex * push
								sy += ey * push
							}
						}
					}
					dx[i], dy[i] = sx*0.8, sy*0.8
				}
			})
			par(func(lo, hi int) {
				for i := lo; i < hi; i++ {
					qx[i] = float32(math.Max(0, math.Min(float64(f.W)-0.01, float64(qx[i]+dx[i]))))
					qy[i] = float32(math.Max(0, math.Min(float64(f.H)-0.01, float64(qy[i]+dy[i]))))
				}
			})
		}
		// 3. The velocity is what really happened (pushes included), a little damped
		par(func(lo, hi int) {
			for i := lo; i < hi; i++ {
				vx[i] = (qx[i] - px[i]) / dt * 0.98
				vy[i] = (qy[i] - py[i]) / dt * 0.98
				px[i], py[i] = qx[i], qy[i]
			}
		})
		if st%sub == 0 {
			store(st / sub)
		}
	}
}

// Render draws instant k (0..1) of the transition into dst (w×h). sa / sb are the two images as
// they are shown now (with their motion): it starts exactly on the first and ends on the second.
func (f *Fluid) Render(dst *image.RGBA, k float64, sa, sb *sampler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k = math.Max(0, math.Min(1, k))
	ft := k * float64(f.snaps-1)
	s0 := min(int(ft), f.snaps-2)
	fr := float32(ft - float64(s0))
	settle := float32(smooth01((k - 0.9) / 0.1))
	W, H := f.W, f.H
	if len(f.acc) != W*H*4 {
		f.acc = make([]float32, W*H*4)
	}
	clear(f.acc)
	// Current position of every particle; particles sorted into horizontal bands so that each
	// worker splats into its own rows
	n := f.n
	xs, ys := make([]float32, n), make([]float32, n)
	bands := runtime.NumCPU() * 4
	bandH := (H + bands - 1) / bands
	R := 2 // splat radius in pixels (the particles are ~3 px apart at 1080p)
	if f.cell > 3 {
		R = int(math.Ceil(f.cell * 0.75))
	}
	lists := make([][]int32, bands)
	for i := range n {
		b0, b1 := s0*n*2+2*i, (s0+1)*n*2+2*i
		x := (float32(f.pos[b0]) + float32(f.pos[b1]-f.pos[b0])*fr) / 8
		y := (float32(f.pos[b0+1]) + float32(f.pos[b1+1]-f.pos[b0+1])*fr) / 8
		x += (f.tx[i] - x) * settle
		y += (f.ty[i] - y) * settle
		xs[i], ys[i] = x, y
		for b := max(0, (int(y)-R)/bandH); b <= min(bands-1, (int(y)+R)/bandH); b++ {
			lists[b] = append(lists[b], int32(i))
		}
	}
	sig2 := float32(2 * (f.cell * 0.55) * (f.cell * 0.55))
	var wg sync.WaitGroup
	jobs := make(chan int, bands)
	for b := range bands {
		jobs <- b
	}
	close(jobs)
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range jobs {
				ylo, yhi := b*bandH, min(H, (b+1)*bandH)
				for _, i := range lists[b] {
					x, y := xs[i], ys[i]
					// Its color goes from the old pixel to the new one as it gets there
					dd := math.Hypot(float64(f.tx[i]-f.sx[i]), float64(f.ty[i]-f.sy[i]))
					rem := math.Hypot(float64(f.tx[i]-x), float64(f.ty[i]-y))
					p := 1.0
					if dd > 1 {
						p = 1 - math.Min(1, rem/dd)
					}
					p = smooth01(math.Max(p, (k-0.75)/0.25))
					ca, cb := f.colA[i], f.colB[i]
					cr := float32(ch(ca, 0) + (ch(cb, 0)-ch(ca, 0))*p)
					cg := float32(ch(ca, 1) + (ch(cb, 1)-ch(ca, 1))*p)
					cbb := float32(ch(ca, 2) + (ch(cb, 2)-ch(ca, 2))*p)
					ix, iy := int(x), int(y)
					for yy := max(ylo, iy-R); yy <= min(yhi-1, iy+R); yy++ {
						dy := float32(yy) + 0.5 - y
						for xx := max(0, ix-R); xx <= min(W-1, ix+R); xx++ {
							dx := float32(xx) + 0.5 - x
							wgt := float32(math.Exp(float64(-(dx*dx + dy*dy) / sig2)))
							o := (yy*W + xx) * 4
							f.acc[o] += cr * wgt
							f.acc[o+1] += cg * wgt
							f.acc[o+2] += cbb * wgt
							f.acc[o+3] += wgt
						}
					}
				}
			}
		}()
	}
	wg.Wait()
	// Coarse level (1/8 resolution, box sums): where the particles moved apart, the gap takes
	// the color of the particles around it, like a continuous fluid
	cw, chh := (W+7)/8, (H+7)/8
	coarse := make([]float32, cw*chh*4)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			o, c := (y*W+x)*4, ((y/8)*cw+x/8)*4
			coarse[c] += f.acc[o]
			coarse[c+1] += f.acc[o+1]
			coarse[c+2] += f.acc[o+2]
			coarse[c+3] += f.acc[o+3]
		}
	}
	// Two passes of a 3×3 box blur on the coarse level: smooth fill, no blocks
	tmp := make([]float32, len(coarse))
	for range 2 {
		for y := 0; y < chh; y++ {
			for x := 0; x < cw; x++ {
				var acc [4]float32
				for yy := max(0, y-1); yy <= min(chh-1, y+1); yy++ {
					for xx := max(0, x-1); xx <= min(cw-1, x+1); xx++ {
						o := (yy*cw + xx) * 4
						acc[0] += coarse[o]
						acc[1] += coarse[o+1]
						acc[2] += coarse[o+2]
						acc[3] += coarse[o+3]
					}
				}
				copy(tmp[(y*cw+x)*4:], acc[:])
			}
		}
		coarse, tmp = tmp, coarse
	}
	in := smooth01(k / 0.05)
	out := smooth01((k - 0.93) / 0.07)
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				o := (y*W + x) * 4
				w := float64(f.acc[o+3])
				A, B := sa.at(float64(x), float64(y)), sb.at(float64(x), float64(y))
				// Coarse color around (bilinear on the 1/8 grid)
				qx, qy := (float64(x)+0.5)/8-0.5, (float64(y)+0.5)/8-0.5
				c0, r0 := min(max(int(math.Floor(qx)), 0), cw-1), min(max(int(math.Floor(qy)), 0), chh-1)
				c1, r1 := min(c0+1, cw-1), min(r0+1, chh-1)
				gx, gy := math.Max(0, math.Min(1, qx-float64(c0))), math.Max(0, math.Min(1, qy-float64(r0)))
				cb := func(ch int) float64 {
					a := float64(coarse[(r0*cw+c0)*4+ch])*(1-gx) + float64(coarse[(r0*cw+c1)*4+ch])*gx
					b := float64(coarse[(r1*cw+c0)*4+ch])*(1-gx) + float64(coarse[(r1*cw+c1)*4+ch])*gx
					return a*(1-gy) + b*gy
				}
				const alpha = 0.0003 // the blurred coarse cells sum 64 × 81 pixels
				ww := w + alpha*cb(3) + 1e-6
				base := mixc(A, B, k) // only where there is nothing at all nearby
				c := rgb{
					(float64(f.acc[o]) + alpha*cb(0) + base[0]*1e-6) / ww,
					(float64(f.acc[o+1]) + alpha*cb(1) + base[1]*1e-6) / ww,
					(float64(f.acc[o+2]) + alpha*cb(2) + base[2]*1e-6) / ww,
				}
				c = mixc(A, c, in)
				c = mixc(c, B, out)
				d := y*dst.Stride + x*4
				dst.Pix[d] = uint8(math.Max(0, math.Min(255, c[0]+0.5)))
				dst.Pix[d+1] = uint8(math.Max(0, math.Min(255, c[1]+0.5)))
				dst.Pix[d+2] = uint8(math.Max(0, math.Min(255, c[2]+0.5)))
				dst.Pix[d+3] = 255
			}
		}
	})
}

// NewSampler exposes the layer sampler to the engine (the fluid renders with the images' motion)
func NewSampler(img *image.RGBA, l Layer, w, h int) *sampler {
	s := newSampler(img, l, w, h)
	return &s
}
