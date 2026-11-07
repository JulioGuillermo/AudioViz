package effects

import (
	"image"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"sync"

	"audioviz/internal/gpu"
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
	// GPU: simulate on this Vulkan device (-1 = automatic) when possible; the CPU otherwise
	GPU       bool
	GPUDevice int
}

const (
	liquidPressure = 0.45 // how strongly crowds spread out and gaps fill in
	// The stream keeps only the turning part of the wanted motion (smaller): amplified
	streamGain = 2.2
)

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
	liquid     bool
	delay      []float32 // liquid: when each particle starts changing color
	// SimGPU: the simulation ran on the GPU; SimErr: why it could not (it ran on the CPU)
	SimGPU bool
	SimErr error
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
	if o.Liquid {
		// A drop ends where the stream left it, not on its matched slot: its color in the new
		// image (for the gap fill) is the one there
		for i := range f.n {
			f.colB[i] = px(b, min(int(f.tx[i]), w-1), min(int(f.ty[i]), h-1))
		}
	}
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
	ka, kb, kc, kd := make([]uint64, n), make([]uint64, n), make([]uint64, n), make([]uint64, n)
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
		go func() { defer wg.Done(); sortByKey(ia, pa, ka, kb) }()
		go func() { defer wg.Done(); sortByKey(ib, pb, kc, kd) }()
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

