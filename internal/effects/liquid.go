package effects

import (
	"bytes"
	"encoding/binary"
	"math"
)

// Simulated liquid (the emitter look "liquid"): the emitter's particles are born as drops of a
// liquid that a FLIP simulation then moves: gravity and wind, a pressure that keeps it from
// being squeezed (it piles up into pools, splashes, flows round things), what is solid (the
// emitter's floor, the scene at its depth). The particles keep their place from step to step;
// a staggered (MAC) grid over the frame carries their velocity for the pressure. They are drawn
// with the water look (waterPixel) as any emitter's particles.
//
// The simulation runs on the CPU, in parallel where the result does not depend on the order;
// its state (the particles) is saved exactly at checkpoints, so the engine seeks as with the gas.

// LiquidPhys: how the liquid moves (px of this frame, s)
type LiquidPhys struct {
	GravX, GravY float32 // px/s² (the emitter's wind and gravity)
	Flip         float32 // 0..1: FLIP's share (lively, splashy) against PIC's (calm, viscous)
	FloorOn      bool
	Floor        float32 // y px of the floor (FloorOn)
}

// LiquidBirth: a drop born now: where, its velocity, its size and color, the emitter's index of
// its particle
type LiquidBirth struct {
	X, Y, VX, VY float32
	Size         float32
	Col          [4]float32
	Idx          int32
}

// LiquidSim: the drops and the grid (GW×GH cells of CS px over the frame)
type LiquidSim struct {
	GW, GH int
	CS     float32
	W, H   float32
	T      float64
	Next   int32 // the next of the emitter's particles to be born
	// the drops: position, velocity (px, px/s), size (px), color, the emitter's index
	X, Y, VX, VY, Size []float32
	Col                [][4]float32
	Idx                []int32
	Born               []float32 // when each was born (T)
	// the grid: velocities on the cells' sides (u: (GW+1)×GH, v: GW×(GH+1)), their weights and
	// values before the forces, the pressure (× dt), the cells' kind, the solid cells (Solid,
	// set per frame: nil none)
	u, v, uw, vw, u0, v0 []float32
	p, div               []float32
	kind                 []uint8 // 0 air, 1 liquid, 2 solid
	Solid                []uint8
	cnt                  []int32 // drops per cell (this step)
	spray                []bool  // which drops fly by themselves (this step)
	exK                  []uint8 // (extrapolate's scratch)
	exV, exN             []float32
}

// liquid cell kinds
const (
	lqAir = iota
	lqFluid
	lqSolid
)

// lqSprayBelow: a drop with fewer drops than this around it (3×3 cells, itself included) is spray
const lqSprayBelow = 6

// lqLaunch: seconds a newborn drop flies by itself (a jet or a splash is thrown, not squeezed)
const lqLaunch = 0.25

// LiquidMax: the most drops at once
const LiquidMax = 40000

// NewLiquidSim: no liquid yet over a W×H frame, cells of about cs px
func NewLiquidSim(W, H int, cs float64) *LiquidSim {
	gw, gh, c := GasGrid(W, H, cs)
	n := gw * gh
	return &LiquidSim{GW: gw, GH: gh, CS: c, W: float32(W), H: float32(H),
		u: make([]float32, (gw+1)*gh), v: make([]float32, gw*(gh+1)),
		uw: make([]float32, (gw+1)*gh), vw: make([]float32, gw*(gh+1)),
		u0: make([]float32, (gw+1)*gh), v0: make([]float32, gw*(gh+1)),
		p: make([]float32, n), div: make([]float32, n), kind: make([]uint8, n)}
}

// Count: the drops now
func (s *LiquidSim) Count() int { return len(s.X) }

// Save: the state (exactly); the pressure is cleared so that going on from here or from what
// is saved gives the same frames
func (s *LiquidSim) Save() []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, s.T)
	binary.Write(&b, binary.LittleEndian, s.Next)
	binary.Write(&b, binary.LittleEndian, int32(len(s.X)))
	for _, f := range [][]float32{s.X, s.Y, s.VX, s.VY, s.Size, s.Born} {
		binary.Write(&b, binary.LittleEndian, f)
	}
	binary.Write(&b, binary.LittleEndian, s.Col)
	binary.Write(&b, binary.LittleEndian, s.Idx)
	clear(s.p)
	return b.Bytes()
}

