package effects

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"math"
)

// Simulated gas (the emitter look "gas"): the emitter's particles are not drawn, they feed a gas
// on a grid over the frame (smoke, heat, fuel, their color and their push), and the gas moves by
// itself: hot gas rises, smoke can sink, the air swirls (vorticity confinement), turbulence and
// wind carry it, fuel burns into heat and soot, smoke fades and heat cools. Seen with light: the
// smoke shades itself from a light (its far side dark), the light around it reaches its core
// less, and fire lights the smoke around it from inside; fine wisps carried by the flow (two sets
// of coordinates carried by the gas, each restarted in turn) eat its edges.
//
// The simulation runs on the CPU (in parallel, always the same result); the engine keeps its
// state at checkpoints (GasSim.Save) to seek. Each frame's GasFrame (the grid with its light) is
// drawn by gasPixel (CPU) and by emit.comp (GPU) the same way.

const (
	// GasMaxCells: the most cells of a gas grid (larger frames get coarser cells)
	GasMaxCells = 131072
	// GasCellWords: 32-bit words per cell of a GasFrame (pairs of halves)
	GasCellWords = 6
	// gasPeriod: seconds between the restarts of each set of wisp coordinates
	gasPeriod = 0.8
)

// GasPhys: how the gas moves (lengths in px of this frame, times in s)
type GasPhys struct {
	// what each particle brings per second at its centre (× its alpha, less towards its edge): smoke, heat, fuel, and
	// how much it pushes the gas its way (0..1)
	Density, Heat, Fuel, Push float32
	// rise per unit of heat and sink per unit of smoke (px/s²)
	Buoyancy, Weight float32
	// swirls (vorticity confinement, ~0..3)
	Swirl float32
	// the air's flow (px/s) and its turbulence: speed (px/s) and the size of its whirls (px)
	WindX, WindY     float32
	Turb, TurbScale  float32
	Fade, Cool, Burn float32 // 1/s: smoke vanishing, heat lost, fuel burnt
	Soot             float32 // smoke made per unit of fuel burnt
	Spread           float32 // px²/s: how much the smoke spreads out by itself (diffusion: fuller, softer)
	SootCol          [3]float32
	Seed             uint32
}

// GasSim is the state of a gas simulation: GW×GH cells of CS px over the frame
type GasSim struct {
	GW, GH int
	CS     float32
	T      float64 // seconds since it started
	// velocity (px/s), pressure, smoke, heat, fuel, smoke's color (× smoke), the two sets of
	// wisp coordinates (how far, px, the gas there has come since its set restarted)
	VX, VY, P, D, Heat, Fuel, Fire, R, G, B []float32
	C1x, C1y, C2x, C2y                      []float32
	t0, t1, t2, t3                          []float32      // scratch
	bx, by                                  []float32      // where each cell's gas came from this step (back)
	adv                                     [][3][]float32 // per field carried: back, lowest and highest source
}

// GasGrid: the grid of a W×H frame with cells of about cs px (coarser past GasMaxCells)
func GasGrid(W, H int, cs float64) (int, int, float32) {
	cs = max(cs, 1)
	for {
		gw, gh := int(math.Ceil(float64(W)/cs)), int(math.Ceil(float64(H)/cs))
		if gw*gh <= GasMaxCells {
			return max(gw, 4), max(gh, 4), float32(cs)
		}
		cs *= 1.05
	}
}

// NewGasSim: an empty gas over a W×H frame, cells of about cs px
func NewGasSim(W, H int, cs float64) *GasSim {
	gw, gh, c := GasGrid(W, H, cs)
	s := &GasSim{GW: gw, GH: gh, CS: c}
	for _, f := range s.fields() {
		*f = make([]float32, gw*gh)
	}
	n := gw * gh
	s.P = make([]float32, n)
	s.t0, s.t1, s.t2, s.t3 = make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n)
	s.bx, s.by = make([]float32, n), make([]float32, n)
	return s
}

