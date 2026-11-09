package effects

import (
	"image"
	"math"
)

// Overlay layers (config.LayerConfig): each visible clip of a frame, resolved (placement, filters,
// blend, the background effects it follows). Drawn per pixel, the same on the CPU (DrawLayer) and
// on the GPU (drawLayer in render.comp).

// Warp is how a clip is moved by the background effects: glitch bands, ripple, zoom, shake and
// chromatic aberration, in that order (as background(q) in render.comp)
type Warp struct {
	Zoom                            float64
	RippleAmp, RippleFreq, RippleTm float64
	ShakeX, ShakeY                  float64
	Chroma                          float64 // px
	ChromaMode                      int     // 0 none, 1 horizontal, 2 radial
	Glitch                          []GlitchSlice
	GlitchBG                        bool    // Glitch is the background's (the GPU reads its own copy)
	FlashCF, FlashBright            float64 // contrast factor (1 = none) and brightness added
}

// LayerFrame is one clip on screen at a frame
type LayerFrame struct {
	Img    *image.RGBA // premultiplied, the clip's own size
	Ver    string      // identifies Img (not re-uploaded to the GPU when unchanged)
	CX, CY float64     // center on screen (px)
	SX, SY float64     // screen px per image px (negative = flipped)
	Rot    float64     // radians
	// Look
	Opacity                        float64
	Blend                          int // index in config.Blends
	Brightness, Contrast, Saturate float64
	Hue                            float64 // radians
	Blur                           float64 // screen px
	Key                            bool    // chroma key
	KeyColor                       [3]float64
	KeyTol, KeySoft                float64
	W                              Warp
	LayerID                        string // the layer (config.LayerConfig.ID) and index of the clip it shows
	Clip                           int
	Side                           int // in a transition: on both images (0), only on A (1) or B (2)
	// Mask: where it is seen (the image's space, any size; nil = everywhere)
	Mask    *image.Alpha
	MaskVer string
	// Particles (an emitter clip): Img is made from them (frame-sized, CX, CY at the center), and
	// their light on what is behind (made with Img on the CPU)
	Emit  *EmitFrame
	Light *LightField
	// Depth (with a background that has one): hidden where the scene is nearer than DepthZ
	// (soft by DepthSoft), and the scene the light and that look at
	Behind            bool
	DepthZ, DepthSoft float32
	InDepth           bool // it has a depth (DepthZ, DepthSoft), behind things or not
	// its depth from its path, per point (DepthZ: at its center): the track, how far along the
	// path its center is (px) and the path's direction there
	ZTrack        *PathTrack
	ZS0, ZTX, ZTY float32
	// an emitter that goes round the scene: on, its depth, the band, where it is born x, y, the
	// free radius around it (the studio's view of what it goes round)
	Solid [6]float32
	Scene *Scene
	// Its properties worked out per pixel (expressions, as ClipPxProps; nil: none), from its
	// values: key color, opacity (times OpK: its fades and reaction), brightness, contrast,
	// saturation, hue (degrees), key tolerance and softness; along its path: how far its center
	// is, the path's direction
	X             [NClipPx]*PixExpr
	XV            [NClipPx]float64
	OpK           float64
	XS0, XTX, XTY float32
}

// ClipPxProps: a clip's properties an expression can give per pixel (their paths), in the order
// of LayerFrame.X (and render.comp's table); the first is a color
var ClipPxProps = []string{"chroma.color", "opacity", "brightness", "contrast", "saturation", "hue",
	"chroma.tolerance", "chroma.softness"}

// NClipPx: len(ClipPxProps)
const NClipPx = 8

// HasX: whether some property is worked out per pixel; FirstX: one of them
func (l *LayerFrame) HasX() bool { return l.FirstX() != nil }
func (l *LayerFrame) FirstX() *PixExpr {
	for _, x := range l.X {
		if x != nil {
			return x
		}
	}
	return nil
}