// Load: a state from Save
func (s *LiquidSim) Load(b []byte) bool {
	r := bytes.NewReader(b)
	var n int32
	if binary.Read(r, binary.LittleEndian, &s.T) != nil || binary.Read(r, binary.LittleEndian, &s.Next) != nil ||
		binary.Read(r, binary.LittleEndian, &n) != nil || n < 0 || n > LiquidMax {
		return false
	}
	mk := func() []float32 { return make([]float32, n) }
	s.X, s.Y, s.VX, s.VY, s.Size, s.Born = mk(), mk(), mk(), mk(), mk(), mk()
	s.Col, s.Idx = make([][4]float32, n), make([]int32, n)
	for _, f := range [][]float32{s.X, s.Y, s.VX, s.VY, s.Size, s.Born} {
		if binary.Read(r, binary.LittleEndian, f) != nil {
			return false
		}
	}
	if binary.Read(r, binary.LittleEndian, s.Col) != nil || binary.Read(r, binary.LittleEndian, s.Idx) != nil {
		return false
	}
	clear(s.p)
	return true
}

// Add: new drops (the oldest go past LiquidMax or max)
func (s *LiquidSim) Add(bs []LiquidBirth, max int) {
	for _, b := range bs {
		s.X, s.Y, s.VX, s.VY = append(s.X, b.X), append(s.Y, b.Y), append(s.VX, b.VX), append(s.VY, b.VY)
		s.Size, s.Col, s.Idx = append(s.Size, b.Size), append(s.Col, b.Col), append(s.Idx, b.Idx)
		s.Born = append(s.Born, float32(s.T))
	}
	if over := len(s.X) - min(max, LiquidMax); over > 0 {
		s.drop(func(i int) bool { return i < over })
	}
}

// drop takes out the drops gone (kept in order)
func (s *LiquidSim) drop(gone func(i int) bool) {
	k := 0
	for i := range s.X {
		if gone(i) {
			continue
		}
		s.X[k], s.Y[k], s.VX[k], s.VY[k], s.Size[k], s.Col[k], s.Idx[k] = s.X[i], s.Y[i], s.VX[i], s.VY[i], s.Size[i], s.Col[i], s.Idx[i]
		s.Born[k] = s.Born[i]
		k++
	}
	s.X, s.Y, s.VX, s.VY, s.Size, s.Col, s.Idx = s.X[:k], s.Y[:k], s.VX[:k], s.VY[:k], s.Size[:k], s.Col[:k], s.Idx[:k]
	s.Born = s.Born[:k]
}

// solidAt: whether (x, y) px is solid (the scene's solid cells, below the floor)
func (s *LiquidSim) solidAt(x, y float32, ph *LiquidPhys) bool {
	if ph.FloorOn && y >= ph.Floor {
		return true
	}
	if s.Solid == nil {
		return false
	}
	i, j := int(math.Floor(float64(x/s.CS))), int(math.Floor(float64(y/s.CS)))
	if i < 0 || j < 0 || i >= s.GW || j >= s.GH {
		return false
	}
	return s.Solid[j*s.GW+i] != 0
}

// Substeps: how many steps a frame of dt needs (no drop goes more than a cell per step)
func (s *LiquidSim) Substeps(dt float32, ph *LiquidPhys) int {
	var vm float32
	for i := range s.VX {
		vm = max(vm, float32(math.Hypot(float64(s.VX[i]), float64(s.VY[i]))))
	}
	vm += float32(math.Hypot(float64(ph.GravX), float64(ph.GravY))) * dt
	return max(1, min(8, int(math.Ceil(float64(vm*dt/s.CS)))))
}

// sampleU / sampleV: the grid velocity at (x, y) px (bilinear on the sides of the cells)
func (s *LiquidSim) sampleU(u []float32, x, y float32) float32 {
	gx, gy := x/s.CS, y/s.CS-0.5
	return bilerp(u, s.GW+1, s.GH, gx, gy)
}
func (s *LiquidSim) sampleV(v []float32, x, y float32) float32 {
	gx, gy := x/s.CS-0.5, y/s.CS
	return bilerp(v, s.GW, s.GH+1, gx, gy)
}

