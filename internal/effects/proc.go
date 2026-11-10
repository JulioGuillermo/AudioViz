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

// cgrad: 3D gradient noise (about 0..1) at (x, y, z): smoother than value noise, no grid
// showing; z is time, so the shapes change instead of only moving. Each corner's gradient from
// one hash (10 bits per axis)
func cgrad(x, y, z float32, seed uint32) float32 {
	fx, fy, fz := float32(math.Floor(float64(x))), float32(math.Floor(float64(y))), float32(math.Floor(float64(z)))
	ix, iy, iz := int32(fx), int32(fy), int32(fz)
	ux, uy, uz := x-fx, y-fy, z-fz
	g := func(i, j, k int32, dx, dy, dz float32) float32 {
		h := pcg(uint32(i)*1597334677 ^ uint32(j)*3812015801 ^ uint32(k)*2798796415 ^ seed)
		return (float32(h&1023)/511.5-1)*dx + (float32((h>>10)&1023)/511.5-1)*dy + (float32((h>>20)&1023)/511.5-1)*dz
	}
	f := func(t float32) float32 { return t * t * t * (t*(t*6-15) + 10) }
	sx, sy, sz := f(ux), f(uy), f(uz)
	lerp := func(a, b, t float32) float32 { return a + (b-a)*t }
	plane := func(k int32, dz float32) float32 {
		a, b := g(ix, iy, k, ux, uy, dz), g(ix+1, iy, k, ux-1, uy, dz)
		c, d := g(ix, iy+1, k, ux, uy-1, dz), g(ix+1, iy+1, k, ux-1, uy-1, dz)
		return lerp(lerp(a, b, sx), lerp(c, d, sx), sy)
	}
	return 0.5 + lerp(plane(iz, uz), plane(iz+1, uz-1), sz)*1.2
}

