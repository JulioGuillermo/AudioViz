package lyrics

import (
	"image"
	"math"
	"runtime"
	"sync"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/visualizer"
)

type GradStop struct {
	Off float64
	C   [3]float64
}

// Style holds the lyrics colors and parameters for a given frame (audio reaction already applied)
type Style struct {
	Z                                  *int // z_index of the lyrics / text among the overlay layers (nil = above all)
	OutlineW, GlowR, GlowI, Chroma     float64
	Color, Upcoming, Outline, GlowC    [4]float64
	ShadowDX, ShadowDY, ShadowOp, Blur float64
	ShadowC                            [4]float64
	Grad                               []GradStop
	// Fixed texts: which one (index in config.Texts; -1 = the lyrics), its anchor on screen (its
	// position after the keys, px) and rotation (rad): the studio's handles (see Multi.Boxes)
	Text     int
	AnchorX  float32
	AnchorY  float32
	Rotation float32
	// Screen mask (config.LyricsConfig.Masks): the config's, and the image the engine makes of
	// it (frame-sized up to effects.MaskSize; nil = seen everywhere)
	Masks          []config.MaskConfig
	Mask           *image.Alpha
	MaskVer        string
	MaskSX, MaskSY float64 // mask px per frame px
	// Its depth (config): the engine places it (InDepth) at DepthZ, hidden where the scene is
	// nearer if Behind (soft by DepthSoft), and blurred by DepthBlur px (the depth of field)
	Depth                        config.ClipDepth
	InDepth, Behind              bool
	DepthZ, DepthSoft, DepthBlur float64
	Scene                        *effects.Scene
	// its depth from its path, per point (each item's PS, PTX, PTY); ZMargin: how far (px) the
	// 2.5D camera and the depth of field can take it at most
	ZTrack  *effects.PathTrack
	ZMargin float64
	// twisting like a ribbon along its path (Motion.Flip "twist"), per pixel (each item's PS,
	// PTX, PTY); or wrapped round a cylinder (Wrap: its letters slanted, Item.Shear)
	Ribbon *effects.Ribbon
	Wrap   bool
	// Its light on the scene (config.TextLight): color × strength now (with the band), reach
	// and end (px of 1080p), add (or soft), fade with depth and shading
	LightOn                 bool
	LightCol                [3]float64
	LightR, LightE          float64
	LightAdd                bool
	LightDepthK, LightShade float64
	Light                   config.TextLight // all of it (range and rim light)
	// Its properties worked out per letter or pixel (expressions, as TextPxProps; nil: none);
	// whether it glows, casts a shadow; its opacity (on its items' alpha unless X[4])
	X                [NTextPx]*effects.PixExpr
	GlowOn, ShadowOn bool
	Opacity          float64
}

// cover: how much of a point at distance d (px, inside < 0) of an edge is covered: a sharp edge,
// or blurred over bl px (as in drawText)
func cover(d, bl float64) float64 {
	if bl > 0.5 {
		return 1 - smoothstep(-bl, bl, d)
	}
	return clampF(0.5-d, 0, 1)
}

func rgba(hex string, def [4]float64) [4]float64 {
	if hex == "" {
		return def
	}
	c, err := visualizer.ParseHexColor(hex)
	if err != nil {
		return def
	}
	return [4]float64{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, float64(c.A) / 255}
}

