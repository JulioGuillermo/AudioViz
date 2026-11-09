package effects

import "math"

// Scene is how near each point of the frame is (0 far … 1 near): the depth maps of the
// backgrounds A and B (fitted to the frame) seen through the background's own ripple, zoom,
// shake, motion and transition. Same math as sceneDepth in render.comp, in float32.
type Scene struct {
	A, B                            []uint16 // W×H, 65535 = near
	NA, NB                          []uint16 // their surface normals (NormalMap; nil: from the slope)
	LA, LB                          Layer
	K                               float64 // transition progress: B from halfway on
	Tr                              Trans   // the transition (its geometry: TransPick) while A and B both have depth
	Zoom, ShakeX, ShakeY            float64
	RippleAmp, RippleFreq, RippleTm float64
	W, H                            int
	Par                             Parallax // the 2.5D camera
	N                               int      // its search steps (Parallax.Steps)
	DoF                             DoF      // the depth of field (clips in the depth blur with it)
	// in a transition, the image the camera and the depth of field are on (tied to a background
	// segment: 1 only A, 2 only B; 0 both)
	ParSide, DoFSide int
}

// On: there is a depth to look at
func (s *Scene) On() bool { return s != nil && (s.A != nil || s.B != nil) }

// share: how much of an effect on image on (0 both) reaches what is in depth on image side (0
// both: as much as the transition shows of the effect's image; as objW in render.comp)
func (s *Scene) share(on, side int) float64 {
	switch {
	case on == 0:
		return 1
	case side != 0:
		if side == on {
			return 1
		}
		return 0
	}
	k := math.Max(0, math.Min(1, s.K))
	if on == 1 {
		return 1 - k
	}
	return k
}

// Cam: the 2.5D camera on what is in depth on image side (0 both)
func (s *Scene) Cam(side int) Parallax {
	if !s.On() {
		return Parallax{}
	}
	p, w := s.Par, s.share(s.ParSide, side)
	p.SX, p.SY, p.Dolly = p.SX*w, p.SY*w, p.Dolly*w
	return p
}

// Lens: the depth of field on what is in depth on image side (0 both)
func (s *Scene) Lens(side int) DoF {
	if !s.On() {
		return DoF{}
	}
	d := s.DoF
	d.Blur *= s.share(s.DoFSide, side)
	return d
}

// camOn: whether the camera moves background image w (0 A, 1 B)
func (s *Scene) camOn(w int) bool { return s.Par.On() && (s.ParSide == 0 || s.ParSide == w+1) }

func depthTexel(g []uint16, x, y, W, H int) float32 {
	x, y = max(0, min(W-1, x)), max(0, min(H-1, y))
	return float32(g[y*W+x]) / 65535
}

// layerDepth: depth image g at output point q with the layer transform (as layerAt in common.glsl)
func layerDepth(g []uint16, L Layer, qx, qy float32, W, H int) float32 {
	cx, cy := float32(W)*0.5, float32(H)*0.5
	dx, dy := qx-cx, qy-cy
	if L.Rot != 0 {
		c, s := float32(math.Cos(-L.Rot)), float32(math.Sin(-L.Rot))
		dx, dy = dx*c-dy*s, dx*s+dy*c
	}
	sc := float32(L.Scale)
	if sc <= 0 {
		sc = 1
	}
	u, v := cx+dx/sc+float32(L.OffX), cy+dy/sc+float32(L.OffY)
	u, v = max(0, min(float32(W-1), u)), max(0, min(float32(H-1), v))
	x0, y0 := int(math.Floor(float64(u))), int(math.Floor(float64(v)))
	fx, fy := u-float32(x0), v-float32(y0)
	a := depthTexel(g, x0, y0, W, H) + (depthTexel(g, x0+1, y0, W, H)-depthTexel(g, x0, y0, W, H))*fx
	b := depthTexel(g, x0, y0+1, W, H) + (depthTexel(g, x0+1, y0+1, W, H)-depthTexel(g, x0, y0+1, W, H))*fx
	return a + (b-a)*fy
}

// bgPoint: frame point q through the background's ripple, zoom and shake
func (s *Scene) bgPoint(qx, qy float32) (float32, float32) {
	cx, cy := float32(s.W)*0.5, float32(s.H)*0.5
	if amp := float32(s.RippleAmp); amp > 0.2 {
		ph := float64(qy)*s.RippleFreq + s.RippleTm
		qx += float32(math.Sin(ph)) * amp
		qy += float32(math.Cos(ph)) * amp
		qx, qy = max(0, min(float32(s.W-1), qx)), max(0, min(float32(s.H-1), qy))
	}
	if z := float32(s.Zoom); z > 1.001 {
		qx, qy = cx+(qx-cx)/z, cy+(qy-cy)/z
	}
	return qx - float32(s.ShakeX), qy - float32(s.ShakeY)
}