// fields: the state kept at checkpoints
func (s *GasSim) fields() []*[]float32 {
	return []*[]float32{&s.VX, &s.VY, &s.D, &s.Heat, &s.Fuel, &s.Fire, &s.R, &s.G, &s.B, &s.C1x, &s.C1y, &s.C2x, &s.C2y}
}

// Save: the state, compressed (halves). The state is set to what is saved (the pressure
// cleared) so that going on from here or from the saved state gives the same frames.
func (s *GasSim) Save() []byte {
	var raw bytes.Buffer
	binary.Write(&raw, binary.LittleEndian, s.T)
	h := make([]byte, 2*s.GW*s.GH)
	for _, f := range s.fields() {
		for i, v := range *f {
			q := F16(v)
			(*f)[i] = HalfF(q)
			binary.LittleEndian.PutUint16(h[2*i:], q)
		}
		raw.Write(h)
	}
	clear(s.P)
	var out bytes.Buffer
	w, _ := flate.NewWriter(&out, flate.BestSpeed)
	w.Write(raw.Bytes())
	w.Close()
	return out.Bytes()
}

// Load: the state saved by Save (false if it does not fit this grid)
func (s *GasSim) Load(b []byte) bool {
	raw, err := io.ReadAll(flate.NewReader(bytes.NewReader(b)))
	n := s.GW * s.GH
	if err != nil || len(raw) != 8+len(s.fields())*2*n {
		return false
	}
	s.T = math.Float64frombits(binary.LittleEndian.Uint64(raw))
	o := 8
	for _, f := range s.fields() {
		for i := range *f {
			(*f)[i] = HalfF(binary.LittleEndian.Uint16(raw[o+2*i:]))
		}
		o += 2 * n
	}
	clear(s.P)
	return true
}

// Empty: whether there is no gas at all (nothing to draw)
func (s *GasSim) Empty() bool {
	for _, v := range s.D {
		if v > 1e-4 {
			return false
		}
	}
	for _, v := range s.Fire {
		if v > 1e-4 {
			return false
		}
	}
	return true
}

// GasSource: a particle feeding the gas: where (px), its radius (px), alpha, velocity (px/s)
// and color
type GasSource struct {
	X, Y, R, A, VX, VY float32
	Col                [3]float32
}

// Feed adds the particles' gas for dt seconds (one after the other: always the same sums)
func (s *GasSim) Feed(src []GasSource, ph *GasPhys, dt float32) {
	cs := s.CS
	for _, p := range src {
		if p.A <= 0 || p.R <= 0 {
			continue
		}
		r := max(p.R, cs*0.75)
		x0, x1 := max(int((p.X-r)/cs), 0), min(int((p.X+r)/cs)+1, s.GW-1)
		y0, y1 := max(int((p.Y-r)/cs), 0), min(int((p.Y+r)/cs)+1, s.GH-1)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				dx, dy := (float32(x)+0.5)*cs-p.X, (float32(y)+0.5)*cs-p.Y
				q := (dx*dx + dy*dy) / (r * r)
				if q >= 1 {
					continue
				}
				w := (1 - q) * (1 - q) * p.A
				i := y*s.GW + x
				a := w * dt // per second at its centre (the same whatever the cells' size)
				d := ph.Density * a
				s.D[i] += d
				s.R[i] += d * p.Col[0]
				s.G[i] += d * p.Col[1]
				s.B[i] += d * p.Col[2]
				s.Heat[i] += ph.Heat * a
				s.Fuel[i] += ph.Fuel * a
				if m := min(1, ph.Push*w*dt*12); m > 0 {
					s.VX[i] += (p.VX - s.VX[i]) * m
					s.VY[i] += (p.VY - s.VY[i]) * m
				}
			}
		}
	}
}

// rows runs fn over the rows of the grid in parallel
func (s *GasSim) rows(fn func(y0, y1 int)) { parallelFor(s.GH, fn) }