func mix3(a, b [3]float64, t float64) [3]float64 {
	return [3]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

// StyleAt computes the frame style: color flash, flame-like flicker and reactive glow
func (e *Engine) StyleAt(t float64, feat *audio.FrameFeatures) Style {
	return StyleFor(e.cfg, t, feat)
}

// StyleFor computes a frame style with the given config (that of the segment active at t)
func StyleFor(cfg *config.LyricsConfig, t float64, feat *audio.FrameFeatures) Style {
	band := 0.0
	if feat != nil {
		band = math.Min(feat.Band(cfg.React.ReactTo, "bass"), 1)
	}
	white := [4]float64{1, 1, 1, 1}
	st := Style{
		Depth:    cfg.Depth,
		Masks:    cfg.Masks,
		OutlineW: cfg.OutlineWidth,
		Color:    rgba(cfg.Color, white),
		Upcoming: rgba(cfg.UpcomingColor, [4]float64{1, 1, 1, 0.4}),
		Outline:  rgba(cfg.OutlineColor, [4]float64{0, 0, 0, 1}),
		ShadowC:  rgba(cfg.Shadow.Color, [4]float64{0, 0, 0, 1}),
	}
	if cfg.OutlineWidth <= 0 {
		st.Outline[3] = 0
	}
	for _, g := range cfg.Gradient {
		c := rgba(g.Color, white)
		st.Grad = append(st.Grad, GradStop{g.Offset, [3]float64{c[0], c[1], c[2]}})
	}
	st.GlowOn, st.ShadowOn, st.Opacity = cfg.Glow.Enabled, cfg.Shadow.Enabled, 1
	if cfg.Glow.Enabled {
		st.GlowR = cfg.Glow.Radius
		st.GlowI = cfg.Glow.Intensity + cfg.React.Glow*band
		gc := st.Color
		if len(st.Grad) > 0 {
			gc = [4]float64{st.Grad[len(st.Grad)/2].C[0], st.Grad[len(st.Grad)/2].C[1], st.Grad[len(st.Grad)/2].C[2], 1}
		}
		st.GlowC = rgba(cfg.Glow.Color, gc)
	}
	if cfg.Shadow.Enabled {
		st.ShadowDX, st.ShadowDY, st.ShadowOp, st.Blur = cfg.Shadow.OffsetX, cfg.Shadow.OffsetY, cfg.Shadow.Opacity, cfg.Shadow.Blur
	}
	st.Chroma = cfg.React.Chromatic * band
	if l := cfg.Light; l.Enabled && l.Intensity > 0 && l.Reach > 0 {
		// its color: the light's, or the text's (its glow's when it glows)
		c := st.Color
		if st.GlowI > 0 {
			c = st.GlowC
		}
		c = rgba(l.Color, c)
		k := l.Intensity * config.LightMusic(l.React, l.Quiet, band)
		st.LightOn, st.LightCol = true, [3]float64{c[0] * k, c[1] * k, c[2] * k}
		st.LightR, st.LightE, st.LightAdd = l.Reach, l.End, l.Mode == "add"
		if st.LightE <= 0 {
			st.LightE = 2.5 * l.Reach
		}
		st.LightDepthK, st.LightShade = l.Depth, l.Shade
		st.Light = l
	}

	// Flash toward another color with the band
	if cfg.React.Flash > 0 && cfg.React.FlashColor != "" {
		fc := rgba(cfg.React.FlashColor, white)
		f3 := [3]float64{fc[0], fc[1], fc[2]}
		k := math.Min(cfg.React.Flash*band, 1)
		c := mix3([3]float64{st.Color[0], st.Color[1], st.Color[2]}, f3, k)
		st.Color = [4]float64{c[0], c[1], c[2], st.Color[3]}
		for i := range st.Grad {
			st.Grad[i].C = mix3(st.Grad[i].C, f3, k)
		}
	}
	// Flame flicker: brightness with fast noise proportional to the band
	if cfg.React.Flicker > 0 {
		n := 0.5*math.Sin(t*31.0) + 0.3*math.Sin(t*57.0+1.1) + 0.2*math.Sin(t*89.0+2.3)
		b := 1 + cfg.React.Flicker*(0.35+0.65*band)*n*0.5
		scale := func(c *[3]float64) {
			for i := range c {
				c[i] = math.Min(c[i]*b, 1)
			}
		}
		c := [3]float64{st.Color[0], st.Color[1], st.Color[2]}
		scale(&c)
		st.Color = [4]float64{c[0], c[1], c[2], st.Color[3]}
		for i := range st.Grad {
			scale(&st.Grad[i].C)
		}
		g := [3]float64{st.GlowC[0], st.GlowC[1], st.GlowC[2]}
		scale(&g)
		st.GlowC = [4]float64{g[0], g[1], g[2], 1}
		st.GlowI *= b
	}
	return st
}

// ---------------------------------------------------------------- CPU render

func (e *Engine) sdfAt(ax, ay float64, it *Item) float64 {
	x0, y0 := float64(it.AX), float64(it.AY)
	x1, y1 := x0+float64(it.AW)-1, y0+float64(it.AH)-1
	ax = math.Max(x0, math.Min(x1, ax))
	ay = math.Max(y0, math.Min(y1, ay))
	ix, iy := int(ax), int(ay)
	fx, fy := ax-float64(ix), ay-float64(iy)
	jx, jy := min(ix+1, int(x1)), min(iy+1, int(y1))
	W := e.Atlas.W
	p := e.Atlas.Pix
	v := (float64(p[iy*W+ix])*(1-fx)+float64(p[iy*W+jx])*fx)*(1-fy) +
		(float64(p[jy*W+ix])*(1-fx)+float64(p[jy*W+jx])*fx)*fy
	return (128 - v) / SDFScale
}

func (st *Style) gradient(t float64) [3]float64 {
	g := st.Grad
	if len(g) == 0 {
		return [3]float64{st.Color[0], st.Color[1], st.Color[2]}
	}
	if len(g) == 1 || t <= g[0].Off {
		return g[0].C
	}
	if t >= g[len(g)-1].Off {
		return g[len(g)-1].C
	}
	for i := 0; i < len(g)-1; i++ {
		if t >= g[i].Off && t <= g[i+1].Off {
			return mix3(g[i].C, g[i+1].C, (t-g[i].Off)/math.Max(g[i+1].Off-g[i].Off, 1e-6))
		}
	}
	return g[len(g)-1].C
}

// hasX: whether some property is worked out per letter / pixel; FirstX: one of them
func (st *Style) hasX() bool { return st.FirstX() != nil }
func (st *Style) FirstX() *effects.PixExpr {
	for _, x := range st.X {
		if x != nil {
			return x
		}
	}
	return nil
}

func clampF(v, a, b float64) float64 { return math.Max(a, math.Min(b, v)) }

func smoothstep(a, b, x float64) float64 {
	t := clampF((x-a)/(b-a), 0, 1)
	return t * t * (3 - 2*t)
}

// chromaOf is the lyric chromatic aberration actually applied (same 0.3 px threshold as the GPU)
func chromaOf(st *Style) float64 {
	if st.Chroma > 0.3 {
		return st.Chroma
	}
	return 0
}

// maskAt: the alpha of m at a point in its px (bilinear, 0 outside), as maskAt in render.comp
func maskAt(m *image.Alpha, x, y float64) float64 {
	x, y = x-0.5, y-0.5
	mw, mh := m.Rect.Dx(), m.Rect.Dy()
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

// textAt replicates the shader's drawText. ch = -1, 0, 1: channel pass for chromatic aberration
// (each item's point is shifted by ch times its style's chroma).
func (e *Engine) textAt(col [3]float64, px0, py float64, ch float64, items []Item, styles []Style) [3]float64 {
	// the order they are drawn in here: as they come, or far to near where they have a depth
	// along their path, twist as ribbons or wrap round a cylinder (as drawText)
	var ordBuf [maxTextOrder]int
	ord := textOrder(ordBuf[:0], px0, py, items, styles)
	nn := len(items)
	if ord != nil {
		nn = len(ord)
	}
	for oi := 0; oi < nn; oi++ {
		i := oi
		if ord != nil {
			i = ord[oi]
		}
		it := &items[i]
		st := &styles[it.Style]
		px := px0 + ch*chromaOf(st)
		sc := float64(it.Scale)
		// its depth here: from its path, per point (moved by the 2.5D camera, blurred by the
		// depth of field as that point is), or one for all of it
		zq, bl, qy := st.DepthZ, st.DepthBlur, py
		zt := st.ZTrack != nil && st.Scene.On()
		if zt {
			zq = float64(st.ZTrack.At(it.PS + float32(px0-float64(it.CX))*it.PTX + float32(py-float64(it.CY))*it.PTY))
			ddx, ddy := st.Scene.Par.Disp(zq, px0-0.5, py-0.5, st.Scene.W, st.Scene.H)
			px, qy = px-ddx, py-ddy
			bl = float64(st.Scene.DoF.CoC(float32(zq)))
		}
		dx, dy := px-float64(it.CX), qy-float64(it.CY)
		rc, rs := 1.0, 0.0
		if it.Rot != 0 {
			rc, rs = math.Cos(float64(it.Rot)), math.Sin(float64(it.Rot))
		}
		unrot := func(x, y float64) (float64, float64) { return x*rc + y*rs, -x*rs + y*rc }
		lx, ly := unrot(dx/sc, dy/sc)
		if st.Ribbon != nil {
			// a ribbon: the height of the letter seen here, as twisted as its point on the path is
			sp := it.PS + float32(px0-float64(it.CX))*it.PTX + float32(py-float64(it.CY))*it.PTY
			v, ok := st.Ribbon.V(sp, float32(ly))
			if !ok {
				continue
			}
			ly = float64(v)
		}
		if it.Flip != 0 || it.FlipY != 0 {
			// turned round: narrower or lower (mirrored when negative)
			w, h := 1-float64(it.Flip), 1-float64(it.FlipY)
			if math.Abs(w) < 0.02 || math.Abs(h) < 0.02 {
				continue
			}
			lx, ly = lx/w, ly/h
		}
		lx -= float64(it.Shear) * ly // wrapped round a cylinder: slanted
		hw, hh := float64(it.AW)/2, float64(it.AH)/2
		if math.Abs(lx) > hw || math.Abs(ly) > hh {
			continue
		}
		ax, ay := float64(it.AX)+hw+lx, float64(it.AY)+hh+ly
		pad := float64(it.Pad)
		alpha := float64(it.Alpha)
		if st.Mask != nil {
			if alpha *= maskAt(st.Mask, px0*st.MaskSX, py*st.MaskSY); alpha <= 0 {
				continue
			}
		}
		if st.Behind && st.Scene.On() {
			// hidden where the scene is nearer (the depth at this pixel)
			z := float64(st.Scene.At(float32(px0-0.5), float32(py-0.5)))
			if alpha *= 1 - smoothstep(zq-st.DepthSoft, zq+st.DepthSoft+1e-4, z); alpha <= 0 {
				continue
			}
		}
		// its properties here: its style's, or worked out per letter / pixel (expressions; as
		// drawText)
		ty := clampF((ay-float64(it.AY)-pad)/math.Max(float64(it.AH)-2*pad, 1), 0, 1)
		main := st.gradient(ty)
		pv := [NTextPx][4]float64{{main[0], main[1], main[2], st.Color[3]}, st.Outline, st.GlowC, st.ShadowC,
			{1}, {st.OutlineW}, {st.GlowI}, {st.GlowR}, {st.ShadowOp}, {st.Blur}}
		if st.hasX() {
			x := st.FirstX()
			pin := effects.PixIn{Index: it.Index, Count: it.Count,
				S:  it.PS + float32(px0-float64(it.CX))*it.PTX + float32(py-float64(it.CY))*it.PTY,
				TX: float64(it.TX) + lx*float64(it.TSX), TY: float64(it.TY) + ly*float64(it.TSY),
				LX: clampF((lx+hw-pad)/math.Max(float64(it.AW)-2*pad, 1), 0, 1),
				LY: clampF((ly+hh-pad)/math.Max(float64(it.AH)-2*pad, 1), 0, 1),
				SX: px0 / float64(x.W), SY: py / float64(x.H)}
			lin := effects.PixIn{Index: it.Index, Count: it.Count, S: it.PS, TX: float64(it.TX), TY: float64(it.TY),
				LX: 0.5, LY: 0.5, SX: float64(it.CX) / float64(x.W), SY: float64(it.CY) / float64(x.H)}
			for k, xk := range st.X {
				if xk == nil {
					continue
				}
				in := pin
				if xk.Letter {
					in = lin
				}
				if k < TextPxColors {
					pv[k] = xk.Color(in, pv[k])
				} else {
					base := pv[k][0]
					if k == 4 {
						base = st.Opacity // (not on its alpha then)
					}
					v := math.Max(0, xk.Scalar(in, base))
					if k == 4 {
						v = math.Min(v, 1)
					}
					pv[k][0] = v
				}
			}
		}
		ow := pv[5][0]
		gr := math.Max(pv[7][0], 0.5)
		alpha *= pv[4][0]
		if st.ShadowOn && pv[8][0] > 0 {
			sx, sy := unrot(st.ShadowDX/sc, st.ShadowDY/sc)
			ds := e.sdfAt(ax-sx, ay-sy, it)*sc - ow
			blur := math.Max(pv[9][0], 0.75)
			sa := (1 - smoothstep(-blur, blur, ds)) * pv[8][0] * alpha
			col = mix3(col, [3]float64{pv[3][0], pv[3][1], pv[3][2]}, sa)
		}
		d := e.sdfAt(ax, ay, it) * sc
		if st.GlowOn && pv[6][0] > 0 {
			g := math.Exp(-math.Max(d-ow, 0)/(gr*0.45)) * pv[6][0] * alpha
			g *= 1 - smoothstep(0.55, 0.95, (d/sc)/math.Max(pad, 1))
			col = mix3(col, [3]float64{pv[2][0], pv[2][1], pv[2][2]}, clampF(g, 0, 1)*0.85)
		}
		if ow > 0 {
			oc := pv[1]
			col = mix3(col, [3]float64{oc[0], oc[1], oc[2]}, cover(d-ow, bl)*oc[3]*alpha)
		}
		fillX := float64(it.AX) + pad + float64(it.Fill)*(float64(it.AW)-2*pad)
		k := clampF(fillX-ax+0.5, 0, 1)
		fc := mix3([3]float64{st.Upcoming[0], st.Upcoming[1], st.Upcoming[2]}, [3]float64{pv[0][0], pv[0][1], pv[0][2]}, k)
		fa := st.Upcoming[3] + (pv[0][3]-st.Upcoming[3])*k
		col = mix3(col, fc, cover(d, bl)*fa*alpha)
	}
	return col
}

// DrawCPU composites the texts onto the image (CPU render), identical to the shader.
// Each item uses styles[item.Style].
func (e *Engine) DrawCPU(img *image.RGBA, items []Item, styles []Style) {
	if len(items) == 0 {
		return
	}
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	// Box covering all words (with scale, glow, shadow and aberration)
	x0, y0, x1, y1 := float64(W), float64(H), 0.0, 0.0
	anyChroma := false
	for _, it := range items {
		st := &styles[it.Style]
		anyChroma = anyChroma || chromaOf(st) > 0
		hw, hh := float64(it.AW)*float64(it.Scale)/2, float64(it.AH)*float64(it.Scale)/2
		if it.Rot != 0 {
			c, s := math.Abs(math.Cos(float64(it.Rot))), math.Abs(math.Sin(float64(it.Rot)))
			hw, hh = hw*c+hh*s, hw*s+hh*c
		}
		ex := chromaOf(st) + math.Abs(st.ShadowDX) + st.DepthBlur + st.ZMargin + 2
		ey := math.Abs(st.ShadowDY) + st.DepthBlur + st.ZMargin + 2
		x0, x1 = math.Min(x0, float64(it.CX)-hw-ex), math.Max(x1, float64(it.CX)+hw+ex)
		y0, y1 = math.Min(y0, float64(it.CY)-hh-ey), math.Max(y1, float64(it.CY)+hh+ey)
	}
	ix0, iy0 := max(int(x0), 0), max(int(y0), 0)
	ix1, iy1 := min(int(x1)+1, W), min(int(y1)+1, H)
	if ix0 >= ix1 || iy0 >= iy1 {
		return
	}
	var wg sync.WaitGroup
	nw := runtime.NumCPU()
	for k := 0; k < nw; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for y := iy0 + k; y < iy1; y += nw {
				for x := ix0; x < ix1; x++ {
					o := y*img.Stride + x*4
					base := [3]float64{float64(img.Pix[o]) / 255, float64(img.Pix[o+1]) / 255, float64(img.Pix[o+2]) / 255}
					px, py := float64(x)+0.5, float64(y)+0.5
					var c [3]float64
					if anyChroma {
						c[0] = e.textAt(base, px, py, -1, items, styles)[0]
						c[1] = e.textAt(base, px, py, 0, items, styles)[1]
						c[2] = e.textAt(base, px, py, 1, items, styles)[2]
					} else {
						c = e.textAt(base, px, py, 0, items, styles)
					}
					for ch := 0; ch < 3; ch++ {
						img.Pix[o+ch] = uint8(clampF(c[ch], 0, 1)*255 + 0.5)
					}
				}
			}
		}(k)
	}
	wg.Wait()
}