// The depths in a transition, per frame pixel (as parallaxPixel in render.comp): each image's depth
// where the transition shows it, the one seen there (w: 0 A, the old one; 1 B, the new one)
func (s *Scene) both() bool { return s.A != nil && s.B != nil }

func (s *Scene) nearW(w int, x, y float32) float32 {
	if (w == 1 && s.B != nil) || s.A == nil {
		return layerDepth(s.B, s.LB, x, y, s.W, s.H)
	}
	return layerDepth(s.A, s.LA, x, y, s.W, s.H)
}

func (s *Scene) normalW(w int, x, y float32) (float32, float32, float32, bool) {
	g, L := s.NA, s.LA
	if (w == 1 && s.B != nil) || s.A == nil {
		g, L = s.NB, s.LB
	}
	if g == nil {
		return 0, 0, 1, false
	}
	nx, ny, nz := layerNormal(g, L, x, y, s.W, s.H)
	return nx, ny, nz, true
}

// HitW: as Hit, and which background's depth the pixel uses
func (s *Scene) HitW(qx, qy float32) (float32, float32, float32, int) {
	if !s.On() {
		return qx, qy, 0, 0
	}
	w := 0
	if s.B != nil {
		w = 1
	}
	ax, ay, bx, by, wb := qx, qy, qx, qy, float64(w)
	if s.both() {
		fax, fay, fbx, fby, fwb := TransPick(s.Tr, float64(qx), float64(qy), s.W, s.H)
		ax, ay, bx, by, wb = float32(fax), float32(fay), float32(fbx), float32(fby), fwb
		w = 0
		if wb >= 0.5 {
			w = 1
		}
	}
	uax, uay := s.bgPoint(ax, ay)
	ubx, uby := s.bgPoint(bx, by)
	ux, uy := uax, uay
	if w == 1 {
		ux, uy = ubx, uby
	}
	near := func(x, y float32) float32 { return s.nearW(w, x, y) }
	if s.camOn(0) || s.camOn(1) {
		// (each image's hit: through the camera if it moves that image)
		find := func(i int, x, y float32) (float32, float32, float32) {
			nr := func(x, y float32) float32 { return s.nearW(i, x, y) }
			if !s.camOn(i) {
				return x, y, nr(x, y)
			}
			return s.Par.Find(x, y, s.N, s.W, s.H, nr)
		}
		x, y, z := find(w, ux, uy)
		if s.both() && wb > 0 && wb < 1 { // (each image's hit: the nearness blends)
			o := 1 - w
			ox, oy := uax, uay
			if o == 1 {
				ox, oy = ubx, uby
			}
			_, _, zo := find(o, ox, oy)
			za, zb := z, zo
			if w == 1 {
				za, zb = zo, z
			}
			z = za + (zb-za)*float32(wb)
		}
		return x, y, z, w
	}
	z := near(ux, uy)
	if s.both() && wb > 0 && wb < 1 { // (blends: both)
		za, zb := s.nearW(0, uax, uay), s.nearW(1, ubx, uby)
		z = za + (zb-za)*float32(wb)
	}
	return ux, uy, z, w
}

// Normal: the surface normal at background point (x, y) (as layerNormal in render.comp); false
// when the depth map has no normal map
func (s *Scene) Normal(x, y float32) (float32, float32, float32, bool) {
	g, L := s.NA, s.LA
	if s.B != nil && (s.A == nil || s.K >= 0.5) {
		g, L = s.NB, s.LB
	}
	if g == nil {
		return 0, 0, 1, false
	}
	nx, ny, nz := layerNormal(g, L, x, y, s.W, s.H)
	return nx, ny, nz, true
}

// Near: the nearness at background point (x, y) (as Hit gives; no camera)
func (s *Scene) Near(x, y float32) float32 {
	if s.B != nil && (s.A == nil || s.K >= 0.5) {
		return layerDepth(s.B, s.LB, x, y, s.W, s.H)
	}
	return layerDepth(s.A, s.LA, x, y, s.W, s.H)
}

// Hit: the background point seen at frame point q (pixel coordinates, as the GPU's q), through
// the camera, and its nearness
func (s *Scene) Hit(qx, qy float32) (float32, float32, float32) {
	x, y, z, _ := s.HitW(qx, qy)
	return x, y, z
}

// Ground: the nearness of the background at frame point q before the camera (where something
// placed there stands: it then moves with the camera as that depth does)
func (s *Scene) Ground(qx, qy float32) float32 {
	if !s.On() {
		return 0
	}
	return s.Near(s.bgPoint(qx, qy))
}

// At: the nearness at frame point q
func (s *Scene) At(qx, qy float32) float32 {
	_, _, z := s.Hit(qx, qy)
	return z
}
