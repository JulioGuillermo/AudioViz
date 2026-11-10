package effects

import "math"

// The particles' route as a guide for the simulated looks (gas, liquid): a channel round the
// route where the flow is pulled towards going along it (at the emitter's speed) and back towards
// the route when it strays. Each cell holds the velocity it is pulled to (x, y px/s) and how much
// (0..1); the simulations relax their velocity towards it as they do towards the wind, at
// GuideRate per second times that weight.

// GuideRate: 1/s, how fast the flow takes the guide's velocity where its weight is 1
const GuideRate = 4

// SimGuide: the guide on a gw×gh grid of cs px from a route sampled as SamplePath (x, y, length
// per sample, px), moved by (sx, sy); width: the channel's half width (px; it fades out by twice
// that), speed: along it (px/s), pull: 0..1 how hard it pulls back to the route, k: the weight
// (0..1). nil: nothing to guide
func SimGuide(path []float32, closed bool, sx, sy float32, gw, gh int, cs, width, speed, pull, k float32) []float32 {
	n := len(path) / 3
	if n < 2 || k <= 0 || width <= 0 || gw <= 0 || gh <= 0 {
		return nil
	}
	cells := gw * gh
	best := make([]float32, cells)
	for i := range best {
		best[i] = math.MaxFloat32
	}
	tx := make([]float32, cells)
	ty := make([]float32, cells)
	nx := make([]float32, cells)
	ny := make([]float32, cells)
	reach := 2 * width
	segs := n - 1
	if closed {
		segs = n
	}
	for s := 0; s < segs; s++ {
		a, b := s, (s+1)%n
		ax, ay := path[a*3]+sx, path[a*3+1]+sy
		bx, by := path[b*3]+sx, path[b*3+1]+sy
		dx, dy := bx-ax, by-ay
		l2 := dx*dx + dy*dy
		if l2 < 1e-6 {
			continue
		}
		l := float32(math.Sqrt(float64(l2)))
		ux, uy := dx/l, dy/l
		x0 := max(0, int((min(ax, bx)-reach)/cs))
		x1 := min(gw-1, int((max(ax, bx)+reach)/cs))
		y0 := max(0, int((min(ay, by)-reach)/cs))
		y1 := min(gh-1, int((max(ay, by)+reach)/cs))
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				px, py := (float32(x)+0.5)*cs, (float32(y)+0.5)*cs
				t := max(0, min(1, ((px-ax)*dx+(py-ay)*dy)/l2))
				cx, cy := ax+t*dx-px, ay+t*dy-py
				d2 := cx*cx + cy*cy
				i := y*gw + x
				if d2 < best[i] {
					best[i] = d2
					tx[i], ty[i] = ux, uy
					nx[i], ny[i] = cx, cy
				}
			}
		}
	}
	out := make([]float32, cells*3)
	any := false
	for i := range best {
		if best[i] >= reach*reach {
			continue
		}
		d := float32(math.Sqrt(float64(best[i])))
		w := k * (1 - smooth32(width, reach, d))
		if w <= 0 {
			continue
		}
		vx, vy := speed*tx[i], speed*ty[i]
		if d > 1e-3 && pull > 0 {
			// back towards the route: harder the farther out (it comes back in about half a second)
			p := pull * 2 * min(d, width) / d
			vx += nx[i] * p
			vy += ny[i] * p
		}
		out[i*3], out[i*3+1], out[i*3+2] = vx, vy, w
		any = true
	}
	if !any {
		return nil
	}
	return out
}

// guideK: how much of the gap to the guide's velocity a step of dt closes at weight w
func guideK(w, dt float32) float32 { return 1 - float32(math.Exp(float64(-GuideRate*w*dt))) }