// sortByKey orders idx (0..n-1) by key[idx] ascending (ties by index): LSD radix sort of the
// keys' order-preserving bits with the index attached; buf, tmp are scratch of len n
func sortByKey(idx []int32, key []float32, buf, tmp []uint64) {
	for i := range idx {
		b := math.Float32bits(key[i])
		if b&0x80000000 != 0 {
			b = ^b // negative: reversed order
		} else {
			b |= 0x80000000
		}
		buf[i] = uint64(b)<<32 | uint64(i)
	}
	const bits = 11
	var count [1 << bits]int
	for shift := 32; shift < 64; shift += bits {
		clear(count[:])
		for _, v := range buf {
			count[(v>>shift)&(1<<bits-1)]++
		}
		sum := 0
		for d := range count {
			count[d], sum = sum, sum+count[d]
		}
		for _, v := range buf {
			d := (v >> shift) & (1<<bits - 1)
			tmp[count[d]] = v
			count[d]++
		}
		buf, tmp = tmp, buf
	}
	for i, v := range buf {
		idx[i] = int32(uint32(v))
	}
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
	if o.GPU {
		// The same steps on the GPU (fluidsim.comp); on any error, the CPU below
		f.SimGPU = f.simulateGPU(o, flow, t0, t1, diam, cs, nx, ny, vmax, gc, lnx, lny, rest, sub, px, py)
	}
	for st := 1; !f.SimGPU && st <= steps; st++ {
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
								// At most a particle's size per pair: two particles exactly on top of each
								// other (piled in a corner) separate instead of shooting across the frame
								push := (diam - d) * 0.5 / max(d, 1e-4)
								sx += min(max(ex*push, -diam), diam)
								sy += min(max(ey*push, -diam), diam)
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

// simulateGPU runs the simulation on the GPU, filling f.pos and the final positions px, py
func (f *Fluid) simulateGPU(o FluidOpts, flow *flowField, t0, t1 []float32, diam float32, cs float64, nx, ny int,
	vmax, gc float32, lnx, lny int, rest float32, sub int, px, py []float32) bool {
	n := f.n
	in := &gpu.FluidSim{W: f.W, H: f.H, N: n, Liquid: o.Liquid, Ends: make([]float32, n*4), Times: make([]float32, n*2),
		Gain: streamGain, Diam: diam, CellSize: float32(cs), GridW: nx, GridH: ny, VMax: vmax,
		LCell: gc, Rest: rest, Visc: float32(o.Viscosity * 0.9), Pres: liquidPressure, LGridW: lnx, LGridH: lny,
		Snaps: f.snaps, Sub: sub}
	for i := range n {
		in.Ends[4*i], in.Ends[4*i+1], in.Ends[4*i+2], in.Ends[4*i+3] = f.sx[i], f.sy[i], f.tx[i], f.ty[i]
		in.Times[2*i], in.Times[2*i+1] = t0[i], t1[i]
	}
	if flow != nil {
		in.Flow = make([]float32, len(flow.u)*2)
		for k := range flow.u {
			in.Flow[2*k], in.Flow[2*k+1] = flow.u[k], flow.v[k]
		}
		in.FlowW, in.FlowH, in.FlowHX, in.FlowHY = flow.nx, flow.ny, flow.hx, flow.hy
	}
	pos, final, err := gpu.RunFluidSim(o.GPUDevice, in)
	if err != nil {
		f.SimErr = err
		return false
	}
	f.pos = pos
	for i := range n {
		px[i], py[i] = final[2*i], final[2*i+1]
	}
	return true
}

// FluidFrame is one instant of a fluid transition, ready to be drawn on the CPU (Render) or on
// the GPU (fluid.comp, the same per-pixel math). Parts holds, per particle, where
// it is now, how far its color has gone from its piece of the old image to its piece of the new
// one, and where those pieces are. The particles are sorted into bins so that each pixel only
// looks at the ones around it. Coarse is the soft color that fills the gaps (1/8 resolution).
type FluidFrame struct {
	W, H int
	K    float64
	// Per particle 8 values: x, y, p, -, piece in the old image (x, y), in the new one (x, y);
	// sorted by bin (the particles of bin b are BinStart[b] .. BinStart[b+1]-1)
	Parts    []float32
	HBW, HBH float32 // half size of a piece
	Bin      int     // bin side in pixels
	NBX, NBY int
	BinStart []uint32 // NBX*NBY+1
	CW, CH   int
	Coarse   []float32 // CW×CH × (r, g, b sums 0..255, weight), blurred
	// New: per pixel, how much of the new image the pieces bring (filled by Render; the depth
	// follows it: TransPick)
	New []float32
}

// FluidBufWords: an upper bound of the 32-bit words a FluidFrame takes on the GPU at w×h
// (parts, bins, coarse), to size the buffer once
func FluidBufWords(w, h int) int {
	cell := math.Max(2, float64(h)/270)
	n := max(8, int(float64(w)/cell)) * max(8, int(float64(h)/cell))
	nb := (w/2 + 1) * (h/2 + 1) // bins are at least 2 px
	return n*8 + nb + 1 + ((w+7)/8)*((h+7)/8)*4
}

// Frame computes instant k (0..1) of the transition: the particles' positions and colors
func (f *Fluid) Frame(k float64) *FluidFrame {
	k = math.Max(0, math.Min(1, k))
	ft := k * float64(f.snaps-1)
	s0 := min(int(ft), f.snaps-2)
	fr := float32(ft - float64(s0))
	settle := float32(smooth01((k - 0.9) / 0.1))
	W, H, n := f.W, f.H, f.n
	// Each particle is drawn as a crisp square the size of its block, carrying its own piece of
	// the image: at rest the squares tile the frame exactly (the old image, pixel for pixel)
	hbw, hbh := float32(W)/float32(f.gx)*0.5, float32(H)/float32(f.gy)*0.5
	if f.liquid {
		// A liquid has no gaps between its drops: slightly bigger pieces close the small ones
		// (at rest the overlap only blends each piece with its neighbors' edges)
		hbw, hbh = hbw*1.3, hbh*1.3
	}
	bin := int(math.Ceil(float64(max(hbw, hbh)))) + 1
	ff := &FluidFrame{W: W, H: H, K: k, HBW: hbw, HBH: hbh, Bin: bin,
		NBX: (W + bin - 1) / bin, NBY: (H + bin - 1) / bin, CW: (W + 7) / 8, CH: (H + 7) / 8}
	raw := make([]float32, n*8) // particle order; Parts gets them by bin
	parallelFor(n, func(i0, i1 int) {
		for i := i0; i < i1; i++ {
			b0, b1 := s0*n*2+2*i, (s0+1)*n*2+2*i
			x := (float32(f.pos[b0]) + float32(f.pos[b1]-f.pos[b0])*fr) / 8
			y := (float32(f.pos[b0+1]) + float32(f.pos[b1+1]-f.pos[b0+1])*fr) / 8
			x += (f.tx[i] - x) * settle
			y += (f.ty[i] - y) * settle
			x, y = min(max(x, 0), float32(W)-0.01), min(max(y, 0), float32(H)-0.01)
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
			q := raw[i*8 : i*8+8 : i*8+8]
			q[0], q[1], q[2] = x, y, float32(p)
			q[4], q[5], q[6], q[7] = f.sx[i], f.sy[i], f.tx[i], f.ty[i]
		}
	})
	// Bins (counting sort) and the coarse level: each particle adds its block's color and area
	// to the 8×8 cell it is in; two passes of a 3×3 box blur make the gap fill smooth
	nb := ff.NBX * ff.NBY
	ff.BinStart = make([]uint32, nb+1)
	binOf := make([]uint32, n)
	coarse := make([]float32, ff.CW*ff.CH*4)
	area := 4 * hbw * hbh
	for i := range n {
		x, y := raw[i*8], raw[i*8+1]
		b := uint32(int(y)/bin*ff.NBX + int(x)/bin)
		binOf[i] = b
		ff.BinStart[b+1]++
		p := raw[i*8+2]
		c := ((int(y)/8)*ff.CW + int(x)/8) * 4
		for ch := range 3 {
			a, bb := float32((f.colA[i]>>(8*ch))&255), float32((f.colB[i]>>(8*ch))&255)
			coarse[c+ch] += (a + (bb-a)*p) * area
		}
		coarse[c+3] += area
	}
	for b := 1; b <= nb; b++ {
		ff.BinStart[b] += ff.BinStart[b-1]
	}
	ff.Parts = make([]float32, n*8)
	fill := make([]uint32, nb)
	for i := range n {
		b := binOf[i]
		j := ff.BinStart[b] + fill[b]
		copy(ff.Parts[j*8:j*8+8], raw[i*8:i*8+8])
		fill[b]++
	}
	cw, chh := ff.CW, ff.CH
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
	ff.Coarse = coarse
	return ff
}

// Render draws the frame into dst (W×H) on the CPU. sa / sb are the two images as they are shown
// now (with their motion): it starts exactly on the first and ends exactly on the second.
func (ff *FluidFrame) Render(dst *image.RGBA, sa, sb *sampler) {
	nw := make([]float32, ff.W*ff.H)
	parallelFor(ff.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < ff.W; x++ {
				c, nb := ff.pixel(x, y, sa, sb)
				nw[y*ff.W+x] = float32(nb)
				d := y*dst.Stride + x*4
				dst.Pix[d] = uint8(math.Max(0, math.Min(255, c[0]+0.5)))
				dst.Pix[d+1] = uint8(math.Max(0, math.Min(255, c[1]+0.5)))
				dst.Pix[d+2] = uint8(math.Max(0, math.Min(255, c[2]+0.5)))
				dst.Pix[d+3] = 255
			}
		}
	})
	ff.New = nw
}