// bilerp: field f (w×h) at (x, y) in its samples, clamped to its edges
func bilerp(f []float32, w, h int, x, y float32) float32 {
	if w < 2 || h < 2 {
		return f[0]
	}
	x, y = max(0, min(float32(w-1), x)), max(0, min(float32(h-1), y))
	ix, iy := min(int(x), w-2), min(int(y), h-2)
	fx, fy := x-float32(ix), y-float32(iy)
	a, b := f[iy*w+ix], f[iy*w+ix+1]
	c, d := f[(iy+1)*w+ix], f[(iy+1)*w+ix+1]
	return (a+(b-a)*fx)*(1-fy) + (c+(d-c)*fx)*fy
}

// Step moves the liquid dt seconds on. The grid's work is done only around the liquid that holds
// together (its box, a few cells wider): what costs is the liquid there is, not the frame.
func (s *LiquidSim) Step(ph *LiquidPhys, dt float32) {
	W, H := s.GW, s.GH
	cs := s.CS
	n := len(s.X)
	// the drops per cell: a drop with few others around (3×3 cells) is spray, and so is a newborn
	// one: it flies by itself (its own speed, the forces); only the liquid that holds together is
	// the grid's
	if len(s.cnt) != W*H {
		s.cnt = make([]int32, W*H)
	}
	clear(s.cnt)
	cellOf := func(q int) (int, int, bool) {
		i, j := int(math.Floor(float64(s.X[q]/cs))), int(math.Floor(float64(s.Y[q]/cs)))
		return i, j, i >= 0 && j >= 0 && i < W && j < H
	}
	for q := 0; q < n; q++ {
		if i, j, ok := cellOf(q); ok {
			s.cnt[j*W+i]++
		}
	}
	s.spray = s.spray[:0]
	i0, j0, i1, j1 := W, H, -1, -1 // the box of the liquid that holds together
	for q := 0; q < n; q++ {
		i, j, ok := cellOf(q)
		var around int32
		if ok {
			for dj := -1; dj <= 1; dj++ {
				for di := -1; di <= 1; di++ {
					if i+di >= 0 && j+dj >= 0 && i+di < W && j+dj < H {
						around += s.cnt[(j+dj)*W+i+di]
					}
				}
			}
		}
		sp := !ok || around < lqSprayBelow || float32(s.T)-s.Born[q] < lqLaunch
		s.spray = append(s.spray, sp)
		if !sp {
			i0, j0, i1, j1 = min(i0, i), min(j0, j), max(i1, i), max(j1, j)
		}
	}
	if i1 >= 0 {
		const m = 4
		i0, j0, i1, j1 = max(0, i0-m), max(0, j0-m), min(W-1, i1+m), min(H-1, j1+m)
		s.grid(ph, dt, i0, j0, i1, j1)
	}
	// the drops: back from the grid (FLIP: their own plus the grid's change; PIC: the grid's) and
	// moved by it (midpoint), or flying by themselves (spray); sliding off what is solid
	flip := ph.Flip
	parallelFor(n, func(q0, q1 int) {
		for q := q0; q < q1; q++ {
			x, y := s.X[q], s.Y[q]
			var nx, ny float32
			if s.spray[q] {
				s.VX[q] += ph.GravX * dt
				s.VY[q] += ph.GravY * dt
				nx, ny = x+dt*s.VX[q], y+dt*s.VY[q]
			} else {
				nu, nv := s.sampleU(s.u, x, y), s.sampleV(s.v, x, y)
				ou, ov := s.sampleU(s.u0, x, y), s.sampleV(s.v0, x, y)
				s.VX[q] = flip*(s.VX[q]+nu-ou) + (1-flip)*nu
				s.VY[q] = flip*(s.VY[q]+nv-ov) + (1-flip)*nv
				mx, my := x+0.5*dt*nu, y+0.5*dt*nv
				nx, ny = x+dt*s.sampleU(s.u, mx, my), y+dt*s.sampleV(s.v, mx, my)
			}
			if s.solidAt(nx, ny, ph) {
				switch {
				case !s.solidAt(nx, y, ph):
					ny, s.VY[q] = y, 0
				case !s.solidAt(x, ny, ph):
					nx, s.VX[q] = x, 0
				default:
					nx, ny, s.VX[q], s.VY[q] = x, y, 0, 0
				}
			}
			s.X[q], s.Y[q] = nx, ny
		}
	})
	// the drops gone out of the frame (down or aside; far up they come back)
	mg := 4 * cs
	s.drop(func(q int) bool {
		return s.X[q] < -mg || s.X[q] > s.W+mg || s.Y[q] > s.H+mg || s.Y[q] < -s.H
	})
	s.T += float64(dt)
}

