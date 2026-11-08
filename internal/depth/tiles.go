package depth

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"time"

	xdraw "golang.org/x/image/draw"
)

// The models see about 500 px at a time: larger than that they lose the scene's layout. For more
// detail (and for the image's own size) the image is gone over in overlapping tiles of that size,
// each one fitted (scale and offset) to one look at the whole image, blended with soft seams, and
// the large shapes taken from that whole look (the tiles only bring the fine detail).

// aiMap: the depth map of an image with a model, at the image's size
func aiMap(ctx context.Context, img *image.RGBA, r Request, stage func(string, float64)) (*Map, error) {
	sp := ModelFor(r.Model)
	paths, err := sp.Ensure(ctx, func(done, total int64) {
		if total > 0 {
			stage("download", float64(done)/float64(total))
		}
	})
	if err != nil {
		return nil, err
	}
	stage("estimate", 0)
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	// the size it works at: the detail's long side, or the image's own (at most 4096 px)
	size := DetailSize(r.Detail)
	if size == 0 {
		size = min(max(W, H), 4096)
	}
	size = min(size, max(W, H))
	var m *Map
	plain := size > sp.Tile+sp.Tile/4 && sp.Plain() && r.Detail != "native"
	var global *Raw
	if !plain { // the whole image, as the model was trained (the larger plain run needs no other)
		if global, err = inferImage(sp, paths, img, false); err != nil {
			return nil, err
		}
		if size > sp.Tile+sp.Tile/4 { // (tiles follow: the look at the whole counts as a quarter)
			stage("estimate", 0.25)
		}
	}
	switch {
	case size <= sp.Tile+sp.Tile/4:
		m = global.Nearness()
	case plain:
		// Depth Anything 3 keeps its layout at 2-3× its size in one go
		k := float64(size) / float64(max(W, H))
		rw, rh := max(14, int(math.Round(float64(W)*k/14))*14), max(14, int(math.Round(float64(H)*k/14))*14)
		raw, err := inferImage(sp, paths, resized(img, rw, rh), true)
		if err != nil {
			return nil, err
		}
		m = raw.Nearness()
	default:
		k := float64(size) / float64(max(W, H))
		in := img
		if k < 1 {
			in = resized(img, int(math.Round(float64(W)*k)), int(math.Round(float64(H)*k)))
		}
		if m, err = tiled(ctx, sp, paths, in, global, func(st string, p float64) {
			if st == "estimate" {
				p = 0.25 + 0.75*p
			}
			stage(st, p)
		}); err != nil {
			return nil, err
		}
	}
	if m.W == W && m.H == H {
		return m, nil
	}
	stage("refine", 0)
	t0 := time.Now()
	g := Guided(m, img)
	debugf("guided %dx%d -> %dx%d %v", m.W, m.H, W, H, time.Since(t0))
	return g, nil
}

// resized: img at w×h
func resized(img image.Image, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	return out
}

// inferImage: the model on an image (through a temporary file: it reads files; a PPM, which costs
// next to nothing to write and read, unlike a PNG)
func inferImage(sp Spec, paths []string, img image.Image, plain bool) (*Raw, error) {
	if sp.Kind == KindVDA { // (the video model: a window of one frame)
		rgba, ok := img.(*image.RGBA)
		if !ok || rgba.Bounds().Min != (image.Point{}) {
			rgba = resized(img, img.Bounds().Dx(), img.Bounds().Dy())
		}
		w, h, out, err := InferVideo(sp, paths, []*image.RGBA{rgba})
		if err != nil {
			return nil, err
		}
		return toRaw(sp, out[0], w, h, 0), nil
	}
	tmp, err := os.CreateTemp("", "audioviz-depth-*.ppm")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(ppm(img))
	tmp.Close()
	if err != nil {
		return nil, err
	}
	return Infer(sp, paths, tmp.Name(), plain)
}