// pxShade: the clip at pixel (x, y) with its properties worked out there (as drawLayer): a copy
// of it with them
func (l *LayerFrame) pxShade(x, y int) LayerFrame {
	q := *l
	x0 := l.FirstX()
	u, v := l.screenToImage(float64(x)+0.5, float64(y)+0.5)
	b := l.Img.Bounds()
	in := PixIn{Count: 1, S: l.XS0 + (float32(x)-float32(l.CX))*l.XTX + (float32(y)-float32(l.CY))*l.XTY,
		TX: u / float64(b.Dx()), TY: v / float64(b.Dy()), SX: (float64(x) + 0.5) / float64(x0.W), SY: (float64(y) + 0.5) / float64(x0.H)}
	in.LX, in.LY = in.TX, in.TY
	for k, xk := range l.X {
		if xk == nil {
			continue
		}
		if k == 0 {
			c := xk.Color(in, [4]float64{l.KeyColor[0], l.KeyColor[1], l.KeyColor[2], 1})
			q.KeyColor = [3]float64{c[0], c[1], c[2]}
			continue
		}
		r := xk.Scalar(in, l.XV[k])
		switch k {
		case 1:
			q.Opacity = math.Max(0, math.Min(1, r)) * l.OpK
		case 2:
			q.Brightness = r
		case 3:
			q.Contrast = r
		case 4:
			q.Saturate = r
		case 5:
			q.Hue = r * math.Pi / 180
		case 6:
			q.KeyTol = math.Max(0, r)
		case 7:
			q.KeySoft = math.Max(0, r)
		}
	}
	return q
}

// screenToImage: position in the clip's image (px, fractional) of the screen point p
func (l *LayerFrame) screenToImage(px, py float64) (float64, float64) {
	dx, dy := px-l.CX, py-l.CY
	if l.Rot != 0 {
		c, s := math.Cos(-l.Rot), math.Sin(-l.Rot)
		dx, dy = dx*c-dy*s, dx*s+dy*c
	}
	b := l.Img.Bounds()
	return dx/l.SX + float64(b.Dx())*0.5, dy/l.SY + float64(b.Dy())*0.5
}

// texel: premultiplied RGBA 0..1, transparent outside
func (l *LayerFrame) texel(x, y int) [4]float64 {
	b := l.Img.Bounds()
	if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
		return [4]float64{}
	}
	o := y*l.Img.Stride + x*4
	p := l.Img.Pix[o : o+4 : o+4]
	return [4]float64{float64(p[0]) / 255, float64(p[1]) / 255, float64(p[2]) / 255, float64(p[3]) / 255}
}

func (l *LayerFrame) bilinear(u, v float64) [4]float64 {
	u, v = u-0.5, v-0.5
	x0, y0 := int(math.Floor(u)), int(math.Floor(v))
	fx, fy := u-float64(x0), v-float64(y0)
	a, b, c, d := l.texel(x0, y0), l.texel(x0+1, y0), l.texel(x0, y0+1), l.texel(x0+1, y0+1)
	var out [4]float64
	for i := range out {
		out[i] = (a[i]*(1-fx)+b[i]*fx)*(1-fy) + (c[i]*(1-fx)+d[i]*fx)*fy
	}
	return out
}

// sample: the clip at screen point q (blurred if blur > 0: 3×3 taps at ±blur)
func (l *LayerFrame) sample(qx, qy, blur float64) [4]float64 {
	if blur <= 0.5 {
		u, v := l.screenToImage(qx+0.5, qy+0.5)
		return l.bilinear(u, v)
	}
	var acc [4]float64
	tw := 0.0
	for j := -1; j <= 1; j++ {
		for i := -1; i <= 1; i++ {
			w := 1.0 / float64(1+i*i+j*j)
			u, v := l.screenToImage(qx+0.5+float64(i)*blur, qy+0.5+float64(j)*blur)
			s := l.bilinear(u, v)
			for k := range acc {
				acc[k] += s[k] * w
			}
			tw += w
		}
	}
	for k := range acc {
		acc[k] /= tw
	}
	return acc
}

// warp maps a screen point through the clip's background effects (glitch, ripple, zoom, shake)
func (w *Warp) warp(qx, qy float64, W, H int) (float64, float64) {
	for _, g := range w.Glitch {
		if qy >= float64(g.Y0) && qy < float64(g.Y1) {
			if nx := qx - float64(g.ShiftX); nx >= 0 && nx < float64(W) {
				qx = nx
			}
		}
	}
	if w.RippleAmp > 0.2 {
		ph := qy*w.RippleFreq + w.RippleTm
		qx += math.Sin(ph) * w.RippleAmp
		qy += math.Cos(ph) * w.RippleAmp
		qx = math.Max(0, math.Min(float64(W-1), qx))
		qy = math.Max(0, math.Min(float64(H-1), qy))
	}
	cx, cy := float64(W)*0.5, float64(H)*0.5
	if w.Zoom > 1.001 {
		qx, qy = cx+(qx-cx)/w.Zoom, cy+(qy-cy)/w.Zoom
	}
	return qx - w.ShakeX, qy - w.ShakeY
}