// sample: field f at (x, y) in cells (centers at i + 0.5): bilinear; outside: the edge (edge)
// or nothing
func (s *GasSim) sample(f []float32, x, y float32, edge bool) float32 {
	x, y = x-0.5, y-0.5
	fx, fy := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
	ix, iy := int(fx), int(fy)
	ux, uy := x-fx, y-fy
	at := func(i, j int) float32 {
		if i < 0 || j < 0 || i >= s.GW || j >= s.GH {
			if !edge {
				return 0
			}
			i, j = max(0, min(s.GW-1, i)), max(0, min(s.GH-1, j))
		}
		return f[j*s.GW+i]
	}
	a, b := at(ix, iy), at(ix+1, iy)
	c, d := at(ix, iy+1), at(ix+1, iy+1)
	return (a+(b-a)*ux)*(1-uy) + (c+(d-c)*ux)*uy
}

// back: where the gas at cell (x, y) came from dt ago (cells; midpoint rule)
func (s *GasSim) back(x, y int, dt float32) (float32, float32) {
	k := dt / s.CS
	px, py := float32(x)+0.5, float32(y)+0.5
	i := y*s.GW + x
	mx, my := px-0.5*k*s.VX[i], py-0.5*k*s.VY[i]
	return px - k*s.sample(s.VX, mx, my, true), py - k*s.sample(s.VY, mx, my, true)
}

