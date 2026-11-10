package effects

import "math"

// Procedural looks of an emitter (no particles, nothing simulated: each pixel from the time, so
// any moment is drawn at once): the clouds (and fog) here, worked out the same way by
// cloudPixel (CPU) and emit.comp's cloudPixel (GPU).

// CloudParams: the clouds look (config.CloudLook) at this frame's size (px)
type CloudParams struct {
	Cov, Dens, Scale, Detail, Billow, EdgeSoft float32
	WindX, WindY, Evolve                       float32
	// a band (px from the top; Top >= Bottom: none) with soft edges (px)
	Top, Bottom, Soft float32
	// on a ceiling going away to the horizon (px from the top)
	Persp   bool
	Horizon float32
	// towards the light (on the frame, unit), its color, the shadows' color, how dark
	LX, LY              float32
	LightCol, ShadowCol [3]float32
	Shadow              float32
	DepthFog            float32 // with the scene's depth (EmitParams.Occl): more the farther
	Flash               float32 // the lightning now (0..1)
	W, H                float32 // the frame
}

// CloudWords: the GPU's words of the clouds' params (EmitParams.Clouds)
const CloudWords = 28

// Pack: the params as emit.comp reads them
func (c *CloudParams) Pack() []float32 {
	b := float32(0)
	if c.Persp {
		b = 1
	}
	return []float32{c.Cov, c.Dens, c.Scale, c.Detail, c.Billow, c.WindX, c.WindY, c.Evolve,
		c.Top, c.Bottom, c.Soft, b, c.Horizon, c.LX, c.LY, c.Shadow,
		c.LightCol[0], c.LightCol[1], c.LightCol[2], c.ShadowCol[0], c.ShadowCol[1], c.ShadowCol[2],
		c.DepthFog, c.Flash, c.W, c.H, c.EdgeSoft, 0}
}

// cgrad: gradient noise (about 0..1) at (x, y): smoother than value noise, no grid showing
func cgrad(x, y float32, seed uint32) float32 {
	fx, fy := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
	ix, iy := int32(fx), int32(fy)
	ux, uy := x-fx, y-fy
	g := func(i, j int32, dx, dy float32) float32 {
		return (hash2(i, j, seed)*2-1)*dx + (hash2(i, j, seed^0x9e3779b9)*2-1)*dy
	}
	a, b := g(ix, iy, ux, uy), g(ix+1, iy, ux-1, uy)
	c, d := g(ix, iy+1, ux, uy-1), g(ix+1, iy+1, ux-1, uy-1)
	sx, sy := ux*ux*ux*(ux*(ux*6-15)+10), uy*uy*uy*(uy*(uy*6-15)+10)
	return 0.5 + (a+(b-a)*sx+(c-a)*sy+(a-b-c+d)*sx*sy)*1.2
}

// cfbm: the clouds' noise, oct octaves, each turned so that no grid shows
func cfbm(x, y float32, seed uint32, oct int) float32 {
	var v, w, a float32 = 0, 0, 0.5
	for k := 0; k < oct; k++ {
		x, y = 0.8*x-0.6*y, 0.6*x+0.8*y
		v += a * cgrad(x, y, seed+uint32(k)*17)
		w += a
		x, y = 2*x+3.1, 2*y+7.3
		a *= 0.5
	}
	return v / w
}

// density: how much cloud at (qx, qy) (px on its plane) at time T: the noise (warped a little
// into billows, its edges eaten by the finer noise with detail) past the coverage's threshold,
// thicker inside. far (0..1): towards the horizon, the fine noise goes (it would only flicker)
func (c *CloudParams) density(qx, qy, T, far float32, fine bool, seed uint32) float32 {
	cx, cy := (qx+c.WindX*T)/c.Scale, (qy+c.WindY*T)/c.Scale
	te := T * c.Evolve
	wx := cfbm(cx+1.7+te*0.1, cy+9.2, seed+40, 2)
	wy := cfbm(cx+8.3-te*0.1, cy+2.8, seed+50, 2)
	b := c.Billow * 0.3
	cx, cy = cx+b*(wx-0.5), cy+b*(wy-0.5)
	t := 1 - c.Cov
	if !fine {
		// (for the shadows: their big shapes, soft)
		n := 0.5 + (cfbm(cx+te*0.13, cy-te*0.07, seed, 3)-0.5)*2
		return smooth32(t-0.15, t+0.4, n)
	}
	n := cfbm(cx+te*0.13, cy-te*0.07, seed, 4)
	n = 0.5 + (n-0.5)*2 // (about 0..1)
	if det := c.Detail * (1 - far); det > 0 {
		n += det * 0.35 * (cfbm(6.1*cx+2.9, 6.1*cy+te*0.2+7.7, seed+90, 2) - 0.5)
	}
	es := c.EdgeSoft
	edge := smooth32(t-0.03-0.25*es, t+0.1+0.2*far+0.35*es, n)
	thick := max(0, min(1, (n-t)/max(0.25, 1.1-t)))
	return edge * (0.35 + 0.65*thick)
}

