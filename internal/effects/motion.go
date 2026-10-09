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
	for _, q := range sh.Points {
		if len(q) < 2 {
			return W / 2, H / 2, 0
		}
	}
	s, _, total := pathSamples(sh, W, H)
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

// FlipTurn: how something turned with a path going in direction (tx, ty) is turned (rad) and
// scaled along it (sx) and across it (sy; negative = mirrored), as Motion.Flip says, going back
// (to the left):
//   - "": upside down as the path is;
//   - "letters": each letter (or the whole clip) turns round like a card, seen from behind;
//   - "twist": like a ribbon that twists round across the path, seen from behind after (texts
//     twist per pixel: Ribbon);
//   - "wrap" ("fold", "mirror"): like a ribbon round a cylinder: its height stays upright, its
//     length follows the path, so going back it is seen from behind and edge-on where the path
//     goes up or down (texts: their letters slanted, IsWrap; a clip: as low as it is edge-on);
//   - "readable": like a card that turns round, upright and readable after.
func FlipTurn(tx, ty float64, mode string) (rot, sx, sy float64) {
	k := math.Min(1, math.Abs(tx)/0.25)
	switch mode {
	case "letters":
		if tx >= 0 {
			return math.Atan2(ty, tx), k, 1
		}
		return math.Atan2(-ty, -tx), -k, 1
	case "twist":
		return math.Atan2(ty, tx), 1, RibbonTwist(tx)
	case "wrap", "fold", "mirror":
		return math.Atan2(ty, tx), 1, tx
	case "readable":
		if tx >= 0 {
			return math.Atan2(ty, tx), k, 1
		}
		return math.Atan2(-ty, -tx), k, 1
	}
	return math.Atan2(ty, tx), 1, 1
}

// IsRibbon: a Motion.Flip mode where a text twists per pixel (Ribbon)
func IsRibbon(mode string) bool { return mode == "twist" }

// IsWrap: a Motion.Flip mode where a text goes round like a ribbon on a cylinder
func IsWrap(mode string) bool { return mode == "wrap" || mode == "fold" || mode == "mirror" }

// WrapFrame: a letter wrapped round a cylinder along a path going in direction (tx, ty), its
// height upright (ux, uy: the text's up turned by its own rotation, pointing down the letter):
// turned with the path (rot), its height seen as ky (negative: from behind) and slanted by shear
// (its x moves by shear × its y), as drawText reads them
func WrapFrame(tx, ty, ux, uy float64) (rot, ky, shear float64) {
	rot = math.Atan2(ty, tx)
	c, s := math.Cos(rot), math.Sin(rot)
	return rot, -s*ux + c*uy, c*ux + s*uy
}

// RibbonTwist: how much a ribbon going along a path in direction (tx, ty) is seen across it: 1
// facing (going right), 0 edge-on (going straight up or down), −1 its back (going left); smooth,
// so that it twists little by little as the path turns
func RibbonTwist(tx float64) float64 {
	return math.Sin(math.Max(-1, math.Min(1, tx/0.6)) * math.Pi / 2)
}

// Ribbon: a text twisting like a ribbon along a path (Motion.Flip "twist"): at RibbonN points
// evenly along it, how much it is seen across it (K: 1 facing … −1 its back, RibbonTwist; spread
// along it so that it turns little by little) and on which side the outside of the curve is (O)
type Ribbon struct {
	L      float32
	Closed bool
	K, O   []float32
}

// RibbonN: points of a Ribbon
const RibbonN = 128

// RibbonSmooth: px along the path its twist is spread over (so that it turns little by little)
const RibbonSmooth = 90

// NewRibbon: the ribbon of a sampled path (pts, L as SamplePath)
func NewRibbon(pts []float32, L float32, closed bool) *Ribbon {
	r := &Ribbon{L: L, Closed: closed, K: make([]float32, RibbonN), O: make([]float32, RibbonN)}
	const d = 30 // px either side to see how it turns
	for i := 0; i < RibbonN; i++ {
		s := L * float32(i) / (RibbonN - 1)
		_, _, tx, _ := pathPoint(pts, L, closed, s)
		_, _, ax, ay := pathPoint(pts, L, closed, s-d)
		_, _, bx, by := pathPoint(pts, L, closed, s+d)
		cross := float64(ax*by - ay*bx) // > 0: it turns towards the text's bottom (y down)
		r.K[i] = float32(RibbonTwist(float64(tx)))
		r.O[i] = float32(-math.Max(-1, math.Min(1, cross/0.15)))
	}
	// spread over RibbonSmooth px: it turns little by little, without parting
	w := int(math.Round(RibbonSmooth / float64(max(L, 1)) * (RibbonN - 1) / 2))
	if w > 0 {
		r.K, r.O = boxSmooth(r.K, w, closed), boxSmooth(r.O, w, closed)
	}
	return r
}

// boxSmooth: v averaged over ±w samples (round when closed)
func boxSmooth(v []float32, w int, closed bool) []float32 {
	n := len(v)
	out := make([]float32, n)
	for i := range v {
		var s float32
		c := 0
		for j := i - w; j <= i+w; j++ {
			k := j
			if closed {
				k = ((j % n) + n) % n
			} else if k < 0 || k >= n {
				continue
			}
			s += v[k]
			c++
		}
		out[i] = s / float32(c)
	}
	return out
}

// At: K and O at s px along the path
func (r *Ribbon) At(s float32) (k, o float32) {
	if r.Closed && r.L > 0 {
		s -= r.L * float32(math.Floor(float64(s/r.L)))
	}
	f := max(0, min(RibbonN-1, s/max(r.L, 1e-6)*(RibbonN-1)))
	i := min(int(f), RibbonN-2)
	t := f - float32(i)
	return r.K[i] + (r.K[i+1]-r.K[i])*t, r.O[i] + (r.O[i+1]-r.O[i])*t
}

// V: the height in a letter (−hh … hh, its px; up < 0) seen at height ly of it (on screen) at sp
// px along the path; false: none (edge-on). As ribbonV in render.comp.
func (r *Ribbon) V(sp, ly float32) (float32, bool) {
	k, _ := r.At(sp)
	if math.Abs(float64(k)) < 0.02 {
		return 0, false
	}
	return ly / k, true
}
