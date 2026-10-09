package effects

import (
	"math"
	"slices"

	"audioviz/internal/config"
)

// Values on a path's nodes (config.NodeVals): a depth and named values, each one interpolated
// along the path (by its length) only between the nodes that define it, with the easing of the
// node it comes from. Before the first node that defines it, it goes from 0 at the path's start
// (unless that node is the first); after the last it stays. Nodes that don't define it don't take
// part. The GPU does the same with the track's points (trackAt in common.glsl).

// PathTrack: one value along a path: its points (px of length along the path), their values and
// the easing towards the next (an index of config.Eases)
type PathTrack struct {
	S, V []float32
	E    []uint8
}

// NodeLens: how far along a path (px) each of its nodes is, and its whole length. As PathAt
// measures it (the same Béziers); a closed path's first node is at 0.
func NodeLens(sh *config.MaskShape, W, H float64) ([]float64, float64) {
	_, nodes, total := pathSamples(sh, W, H)
	return nodes, total
}

// Track: the track of a value (key; "z": the depth) of a path in a W×H frame; nil when no node
// defines it
func Track(sh *config.MaskShape, key string, W, H float64) *PathTrack {
	if sh == nil || len(sh.Vals) == 0 {
		return nil
	}
	nodes, _ := NodeLens(sh, W, H)
	var t PathTrack
	for i, s := range nodes {
		v, e, ok := sh.ValAt(i, key)
		if !ok {
			continue
		}
		if len(t.S) == 0 && i > 0 {
			// from 0 at the start of the path
			t.S, t.V, t.E = append(t.S, 0), append(t.V, 0), append(t.E, 0)
		}
		t.S, t.V, t.E = append(t.S, float32(s)), append(t.V, float32(v)), append(t.E, easeID(e))
	}
	if len(t.S) == 0 {
		return nil
	}
	return &t
}

// easeID: an easing's index in config.Eases (linear when unknown)
func easeID(name string) uint8 {
	if i := slices.Index(config.Eases, name); i > 0 {
		return uint8(i)
	}
	return 0
}

// At: the value s px along the path
func (t *PathTrack) At(s float32) float32 {
	n := len(t.S)
	if s <= t.S[0] {
		return t.V[0]
	}
	if s >= t.S[n-1] {
		return t.V[n-1]
	}
	j := 1
	for j < n-1 && t.S[j] < s {
		j++
	}
	a := j - 1
	k := float64((s - t.S[a]) / max(t.S[j]-t.S[a], 1e-6))
	e := config.Ease(config.Eases[min(int(t.E[a]), len(config.Eases)-1)], k)
	return t.V[a] + (t.V[j]-t.V[a])*float32(e)
}

// pathSamples: a path measured in px (as PathAt): its samples (x, y, length so far), how far
// along it each node is, and its length
func pathSamples(sh *config.MaskShape, W, H float64) ([]pathSmp, []float64, float64) {
	pts := sh.Points
	n := len(pts)
	if n == 0 {
		return []pathSmp{{W / 2, H / 2, 0}}, nil, 0
	}
	for _, q := range pts {
		if len(q) < 2 {
			return []pathSmp{{W / 2, H / 2, 0}}, make([]float64, n), 0
		}
	}
	segs := n - 1
	if !sh.Open {
		segs = n
	}
	const sub = 24 // samples per segment
	s := make([]pathSmp, 0, segs*sub+1)
	nodes := make([]float64, n)
	at := func(i int) []float64 { return pts[((i%n)+n)%n] }
	total := 0.0
	for i := 0; i < segs; i++ {
		if i < n {
			nodes[i] = total
		}
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
			s = append(s, pathSmp{bx, by, total})
		}
	}
	if sh.Open && n > 0 {
		nodes[n-1] = total
	}
	return s, nodes, total
}

type pathSmp struct{ x, y, l float64 }