// plane: where pixel (x, y) is on the clouds' plane (the frame, or the ceiling to the horizon),
// how much it fades there and how far it is (0..1, near the horizon); ok false: below the horizon
func (c *CloudParams) plane(x, y float32) (float32, float32, float32, float32, bool) {
	if !c.Persp {
		return x, y, 1, 0, true
	}
	dy := c.Horizon - y
	if dy <= 0 {
		return 0, 0, 0, 0, false
	}
	z := c.H / dy
	return (x - c.W/2) * z * 0.5, c.H * z * 0.5, smooth32(0, 0.1*c.H, dy), smooth32(2, 6, z), true
}

// cloudPixel: the clouds at (px, py) at time T (premultiplied), as cloudPixel in emit.comp
func cloudPixel(p *EmitParams, c *CloudParams, px, py, T float32) [4]float32 {
	qx, qy, fade, far, ok := c.plane(px, py)
	if !ok {
		return [4]float32{}
	}
	if c.Top < c.Bottom {
		fade *= smooth32(c.Top-c.Soft, c.Top+c.Soft, py) * (1 - smooth32(c.Bottom-c.Soft, c.Bottom+c.Soft, py))
	}
	d := c.density(qx, qy, T, far, true, p.Seed)
	// far away: towards their average (a haze of clouds, not a flicker)
	d = (d + (c.Cov*0.55-d)*far) * fade
	if d <= 0.001 {
		return [4]float32{}
	}
	// its own shadow: the cloud between it and the light (3 steps towards it)
	var sum float32
	for k := 1; k <= 3; k++ {
		o := float32(k) * c.Scale * 0.12
		sum += c.density(qx+c.LX*o, qy+c.LY*o, T, far, false, p.Seed)
	}
	// (and its own thickness here: darker in its core, light at its edges)
	sun := float32(math.Exp(float64(-c.Shadow * (sum*0.4 + d*0.6))))
	a := 1 - float32(math.Exp(float64(-c.Dens*d*4)))
	if c.DepthFog > 0 && p.Occl != nil {
		// fog: thicker the farther the scene is here
		z := p.sceneDepthAt(px, py)
		a *= 1 - float32(math.Exp(float64(-c.DepthFog*(1-z)*3)))
	}
	var o [4]float32
	for k := 0; k < 3; k++ {
		col := c.ShadowCol[k] + (c.LightCol[k]-c.ShadowCol[k])*sun
		col += c.LightCol[k] * (1 - d) * sun * 0.2     // (the thin edges lit through)
		col += c.Flash * (0.2 + 0.7*d) * c.LightCol[k] // (lightning: lit from inside)
		o[k] = min(col, 1.5) * a
	}
	o[3] = a
	return o
}

// sceneDepthAt: the scene's depth at (x, y) on its grid (EmitParams.Occl: 0 far … 1 near)
func (p *EmitParams) sceneDepthAt(x, y float32) float32 {
	d := float32(p.ODiv)
	qx, qy := max(0, min(float32(p.OGW-1), x/d-0.5)), max(0, min(float32(p.OGH-1), y/d-0.5))
	ix, iy := max(min(int(qx), p.OGW-2), 0), max(min(int(qy), p.OGH-2), 0)
	fx, fy := qx-float32(ix), qy-float32(iy)
	at := func(cx, cy int) float32 { return p.Occl[min(cy, p.OGH-1)*p.OGW+min(cx, p.OGW-1)] }
	return (at(ix, iy)*(1-fx)+at(ix+1, iy)*fx)*(1-fy) + (at(ix, iy+1)*(1-fx)+at(ix+1, iy+1)*fx)*fy
}
