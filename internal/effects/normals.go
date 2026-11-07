package effects

import "math"

// Surface normals of a depth map, for the 2.5D lights (LightField.DepthFactor, depthLight in
// render.comp). Depth maps of a single image are layers with relief inside (an arm, a wing's
// feathers) and jumps between them (a figure in front of the sky): the slope is taken without
// crossing the jumps (one-sided there), then smoothed at a fine and a coarse scale (the slope, not
// the depth, so a jump never smears into a wide dark band), and the outlines are rounded a little
// (tilted outwards in a narrow band along the jumps), so the light wraps the shapes: strongest
// where a surface faces it, fading as it turns away. Worked out once per fitted depth map; the GPU
// reads it packed like the depth (16 bits per pixel).

const (
	normalJump    = 0.006 // a jump: more nearness than this from one pixel to the next
	normalRelief  = 400   // px of height for the whole range of nearness (× the depth's relief)
	normalInflate = 0.2   // how much the outlines round off
	normalBand    = 4     // and over how many px (box radius)
)

// NormalMap: the normals of depth map d (W×H, 65535 = near) with relief k (1 = default), packed
// per pixel as two int8: low byte x (right), high byte y (down), scaled by 127; z (towards the
// viewer) = √(1 − x² − y²)
func NormalMap(d []uint16, W, H int, k float64) []uint16 {
	if k <= 0 {
		k = 1
	}
	relief := float32(normalRelief * k)
	if len(d) != W*H || W < 3 || H < 3 {
		return nil
	}
	z := make([]float32, W*H)
	for i, v := range d {
		z[i] = float32(v) / 65535
	}
	gx, gy, edge := make([]float32, W*H), make([]float32, W*H), make([]float32, W*H)
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				c := z[y*W+x]
				r := z[y*W+min(x+1, W-1)] - c
				l := c - z[y*W+max(x-1, 0)]
				dn := z[min(y+1, H-1)*W+x] - c
				up := c - z[max(y-1, 0)*W+x]
				var ex, ey bool
				gx[y*W+x], ex = jumpAware(r, l)
				gy[y*W+x], ey = jumpAware(dn, up)
				if ex || ey {
					edge[y*W+x] = 1
				}
			}
		}
	})
	fx, fy := blurN(gx, W, H, 2, 2), blurN(gy, W, H, 2, 2)
	cx, cy := blurN(gx, W, H, 6, 3), blurN(gy, W, H, 6, 3)
	near := blurN(z, W, H, normalBand, 2)
	band := blurN(edge, W, H, normalBand, 2)
	out := make([]uint16, W*H)
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			ym, yp := max(y-1, 0), min(y+1, H-1)
			for x := 0; x < W; x++ {
				i := y*W + x
				sx := (0.4*fx[i] + 0.6*cx[i]) * relief
				sy := (0.4*fy[i] + 0.6*cy[i]) * relief
				// (only the nearer side of a jump: the object's outline, not what is behind it)
				if m := min(1, band[i]*4); m > 0 && z[i] > near[i] {
					xm, xp := max(x-1, 0), min(x+1, W-1)
					nx := (near[y*W+xp] - near[y*W+xm]) / float32(xp-xm)
					ny := (near[yp*W+x] - near[ym*W+x]) / float32(yp-ym)
					sx += normalInflate * 2 * relief * nx * m
					sy += normalInflate * 2 * relief * ny * m
				}
				// the surface rises towards the viewer where it gets nearer: n = (−∂z/∂x, −∂z/∂y, 1)
				l := float32(math.Sqrt(float64(sx*sx + sy*sy + 1)))
				out[i] = uint16(uint8(int8(clampNorm(-sx/l)))) | uint16(uint8(int8(clampNorm(-sy/l))))<<8
			}
		}
	})
	return out
}

// jumpAware: the slope from the differences after (a) and before (b) a pixel: their mean, the one
// on the pixel's side of a jump, or 0 between two; and whether there was a jump
func jumpAware(a, b float32) (float32, bool) {
	ja, jb := a > normalJump || a < -normalJump, b > normalJump || b < -normalJump
	switch {
	case ja && jb:
		return 0, true
	case ja:
		return b, true
	case jb:
		return a, true
	}
	return (a + b) / 2, false
}

func clampNorm(v float32) float32 {
	return float32(math.Round(float64(max(-1, min(1, v)) * 127)))
}

// normalTexel: the packed normal at a pixel (clamped to the map) as x, y
func normalTexel(g []uint16, x, y, W, H int) (float32, float32) {
	x, y = max(0, min(W-1, x)), max(0, min(H-1, y))
	v := g[y*W+x]
	return float32(int8(uint8(v))) / 127, float32(int8(uint8(v>>8))) / 127
}

// layerNormal: normal map g at output point q with the layer transform (as layerNormal in
// render.comp): bilinear, turned with the layer, z from x and y
func layerNormal(g []uint16, L Layer, qx, qy float32, W, H int) (float32, float32, float32) {
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
	ax, ay := normalTexel(g, x0, y0, W, H)
	bx, by := normalTexel(g, x0+1, y0, W, H)
	cx0, cy0 := normalTexel(g, x0, y0+1, W, H)
	ex, ey := normalTexel(g, x0+1, y0+1, W, H)
	nx := (ax+(bx-ax)*fx)*(1-fy) + (cx0+(ex-cx0)*fx)*fy
	ny := (ay+(by-ay)*fx)*(1-fy) + (cy0+(ey-cy0)*fx)*fy
	if L.Rot != 0 { // the map's directions turn with the layer
		c, s := float32(math.Cos(L.Rot)), float32(math.Sin(L.Rot))
		nx, ny = nx*c-ny*s, nx*s+ny*c
	}
	nz := float32(math.Sqrt(float64(max(0, 1-nx*nx-ny*ny))))
	l := float32(math.Sqrt(float64(nx*nx+ny*ny+nz*nz))) + 1e-6
	return nx / l, ny / l, nz / l
}

// blurN: n passes of a box blur of radius r (≈ a Gaussian of σ ≈ r·√(n/3)·… for n ≥ 2)
func blurN(src []float32, W, H, r, n int) []float32 {
	a := append([]float32(nil), src...)
	b := make([]float32, len(src))
	for i := 0; i < n; i++ {
		boxH(a, b, W, H, r)
		boxV(b, a, W, H, r)
	}
	return a
}

func boxH(src, dst []float32, W, H, r int) {
	parallelFor(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := src[y*W : (y+1)*W]
			var s float32
			for x := -r; x <= r; x++ {
				s += row[max(0, min(W-1, x))]
			}
			k := 1 / float32(2*r+1)
			for x := 0; x < W; x++ {
				dst[y*W+x] = s * k
				s += row[min(W-1, x+r+1)] - row[max(0, x-r)]
			}
		}
	})
}

func boxV(src, dst []float32, W, H, r int) {
	parallelFor(W, func(x0, x1 int) {
		k := 1 / float32(2*r+1)
		for x := x0; x < x1; x++ {
			var s float32
			for y := -r; y <= r; y++ {
				s += src[max(0, min(H-1, y))*W+x]
			}
			for y := 0; y < H; y++ {
				dst[y*W+x] = s * k
				s += src[min(H-1, y+r+1)*W+x] - src[max(0, y-r)*W+x]
			}
		}
	})
}
