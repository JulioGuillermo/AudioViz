package effects

import (
	"math"

	"audioviz/internal/config"
)

// Burn transition: the old image burns like paper. Each point of the frame catches fire when the
// fire front, growing from the ignition points, reaches it; the distance to the points is
// distorted by fractal noise (the fibers of the paper) so the front is irregular. Ahead of the
// front the paper chars (browns, then blackens), at the front an ember line glows and flickers,
// with sparks in the charred band, and behind it the new image shows through the hole, sooty
// near the edge. Same math in burnPixel and in render.comp (GPU); coordinates are in units of the
// frame height so it looks the same at any resolution.

// BurnParams: what the renderer needs (computed once per transition by BurnSetup)
type BurnParams struct {
	Pts         [][2]float64 // ignition points, x in 0..W/H, y in 0..1
	Edge, Rough float64
	Color       [3]float64 // 0..1
	MaxD        float64    // largest distance from a frame point to its nearest ignition point
}

// BurnSetup turns the options into ignition points (random ones from the seed) and the distances
func BurnSetup(b *config.Burn, seed uint32, w, h int) BurnParams {
	ar := float64(w) / float64(h)
	p := BurnParams{Edge: 0.5, Rough: 0.5, Color: [3]float64{1, 0.48, 0.12}}
	n := 3
	if b != nil {
		if b.Edge > 0 {
			p.Edge = math.Min(1, b.Edge)
		}
		if b.Roughness > 0 {
			p.Rough = math.Min(1, b.Roughness)
		}
		if c, ok := parseHex(b.Color); ok {
			p.Color = c
		}
		if b.Count > 0 {
			n = b.Count
		}
		if b.Seed > 0 {
			seed = uint32(b.Seed)
		}
		if b.Origins == "points" {
			for _, q := range b.Points {
				if len(p.Pts) < config.MaxBurnPoints {
					p.Pts = append(p.Pts, [2]float64{math.Max(0, math.Min(1, q[0])) * ar, math.Max(0, math.Min(1, q[1]))})
				}
			}
			if len(p.Pts) == 0 {
				p.Pts = [][2]float64{{0.5 * ar, 0.5}}
			}
		}
	}
	if len(p.Pts) == 0 {
		n = max(1, min(n, config.MaxBurnPoints))
		for i := range n {
			x := 0.08 + 0.84*hash3(uint32(i), 1, seed)
			y := 0.08 + 0.84*hash3(uint32(i), 2, seed)
			p.Pts = append(p.Pts, [2]float64{x * ar, y})
		}
	}
	// The front has to sweep the whole frame: farthest point from the ignition points
	for gy := 0; gy <= 36; gy++ {
		for gx := 0; gx <= 64; gx++ {
			u := [2]float64{float64(gx) / 64 * ar, float64(gy) / 36}
			p.MaxD = math.Max(p.MaxD, nearest(p.Pts, u))
		}
	}
	return p
}

func nearest(pts [][2]float64, u [2]float64) float64 {
	d := math.Inf(1)
	for _, q := range pts {
		d = math.Min(d, math.Hypot(u[0]-q[0], u[1]-q[1]))
	}
	return d
}

func parseHex(s string) ([3]float64, bool) {
	if len(s) == 7 && s[0] == '#' {
		var v [3]float64
		for i := range 3 {
			var x int
			for _, c := range s[1+2*i : 3+2*i] {
				x *= 16
				switch {
				case c >= '0' && c <= '9':
					x += int(c - '0')
				case c >= 'a' && c <= 'f':
					x += int(c-'a') + 10
				case c >= 'A' && c <= 'F':
					x += int(c-'A') + 10
				default:
					return v, false
				}
			}
			v[i] = float64(x) / 255
		}
		return v, true
	}
	return [3]float64{}, false
}

// vnoise: value noise on a unit grid, 0..1 (as in render.comp)
func vnoise(x, y float64, seed uint32) float64 {
	ix, iy := math.Floor(x), math.Floor(y)
	fx, fy := x-ix, y-iy
	ux, uy := fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	cx, cy := uint32(int32(ix)+4096), uint32(int32(iy)+4096)
	a := hash3(cx, cy, seed)
	b := hash3(cx+1, cy, seed)
	c := hash3(cx, cy+1, seed)
	d := hash3(cx+1, cy+1, seed)
	return a + (b-a)*ux + (c-a)*uy + (a-b-c+d)*ux*uy
}

// fbm: 4 octaves of value noise, about 0..1
func fbm(x, y float64, seed uint32) float64 {
	s, amp, tot := 0.0, 0.5, 0.0
	for o := range 4 {
		s += amp * vnoise(x, y, seed+uint32(o)*101)
		tot += amp
		x, y = x*2+17.3, y*2+5.1
		amp *= 0.5
	}
	return s / tot
}

func burnPixel(sa, sb *sampler, x, y float64, w, h int, tr Trans) rgb {
	H := float64(h)
	k := math.Max(0, math.Min(1, tr.K))
	ke := k * k * (3 - 2*k)
	bp := tr.Burn
	u := [2]float64{x / H, y / H}
	amp := 0.25 * bp.Rough
	charW := 0.02 + 0.1*bp.Edge
	emberW := 0.004 + 0.012*bp.Edge
	// Distance to the fire: irregular at large scale (roughness) and crinkled at small scale
	d := nearest(bp.Pts, u) + amp*(fbm(u[0]*5, u[1]*5, tr.Seed)-0.5)*2 + 0.012*(vnoise(u[0]*90, u[1]*90, tr.Seed+5)-0.5)
	r := ke*(bp.MaxD+2*amp+2*charW) - (amp + charW)
	s := r - d // > 0: burned
	a, b := sa.at(x, y), sb.at(x, y)
	// The paper chars ahead of the front: browner, then nearly black at the edge
	ch := smoothstep(-charW, 0, s)
	a = mixc(a, rgb{a[0] * 0.55, a[1] * 0.38, a[2] * 0.22}, ch*0.8)
	a = mixc(a, rgb{13, 8, 5}, smoothstep(-charW*0.35, 0, s)*0.9)
	// Soot on the new image along the hole
	if s > 0 {
		f := 1 - 0.45*math.Exp(-s/(charW*0.4))
		b = rgb{b[0] * f, b[1] * f, b[2] * f}
	}
	c := mixc(a, b, smoothstep(0, 1.5/H, s))
	// Ember: a thin line on the paper side of the edge (a black crisp rim stays between it and the
	// hole), broken into glowing stretches that flicker, plus a soft halo of light
	brk := smoothstep(0.3, 0.7, vnoise(u[0]*25+k*6, u[1]*25-k*4, tr.Seed+11))
	e := (s + emberW*1.2) / emberW
	em := math.Exp(-e*e) * (0.35 + 0.9*brk)
	g := s / (emberW * 5)
	em += 0.22 * math.Exp(-g*g) * (0.5 + 0.5*brk)
	// Sparks in the charred band
	if s < 0 && s > -charW*0.6 {
		cell := uint32(math.Floor(x / (H / 360)))
		row := uint32(math.Floor(y / (H / 360)))
		if hash3(cell, row, tr.Seed+uint32(k*24)) > 0.985 {
			em += 0.8 * (1 + s/(charW*0.6))
		}
	}
	hot := math.Pow(math.Min(em, 1), 3) * 0.6
	return rgb{
		c[0] + 255*(bp.Color[0]*em*1.6+hot),
		c[1] + 255*(bp.Color[1]*em*1.6+hot*0.9),
		c[2] + 255*(bp.Color[2]*em*1.6+hot*0.6),
	}
}