// Step moves the gas dt seconds on
func (s *GasSim) Step(ph *GasPhys, dt float32) {
	W := s.GW
	cs := s.CS
	// 1. forces: heat rises, smoke sinks, the wind, the turbulence (a curl of noise: no
	// divergence), swirls kept alive (vorticity confinement)
	wk := 1 - float32(math.Exp(-0.4*float64(dt)))
	turbOn := ph.Turb > 0 && ph.TurbScale > 0
	var psi []float32
	if turbOn {
		psi = s.t2
		tt := float32(s.T)
		ls := ph.TurbScale
		s.rows(func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < W; x++ {
					u, v := (float32(x)+0.5)*cs/ls, (float32(y)+0.5)*cs/ls
					psi[y*W+x] = evnoise(u+tt*0.13, v-tt*0.05, ph.Seed+101) + 0.5*evnoise(1.9*u-tt*0.21+3.3, 1.9*v+tt*0.07, ph.Seed+202)
				}
			}
		})
	}
	// the curl of the velocity (its spin)
	w := s.t0
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				xl, xr, yu, yd := max(x-1, 0), min(x+1, W-1), max(y-1, 0), min(y+1, s.GH-1)
				w[y*W+x] = ((s.VY[y*W+xr] - s.VY[y*W+xl]) - (s.VX[yd*W+x] - s.VX[yu*W+x])) / (2 * cs)
			}
		}
	})
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				fx := wk * (ph.WindX - s.VX[i]) / dt
				fy := wk*(ph.WindY-s.VY[i])/dt - ph.Buoyancy*s.Heat[i] + ph.Weight*s.D[i]
				xl, xr, yu, yd := max(x-1, 0), min(x+1, W-1), max(y-1, 0), min(y+1, s.GH-1)
				if ph.Swirl > 0 {
					abs := func(v float32) float32 { return float32(math.Abs(float64(v))) }
					gx := (abs(w[y*W+xr]) - abs(w[y*W+xl])) / (2 * cs)
					gy := (abs(w[yd*W+x]) - abs(w[yu*W+x])) / (2 * cs)
					if l := float32(math.Sqrt(float64(gx*gx + gy*gy))); l > 1e-6 {
						e := ph.Swirl * cs * 1.5
						fx += e * (gy / l) * w[i]
						fy -= e * (gx / l) * w[i]
					}
				}
				if turbOn {
					// ψ's curl, about 1/scale: the turbulence's speed reached in about half a second
					k := ph.Turb * ph.TurbScale * 2 / (2 * cs)
					fx += k * (psi[yd*W+x] - psi[yu*W+x])
					fy -= k * (psi[y*W+xr] - psi[y*W+xl])
				}
				s.t1[i] = s.VX[i] + fx*dt
				s.t3[i] = s.VY[i] + fy*dt
			}
		}
	})
	copy(s.VX, s.t1)
	copy(s.VY, s.t3)
	// 2. the velocity carried by itself
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				bx, by := s.back(x, y, dt)
				s.t1[y*W+x] = s.sample(s.VX, bx, by, true)
				s.t3[y*W+x] = s.sample(s.VY, bx, by, true)
			}
		}
	})
	copy(s.VX, s.t1)
	copy(s.VY, s.t3)
	// 3. no divergence: the pressure (red-black SOR, from the last one), its gradient taken off;
	// open edges (no pressure outside)
	s.project()
	// where each cell's gas came from (the same for everything it carries)
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				s.bx[y*W+x], s.by[y*W+x] = s.back(x, y, dt)
			}
		}
	})
	// 4. the smoke, its color, heat and fuel carried by the flow (MacCormack, clamped)
	s.advectAll([][]float32{s.D, s.R, s.G, s.B, s.Heat, s.Fuel, s.Fire}, dt)
	// the smoke spreads out a little by itself (and its heat): fuller, softer volumes
	if ph.Spread > 0 {
		k := min(0.2, ph.Spread*dt/(cs*cs))
		for _, f := range [][]float32{s.D, s.R, s.G, s.B, s.Heat} {
			s.diffuse(f, k)
		}
	}
	// the wisp coordinates: carried (semi-Lagrangian), plus the way the gas went; each set restarts
	// in turn (gasPeriod; half a period apart)
	k1 := math.Floor(s.T/gasPeriod) != math.Floor((s.T+float64(dt))/gasPeriod)
	k2 := math.Floor(s.T/gasPeriod+0.5) != math.Floor((s.T+float64(dt))/gasPeriod+0.5)
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				bx, by := s.bx[i], s.by[i]
				vx, vy := s.VX[i]*dt, s.VY[i]*dt
				s.t0[i] = s.sample(s.C1x, bx, by, true) - vx
				s.t1[i] = s.sample(s.C1y, bx, by, true) - vy
				s.t2[i] = s.sample(s.C2x, bx, by, true) - vx
				s.t3[i] = s.sample(s.C2y, bx, by, true) - vy
			}
		}
	})
	copy(s.C1x, s.t0)
	copy(s.C1y, s.t1)
	copy(s.C2x, s.t2)
	copy(s.C2y, s.t3)
	if k1 {
		clear(s.C1x)
		clear(s.C1y)
	}
	if k2 {
		clear(s.C2x)
		clear(s.C2y)
	}
	// 5. fuel burns into heat and soot; smoke fades, heat cools
	fade := float32(math.Exp(-float64(ph.Fade * dt)))
	// the flames cool fast (Cool); the warmth that makes the gas rise stays longer
	cool := float32(math.Exp(-float64(ph.Cool * dt)))
	warm := float32(math.Exp(-float64(ph.Cool * 0.3 * dt)))
	burn := 1 - float32(math.Exp(-float64(ph.Burn*dt)))
	s.rows(func(y0, y1 int) {
		for i := y0 * W; i < y1*W; i++ {
			if b := s.Fuel[i] * burn; b > 0 {
				s.Fuel[i] -= b
				s.Heat[i] += b * 3
				s.Fire[i] += b * 3
				so := b * ph.Soot
				s.D[i] += so
				s.R[i] += so * ph.SootCol[0]
				s.G[i] += so * ph.SootCol[1]
				s.B[i] += so * ph.SootCol[2]
			}
			s.D[i] *= fade
			s.R[i] *= fade
			s.G[i] *= fade
			s.B[i] *= fade
			s.Heat[i] *= warm
			s.Fire[i] *= cool
			if s.D[i] < 1e-5 {
				s.D[i], s.R[i], s.G[i], s.B[i] = 0, 0, 0, 0
			}
			if s.Heat[i] < 1e-5 {
				s.Heat[i] = 0
			}
			if s.Fire[i] < 1e-5 {
				s.Fire[i] = 0
			}
		}
	})
	s.T += float64(dt)
}