// grid: the grid's part of a step over the cells [i0, i1]×[j0, j1] (the liquid's box): the
// cells' kinds, the drops' velocity onto the sides, the forces, the pressure, the velocity
// carried into the air around
func (s *LiquidSim) grid(ph *LiquidPhys, dt float32, i0, j0, i1, j1 int) {
	W := s.GW
	cs := s.CS
	n := len(s.X)
	// cells out of the box: air (nothing there)
	kindAt := func(i, j int) uint8 {
		if i < i0 || j < j0 || i > i1 || j > j1 {
			return lqAir
		}
		return s.kind[j*W+i]
	}
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			k := j*W + i
			s.kind[k] = lqAir
			if s.solidAt((float32(i)+0.5)*cs, (float32(j)+0.5)*cs, ph) {
				s.kind[k] = lqSolid
			}
		}
	}
	for q := 0; q < n; q++ {
		if s.spray[q] {
			continue
		}
		i, j := int(math.Floor(float64(s.X[q]/cs))), int(math.Floor(float64(s.Y[q]/cs)))
		if s.kind[j*W+i] == lqAir {
			s.kind[j*W+i] = lqFluid
		}
	}
	// the drops' velocity onto the cells' sides (weights of a tent one cell wide), in the drops'
	// order (the same sums every time)
	clear(s.u)
	clear(s.v)
	clear(s.uw)
	clear(s.vw)
	splat := func(f, fw []float32, w, h int, gx, gy, val float32) {
		ix, iy := int(math.Floor(float64(gx))), int(math.Floor(float64(gy)))
		fx, fy := gx-float32(ix), gy-float32(iy)
		wts := [4]float32{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
		for c := 0; c < 4; c++ {
			x, y := ix+(c&1), iy+(c>>1)
			if x < 0 || y < 0 || x >= w || y >= h {
				continue
			}
			f[y*w+x] += wts[c] * val
			fw[y*w+x] += wts[c]
		}
	}
	for q := 0; q < n; q++ {
		if s.spray[q] {
			continue
		}
		splat(s.u, s.uw, W+1, s.GH, s.X[q]/cs, s.Y[q]/cs-0.5, s.VX[q])
		splat(s.v, s.vw, W, s.GH+1, s.X[q]/cs-0.5, s.Y[q]/cs, s.VY[q])
	}
	// the faces of the box (u: i0..i1+1, j0..j1; v: i0..i1, j0..j1+1)
	eachU := func(fn func(i, j, k int)) {
		for j := j0; j <= j1; j++ {
			for i := i0; i <= i1+1; i++ {
				fn(i, j, j*(W+1)+i)
			}
		}
	}
	eachV := func(fn func(i, j, k int)) {
		for j := j0; j <= j1+1; j++ {
			for i := i0; i <= i1; i++ {
				fn(i, j, j*W+i)
			}
		}
	}
	eachU(func(_, _, k int) {
		if s.uw[k] > 0 {
			s.u[k] /= s.uw[k]
		}
	})
	eachV(func(_, _, k int) {
		if s.vw[k] > 0 {
			s.v[k] /= s.vw[k]
		}
	})
	// the forces (on the sides that have liquid; FLIP's change counts them)
	copy(s.u0, s.u)
	copy(s.v0, s.v)
	eachU(func(_, _, k int) {
		if s.uw[k] > 0 {
			s.u[k] += ph.GravX * dt
		}
	})
	eachV(func(_, _, k int) {
		if s.vw[k] > 0 {
			s.v[k] += ph.GravY * dt
		}
	})
	// nothing goes through what is solid
	solidSides := func() {
		eachU(func(i, j, k int) {
			if kindAt(i-1, j) == lqSolid || kindAt(i, j) == lqSolid {
				s.u[k] = 0
			}
		})
		eachV(func(i, j, k int) {
			if kindAt(i, j-1) == lqSolid || kindAt(i, j) == lqSolid {
				s.v[k] = 0
			}
		})
	}
	solidSides()
	// the pressure (× dt): no divergence in the liquid; air has none, solid walls let nothing
	// through (red-black SOR over the liquid cells only, from the last step's)
	var cells [2][]int32
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			k := j*W + i
			if s.kind[k] != lqFluid {
				s.p[k] = 0
				continue
			}
			s.div[k] = (s.u[j*(W+1)+i+1] - s.u[j*(W+1)+i] + s.v[(j+1)*W+i] - s.v[j*W+i]) / cs
			cells[(i+j)&1] = append(cells[(i+j)&1], int32(k))
		}
	}
	h2 := cs * cs
	relax := func(k int) {
		i, j := k%W, k/W
		var sum float32
		cnt := 0
		for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			switch kindAt(i+d[0], j+d[1]) {
			case lqFluid:
				sum += s.p[(j+d[1])*W+i+d[0]]
				cnt++
			case lqAir:
				cnt++
			}
		}
		if cnt > 0 {
			v := (sum - h2*s.div[k]) / float32(cnt)
			s.p[k] += 1.7 * (v - s.p[k])
		}
	}
	for it := 0; it < 60; it++ {
		for c := 0; c < 2; c++ {
			list := cells[c]
			if len(list) < 2048 {
				for _, k := range list {
					relax(int(k))
				}
				continue
			}
			parallelFor(len(list), func(a, b int) {
				for _, k := range list[a:b] {
					relax(int(k))
				}
			})
		}
	}
	pAt := func(i, j int) float32 {
		if kindAt(i, j) == lqFluid {
			return s.p[j*W+i]
		}
		return 0
	}
	eachU(func(i, j, k int) {
		a, b := kindAt(i-1, j), kindAt(i, j)
		if a != lqSolid && b != lqSolid && (a == lqFluid || b == lqFluid) {
			s.u[k] -= (pAt(i, j) - pAt(i-1, j)) / cs
		}
	})
	eachV(func(i, j, k int) {
		a, b := kindAt(i, j-1), kindAt(i, j)
		if a != lqSolid && b != lqSolid && (a == lqFluid || b == lqFluid) {
			s.v[k] -= (pAt(i, j) - pAt(i, j-1)) / cs
		}
	})
	// the velocity carried out into the air around (two cells), for the drops at the surface
	s.extrapolate(s.u, s.uw, W+1, i0, j0, i1+1, j1)
	s.extrapolate(s.v, s.vw, W, i0, j0, i1, j1+1)
	solidSides()
}