// zAt: its depth at screen pixel q (from its path's track; DepthZ without one)
func (l *LayerFrame) zAt(qx, qy float64) float32 {
	if l.ZTrack == nil {
		return l.DepthZ
	}
	return l.ZTrack.At(l.ZS0 + (float32(qx)-float32(l.CX))*l.ZTX + (float32(qy)-float32(l.CY))*l.ZTY)
}

// at: the clip's color at screen pixel q (premultiplied), with its background effects; moved by
// (dx, dy) more and blurred by blur (its depth here, see DrawLayer)
func (l *LayerFrame) at(qx, qy float64, W, H int, dx, dy, blur float64) [4]float64 {
	wx, wy := l.W.warp(qx, qy, W, H)
	wx, wy = wx-dx, wy-dy
	s := l.sample(wx, wy, blur)
	if l.Mask != nil {
		u, v := l.screenToImage(wx+0.5, wy+0.5)
		m := l.maskAt(u, v)
		for i := range s {
			s[i] *= m
		}
	}
	if l.W.ChromaMode != 0 && l.W.Chroma > 0.5 {
		var ox, oy float64
		if l.W.ChromaMode == 2 {
			cx, cy := float64(W)*0.5, float64(H)*0.5
			n := math.Hypot(cx, cy)
			ox, oy = (qx-cx)/n*l.W.Chroma, (qy-cy)/n*l.W.Chroma
		} else {
			ox = -math.Round(l.W.Chroma)
		}
		inside := func(x, y float64) bool { return x >= 0 && x < float64(W) && y >= 0 && y < float64(H) }
		if inside(qx+ox, qy+oy) {
			rx, ry := l.W.warp(qx+ox, qy+oy, W, H)
			s[0] = l.sample(rx-dx, ry-dy, blur)[0]
		}
		if inside(qx-ox, qy-oy) {
			bx, by := l.W.warp(qx-ox, qy-oy, W, H)
			s[2] = l.sample(bx-dx, by-dy, blur)[2]
		}
	}
	return s
}

// shadeLayer: color c (straight, 0..1) with the clip's filters, its flash, its chroma key;
// returns the color and the alpha factor of the key
func (l *LayerFrame) shade(c [3]float64) ([3]float64, float64) {
	key := 1.0
	if l.Key {
		d := math.Sqrt((c[0]-l.KeyColor[0])*(c[0]-l.KeyColor[0]) + (c[1]-l.KeyColor[1])*(c[1]-l.KeyColor[1]) + (c[2]-l.KeyColor[2])*(c[2]-l.KeyColor[2]))
		key = smoothstep(l.KeyTol, l.KeyTol+l.KeySoft+1e-4, d)
	}
	for i := range c {
		c[i] = (c[i]-0.5)*(1+l.Contrast) + 0.5 + l.Brightness
	}
	if l.Saturate != 0 {
		g := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
		for i := range c {
			c[i] = g + (c[i]-g)*(1+l.Saturate)
		}
	}
	if l.Hue != 0 {
		c = hueRotate(c, l.Hue)
	}
	for i := range c {
		c[i] = l.W.FlashCF*(c[i]-0.5) + 0.5 + l.W.FlashBright
	}
	return c, key
}

// hueRotate turns the color around the gray axis (same matrix as render.comp)
func hueRotate(c [3]float64, a float64) [3]float64 {
	cs, sn := math.Cos(a), math.Sin(a)
	const k = 1.0 / 3
	s3 := math.Sqrt(k)
	r0, r1, r2 := cs+(1-cs)*k, k*(1-cs)-s3*sn, k*(1-cs)+s3*sn
	return [3]float64{
		c[0]*r0 + c[1]*r1 + c[2]*r2,
		c[0]*r2 + c[1]*r0 + c[2]*r1,
		c[0]*r1 + c[1]*r2 + c[2]*r0,
	}
}

// blendColor: the blend mode of the clip (src) over the frame (dst), straight colors 0..1
func blendColor(mode int, dst, src [3]float64) [3]float64 {
	var o [3]float64
	for i := range o {
		d, s := dst[i], src[i]
		switch mode {
		case 1: // add
			o[i] = math.Min(1, d+s)
		case 2: // multiply
			o[i] = d * s
		case 3: // screen
			o[i] = 1 - (1-d)*(1-s)
		case 4: // overlay
			if d < 0.5 {
				o[i] = 2 * d * s
			} else {
				o[i] = 1 - 2*(1-d)*(1-s)
			}
		case 5: // lighten
			o[i] = math.Max(d, s)
		case 6: // darken
			o[i] = math.Min(d, s)
		default:
			o[i] = s
		}
	}
	return o
}

