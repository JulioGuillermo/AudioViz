package lyrics

import (
	"image"
	"math"
	"runtime"
	"sync"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/visualizer"
)

type GradStop struct {
	Off float64
	C   [3]float64
}

// Style son los colores y parámetros de la letra en un cuadro concreto (ya con la reacción al audio)
type Style struct {
	OutlineW, GlowR, GlowI, Chroma      float64
	Color, Upcoming, Outline, GlowC     [4]float64
	ShadowDX, ShadowDY, ShadowOp, Blur  float64
	ShadowC                             [4]float64
	Grad                                []GradStop
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

// StyleAt calcula el estilo del cuadro: flash de color, parpadeo tipo llama y glow reactivo
func (e *Engine) StyleAt(t float64, feat *audio.FrameFeatures) Style {
	return StyleFor(e.cfg, t, feat)
}

// StyleFor calcula el estilo de un cuadro con la config indicada (la del tramo activo en t)
func StyleFor(cfg *config.LyricsConfig, t float64, feat *audio.FrameFeatures) Style {
	band := 0.0
	if feat != nil {
		band = math.Min(feat.Band(cfg.React.ReactTo, "bass"), 1)
	}
	white := [4]float64{1, 1, 1, 1}
	st := Style{
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

	// Flash hacia otro color con la banda
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
	// Parpadeo de llama: brillo con ruido rápido proporcional a la banda
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

// ---------------------------------------------------------------- render por CPU

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

func clampF(v, a, b float64) float64 { return math.Max(a, math.Min(b, v)) }

func smoothstep(a, b, x float64) float64 {
	t := clampF((x-a)/(b-a), 0, 1)
	return t * t * (3 - 2*t)
}

// textAt replica drawText del shader
func (e *Engine) textAt(col [3]float64, px, py float64, items []Item, st *Style) [3]float64 {
	ow := st.OutlineW
	gr := math.Max(st.GlowR, 0.5)
	for i := range items {
		it := &items[i]
		sc := float64(it.Scale)
		lx, ly := (px-float64(it.CX))/sc, (py-float64(it.CY))/sc
		hw, hh := float64(it.AW)/2, float64(it.AH)/2
		if math.Abs(lx) > hw || math.Abs(ly) > hh {
			continue
		}
		ax, ay := float64(it.AX)+hw+lx, float64(it.AY)+hh+ly
		pad := float64(it.Pad)
		alpha := float64(it.Alpha)
		if st.ShadowOp > 0 {
			ds := e.sdfAt(ax-st.ShadowDX/sc, ay-st.ShadowDY/sc, it)*sc - ow
			blur := math.Max(st.Blur, 0.75)
			sa := (1 - smoothstep(-blur, blur, ds)) * st.ShadowOp * alpha
			col = mix3(col, [3]float64{st.ShadowC[0], st.ShadowC[1], st.ShadowC[2]}, sa)
		}
		d := e.sdfAt(ax, ay, it) * sc
		if st.GlowI > 0 {
			g := math.Exp(-math.Max(d-ow, 0)/(gr*0.45)) * st.GlowI * alpha
			col = mix3(col, [3]float64{st.GlowC[0], st.GlowC[1], st.GlowC[2]}, clampF(g, 0, 1)*0.85)
		}
		if ow > 0 {
			col = mix3(col, [3]float64{st.Outline[0], st.Outline[1], st.Outline[2]}, clampF(0.5-(d-ow), 0, 1)*st.Outline[3]*alpha)
		}
		fillX := float64(it.AX) + pad + float64(it.Fill)*(float64(it.AW)-2*pad)
		ty := clampF((ay-float64(it.AY)-pad)/math.Max(float64(it.AH)-2*pad, 1), 0, 1)
		main := st.gradient(ty)
		k := clampF(fillX-ax+0.5, 0, 1)
		fc := mix3([3]float64{st.Upcoming[0], st.Upcoming[1], st.Upcoming[2]}, main, k)
		fa := st.Upcoming[3] + (st.Color[3]-st.Upcoming[3])*k
		col = mix3(col, fc, clampF(0.5-d, 0, 1)*fa*alpha)
	}
	return col
}

// DrawCPU compone la letra sobre la imagen (render por CPU), idéntico al shader
func (e *Engine) DrawCPU(img *image.RGBA, items []Item, st Style) {
	if len(items) == 0 {
		return
	}
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	// Caja que cubre todas las palabras (con escala, glow, sombra y aberración)
	x0, y0, x1, y1 := float64(W), float64(H), 0.0, 0.0
	for _, it := range items {
		hw, hh := float64(it.AW)*float64(it.Scale)/2, float64(it.AH)*float64(it.Scale)/2
		ex := st.Chroma + math.Abs(st.ShadowDX) + 2
		ey := math.Abs(st.ShadowDY) + 2
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
					if st.Chroma > 0 {
						c[0] = e.textAt(base, px-st.Chroma, py, items, &st)[0]
						c[1] = e.textAt(base, px, py, items, &st)[1]
						c[2] = e.textAt(base, px+st.Chroma, py, items, &st)[2]
					} else {
						c = e.textAt(base, px, py, items, &st)
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