// diffuse: one explicit step of diffusion (k = ν dt / cell², at most ¼); outside counts as nothing
func (s *GasSim) diffuse(f []float32, k float32) {
	W, H := s.GW, s.GH
	out := s.t0
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				var sum float32
				if x > 0 {
					sum += f[i-1]
				}
				if x < W-1 {
					sum += f[i+1]
				}
				if y > 0 {
					sum += f[i-W]
				}
				if y < H-1 {
					sum += f[i+W]
				}
				out[i] = f[i] + k*(sum-4*f[i])
			}
		}
	})
	copy(f, out)
}

// project takes the divergence out of the velocity
func (s *GasSim) project() {
	W, H := s.GW, s.GH
	cs := s.CS
	div := s.t0
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				xl, xr, yu, yd := max(x-1, 0), min(x+1, W-1), max(y-1, 0), min(y+1, H-1)
				div[y*W+x] = ((s.VX[y*W+xr] - s.VX[y*W+xl]) + (s.VY[yd*W+x] - s.VY[yu*W+x])) / (2 * cs)
			}
		}
	})
	p := s.P
	const omega = 1.8
	h2 := cs * cs
	for it := 0; it < 24; it++ {
		for color := 0; color < 2; color++ {
			s.rows(func(y0, y1 int) {
				for y := y0; y < y1; y++ {
					for x := (y + color) & 1; x < W; x += 2 {
						i := y*W + x
						var sum float32
						if x > 0 {
							sum += p[i-1]
						}
						if x < W-1 {
							sum += p[i+1]
						}
						if y > 0 {
							sum += p[i-W]
						}
						if y < H-1 {
							sum += p[i+W]
						}
						v := (sum - h2*div[i]) * 0.25
						p[i] += omega * (v - p[i])
					}
				}
			})
		}
	}
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				var pl, pr, pu, pd float32
				if x > 0 {
					pl = p[i-1]
				}
				if x < W-1 {
					pr = p[i+1]
				}
				if y > 0 {
					pu = p[i-W]
				}
				if y < H-1 {
					pd = p[i+W]
				}
				s.VX[i] -= (pr - pl) / (2 * cs)
				s.VY[i] -= (pd - pu) / (2 * cs)
			}
		}
	})
}

// advectAll carries the fields with the flow for dt (MacCormack: back, then forth to see the
// error, half of it corrected, kept within the values it came from). The weights of each cell are
// worked out once for all of them; a field that is all zero stays so.
func (s *GasSim) advectAll(fs [][]float32, dt float32) {
	W, H := s.GW, s.GH
	n := W * H
	k := dt / s.CS
	var live [][]float32
	var bufs [][3][]float32
	for j, f := range fs {
		if !anyNonZero(f) {
			continue
		}
		for len(s.adv) <= j {
			s.adv = append(s.adv, [3][]float32{make([]float32, n), make([]float32, n), make([]float32, n)})
		}
		live = append(live, f)
		bufs = append(bufs, s.adv[j])
	}
	if len(live) == 0 {
		return
	}
	// the four cells around (x, y) (in cells, centers at i + 0.5) and their weights; outside: none
	corners := func(x, y float32) (idx [4]int, w [4]float32) {
		x, y = x-0.5, y-0.5
		fx, fy := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
		ix, iy := int(fx), int(fy)
		ux, uy := x-fx, y-fy
		w = [4]float32{(1 - ux) * (1 - uy), ux * (1 - uy), (1 - ux) * uy, ux * uy}
		for q := 0; q < 4; q++ {
			cx, cy := ix+(q&1), iy+(q>>1)
			if cx < 0 || cy < 0 || cx >= W || cy >= H {
				idx[q] = -1
			} else {
				idx[q] = cy*W + cx
			}
		}
		return
	}
	// back: the value it came from, and the range of the four cells it was taken from
	s.rows(func(y0, y1 int) {
		for i := y0 * W; i < y1*W; i++ {
			idx, w := corners(s.bx[i], s.by[i])
			for j, f := range live {
				var v float32
				lo, hi := float32(math.MaxFloat32), float32(-math.MaxFloat32)
				for q := 0; q < 4; q++ {
					var c float32
					if idx[q] >= 0 {
						c = f[idx[q]]
					}
					v += w[q] * c
					lo, hi = min(lo, c), max(hi, c)
				}
				bufs[j][0][i], bufs[j][1][i], bufs[j][2][i] = v, lo, hi
			}
		}
	})
	// forth from there (the error) and the correction, within the range
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				idx, w := corners(float32(x)+0.5+k*s.VX[i], float32(y)+0.5+k*s.VY[i])
				for j, f := range live {
					fwd := bufs[j][0]
					var back float32
					for q := 0; q < 4; q++ {
						if idx[q] >= 0 {
							back += w[q] * fwd[idx[q]]
						}
					}
					v := fwd[i] + 0.5*(f[i]-back)
					bufs[j][1][i] = max(bufs[j][1][i], min(bufs[j][2][i], v)) // (the result, in place of lo)
				}
			}
		}
	})
	for j, f := range live {
		copy(f, bufs[j][1])
	}
}

