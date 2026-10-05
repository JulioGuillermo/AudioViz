package effects

import (
	"image"
	"math"
	"math/rand/v2"
	"os"
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
//  3. Rendering. Each frame draws every particle as a crisp square carrying its own piece of the
//     image (the pixels of its block in the old image, changing to those of its slot in the new
//     one as it arrives). At rest the squares tile the frame: it starts exactly as the old image
//     and ends exactly as the new one. Only where particles left a gap, a soft fill shows.

// FluidOpts: 0..1 knobs (0 = default)
type FluidOpts struct {
	ColorWeight float64 // 0: particles stay near, 1: they go to the most similar color anywhere
	Interaction float64 // how solid the particles are: 1 = they cannot overlap at all
	Stagger     float64 // how spread out in time the departures are
	Seed        uint32
	// Liquid: the particles move as a liquid instead of a swarm (the "liquid" transition):
	// neighbors share their velocity (viscosity), pressure keeps the density even in both
	// directions (crowds spread out, gaps fill in) and whole areas leave together
	Liquid    bool
	Viscosity float64 // liquid: how much neighbors move together
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
	liquid     bool
	delay      []float32 // liquid: when each particle starts changing color
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
	if o.Liquid {
		def(&o.ColorWeight, 0.3) // a liquid flows more locally
		def(&o.Viscosity, 0.6)
	}
	def(&o.ColorWeight, 0.6)
	def(&o.Interaction, 0.5)
	def(&o.Stagger, 0.5)
	f := &Fluid{W: w, H: h, liquid: o.Liquid}
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
	return sliceOT(A, B, o.Seed)
}

// sliceOT: sliced optimal transport between two equal-size point sets; returns the B index of each A
func sliceOT(A, B [][5]float32, seed uint32) []int {
	n := len(A)
	rng := rand.New(rand.NewPCG(uint64(seed)+1, 0x9e3779b97f4a7c15))
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
		h2 := hash3(uint32(i), 2, o.Seed)
		if o.Liquid {
			// Whole areas leave (and arrive) together: smooth noise of the place, not per particle
			u, v := float64(f.sx[i])/float64(f.H)*2.5, float64(f.sy[i])/float64(f.H)*2.5
			h = fbm(u, v, o.Seed+31)*1.6 - 0.3
			h2 = fbm(u+9.1, v+3.7, o.Seed+57)*1.6 - 0.3
			h, h2 = math.Max(0, math.Min(1, h)), math.Max(0, math.Min(1, h2))
		}
		// Departure: spread over the first part (stagger); far travelers leave earlier
		dist := math.Hypot(float64(f.tx[i]-f.sx[i]), float64(f.ty[i]-f.sy[i])) / diag
		t0[i] = float32(o.Stagger * 0.35 * (0.6*h + 0.4*(1-math.Min(1, dist*2))))
		// Arrival time it aims for, also staggered
		t1[i] = float32(0.86 - o.Stagger*0.16*h2)
	}
	// Liquid: where the particle's area as a whole is going (the displacements of its neighbors,
	// smoothed): it follows that stream first and only near the end heads for its exact slot
	var flow *flowField
	if o.Liquid {
		flow = f.streamField(8)
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
	// Liquid: coarse grid (3 particles per cell side) for density and mean velocity
	gc := float32(f.cell * 3)
	lnx, lny := int(float32(f.W)/gc)+2, int(float32(f.H)/gc)+2
	var den, mvx, mvy []float32
	if o.Liquid {
		den, mvx, mvy = make([]float32, lnx*lny), make([]float32, lnx*lny), make([]float32, lnx*lny)
	}
	rest := (gc / (float32(f.W) / float32(f.gx))) * (gc / (float32(f.H) / float32(f.gy))) // particles per cell at rest
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
				if flow != nil {
					// Liquid: carried by the incompressible stream (no own goal), so the density
					// stays even: the liquid moves and turns without holes. The rate integrates to
					// 1 over the transition and slows down to rest at the end
					fu, fv := flow.at(px[i], py[i])
					rate := float32(math.Pi / 2 * math.Sin(math.Pi*float64(t)))
					// The stream kept only the turning part of the wanted motion (smaller): amplified
					const streamGain = 2.2
					wx, wy = fu*rate*streamGain, fv*rate*streamGain
				} else if t >= t0[i] {
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
		if o.Liquid {
			f.liquidStep(px, py, vx, vy, qx, qy, den, mvx, mvy, gc, lnx, lny, rest, t, dt, o, par)
		}
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
	if o.Liquid {
		// Each particle ends where the stream left it: that is its place in the new image (the
		// flow is incompressible, so those places cover the frame). It changes color little by
		// little from its piece of the old image to the piece of the new one there, each area at
		// its own moment
		f.delay = make([]float32, n)
		for i := range n {
			f.tx[i], f.ty[i] = px[i], py[i]
			u, v := float64(f.sx[i])/float64(f.H)*2, float64(f.sy[i])/float64(f.H)*2
			f.delay[i] = float32(0.12 + 0.4*math.Max(0, math.Min(1, fbm(u, v, o.Seed+91)*1.8-0.4)))
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
	// Each particle is drawn as a crisp square the size of its block, carrying its own piece of
	// the image: at rest the squares tile the frame exactly (the old image, pixel for pixel)
	hbw, hbh := float32(f.W)/float32(f.gx)*0.5, float32(f.H)/float32(f.gy)*0.5
	if f.liquid {
		// A liquid has no gaps between its drops: slightly bigger pieces close the small ones
		// (at rest the overlap only blends each piece with its neighbors' edges)
		hbw, hbh = hbw*1.3, hbh*1.3
	}
	R := int(math.Ceil(float64(hbh))) + 1
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
					// From its old piece to its new piece as it gets there
					dd := math.Hypot(float64(f.tx[i]-f.sx[i]), float64(f.ty[i]-f.sy[i]))
					rem := math.Hypot(float64(f.tx[i]-x), float64(f.ty[i]-y))
					p := 1.0
					if dd > 1 {
						p = 1 - math.Min(1, rem/dd)
					}
					p = smooth01(math.Max(p, (k-0.8)/0.2))
					if f.liquid {
						// Liquid: the color changes little by little, each area at its own moment
						p = smooth01((k - float64(f.delay[i])) / 0.42)
					}
					// Square [x-hbw, x+hbw) × [y-hbh, y+hbh), antialiased by pixel coverage
					x0, x1, y0, y1 := x-hbw, x+hbw, y-hbh, y+hbh
					for yy := max(ylo, int(math.Floor(float64(y0)))); yy <= min(yhi-1, int(math.Ceil(float64(y1)))-1); yy++ {
						cy := min(float32(yy+1), y1) - max(float32(yy), y0)
						if cy <= 0 {
							continue
						}
						for xx := max(0, int(math.Floor(float64(x0)))); xx <= min(W-1, int(math.Ceil(float64(x1)))-1); xx++ {
							cx := min(float32(xx+1), x1) - max(float32(xx), x0)
							if cx <= 0 {
								continue
							}
							wgt := cx * cy
							// The piece it carries: the same offset inside its block, in each image
							ox, oy := float64(xx)+0.5-float64(x), float64(yy)+0.5-float64(y)
							var c rgb
							if p < 1 {
								c = sa.at(float64(f.sx[i])+ox-0.5, float64(f.sy[i])+oy-0.5)
							}
							if p > 0 {
								c = mixc(c, sb.at(float64(f.tx[i])+ox-0.5, float64(f.ty[i])+oy-0.5), p)
							}
							o := (yy*W + xx) * 4
							f.acc[o] += float32(c[0]) * wgt
							f.acc[o+1] += float32(c[1]) * wgt
							f.acc[o+2] += float32(c[2]) * wgt
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
	out := smooth01((k - 0.96) / 0.04)
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
				// Covered pixels show their particles as they are; only the uncovered part of a pixel
				// takes the soft color of the particles around (a gap in the fluid)
				var c rgb
				if w > 1e-4 {
					c = rgb{float64(f.acc[o]) / w, float64(f.acc[o+1]) / w, float64(f.acc[o+2]) / w}
				}
				if w < 1 {
					// Where the area is almost empty (e.g. edges the particles left), fade to a
					// dim mix of both images instead of stretching a few particles' colors
					dim := mixc(A, B, k)
					dim = rgb{dim[0] * 0.5, dim[1] * 0.5, dim[2] * 0.5}
					g := dim
					if cw := cb(3); cw > 1e-3 {
						g = mixc(dim, rgb{cb(0) / cw, cb(1) / cw, cb(2) / cw}, smooth01(cw/40))
					}
					c = mixc(g, c, w)
				}
				if fluidDebug && w < 0.5 {
					c = rgb{255, 0, 255}
				}
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

// liquidStep makes the particles behave as a liquid (the prediction qx, qy is redone):
// viscosity (each one takes part of its neighbors' mean velocity) and pressure in both
// directions (particles move down the density gradient: out of crowds and into gaps).
// The viscosity relaxes at the end so that every particle can settle on its own slot.
func (f *Fluid) liquidStep(px, py, vx, vy, qx, qy, den, mvx, mvy []float32, gc float32, nx, ny int,
	rest, t, dt float32, o FluidOpts, par func(func(lo, hi int))) {
	clear(den)
	clear(mvx)
	clear(mvy)
	for i := range px {
		gx, gy := px[i]/gc, py[i]/gc
		x0, y0 := int(gx), int(gy)
		fx, fy := gx-float32(x0), gy-float32(y0)
		for _, c := range [4]struct {
			k int
			w float32
		}{{y0*nx + x0, (1 - fx) * (1 - fy)}, {y0*nx + x0 + 1, fx * (1 - fy)}, {(y0+1)*nx + x0, (1 - fx) * fy}, {(y0+1)*nx + x0 + 1, fx * fy}} {
			den[c.k] += c.w
			mvx[c.k] += vx[i] * c.w
			mvy[c.k] += vy[i] * c.w
		}
	}
	for k := range den {
		if den[k] > 1e-6 {
			mvx[k] /= den[k]
			mvy[k] /= den[k]
		}
		den[k] /= rest // 1 = rest density
	}
	visc := float32(o.Viscosity * 0.9)
	pres := float32(0.45)
	at := func(g []float32, x, y int) float32 { return g[min(max(y, 0), ny-1)*nx+min(max(x, 0), nx-1)] }
	par(func(lo, hi int) {
		for i := lo; i < hi; i++ {
			gx, gy := px[i]/gc, py[i]/gc
			x0, y0 := int(gx), int(gy)
			fx, fy := gx-float32(x0), gy-float32(y0)
			bil := func(g []float32) float32 {
				return (at(g, x0, y0)*(1-fx)+at(g, x0+1, y0)*fx)*(1-fy) + (at(g, x0, y0+1)*(1-fx)+at(g, x0+1, y0+1)*fx)*fy
			}
			// Viscosity: move like the neighbors
			vx[i] += (bil(mvx) - vx[i]) * visc
			vy[i] += (bil(mvy) - vy[i]) * visc
			// Pressure: down the density gradient (central differences, per cell)
			gdx := ((at(den, x0+1, y0)-at(den, x0-1, y0))*(1-fy) + (at(den, x0+2, y0+1)-at(den, x0, y0+1))*fy) * 0.5
			gdy := ((at(den, x0, y0+1)-at(den, x0, y0-1))*(1-fx) + (at(den, x0+1, y0+2)-at(den, x0+1, y0))*fx) * 0.5
			qx[i] = float32(math.Max(0, math.Min(float64(f.W)-0.01, float64(px[i]+vx[i]*dt-pres*gc*gdx))))
			qy[i] = float32(math.Max(0, math.Min(float64(f.H)-0.01, float64(py[i]+vy[i]*dt-pres*gc*gdy))))
		}
	})
}

// smoothedTargets: each particle's start plus the mean displacement of the particles around it
// (box blur of radius r particles, two passes) — the stream its area follows
func (f *Fluid) smoothedTargets(r int) ([]float32, []float32) {
	n, gx, gy := f.n, f.gx, f.gy
	dx, dy := make([]float32, n), make([]float32, n)
	for i := range n {
		dx[i], dy[i] = f.tx[i]-f.sx[i], f.ty[i]-f.sy[i]
	}
	tmp := make([]float32, n)
	blur := func(g []float32) {
		for range 2 {
			// rows
			parallelFor(gy, func(y0, y1 int) {
				for y := y0; y < y1; y++ {
					var acc float32
					cnt := 0
					for x := -r; x < gx+r; x++ {
						if x+r < gx {
							acc += g[y*gx+x+r]
							cnt++
						}
						if x-r-1 >= 0 {
							acc -= g[y*gx+x-r-1]
							cnt--
						}
						if x >= 0 && x < gx {
							tmp[y*gx+x] = acc / float32(cnt)
						}
					}
				}
			})
			// columns
			parallelFor(gx, func(x0, x1 int) {
				for x := x0; x < x1; x++ {
					var acc float32
					cnt := 0
					for y := -r; y < gy+r; y++ {
						if y+r < gy {
							acc += tmp[(y+r)*gx+x]
							cnt++
						}
						if y-r-1 >= 0 {
							acc -= tmp[(y-r-1)*gx+x]
							cnt--
						}
						if y >= 0 && y < gy {
							g[y*gx+x] = acc / float32(cnt)
						}
					}
				}
			})
		}
	}
	blur(dx)
	blur(dy)
	ox, oy := make([]float32, n), make([]float32, n)
	for i := range n {
		ox[i] = float32(math.Max(0, math.Min(float64(f.W-1), float64(f.sx[i]+dx[i]))))
		oy[i] = float32(math.Max(0, math.Min(float64(f.H-1), float64(f.sy[i]+dy[i]))))
	}
	return ox, oy
}

// AUDIOVIZ_FLUID_DEBUG=1 paints the gaps (pixels without particles) magenta
var fluidDebug = os.Getenv("AUDIOVIZ_FLUID_DEBUG") != ""

// streamTargets: the stream each area follows (liquid, first stage). The particles' wanted
// displacements are smoothed over their neighborhood and then made divergence-free (Helmholtz
// projection on a coarse grid, no flow through the edges): the liquid moves and turns but is
// neither compressed nor stretched, so it does not shrink toward the center.
func (f *Fluid) streamField(r int) *flowField {
	sx, sy := f.smoothedTargets(r)
	n, gx, gy := f.n, f.gx, f.gy
	dx, dy := make([]float32, n), make([]float32, n)
	for i := range n {
		dx[i], dy[i] = sx[i]-f.sx[i], sy[i]-f.sy[i]
	}
	// Coarse grid (4 particles per cell side)
	const cg = 4
	cx, cy := (gx+cg-1)/cg, (gy+cg-1)/cg
	U, V, cnt := make([]float64, cx*cy), make([]float64, cx*cy), make([]float64, cx*cy)
	for i := range n {
		k := (i/gx/cg)*cx + (i%gx)/cg
		U[k] += float64(dx[i])
		V[k] += float64(dy[i])
		cnt[k]++
	}
	for k := range U {
		if cnt[k] > 0 {
			U[k] /= cnt[k]
			V[k] /= cnt[k]
		}
	}
	// Divergence with zero flow outside the frame
	at := func(g []float64, x, y int) float64 {
		if x < 0 || y < 0 || x >= cx || y >= cy {
			return 0
		}
		return g[y*cx+x]
	}
	div := make([]float64, cx*cy)
	for y := range cy {
		for x := range cx {
			div[y*cx+x] = (at(U, x+1, y) - at(U, x-1, y) + at(V, x, y+1) - at(V, x, y-1)) * 0.5
		}
	}
	// Poisson ∇²φ = div (successive over-relaxation, red-black, Neumann edges): converges far
	// faster than Jacobi, so no compression is left even at large scales
	phi := make([]float64, cx*cy)
	pa := func(x, y int) float64 { return phi[min(max(y, 0), cy-1)*cx+min(max(x, 0), cx-1)] }
	const omega = 1.9
	for range 1500 {
		for color := range 2 {
			for y := range cy {
				for x := (y + color) % 2; x < cx; x += 2 {
					k := y*cx + x
					g := (pa(x+1, y) + pa(x-1, y) + pa(x, y+1) + pa(x, y-1) - div[k]) * 0.25
					phi[k] += omega * (g - phi[k])
				}
			}
		}
	}
	// Subtract ∇φ (bilinear on the particle grid)
	gpx, gpy := make([]float64, cx*cy), make([]float64, cx*cy)
	for y := range cy {
		for x := range cx {
			gpx[y*cx+x] = (pa(x+1, y) - pa(x-1, y)) * 0.5
			gpy[y*cx+x] = (pa(x, y+1) - pa(x, y-1)) * 0.5
		}
	}
	ff := &flowField{cx: cx, cy: cy, cell: float32(f.W) / float32(gx) * cg, u: make([]float32, cx*cy), v: make([]float32, cx*cy)}
	// Near the edges the flow toward them fades out (a wall: the liquid slides along it
	// instead of piling up against it)
	wall := func(i, n int) float32 {
		d := float64(min(i, n-1-i)) / 4 // cells to the edge, 4 cells of transition
		return float32(smooth01(d))
	}
	for k := range U {
		x, y := k%cx, k/cx
		ff.u[k] = float32(U[k]-gpx[k]) * wall(x, cx)
		ff.v[k] = float32(V[k]-gpy[k]) * wall(y, cy)
	}
	return ff
}

// flowField: a displacement field on a coarse grid (pixels), sampled bilinearly anywhere
type flowField struct {
	cx, cy int
	cell   float32
	u, v   []float32
}

func (g *flowField) at(x, y float32) (float32, float32) {
	qx, qy := x/g.cell-0.5, y/g.cell-0.5
	x0, y0 := min(max(int(math.Floor(float64(qx))), 0), g.cx-1), min(max(int(math.Floor(float64(qy))), 0), g.cy-1)
	x1, y1 := min(x0+1, g.cx-1), min(y0+1, g.cy-1)
	fx, fy := min(max(qx-float32(x0), 0), 1), min(max(qy-float32(y0), 0), 1)
	b := func(a []float32) float32 {
		return (a[y0*g.cx+x0]*(1-fx)+a[y0*g.cx+x1]*fx)*(1-fy) + (a[y1*g.cx+x0]*(1-fx)+a[y1*g.cx+x1]*fx)*fy
	}
	return b(g.u), b(g.v)
}