// ppm: img as a binary PPM (P6, RGB)
func ppm(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	head := fmt.Sprintf("P6\n%d %d\n255\n", w, h)
	out := make([]byte, len(head)+w*h*3)
	copy(out, head)
	px := out[len(head):]
	rgba, ok := img.(*image.RGBA)
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			o := y * w * 3
			if ok {
				src := rgba.Pix[(y+b.Min.Y-rgba.Rect.Min.Y)*rgba.Stride+(b.Min.X-rgba.Rect.Min.X)*4:]
				for x := 0; x < w; x++ {
					px[o], px[o+1], px[o+2] = src[x*4], src[x*4+1], src[x*4+2]
					o += 3
				}
				continue
			}
			for x := 0; x < w; x++ {
				r, g, bb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				px[o], px[o+1], px[o+2] = byte(r>>8), byte(g>>8), byte(bb>>8)
				o += 3
			}
		}
	})
	return out
}

// tiled: the depth of img (W×H) by tiles, fitted to global (a coarse look at the whole image)
func tiled(ctx context.Context, sp Spec, paths []string, img *image.RGBA, global *Raw, stage func(string, float64)) (*Map, error) {
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	ts := sp.Tile
	tw, th := min(ts, W), min(ts, H)
	// the coarse look at this size (and its sky)
	gu := bilinear(global.Nearness(), W, H)
	var gs []float32
	if global.Sky != nil {
		gs = resample(global.Sky, global.W, global.H, W, H)
	}
	starts := func(n, t int) []int {
		if n <= t {
			return []int{0}
		}
		k := int(math.Ceil(float64(n-t)/float64(t*2/3))) + 1
		out := make([]int, k)
		for i := range out {
			out[i] = int(math.Round(float64(i) * float64(n-t) / float64(k-1)))
		}
		return out
	}
	xs, ys := starts(W, tw), starts(H, th)
	acc := make([]float64, W*H)
	wsum := make([]float64, W*H)
	sky := make([]float64, W*H)
	ov := float64(ts) / 3 // the width of a seam
	done, total := 0, len(xs)*len(ys)
	for _, y0 := range ys {
		for _, x0 := range xs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			tile := image.NewRGBA(image.Rect(0, 0, tw, th))
			for y := 0; y < th; y++ {
				copy(tile.Pix[y*tile.Stride:y*tile.Stride+tw*4], img.Pix[(y0+y)*img.Stride+x0*4:])
			}
			raw, err := inferImage(sp, paths, tile, false)
			if err != nil {
				return nil, err
			}
			rv := resample(raw.V, raw.W, raw.H, tw, th)
			var rs []float32
			if raw.Sky != nil {
				rs = resample(raw.Sky, raw.W, raw.H, tw, th)
			}
			// fit a·v + b to the coarse look over this tile (least squares, leaving out the sky)
			var n, sv, sg, svv, svg float64
			for y := 0; y < th; y++ {
				for x := 0; x < tw; x++ {
					i := y*tw + x
					if rs != nil && rs[i] > 0.5 {
						continue
					}
					v, g := float64(rv[i]), float64(gu[(y0+y)*W+x0+x])
					n++
					sv, sg, svv, svg = sv+v, sg+g, svv+v*v, svg+v*g
				}
			}
			a, b := 0.0, 0.0
			if n > 0 {
				mv, mg := sv/n, sg/n
				if vv := svv/n - mv*mv; vv > 1e-12 {
					a = max(0, (svg/n-mv*mg)/vv)
				}
				b = mg - a*mv
			}
			// blended with soft seams (none at the image's own edges)
			ramp := func(d, t int, first, last bool) float64 {
				w := 1.0
				if !first {
					w = min(w, (float64(d)+0.5)/ov)
				}
				if !last {
					w = min(w, (float64(t-d)-0.5)/ov)
				}
				w = max(0, min(1, w))
				return w * w * (3 - 2*w)
			}
			for y := 0; y < th; y++ {
				wy := ramp(y, th, y0 == 0, y0+th >= H)
				for x := 0; x < tw; x++ {
					wx := ramp(x, tw, x0 == 0, x0+tw >= W)
					w := max(wx*wy, 1e-6)
					i, o := y*tw+x, (y0+y)*W+x0+x
					acc[o] += w * (a*float64(rv[i]) + b)
					wsum[o] += w
					if rs != nil {
						sky[o] += w * float64(rs[i])
					}
				}
			}
			done++
			stage("estimate", float64(done)/float64(total))
		}
	}
	// the blend, with its large shapes from the coarse look: the tiles bring the detail (the
	// sky left out of that, so it doesn't bleed around it)
	fine := make([]float64, W*H)
	diff := make([]float64, W*H)
	wt := make([]float64, W*H)
	for i := range fine {
		fine[i] = acc[i] / max(wsum[i], 1e-9)
		w := 1.0
		if gs != nil { // the whole look's sky: the same for every tile
			w = 1 - min(1, float64(gs[i])*2)
		}
		wt[i] = max(w, 0.02)
		diff[i] = (float64(gu[i]) - fine[i]) * wt[i]
	}
	r := max(4, ts/6)
	diff = boxBlur(boxBlur(diff, W, H, r), W, H, r)
	wt = boxBlur(boxBlur(wt, W, H, r), W, H, r)
	for i := range diff {
		diff[i] /= wt[i]
	}
	m := &Map{W: W, H: H, V: make([]float32, W*H)}
	for i := range m.V {
		v := fine[i] + diff[i]
		if s := sky[i] / max(wsum[i], 1e-9); s > 0 {
			v *= 1 - float64(smooth(0.3, 0.7, float32(s)))
		}
		m.V[i] = float32(max(0, min(1, v)))
	}
	return m, nil
}

