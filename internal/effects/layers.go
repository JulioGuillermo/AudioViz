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
	// Mask: where it is seen (the image's space, any size; nil = everywhere)
	Mask    *image.Alpha
	MaskVer string
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

// sample: the clip at screen point q (blurred if Blur > 0: 3×3 taps at ±Blur)
func (l *LayerFrame) sample(qx, qy float64) [4]float64 {
	if l.Blur <= 0.5 {
		u, v := l.screenToImage(qx+0.5, qy+0.5)
		return l.bilinear(u, v)
	}
	var acc [4]float64
	tw := 0.0
	for j := -1; j <= 1; j++ {
		for i := -1; i <= 1; i++ {
			w := 1.0 / float64(1+i*i+j*j)
			u, v := l.screenToImage(qx+0.5+float64(i)*l.Blur, qy+0.5+float64(j)*l.Blur)
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

// at: the clip's color at screen pixel q (premultiplied), with its background effects
func (l *LayerFrame) at(qx, qy float64, W, H int) [4]float64 {
	wx, wy := l.W.warp(qx, qy, W, H)
	s := l.sample(wx, wy)
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
			s[0] = l.sample(rx, ry)[0]
		}
		if inside(qx-ox, qy-oy) {
			bx, by := l.W.warp(qx-ox, qy-oy, W, H)
			s[2] = l.sample(bx, by)[2]
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
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				s := l.at(float64(x), float64(y), W, H)
				if s[3] <= 1e-4 {
					continue
				}
				c, key := l.shade([3]float64{s[0] / s[3], s[1] / s[3], s[2] / s[3]})
				a := math.Max(0, math.Min(1, s[3]*key*l.Opacity))
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