// cfbm: the clouds' noise, oct octaves, each turned so that no grid shows (the finer change
// faster: z × 1.3 with each)
func cfbm(x, y, z float32, seed uint32, oct int) float32 {
	var v, w, a float32 = 0, 0, 0.5
	for k := 0; k < oct; k++ {
		x, y = 0.8*x-0.6*y, 0.6*x+0.8*y
		v += a * cgrad(x, y, z, seed+uint32(k)*17)
		w += a
		x, y, z = 2*x+3.1, 2*y+7.3, 1.3*z+1.9
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
	wx := cfbm(cx+1.7, cy+9.2, te*0.7, seed+40, 2)
	wy := cfbm(cx+8.3, cy+2.8, te*0.7, seed+50, 2)
	b := c.Billow * 0.3
	cx, cy = cx+b*(wx-0.5), cy+b*(wy-0.5)
	t := 1 - c.Cov
	if !fine {
		// (for the shadows: their big shapes, soft)
		n := 0.5 + (cfbm(cx, cy, te, seed, 3)-0.5)*2
		return smooth32(t-0.15, t+0.4, n)
	}
	n := cfbm(cx, cy, te, seed, 4)
	n = 0.5 + (n-0.5)*2 // (about 0..1)
	if det := c.Detail * (1 - far); det > 0 {
		n += det * 0.35 * (cfbm(6.1*cx+2.9, 6.1*cy+7.7, 2*te, seed+90, 2) - 0.5)
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

// OceanParams: the ocean look (config.OceanLook) at this frame's size (px). The water is a plane
// below the horizon seen from a camera H/2 above it, its focal length H: the pixel (x, y) is at
// (wx, wz) = ((x − W/2)·z/2, H·z/2), z = H/(y − horizon)
type OceanParams struct {
	Horizon, W, H float32
	// the waves: the longest (px), how high, how sharp their crests, how fast, how far their
	// directions spread (radians), where they go (on the water: x right, z away)
	Size, Height, Chop, Speed, Spread float32
	DirX, DirZ                        float32
	G                                 float32 // gravity (px/s²: how fast waves of a size go)
	Deep, Shallow                     [3]float32
	// the sun (px on the frame), its color, how much it shines on the water
	SunX, SunY float32
	SunCol     [3]float32
	Glint      float32
	Foam       float32
	FoamCol    [3]float32
	Reflect    float32 // how much it reflects what is behind (through its lens)
	Fade       float32 // the horizon's softness (px)
}

// OceanWords: the GPU's words of the ocean's params (EmitParams.Ocean)
const OceanWords = 32

// OceanWaves: how many waves are summed
const OceanWaves = 12

// Pack: the params as emit.comp reads them
func (o *OceanParams) Pack() []float32 {
	return []float32{o.Horizon, o.W, o.H, o.Size, o.Height, o.Chop, o.Speed, o.Spread,
		o.DirX, o.DirZ, o.G, o.Deep[0], o.Deep[1], o.Deep[2], o.Shallow[0], o.Shallow[1], o.Shallow[2],
		o.SunX, o.SunY, o.SunCol[0], o.SunCol[1], o.SunCol[2], o.Glint, o.Foam,
		o.FoamCol[0], o.FoamCol[1], o.FoamCol[2], o.Reflect, o.Fade, 0, 0, 0}
}

func norm3(x, y, z float32) (float32, float32, float32) {
	l := float32(math.Sqrt(float64(x*x + y*y + z*z)))
	if l == 0 {
		return 0, 1, 0
	}
	return x / l, y / l, z / l
}

// oceanPixel: the ocean at (px, py) at time T (premultiplied) and its lens (where what is behind
// is seen from: the reflection), as oceanPixel in emit.comp
func oceanPixel(p *EmitParams, o *OceanParams, px, py, T float32) ([4]float32, [2]float32) {
	dy := py - o.Horizon
	if dy <= 0 {
		return [4]float32{}, [2]float32{}
	}
	z := o.H / dy
	wx, wz := (px-o.W/2)*z*0.5, o.H*z*0.5
	// the waves: each sharper at its crest (e^(chop·(sin − 1))), the shorter ones gone where they
	// would be smaller than a few px; each moves the next a little along its slope (they bunch up)
	var ht, hx, hz, asum float32
	qx, qz := wx, wz
	L := o.Size
	baseA := float32(math.Atan2(float64(o.DirX), float64(o.DirZ)))
	for i := 0; i < OceanWaves; i++ {
		fade := smooth32(1, 4, 2*L/(z*z))
		if fade > 0 {
			ang := baseA + o.Spread*(hash2(int32(i), 0, p.Seed)*2-1)
			dx, dz := float32(math.Sin(float64(ang))), float32(math.Cos(float64(ang)))
			k := 2 * math.Pi / L
			w := float32(math.Sqrt(float64(o.G*k))) * o.Speed
			ph := k*(dx*qx+dz*qz) - w*T + hash2(int32(i), 1, p.Seed)*2*math.Pi
			s, c := math.Sincos(float64(ph))
			e := float32(math.Exp(float64(o.Chop * (float32(s) - 1))))
			A := o.Height * L * 0.012 * fade
			ht += A * e
			sl := A * o.Chop * float32(c) * e * k
			hx += sl * dx
			hz += sl * dz
			asum += A
			qx -= dx * sl * L * 0.05
			qz -= dz * sl * L * 0.05
		}
		L *= 0.72
	}
	hn := float32(0)
	if asum > 0 {
		hn = ht / asum
	}
	nx, ny, nz := norm3(-hx, 1, -hz)
	c := o.H * 0.5
	vx, vy, vz := norm3(-wx, c, -wz)
	ndv := max(0, nx*vx+ny*vy+nz*vz)
	// the reflection: where it goes, on the frame (what is behind there, through the lens)
	rx, ry, rz := -vx+2*ndv*nx, -vy+2*ndv*ny, -vz+2*ndv*nz
	ry = max(ry, 0.005)
	rz = max(rz, 0.05)
	sx, sy := o.W/2+o.H*rx/rz, o.Horizon-o.H*ry/rz
	F := 0.02 + 0.98*float32(math.Pow(float64(1-ndv), 5))
	R := min(1, F*o.Reflect) * smooth32(-0.1*o.H, 0, sy) // (nothing above the frame to reflect)
	// the sun: on the frame at (SunX, SunY), its direction from the camera
	ux, uy, uz := norm3(o.SunX-o.W/2, o.Horizon-o.SunY, o.H)
	rl := float32(math.Sqrt(float64(rx*rx + ry*ry + rz*rz)))
	rd := max(0, (rx*ux+ry*uy+rz*uz)/rl)
	spec := (float32(math.Pow(float64(rd), 600))*6 + float32(math.Pow(float64(rd), 150))*0.15) * o.Glint * (0.3 + F)
	ndl := max(0, nx*ux+ny*uy+nz*uz)
	// the water itself: deep, lighter where the waves rise (light through their crests)
	sss := max(0, hn-0.15) * max(0, hn-0.15) * 1.2
	foam := float32(0)
	if o.Foam > 0 {
		f := cfbm(wx/(o.Size*0.08), wz/(o.Size*0.08), T*0.4, p.Seed+7, 3)
		fl := 0.62 - 0.3*o.Foam
		foam = min(1, o.Foam*3) * smooth32(fl, fl+0.25, hn+0.3*(f-0.5)) * smooth32(0.4, 0.65, f) * smooth32(2, 8, 2*o.Size*0.3/(z*z))
	}
	var out [4]float32
	for k := 0; k < 3; k++ {
		wcol := o.Deep[k]*(0.55+0.45*ndl) + o.Shallow[k]*sss
		col := wcol*(1-R) + spec*o.SunCol[k]
		out[k] = col*(1-foam) + o.FoamCol[k]*foam
	}
	out[3] = (1-R)*(1-foam) + foam
	hf := smooth32(0, o.Fade, dy)
	for k := range out {
		out[k] *= hf
	}
	return out, [2]float32{(sx - px) * hf, (sy - py) * hf}
}