// bilinear: a map at w×h
func bilinear(m *Map, w, h int) []float32 { return resample(m.V, m.W, m.H, w, h) }

// resample: a w0×h0 field at w×h (bilinear, pixel centres)
func resample(v []float32, w0, h0, w, h int) []float32 {
	if w0 == w && h0 == h {
		return v
	}
	out := make([]float32, w*h)
	rows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			fy := max(0, min(float64(h0-1), (float64(y)+0.5)*float64(h0)/float64(h)-0.5))
			iy := min(int(fy), h0-2)
			if h0 == 1 {
				iy = 0
			}
			ty := float32(fy - float64(iy))
			iy1 := min(iy+1, h0-1)
			for x := 0; x < w; x++ {
				fx := max(0, min(float64(w0-1), (float64(x)+0.5)*float64(w0)/float64(w)-0.5))
				ix := min(int(fx), max(0, w0-2))
				tx := float32(fx - float64(ix))
				ix1 := min(ix+1, w0-1)
				a := v[iy*w0+ix] + (v[iy*w0+ix1]-v[iy*w0+ix])*tx
				b := v[iy1*w0+ix] + (v[iy1*w0+ix1]-v[iy1*w0+ix])*tx
				out[y*w+x] = a + (b-a)*ty
			}
		}
	})
	return out
}

// boxBlur: the mean over (2r+1)² around each point (clamped at the edges), rows then columns
func boxBlur(v []float64, w, h, r int) []float64 {
	tmp := make([]float64, w*h)
	out := make([]float64, w*h)
	pass := func(src, dst []float64, n, m, stride, step int) {
		// n lines of m samples: line i starts at i*stride, samples step apart
		rows(n, func(i0, i1 int) {
			for i := i0; i < i1; i++ {
				base := i * stride
				var s float64
				at := func(k int) float64 { return src[base+max(0, min(m-1, k))*step] }
				for k := -r; k <= r; k++ {
					s += at(k)
				}
				for k := 0; k < m; k++ {
					dst[base+k*step] = s / float64(2*r+1)
					s += at(k+r+1) - at(k-r)
				}
			}
		})
	}
	pass(v, tmp, h, w, w, 1)
	pass(tmp, out, w, h, 1, w)
	return out
}