// extrapolate: within [x0, x1]×[y0, y1] of field f (w wide), the samples with no liquid take the
// mean of their neighbours that have some, twice
func (s *LiquidSim) extrapolate(f, wt []float32, w, x0, y0, x1, y1 int) {
	bw, bh := x1-x0+1, y1-y0+1
	nb := bw * bh
	if cap(s.exK) < nb {
		s.exK, s.exV, s.exN = make([]uint8, nb), make([]float32, nb), make([]float32, nb)
	}
	known, vals, next := s.exK[:nb], s.exV[:nb], s.exN[:nb]
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			k := (y+y0)*w + x + x0
			known[y*bw+x] = 0
			if wt[k] > 0 {
				known[y*bw+x] = 2 // (2: known before this pass)
			}
			vals[y*bw+x] = f[k]
		}
	}
	for pass := 0; pass < 2; pass++ {
		copy(next, vals)
		for y := 0; y < bh; y++ {
			row := y * bw
			for x := 0; x < bw; x++ {
				k := row + x
				if known[k] != 0 {
					continue
				}
				var sum float32
				c := 0
				if x > 0 && known[k-1] == 2 {
					sum += vals[k-1]
					c++
				}
				if x < bw-1 && known[k+1] == 2 {
					sum += vals[k+1]
					c++
				}
				if y > 0 && known[k-bw] == 2 {
					sum += vals[k-bw]
					c++
				}
				if y < bh-1 && known[k+bw] == 2 {
					sum += vals[k+bw]
					c++
				}
				if c > 0 {
					next[k] = sum / float32(c)
					known[k] = 1 // (known from the next pass on)
				}
			}
		}
		vals, next = next, vals
		for k, v := range known {
			if v == 1 {
				known[k] = 2
			}
		}
	}
	for y := 0; y < bh; y++ {
		copy(f[(y+y0)*w+x0:(y+y0)*w+x0+bw], vals[y*bw:(y+1)*bw])
	}
}

// Particles: the drops as the emitter's particles to draw
func (s *LiquidSim) Particles() []Particle {
	out := make([]Particle, len(s.X))
	for q := range s.X {
		c := s.Col[q]
		out[q] = Particle{X: s.X[q], Y: s.Y[q], VX: s.VX[q], VY: s.VY[q], Size: s.Size[q], R: c[0], G: c[1], B: c[2], A: c[3], Z: -1}
	}
	return out
}
