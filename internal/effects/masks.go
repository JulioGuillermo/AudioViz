package effects

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"

	"audioviz/internal/config"
)

// MaskSize: the size of a clip's mask for an image of w×h (the same, up to 1024 on its long side)
func MaskSize(w, h int) (int, int) {
	s := math.Min(1, 1024/float64(max(w, h)))
	return max(1, int(float64(w)*s+0.5)), max(1, int(float64(h)*s+0.5))
}

// MaskImage combines a clip's masks into one alpha image of w×h (the clip's image space: 0..1
// coordinates; scale = mask px per image px, for the feather): the "inside" masks add up (the clip
// is seen in their union; everywhere when there are none) and the "outside" ones cut holes.
// raster(path) loads a painted mask (nil if it cannot). Returns nil when no mask is on.
func MaskImage(masks []config.MaskConfig, w, h int, scale float64, raster func(string) image.Image) *image.Alpha {
	var union, holes []float32
	any := false
	for i := range masks {
		m := &masks[i]
		if m.Off {
			continue
		}
		any = true
		a := image.NewAlpha(image.Rect(0, 0, w, h))
		switch m.Kind {
		case "raster":
			if img := raster(m.Path); img != nil {
				xdraw.ApproxBiLinear.Scale(a, a.Bounds(), img, img.Bounds(), draw.Src, nil)
			}
		default:
			r := vector.NewRasterizer(w, h)
			for _, sh := range m.Shapes {
				if !sh.Open { // an open shape is a line, not an area
					shapePath(r, sh.Points, float32(w), float32(h))
				}
			}
			r.Draw(a, a.Bounds(), image.Opaque, image.Point{})
		}
		if m.Feather > 0 {
			// the feather is in px of the clip's image (the mask may be smaller); 3 boxes ≈ a
			// gaussian of that reach
			boxBlurAlpha(a, m.Feather*scale*0.58)
		}
		op := float32(math.Max(0, math.Min(1, m.Opacity)))
		if m.Mode == "outside" {
			if holes == nil {
				holes = make([]float32, w*h)
				for j := range holes {
					holes[j] = 1
				}
			}
			for j, v := range a.Pix {
				holes[j] *= 1 - op*float32(v)/255
			}
			continue
		}
		if union == nil {
			union = make([]float32, w*h)
		}
		for j, v := range a.Pix {
			union[j] = max(union[j], op*float32(v)/255)
		}
	}
	if !any {
		return nil
	}
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	for j := range out.Pix {
		f := float32(1)
		if union != nil {
			f = union[j]
		}
		if holes != nil {
			f *= holes[j]
		}
		out.Pix[j] = uint8(f*255 + 0.5)
	}
	return out
}

// Node types of a mask shape: [x, y] smooth (automatic, Catmull-Rom), [x, y, 1] corner,
// [x, y, 2, inX, inY, outX, outY] symmetric handles, [x, y, 3, ...] free handles, [x, y, 4, ...]
// aligned handles (in line, each its own length); offsets from the node, in the same 0..1 units.
// The renderer only needs the handles: the type says how the editor moves them.
const (
	NodeSmooth = iota
	NodeCorner
	NodeSymmetric
	NodeFree
	NodeAligned
)

// NodeHandles: the in / out handle offsets of node i of a closed shape (automatic for smooth nodes,
// none for corners, the stored ones for custom nodes)
func NodeHandles(pts [][]float64, i int) (in, out [2]float64) {
	return nodeHandles(pts, i, false)
}

// nodeHandles: as NodeHandles; for an open shape the ends have no neighbor beyond them
func nodeHandles(pts [][]float64, i int, open bool) (in, out [2]float64) {
	n := len(pts)
	at := func(k int) []float64 {
		if open {
			return pts[min(max(k, 0), n-1)]
		}
		return pts[((k%n)+n)%n]
	}
	p := at(i)
	typ := 0
	if len(p) > 2 {
		typ = int(p[2])
	}
	switch {
	case typ == NodeCorner:
		return
	case typ >= NodeSymmetric && len(p) >= 7:
		return [2]float64{p[3], p[4]}, [2]float64{p[5], p[6]}
	}
	a, c := at(i-1), at(i+1)
	if len(a) < 2 || len(c) < 2 {
		return
	}
	dx, dy := (c[0]-a[0])/6, (c[1]-a[1])/6
	return [2]float64{-dx, -dy}, [2]float64{dx, dy}
}

// shapePath adds a closed curve through the points (cubic Béziers with each node's handles)
func shapePath(r *vector.Rasterizer, pts [][]float64, w, h float32) {
	n := len(pts)
	if n < 3 {
		return
	}
	for _, q := range pts {
		if len(q) < 2 {
			return
		}
	}
	at := func(i int) []float64 { return pts[((i%n)+n)%n] }
	px := func(x, y float64) (float32, float32) { return float32(x) * w, float32(y) * h }
	x0, y0 := px(at(0)[0], at(0)[1])
	r.MoveTo(x0, y0)
	for i := 0; i < n; i++ {
		b, c := at(i), at(i+1)
		_, out := NodeHandles(pts, i)
		in, _ := NodeHandles(pts, i+1)
		c1x, c1y := px(b[0]+out[0], b[1]+out[1])
		c2x, c2y := px(c[0]+in[0], c[1]+in[1])
		cx, cy := px(c[0], c[1])
		r.CubeTo(c1x, c1y, c2x, c2y, cx, cy)
	}
	r.ClosePath()
}

// boxBlurAlpha softens an alpha image: three box blurs (≈ gaussian) of radius r px
func boxBlurAlpha(a *image.Alpha, r float64) {
	ri := int(math.Round(r))
	if ri < 1 {
		return
	}
	w, h := a.Rect.Dx(), a.Rect.Dy()
	tmp := make([]float32, w*h)
	src := make([]float32, w*h)
	for i, v := range a.Pix {
		src[i] = float32(v)
	}
	pass := func(in, out []float32, n, stride, count, step int) {
		for c := 0; c < count; c++ {
			base := c * step
			var acc float32
			for k := -ri; k <= ri; k++ {
				acc += in[base+min(max(k, 0), n-1)*stride]
			}
			for i := 0; i < n; i++ {
				out[base+i*stride] = acc / float32(2*ri+1)
				acc += in[base+min(i+ri+1, n-1)*stride] - in[base+max(i-ri, 0)*stride]
			}
		}
	}
	for range 3 {
		pass(src, tmp, w, 1, h, w)
		pass(tmp, src, h, w, w, 1)
	}
	for i := range a.Pix {
		a.Pix[i] = uint8(math.Max(0, math.Min(255, float64(src[i])+0.5)))
	}
}

// maskAt: the clip's mask at a position of its image (bilinear; 1 without mask)
func (l *LayerFrame) maskAt(u, v float64) float64 {
	m := l.Mask
	if m == nil {
		return 1
	}
	b := l.Img.Bounds()
	mw, mh := m.Rect.Dx(), m.Rect.Dy()
	x, y := u*float64(mw)/float64(b.Dx())-0.5, v*float64(mh)/float64(b.Dy())-0.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(xx, yy int) float64 {
		if xx < 0 || yy < 0 || xx >= mw || yy >= mh {
			return 0
		}
		return float64(m.Pix[yy*m.Stride+xx]) / 255
	}
	return (at(x0, y0)*(1-fx)+at(x0+1, y0)*fx)*(1-fy) + (at(x0, y0+1)*(1-fx)+at(x0+1, y0+1)*fx)*fy
}