// DrawLayer composites the clip onto the frame (CPU render)
func DrawLayer(dst *image.RGBA, l *LayerFrame) {
	W, H := dst.Bounds().Dx(), dst.Bounds().Dy()
	if l.Emit != nil && l.Emit.Lens != nil {
		refract(dst, l.Emit.Lens, float32(l.Opacity))
	}
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				if l.Light != nil {
					// the emitter's light on what is already there, before its particles (moved
					// with them)
					L := l.Light.LightAt(float32(x)-float32(l.W.ShakeX), float32(y)-float32(l.W.ShakeY))
					if L > 1e-4 && l.Light.D3 && l.Scene.On() {
						L *= l.Light.DepthFactor(l.Scene, x, y)
					}
					if L > 1e-4 {
						o := y*dst.Stride + x*4
						c := l.Light.Lit([3]float64{float64(dst.Pix[o]) / 255, float64(dst.Pix[o+1]) / 255, float64(dst.Pix[o+2]) / 255}, L)
						for i := 0; i < 3; i++ {
							dst.Pix[o+i] = uint8(c[i]*255 + 0.5)
						}
					}
				}
				// its depth here: from its path, per point (moved by the 2.5D camera and blurred by the
				// depth of field as that point is), or one for all of it
				zq := l.DepthZ
				var zx, zy float64
				zb := l.Blur
				if l.ZTrack != nil && l.Scene.On() {
					zq = l.zAt(float64(x), float64(y))
					zx, zy = l.Scene.Disp(l.Side, float64(zq), float64(x), float64(y))
					zb += float64(l.Scene.CoC(l.Side, zq))
				} else if l.Emit != nil && l.Emit.ZPlane != nil && l.Scene.On() {
					// an emitter's particles each at the depth of their point on their path
					if v := l.Emit.ZPlane[y*W+x]; v >= 0 {
						zq = v
					}
					zx, zy = l.Scene.Disp(l.Side, float64(zq), float64(x), float64(y))
					zb += float64(l.Scene.CoC(l.Side, zq))
				}
				s := l.at(float64(x), float64(y), W, H, zx, zy, zb)
				if s[3] <= 1e-4 {
					continue
				}
				lx := l
				if l.HasX() {
					q := l.pxShade(x, y)
					lx = &q
				}
				c, key := lx.shade([3]float64{s[0] / s[3], s[1] / s[3], s[2] / s[3]})
				a := math.Max(0, math.Min(1, s[3]*key*lx.Opacity))
				if l.Behind && l.Scene.On() {
					// behind what is nearer than it
					d := l.Scene.At(float32(x), float32(y))
					a *= float64(1 - smooth32(zq-l.DepthSoft, zq+l.DepthSoft+1e-4, d))
				}
				if a <= 0 {
					continue
				}
				o := y*dst.Stride + x*4
				d := [3]float64{float64(dst.Pix[o]) / 255, float64(dst.Pix[o+1]) / 255, float64(dst.Pix[o+2]) / 255}
				for i := range c {
					c[i] = math.Max(0, math.Min(1, c[i]))
				}
				b := blendColor(l.Blend, d, c)
				for i := 0; i < 3; i++ {
					dst.Pix[o+i] = uint8(math.Max(0, math.Min(255, (d[i]+(b[i]-d[i])*a)*255+0.5)))
				}
			}
		}
	})
}

// refract: water that bends the light (a lens): what is under each pixel is seen from where its
// lens says (x, y px per pixel, times k), as render.comp does before drawing what is above it
func refract(dst *image.RGBA, lens []float32, k float32) {
	W, H := dst.Bounds().Dx(), dst.Bounds().Dy()
	src := append([]uint8(nil), dst.Pix...)
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				ox, oy := lens[(y*W+x)*2]*k, lens[(y*W+x)*2+1]*k
				if ox == 0 && oy == 0 {
					continue
				}
				fx := max(0, min(float32(W-1), float32(x)+ox))
				fy := max(0, min(float32(H-1), float32(y)+oy))
				x0, y0 := int(fx), int(fy)
				x1, y1 := min(x0+1, W-1), min(y0+1, H-1)
				ax, ay := fx-float32(x0), fy-float32(y0)
				o := y*dst.Stride + x*4
				for j := 0; j < 3; j++ {
					p := func(xx, yy int) float32 { return float32(src[yy*dst.Stride+xx*4+j]) }
					a := p(x0, y0) + (p(x1, y0)-p(x0, y0))*ax
					b := p(x0, y1) + (p(x1, y1)-p(x0, y1))*ax
					dst.Pix[o+j] = uint8(max(0, min(255, a+(b-a)*ay+0.5)))
				}
			}
		}
	})
}