// pixel: the pieces covering pixel (x, y), each weighted by how much of the pixel it covers
// (antialiased squares); the uncovered part takes the soft color around. Mirrors main() in
// fluid.comp.
func (ff *FluidFrame) pixel(x, y int, sa, sb *sampler) (rgb, float64) {
	var acc rgb
	var w, pw float64
	fx, fy := float32(x), float32(y)
	// Only the bins that can hold a piece touching the pixel (its center within half a piece)
	bs := float32(ff.Bin)
	bx0, bx1 := max(0, int((fx-ff.HBW)/bs)), min(ff.NBX-1, int((fx+1+ff.HBW)/bs))
	by0, by1 := max(0, int((fy-ff.HBH)/bs)), min(ff.NBY-1, int((fy+1+ff.HBH)/bs))
	for yy := by0; yy <= by1; yy++ {
		for xx := bx0; xx <= bx1; xx++ {
			b := yy*ff.NBX + xx
			for i := ff.BinStart[b]; i < ff.BinStart[b+1]; i++ {
				q := ff.Parts[i*8 : i*8+8 : i*8+8]
				cx := min(fx+1, q[0]+ff.HBW) - max(fx, q[0]-ff.HBW)
				cy := min(fy+1, q[1]+ff.HBH) - max(fy, q[1]-ff.HBH)
				if cx <= 0 || cy <= 0 {
					continue
				}
				wgt := float64(cx * cy)
				// The piece it carries: the same offset inside its block, in each image
				ox, oy := float64(fx-q[0]), float64(fy-q[1])
				p := float64(q[2])
				var c rgb
				if p < 1 {
					c = sa.at(float64(q[4])+ox, float64(q[5])+oy)
				}
				if p > 0 {
					c = mixc(c, sb.at(float64(q[6])+ox, float64(q[7])+oy), p)
				}
				acc = rgb{acc[0] + c[0]*wgt, acc[1] + c[1]*wgt, acc[2] + c[2]*wgt}
				pw += p * wgt
				w += wgt
			}
		}
	}
	var c rgb
	nb := ff.K
	if w > 1e-4 {
		c = rgb{acc[0] / w, acc[1] / w, acc[2] / w}
		nb = pw / w
	}
	if w < 1 {
		nb = ff.K + (nb-ff.K)*w
		// Where the area is almost empty (e.g. edges the particles left), fade to a dim mix of
		// both images instead of stretching a few particles' colors
		dim := mixc(sa.at(float64(x), float64(y)), sb.at(float64(x), float64(y)), ff.K)
		g := rgb{dim[0] * 0.5, dim[1] * 0.5, dim[2] * 0.5}
		// Coarse color around (bilinear on the 1/8 grid)
		cw, chh := ff.CW, ff.CH
		qx, qy := (float64(x)+0.5)/8-0.5, (float64(y)+0.5)/8-0.5
		c0, r0 := min(max(int(math.Floor(qx)), 0), cw-1), min(max(int(math.Floor(qy)), 0), chh-1)
		c1, r1 := min(c0+1, cw-1), min(r0+1, chh-1)
		gx, gy := math.Max(0, math.Min(1, qx-float64(c0))), math.Max(0, math.Min(1, qy-float64(r0)))
		cb := func(ch int) float64 {
			a := float64(ff.Coarse[(r0*cw+c0)*4+ch])*(1-gx) + float64(ff.Coarse[(r0*cw+c1)*4+ch])*gx
			b := float64(ff.Coarse[(r1*cw+c0)*4+ch])*(1-gx) + float64(ff.Coarse[(r1*cw+c1)*4+ch])*gx
			return a*(1-gy) + b*gy
		}
		if cw := cb(3); cw > 1e-3 {
			g = mixc(g, rgb{cb(0) / cw, cb(1) / cw, cb(2) / cw}, smooth01(cw/40))
		}
		c = mixc(g, c, w)
	}
	if FluidDebug && w < 0.5 {
		c = rgb{255, 0, 255}
	}
	if out := smooth01((ff.K - 0.96) / 0.04); out > 0 {
		c = mixc(c, sb.at(float64(x), float64(y)), out)
		nb += (1 - nb) * out
	}
	return c, nb
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
	lx, ly := float32(f.W)/gc, float32(f.H)/gc
	for k := range den {
		if den[k] > 1e-6 {
			mvx[k] /= den[k]
			mvy[k] /= den[k]
		}
		// 1 = rest density. Nodes on the edges only gather particles from the part of their
		// cells inside the frame: measured against that part, so the walls do not look empty
		// (which would pull the liquid against them and leave holes behind)
		den[k] /= rest * max(0.05, tentIn(float32(k%nx), lx)*tentIn(float32(k/nx), ly))
	}
	visc := float32(o.Viscosity * 0.9)
	pres := float32(liquidPressure)
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

// tentIn: how much of the bilinear weight of grid node x (a tent of half width 1) falls inside
// [0, l]: 1 inside, 1/2 on the edge, 0 far outside
func tentIn(x, l float32) float32 {
	F := func(u float32) float32 { // ∫ 1-|u| du
		if u < 0 {
			return u + u*u/2
		}
		return u - u*u/2
	}
	a, b := max(-1, -x), min(1, l-x)
	if b <= a {
		return 0
	}
	return F(b) - F(a)
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
var FluidDebug = os.Getenv("AUDIOVIZ_FLUID_DEBUG") != ""

// streamField: the stream each area of the liquid follows. The particles' wanted displacements
// are smoothed over their neighborhood; the stream keeps only their turning (the vorticity) and
// is rebuilt from a stream function that is zero on the edges of the frame: the liquid moves and
// turns but is neither compressed nor stretched (it does not shrink toward the center or tear
// away from the walls), and nothing flows through the edges.
func (f *Fluid) streamField(r int) *flowField {
	sx, sy := f.smoothedTargets(r)
	n, gx, gy := f.n, f.gx, f.gy
	// Coarse cells (4 particles per side): mean displacement U, V
	const cg = 4
	cx, cy := (gx+cg-1)/cg, (gy+cg-1)/cg
	U, V, cnt := make([]float64, cx*cy), make([]float64, cx*cy), make([]float64, cx*cy)
	for i := range n {
		k := (i/gx/cg)*cx + (i%gx)/cg
		U[k] += float64(sx[i] - f.sx[i])
		V[k] += float64(sy[i] - f.sy[i])
		cnt[k]++
	}
	for k := range U {
		if cnt[k] > 0 {
			U[k] /= cnt[k]
			V[k] /= cnt[k]
		}
	}
	// Nodes on the cell corners, spanning the frame exactly (the outer ones lie on the edges)
	nx, ny := cx+1, cy+1
	hx, hy := float64(f.W)/float64(cx), float64(f.H)/float64(cy)
	cell := func(g []float64, x, y int) float64 { return g[min(max(y, 0), cy-1)*cx+min(max(x, 0), cx-1)] }
	// Vorticity at the inner nodes, from the four cells around each
	w := make([]float64, nx*ny)
	for y := 1; y < ny-1; y++ {
		for x := 1; x < nx-1; x++ {
			dvdx := (cell(V, x, y-1) + cell(V, x, y) - cell(V, x-1, y-1) - cell(V, x-1, y)) / (2 * hx)
			dudy := (cell(U, x-1, y) + cell(U, x, y) - cell(U, x-1, y-1) - cell(U, x, y-1)) / (2 * hy)
			w[y*nx+x] = dvdx - dudy
		}
	}
	// Stream function: ∇²ψ = -ω with ψ = 0 on the edges (successive over-relaxation, red-black)
	psi := make([]float64, nx*ny)
	ax, ay := 1/(hx*hx), 1/(hy*hy)
	den := 2*ax + 2*ay
	const omega = 1.9
	for range 1500 {
		for color := range 2 {
			for y := 1; y < ny-1; y++ {
				for x := 1 + (y+color)%2; x < nx-1; x += 2 {
					k := y*nx + x
					g := ((psi[k-1]+psi[k+1])*ax + (psi[k-nx]+psi[k+nx])*ay + w[k]) / den
					psi[k] += omega * (g - psi[k])
				}
			}
		}
	}
	// Velocity at the nodes: u = ∂ψ/∂y, v = -∂ψ/∂x (one-sided on the edges). ψ is constant
	// along each edge, so the flow across it is exactly zero there
	ff := &flowField{nx: nx, ny: ny, hx: float32(hx), hy: float32(hy), u: make([]float32, nx*ny), v: make([]float32, nx*ny)}
	ps := func(x, y int) float64 { return psi[y*nx+x] }
	for y := range ny {
		for x := range nx {
			y0, y1 := max(y-1, 0), min(y+1, ny-1)
			x0, x1 := max(x-1, 0), min(x+1, nx-1)
			ff.u[y*nx+x] = float32((ps(x, y1) - ps(x, y0)) / (float64(y1-y0) * hy))
			ff.v[y*nx+x] = float32(-(ps(x1, y) - ps(x0, y)) / (float64(x1-x0) * hx))
		}
	}
	return ff
}

// flowField: a displacement field (pixels) on a grid of nodes spanning the frame (node (0, 0) on
// the top-left corner, spacing hx × hy), sampled bilinearly anywhere
type flowField struct {
	nx, ny int
	hx, hy float32
	u, v   []float32
}

func (g *flowField) at(x, y float32) (float32, float32) {
	qx, qy := min(max(x/g.hx, 0), float32(g.nx-1)), min(max(y/g.hy, 0), float32(g.ny-1))
	x0, y0 := min(int(qx), g.nx-2), min(int(qy), g.ny-2)
	fx, fy := qx-float32(x0), qy-float32(y0)
	b := func(a []float32) float32 {
		return (a[y0*g.nx+x0]*(1-fx)+a[y0*g.nx+x0+1]*fx)*(1-fy) + (a[(y0+1)*g.nx+x0]*(1-fx)+a[(y0+1)*g.nx+x0+1]*fx)*fy
	}
	return b(g.u), b(g.v)
}