func anyNonZero(f []float32) bool {
	for _, v := range f {
		if v != 0 {
			return true
		}
	}
	return false
}

// GasLight: how a GasFrame is lit: the light's direction (rad, 0 = from above, clockwise), how
// dark the smoke's own shadow gets, its thickness (opacity per unit of smoke), how hot looks hot
// (the flame look's heat), the reach of the light's walk and of the fire's light (px)
type GasLight struct {
	Angle, Shadow, Opacity, HeatK float32
	Reach, FireReach              float32
	Depth                         float32 // px of smoke the light goes through as through one unit of it
}

// GasFrame is the gas of one frame, ready to draw: per cell (GasCellWords halves pairs): smoke,
// the flames' heat; the light reaching it, the light around it; the fire's light, red; green, blue (× smoke);
// the two sets of wisp coordinates; and how much the first set counts now (W1)
type GasFrame struct {
	GW, GH int
	CS     float32
	W1     float32
	Cells  []uint32
}

// Frame: the gas now with its light
func (s *GasSim) Frame(l GasLight) *GasFrame {
	W, H := s.GW, s.GH
	n := W * H
	cs := s.CS
	// the light reaching each cell: through the smoke towards the light (24 steps)
	sun, amb, fire := make([]float32, n), make([]float32, n), make([]float32, n)
	dx, dy := float32(math.Sin(float64(l.Angle))), -float32(math.Cos(float64(l.Angle)))
	const steps = 24
	st := max(l.Reach, cs) / steps
	s.rows(func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				i := y*W + x
				if s.D[i] <= 0 {
					sun[i] = 1
					continue
				}
				px, py := float32(x)+0.5, float32(y)+0.5
				var tau float32
				for k := 1; k <= steps; k++ {
					d := (float32(k) - 0.5) * st / cs
					tau += s.sample(s.D, px+dx*d, py+dy*d, false)
				}
				tau *= st / l.Depth
				// light scattered many times inside gets deeper (a softer second term)
				k := float64(l.Opacity * l.Shadow * tau)
				sun[i] = float32(0.7*math.Exp(-k) + 0.3*math.Exp(-0.25*k))
			}
		}
	})
	// the light around: less where the smoke around is thick (a wide blur of it)
	rad := max(1, int(math.Round(float64(l.Reach/12/cs))))
	blur := func(dst, src []float32, r int) {
		tmp := make([]float32, n)
		boxBlur(tmp, src, W, H, r)
		boxBlur(dst, tmp, W, H, r)
	}
	blur(amb, s.D, rad)
	for i := range amb {
		// (never quite dark: light bounces off its surface)
		amb[i] = 0.35 + 0.65*float32(math.Exp(float64(-l.Opacity*0.5*amb[i])))
	}
	// the fire's light on the smoke around it: its glow, blurred wide
	for i := range fire {
		fire[i] = 1 - float32(math.Exp(float64(-s.Fire[i]*l.HeatK)))
	}
	fr := max(1, int(math.Round(float64(l.FireReach/cs/2))))
	tmp := make([]float32, n)
	blur(tmp, fire, fr)
	for i := range fire {
		fire[i] = min(1, tmp[i]*2)
	}
	g := &GasFrame{GW: W, GH: H, CS: cs, Cells: make([]uint32, n*GasCellWords)}
	ph := float32(math.Mod(s.T, gasPeriod) / gasPeriod)
	g.W1 = 1 - float32(math.Abs(float64(2*ph-1)))
	pk := func(a, b float32) uint32 { return uint32(F16(a)) | uint32(F16(b))<<16 }
	for i := 0; i < n; i++ {
		o := i * GasCellWords
		g.Cells[o] = pk(s.D[i], s.Fire[i])
		g.Cells[o+1] = pk(sun[i], amb[i])
		g.Cells[o+2] = pk(fire[i], s.R[i])
		g.Cells[o+3] = pk(s.G[i], s.B[i])
		g.Cells[o+4] = pk(s.C1x[i], s.C1y[i])
		g.Cells[o+5] = pk(s.C2x[i], s.C2y[i])
	}
	return g
}

