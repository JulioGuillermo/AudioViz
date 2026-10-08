package effects

import "math"

// Parallax is the 2.5D camera: with a depth map, a point of the background at nearness z (0 far
// … 1 near) seen at frame point u appears moved by (z − Focus)·(S + Dolly·(u − c)): what is
// nearer than the focus goes one way, what is farther the other, and moving in (Dolly) makes the
// near grow more than the far. Same math as bgParallax in render.comp.
type Parallax struct {
	SX, SY float64 // px the nearest things move (against the focus)
	Dolly  float64
	Focus  float64
}

// On: the camera moves
func (p Parallax) On() bool { return p.SX != 0 || p.SY != 0 || p.Dolly != 0 }

// MaxDisp: how far (px) anything moves at most
func (p Parallax) MaxDisp(W, H int) float64 {
	m := math.Max(p.Focus, 1-p.Focus)
	return m * (math.Hypot(p.SX, p.SY) + math.Abs(p.Dolly)*math.Hypot(float64(W), float64(H))/2)
}

// EdgeZoom: the zoom that keeps the frame's edges out of sight: the shift's room, and what
// moving in (or out) shrinks (moving in with the focus at the back shrinks nothing)
func (p Parallax) EdgeZoom(W, H int) float64 {
	if !p.On() {
		return 1
	}
	m := math.Max(p.Focus, 1-p.Focus)
	shrink := p.Dolly * p.Focus
	if p.Dolly < 0 {
		shrink = -p.Dolly * (1 - p.Focus)
	}
	return (1 + 2*m*math.Hypot(p.SX, p.SY)/float64(max(min(W, H), 64))) * (1 + 1.1*shrink)
}

// Steps: how many depths the search tries (more for larger moves)
func (p Parallax) Steps(W, H int) int {
	return max(6, min(32, int(p.MaxDisp(W, H)/2)+4))
}

// Disp: how far a thing at nearness z at frame point (x, y) moves (clips and emitters in the depth)
func (p Parallax) Disp(z, x, y float64, W, H int) (float64, float64) {
	dx, dy := p.disp(float32(z), float32(x), float32(y), float32(W)*0.5, float32(H)*0.5)
	return float64(dx), float64(dy)
}

// disp: how far a point at nearness z seen at (ux, uy) is moved
func (p Parallax) disp(z, ux, uy, cx, cy float32) (float32, float32) {
	k := z - float32(p.Focus)
	return k * (float32(p.SX) + float32(p.Dolly)*(ux-cx)), k * (float32(p.SY) + float32(p.Dolly)*(uy-cy))
}

// Find: the point of the background seen at frame point (ux, uy) and its nearness. From the
// nearest depth to the farthest, the first one where the scene is at least that near is what is
// seen (what is near hides what is behind it), refined between the last two tries. Things have a
// thickness: a hit whose depth doesn't match once refined is the side of something nearer (not a
// surface seen there) and the search goes on behind it, so what the camera uncovers is filled
// with what is farther instead of a smear of the near thing. depthAt: the depth of the
// background (before the camera) at a frame point. As parallaxFind in render.comp.
func (p Parallax) Find(ux, uy float32, n, W, H int, depthAt func(x, y float32) float32) (float32, float32, float32) {
	cx, cy := float32(W)*0.5, float32(H)*0.5
	thick := 1.5/float32(n) + 0.02
	var pz, pe float32 // the previous try: its depth, how far the scene was in front of it (< 0)
	for k := 0; k <= n; k++ {
		z := 1 - float32(k)/float32(n)
		dx, dy := p.disp(z, ux, uy, cx, cy)
		x, y := ux-dx, uy-dy
		d := depthAt(x, y)
		e := d - z
		if e < 0 {
			pz, pe = z, e
			continue
		}
		if k == 0 {
			return x, y, d
		}
		t := pe / (pe - e)
		zr := pz + (z-pz)*t
		dx, dy = p.disp(zr, ux, uy, cx, cy)
		rx, ry := ux-dx, uy-dy
		if dr := depthAt(rx, ry); dr-zr <= thick {
			return rx, ry, dr
		}
		pz, pe = z, -1e-4 // the side of something nearer: on behind it
	}
	dx, dy := p.disp(0, ux, uy, cx, cy)
	return ux - dx, uy - dy, depthAt(ux-dx, uy-dy)
}