// maxTextOrder: texts sorted at one pixel at most (as MAX_ORD in render.comp)
const maxTextOrder = 16

// textOrder: the items drawn at pixel (px, py) far to near (the items near it: within their box
// and their glow), when some style has a depth along its path, twists or wraps; nil: draw
// them all as they come. As drawText's order.
func textOrder(buf []int, px, py float64, items []Item, styles []Style) []int {
	any := false
	for i := range styles {
		if styles[i].Ribbon != nil || styles[i].Wrap || (styles[i].ZTrack != nil && styles[i].Scene.On()) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	var keys [maxTextOrder]float32
	for i := range items {
		it := &items[i]
		st := &styles[it.Style]
		reach := math.Hypot(float64(it.AW), float64(it.AH))*0.5*float64(it.Scale)*(1+math.Abs(float64(it.Shear))) + chromaOf(st) +
			math.Max(math.Abs(st.ShadowDX), math.Abs(st.ShadowDY)) + st.DepthBlur + st.ZMargin + st.GlowR + 2
		if math.Hypot(px-float64(it.CX), py-float64(it.CY)) > reach {
			continue
		}
		key := float32(i) * 1e-4
		zt := st.ZTrack != nil && st.Scene.On()
		if zt || st.Ribbon != nil || st.Wrap {
			sp := it.PS + float32(px-float64(it.CX))*it.PTX + float32(py-float64(it.CY))*it.PTY
			switch {
			case zt:
				key = 10 + st.ZTrack.At(sp)
			case st.Wrap:
				key = 10.5 + 0.5*it.PTX // going right: in front
			default:
				k, _ := st.Ribbon.At(sp)
				key = 10.5 + 0.5*k
			}
		}
		if len(buf) == maxTextOrder {
			break
		}
		j := len(buf)
		buf = append(buf, i)
		for j > 0 && keys[j-1] > key {
			buf[j], keys[j] = buf[j-1], keys[j-1]
			j--
		}
		buf[j], keys[j] = i, key
	}
	return buf
}
