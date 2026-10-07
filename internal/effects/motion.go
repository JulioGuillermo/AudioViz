package effects

import (
	"math"

	"audioviz/internal/config"
)

// PathAt is the point of a motion path at progress p (0..1 of its length; a closed path wraps
// around, an open one stops at its ends) in frame px, and its direction there (radians, 0 = right,
// clockwise on screen). The curve is the one the studio draws: cubic Béziers with each node's
// handles (nodeHandles), measured in px so that the speed is even.
func PathAt(sh *config.MaskShape, W, H, p float64) (x, y, angle float64) {
	pts := sh.Points
	n := len(pts)
	for _, q := range pts {
		if len(q) < 2 {
			return W / 2, H / 2, 0
		}
	}
	segs := n - 1
	if !sh.Open {
		segs = n
	}
	const sub = 24 // samples per segment
	type smp struct{ x, y, l float64 }
	s := make([]smp, 0, segs*sub+1)
	at := func(i int) []float64 { return pts[((i%n)+n)%n] }
	total := 0.0
	for i := 0; i < segs; i++ {
		b, c := at(i), at(i+1)
		_, out := nodeHandles(pts, i, sh.Open)
		in, _ := nodeHandles(pts, (i+1)%n, sh.Open)
		p0 := [2]float64{b[0] * W, b[1] * H}
		p1 := [2]float64{(b[0] + out[0]) * W, (b[1] + out[1]) * H}
		p2 := [2]float64{(c[0] + in[0]) * W, (c[1] + in[1]) * H}
		p3 := [2]float64{c[0] * W, c[1] * H}
		for k := 0; k <= sub; k++ {
			if k == 0 && i > 0 {
				continue
			}
			t := float64(k) / sub
			u := 1 - t
			bx := u*u*u*p0[0] + 3*u*u*t*p1[0] + 3*u*t*t*p2[0] + t*t*t*p3[0]
			by := u*u*u*p0[1] + 3*u*u*t*p1[1] + 3*u*t*t*p2[1] + t*t*t*p3[1]
			if len(s) > 0 {
				total += math.Hypot(bx-s[len(s)-1].x, by-s[len(s)-1].y)
			}
			s = append(s, smp{bx, by, total})
		}
	}
	if len(s) < 2 || total <= 0 {
		return s[0].x, s[0].y, 0
	}
	if sh.Open {
		p = math.Max(0, math.Min(1, p))
	} else {
		p -= math.Floor(p)
	}
	d := p * total
	j := 1
	for j < len(s)-1 && s[j].l < d {
		j++
	}
	a, b := s[j-1], s[j]
	f := 0.0
	if b.l > a.l {
		f = (d - a.l) / (b.l - a.l)
	}
	return a.x + (b.x-a.x)*f, a.y + (b.y-a.y)*f, math.Atan2(b.y-a.y, b.x-a.x)
}
