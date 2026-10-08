package effects

import "math"

// Scene is how near each point of the frame is (0 far … 1 near): the depth maps of the
// backgrounds A and B (fitted to the frame) seen through the background's own ripple, zoom,
// shake, motion and transition. Same math as sceneDepth in render.comp, in float32.
type Scene struct {
	A, B                            []uint16 // W×H, 65535 = near
	LA, LB                          Layer
	K                               float64 // transition progress: B from halfway on
	Zoom, ShakeX, ShakeY            float64
	RippleAmp, RippleFreq, RippleTm float64
	W, H                            int
}

// On: there is a depth to look at
func (s *Scene) On() bool { return s != nil && (s.A != nil || s.B != nil) }

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

// At: the nearness at frame point q (pixel coordinates, as the GPU's q)
func (s *Scene) At(qx, qy float32) float32 {
	if !s.On() {
		return 0
	}
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
	qx, qy = qx-float32(s.ShakeX), qy-float32(s.ShakeY)
	useB := s.B != nil && (s.A == nil || s.K >= 0.5)
	if useB {
		return layerDepth(s.B, s.LB, qx, qy, s.W, s.H)
	}
	return layerDepth(s.A, s.LA, qx, qy, s.W, s.H)
}