// boxBlur: a box of radius r, across then down; outside counts as nothing
func boxBlur(dst, src []float32, W, H, r int) {
	tmp := make([]float32, W*H)
	k := 1 / float32(2*r+1)
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			var acc float32
			for x := -r; x <= r; x++ {
				if x >= 0 && x < W {
					acc += src[y*W+x]
				}
			}
			for x := 0; x < W; x++ {
				tmp[y*W+x] = acc * k
				if a := x - r; a >= 0 {
					acc -= src[y*W+a]
				}
				if b := x + r + 1; b < W {
					acc += src[y*W+b]
				}
			}
		}
	})
	parallelFor(W, func(x0, x1 int) {
		for x := x0; x < x1; x++ {
			var acc float32
			for y := -r; y <= r; y++ {
				if y >= 0 && y < H {
					acc += tmp[y*W+x]
				}
			}
			for y := 0; y < H; y++ {
				dst[y*W+x] = acc * k
				if a := y - r; a >= 0 {
					acc -= tmp[a*W+x]
				}
				if b := y + r + 1; b < H {
					acc += tmp[b*W+x]
				}
			}
		}
	})
}

// F16: a float32 as a half (rounded to nearest; as GLSL packHalf2x16)
func F16(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int((b>>23)&0xff) - 127 + 15
	man := b & 0x7fffff
	switch {
	case (b>>23)&0xff == 0xff:
		if man != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 31:
		return sign | 0x7bff // the largest half (not infinity)
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		man |= 0x800000
		sh := uint(14 - exp)
		h := man >> sh
		if rem := man & (1<<sh - 1); rem > 1<<(sh-1) || (rem == 1<<(sh-1) && h&1 == 1) {
			h++
		}
		return sign | uint16(h)
	}
	h := uint32(exp)<<10 | man>>13
	if rem := man & 0x1fff; rem > 0x1000 || (rem == 0x1000 && h&1 == 1) {
		h++
	}
	if h >= 0x7c00 {
		h = 0x7bff
	}
	return sign | uint16(h)
}

