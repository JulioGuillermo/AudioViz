package effects

import (
	"image"
	"math"
)

// Background effects with the scene's depth (a depth map): only on a range of depths, and a
// depth of field. The GPU does the same in render.comp (rangeMask, warpedDof).

// RangeMask: how much an effect applies at nearness z: r = from (near), to (far), soft, on
// (off: everywhere)
func RangeMask(z float32, r [4]float64) float32 {
	if r[3] < 0.5 {
		return 1
	}
	return LightRange(z, float32(r[0]), float32(r[1]), float32(r[2]))
}

// DoF is a depth of field: in focus between To (far) and From (near); Soft further away it is
// blurred by all of Blur px
type DoF struct {
	From, To, Soft, Blur float64
}

// On: something is blurred
func (d DoF) On() bool { return d.Blur > 0.5 }

// CoC: how blurred (px, the radius) a point at nearness z is
func (d DoF) CoC(z float32) float32 {
	out := max(float32(d.To)-z, z-float32(d.From), 0)
	return float32(d.Blur) * smooth32(0, float32(math.Max(d.Soft, 1e-3)), out)
}

// DoFTaps: the gather's samples, a golden-angle spiral over the unit disc
const DoFTaps = 24

func dofTap(k int) (float32, float32) {
	r := float32(math.Sqrt((float64(k) + 0.5) / DoFTaps))
	a := float64(k) * 2.39996323
	return r * float32(math.Cos(a)), r * float32(math.Sin(a))
}

// dofWeight: how much a sample at distance dist (px) with nearness zt and blur ct counts for a
// point at nearness zc: what is behind (or as far) always; what is nearer only if it is blurred
// enough to reach (a sharp thing in front doesn't bleed onto the blurred background)
func dofWeight(zc, zt, ct, dist float32) float32 {
	if zt <= zc+0.01 {
		return 1
	}
	return max(0, min(1, ct/max(dist, 1e-3)))
}

// DepthField: the scene's nearness at every pixel
func DepthField(s *Scene, W, H int) []float32 {
	z := make([]float32, W*H)
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				z[y*W+x] = s.At(float32(x), float32(y))
			}
		}
	})
	return z
}

// ApplyDoF blurs src by the depth z (one value per pixel) as d says
func ApplyDoF(src *image.RGBA, z []float32, d DoF) *image.RGBA {
	if !d.On() || z == nil {
		return src
	}
	W, H := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	at := func(x, y float32) ([3]float32, int) {
		x, y = max(0, min(float32(W-1), x)), max(0, min(float32(H-1), y))
		x0, y0 := int(x), int(y)
		x1, y1 := min(x0+1, W-1), min(y0+1, H-1)
		fx, fy := x-float32(x0), y-float32(y0)
		var c [3]float32
		for j := 0; j < 3; j++ {
			p := func(xx, yy int) float32 { return float32(src.Pix[yy*src.Stride+xx*4+j]) }
			a := p(x0, y0) + (p(x1, y0)-p(x0, y0))*fx
			b := p(x0, y1) + (p(x1, y1)-p(x0, y1))*fx
			c[j] = a + (b-a)*fy
		}
		return c, int(math.Round(float64(y)))*W + int(math.Round(float64(x)))
	}
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				zc := z[y*W+x]
				cc := d.CoC(zc)
				if cc < 0.5 {
					continue
				}
				var sum [3]float32
				var ws float32
				for k := 0; k < DoFTaps; k++ {
					ox, oy := dofTap(k)
					ox, oy = ox*cc, oy*cc
					c, i := at(float32(x)+ox, float32(y)+oy)
					zt := z[i]
					w := dofWeight(zc, zt, d.CoC(zt), float32(math.Hypot(float64(ox), float64(oy))))
					for j := 0; j < 3; j++ {
						sum[j] += c[j] * w
					}
					ws += w
				}
				if ws > 1e-4 {
					o := y*dst.Stride + x*4
					for j := 0; j < 3; j++ {
						dst.Pix[o+j] = uint8(max(0, min(255, sum[j]/ws+0.5)))
					}
				}
			}
		}
	})
	return dst
}

// MaskedBlend: eff where the effect applies and base elsewhere (mask: one value per pixel, nil =
// all of eff); returns eff
func MaskedBlend(base, eff *image.RGBA, mask []float32) *image.RGBA {
	if mask == nil || base == eff {
		return eff
	}
	W, H := eff.Bounds().Dx(), eff.Bounds().Dy()
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				m := mask[y*W+x]
				if m >= 0.999 {
					continue
				}
				o := y*eff.Stride + x*4
				for j := 0; j < 3; j++ {
					b, e := float32(base.Pix[o+j]), float32(eff.Pix[o+j])
					eff.Pix[o+j] = uint8(b + (e-b)*m + 0.5)
				}
			}
		}
	})
	return eff
}

// RangeMaskField: RangeMask at every pixel of the depth z (nil when the effect is everywhere)
func RangeMaskField(z []float32, r [4]float64) []float32 {
	if r[3] < 0.5 || z == nil {
		return nil
	}
	m := make([]float32, len(z))
	for i, v := range z {
		m[i] = RangeMask(v, r)
	}
	return m
}