// HalfF: a half as a float32
func HalfF(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	man := uint32(h & 0x3ff)
	switch {
	case exp == 0:
		if man == 0 {
			return math.Float32frombits(sign)
		}
		v := float32(man) / (1 << 24)
		if sign != 0 {
			v = -v
		}
		return v
	case exp == 31:
		return math.Float32frombits(sign | 0x7f800000 | man<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | man<<13)
}

// cellAt: the 12 values of cell (i, j) of the gas frame (clamped to the grid)
func (g *GasFrame) cellAt(i, j int, v *[12]float32, w float32) {
	i, j = max(0, min(g.GW-1, i)), max(0, min(g.GH-1, j))
	o := (j*g.GW + i) * GasCellWords
	for k := 0; k < GasCellWords; k++ {
		c := g.Cells[o+k]
		v[2*k] += w * HalfF(uint16(c))
		v[2*k+1] += w * HalfF(uint16(c>>16))
	}
}

// at: the gas frame at (px, py) px: quadratic B-spline over the 3×3 nearest cells (smooth, as
// smoke is), as gasAt in emit.comp
func (g *GasFrame) at(px, py float32) [12]float32 {
	qx, qy := px/g.CS-0.5, py/g.CS-0.5
	cx, cy := float32(math.Floor(float64(qx+0.5))), float32(math.Floor(float64(qy+0.5)))
	tx, ty := qx-cx, qy-cy
	wx := [3]float32{0.5 * (0.5 - tx) * (0.5 - tx), 0.75 - tx*tx, 0.5 * (0.5 + tx) * (0.5 + tx)}
	wy := [3]float32{0.5 * (0.5 - ty) * (0.5 - ty), 0.75 - ty*ty, 0.5 * (0.5 + ty) * (0.5 + ty)}
	var v [12]float32
	for j := 0; j < 3; j++ {
		for i := 0; i < 3; i++ {
			g.cellAt(int(cx)+i-1, int(cy)+j-1, &v, wx[i]*wy[j])
		}
	}
	return v
}

// gasNoise: the wisps' noise (0..1, mean about ½): two fbm at different sizes
func gasNoise(x, y float32, seed uint32) float32 {
	return 0.7*efbm(x, y, seed) + 0.3*efbm(2.7*x+5.1, 2.7*y+1.9, seed+7)
}

// gasPixel: the gas look at (px, py) (premultiplied), as gasPixel in emit.comp: the smoke eaten
// at its edges by wisps carried by the flow, lit by the light that reaches it (and by the fire),
// the flame where it is hot in front
func gasPixel(p *EmitParams, g *GasFrame, px, py float32) [4]float32 {
	v := g.at(px, py)
	d0, heat := v[0], v[1]
	if d0 < 1e-4 && heat < 1e-4 {
		return [4]float32{}
	}
	sc := max(float32(p.GasWispScale), 1)
	w1 := g.W1
	w2 := 1 - w1
	na := gasNoise((px+v[8])/sc, (py+v[9])/sc, p.Seed+41)
	nb := gasNoise((px+v[10])/sc+31.7, (py+v[11])/sc+17.1, p.Seed+41)
	n := w1*na + w2*nb
	n = 0.5 + (n-0.5)/float32(math.Sqrt(float64(w1*w1+w2*w2)))
	wsp := float32(p.GasWisp)
	d := max(0, d0*(1+wsp*2.4*(n-0.5))-wsp*0.12*(1-n))
	al := 1 - float32(math.Exp(float64(-float32(p.GasOpacity)*d)))
	inv := 1 / max(d0, 1e-3)
	sun := max(0, min(1, v[2]*(1+wsp*0.5*(n-0.5))))
	amb, fire := v[3], v[4]
	var o [4]float32
	for c := 0; c < 3; c++ {
		dye := max(0, min(1, v[5+c]*inv))
		lit := dye * (float32(p.GasLightCol[c])*sun + float32(p.GasShadow[c])*amb + float32(p.FlameCol[1][c])*fire*float32(p.GasFireLight))
		o[c] = lit * al
	}
	o[3] = al
	// the flame: its heat, its edges eaten by the same wisps
	if heat > 1e-4 {
		ht := 1 - float32(math.Exp(float64(-heat*float32(p.Heat))))
		if p.Edge > 0 {
			k := float32(p.Edge) * (1 - n) * 0.6
			ht = max(0, min(1, (ht-k)/(1-k)))
		}
		// light: it adds its glow and only partly hides what is behind it
		fc, af := flameColor(p, ht)
		ac := af * 0.7
		for c := 0; c < 3; c++ {
			o[c] = fc[c]*af + o[c]*(1-ac)
		}
		o[3] = ac + o[3]*(1-ac)
	}
	return o
}
