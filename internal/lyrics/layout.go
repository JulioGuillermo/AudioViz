package lyrics

import (
	"fmt"
	"image"
	"math"
	"os"
	"strings"

	"audioviz/internal/audio"
	"audioviz/internal/config"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// SDFScale: niveles de cuantización por píxel del campo de distancias (byte = 128 - d*SDFScale)
const SDFScale = 4.0

// Atlas contiene el campo de distancias con signo (SDF) de todas las palabras, 1 byte por píxel
type Atlas struct {
	W, H int
	Pix  []byte
}

type glyphWord struct {
	ax, ay, aw, ah int     // rectángulo en el atlas (incluye margen)
	pad            int     // margen alrededor del texto
	adv            float64 // avance horizontal del texto
	offX, offY     float64 // centro de la palabra relativo al centro de su línea (px)
	start, end     float64
}

type layLine struct {
	start, end float64
	words      []glyphWord
	side       int     // -1 izquierda, 0 centro, 1 derecha
	halfH      float64 // media altura del bloque
}

// Item es una palabra visible en un cuadro, lista para dibujar
type Item struct {
	AX, AY, AW, AH float32 // rectángulo en el atlas
	CX, CY         float32 // centro en pantalla
	Scale, Alpha   float32
	Fill           float32 // progreso karaoke 0..1 (1 = todo del color principal)
	Pad            float32
}

type Engine struct {
	cfg   *config.LyricsConfig
	Atlas Atlas
	lines []layLine
	w, h  int
	Pad   int
	maxW  float64
}

func loadFace(cfg *config.LyricsConfig) (font.Face, error) {
	data := gobold.TTF
	if cfg.Font != "" {
		b, err := os.ReadFile(cfg.Font)
		if err != nil {
			return nil, fmt.Errorf("no se pudo leer la fuente '%s': %w", cfg.Font, err)
		}
		data = b
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("fuente inválida: %w", err)
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: cfg.Size, DPI: 72, Hinting: font.HintingNone})
}

// New carga la letra, maqueta las líneas (con saltos de fila) y construye el atlas SDF
func New(cfg *config.LyricsConfig, w, h int) (*Engine, error) {
	lines, err := Load(cfg.Path, cfg.Offset)
	if err != nil {
		return nil, err
	}
	face, err := loadFace(cfg)
	if err != nil {
		return nil, err
	}
	defer face.Close()

	// Margen suficiente para contorno, glow y sombra
	pad := int(math.Ceil(math.Max(cfg.OutlineWidth+cfg.Glow.Radius, cfg.Shadow.Blur+cfg.OutlineWidth) + 4))
	pad = min(max(pad, 6), 30)
	e := &Engine{cfg: cfg, w: w, h: h, Pad: pad}

	m := face.Metrics()
	ascent := float64(m.Ascent) / 64
	descent := float64(m.Descent) / 64
	boxH := int(math.Ceil(ascent+descent)) + 2*pad
	spaceAdv := float64(font.MeasureString(face, " "))/64 + cfg.WordSpacing*cfg.Size
	lineH := (ascent + descent) * max(cfg.LineSpacing, 0.5)
	maxW := float64(w) * cfg.MaxWidth
	if maxW <= 0 {
		maxW = float64(w) * 0.9
	}

	// Rasterizar cada palabra y empaquetarla en el atlas (estanterías)
	const atlasW = 2048
	type bmp struct {
		sdf    []byte
		bw, bh int
	}
	var bmps []bmp
	shelfX, shelfY, shelfH := 0, 0, 0

	for li, ln := range lines {
		ll := layLine{start: ln.Start, end: ln.End}
		switch cfg.Position {
		case "left":
			ll.side = -1
		case "right":
			ll.side = 1
		case "side":
			ll.side = -1
			if li%2 == 1 {
				ll.side = 1
			}
		}
		for _, wd := range ln.Words {
			text := wd.Text
			if cfg.Uppercase {
				text = strings.ToUpper(text)
			}
			adv := float64(font.MeasureString(face, text)) / 64
			bw := int(math.Ceil(adv)) + 2*pad
			bw = min(bw, atlasW)
			cov := image.NewAlpha(image.Rect(0, 0, bw, boxH))
			d := &font.Drawer{Dst: cov, Src: image.Opaque, Face: face,
				Dot: fixed.Point26_6{X: fixed.I(pad), Y: fixed.Int26_6((float64(pad) + ascent) * 64)}}
			d.DrawString(text)
			sdf := buildSDF(cov, float64(pad))

			if shelfX+bw > atlasW {
				shelfX, shelfY, shelfH = 0, shelfY+shelfH, 0
			}
			g := glyphWord{ax: shelfX, ay: shelfY, aw: bw, ah: boxH, pad: pad, adv: adv, start: wd.Start, end: wd.End}
			shelfX += bw
			shelfH = max(shelfH, boxH)
			bmps = append(bmps, bmp{sdf, bw, boxH})
			ll.words = append(ll.words, g)
		}
		align := cfg.Align
		if ll.side < 0 {
			align = "left"
		} else if ll.side > 0 {
			align = "right"
		}
		ll.halfH = layoutRows(&ll, spaceAdv, maxW, lineH, align)
		e.maxW = maxW
		e.lines = append(e.lines, ll)
	}

	// La línea anterior sale mientras entra la siguiente (no se pisan)
	inD := math.Max(cfg.InDuration, 0.01)
	for i := 0; i+1 < len(e.lines); i++ {
		cur, next := &e.lines[i], &e.lines[i+1]
		if cur.end > next.start-inD {
			cur.end = math.Max(next.start-inD, cur.start+0.2)
		}
	}

	e.Atlas = Atlas{W: atlasW, H: max(shelfY+shelfH, 1)}
	e.Atlas.Pix = make([]byte, e.Atlas.W*e.Atlas.H)
	for i := range e.Atlas.Pix {
		e.Atlas.Pix[i] = 0 // lejos del texto
	}
	k := 0
	for _, ll := range e.lines {
		for _, g := range ll.words {
			b := bmps[k]
			k++
			for y := 0; y < b.bh; y++ {
				copy(e.Atlas.Pix[(g.ay+y)*e.Atlas.W+g.ax:], b.sdf[y*b.bw:(y+1)*b.bw])
			}
		}
	}
	return e, nil
}

// layoutRows reparte las palabras en filas que caben en maxW y calcula su posición relativa al centro
func layoutRows(ll *layLine, space, maxW, lineH float64, align string) float64 {
	var rows [][]int
	var cur []int
	width := 0.0
	for i, g := range ll.words {
		add := g.adv
		if len(cur) > 0 {
			add += space
		}
		if len(cur) > 0 && width+add > maxW {
			rows = append(rows, cur)
			cur, width, add = nil, 0, g.adv
		}
		cur = append(cur, i)
		width += add
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	totalH := lineH * float64(len(rows))
	for r, idx := range rows {
		rowW := 0.0
		for k, i := range idx {
			if k > 0 {
				rowW += space
			}
			rowW += ll.words[i].adv
		}
		x := -rowW / 2
		switch align {
		case "left":
			x = -maxW / 2
		case "right":
			x = maxW/2 - rowW
		}
		y := -totalH/2 + lineH*(float64(r)+0.5)
		for _, i := range idx {
			g := &ll.words[i]
			g.offX = x + g.adv/2
			g.offY = y
			x += g.adv + space
		}
	}
	return totalH / 2
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func easeOut(t float64) float64 { t = clamp01(t); return 1 - (1-t)*(1-t)*(1-t) }

// anchor devuelve el centro en pantalla del bloque de una línea según la posición configurada
func (e *Engine) anchor(ll *layLine) (float64, float64) {
	cfg := e.cfg
	W, H := float64(e.w), float64(e.h)
	m := cfg.Margin
	cy := H * cfg.PositionY
	if cfg.PositionY <= 0 {
		cy = H / 2
	}
	switch cfg.Position {
	case "top":
		return W / 2, m + ll.halfH
	case "bottom":
		return W / 2, H - m - ll.halfH
	case "center":
		return W / 2, H / 2
	case "left", "right", "side":
		if ll.side < 0 {
			return m + e.maxW/2, cy
		}
		return W - m - e.maxW/2, cy
	default: // custom
		return W * cfg.PositionX, cy
	}
}

// anim devuelve desplazamiento, escala y alfa de una animación de entrada/salida.
// p = 0 oculto, 1 totalmente visible.
func (e *Engine) anim(name string, p float64, side int, out bool) (dx, dy, scale, alpha float64) {
	d := e.cfg.SlideDistance
	q := 1 - p
	scale, alpha = 1, p
	switch name {
	case "none":
		alpha = 0
		if p >= 1 {
			alpha = 1
		}
	case "slide_up": // entra subiendo desde abajo / sale hacia arriba
		dy = d * q
		if out {
			dy = -d * q
		}
	case "slide_down":
		dy = -d * q
		if out {
			dy = d * q
		}
	case "slide_left": // se mueve hacia la izquierda
		dx = d * q
		if out {
			dx = -d * q
		}
	case "slide_right":
		dx = -d * q
		if out {
			dx = d * q
		}
	case "slide_side": // entra y sale por su propio lado
		s := float64(side)
		if s == 0 {
			dy = d * q
		} else {
			dx = s * d * 2 * q
		}
	case "pop": // crece desde pequeño; al salir se encoge
		scale = 0.5 + 0.5*p
	case "zoom": // llega desde grande; al salir se agranda
		scale = 1 + 0.6*q
	}
	return
}

// Frame calcula las palabras visibles en el instante t con sus animaciones
func (e *Engine) Frame(t float64, feat *audio.FrameFeatures) []Item {
	cfg := e.cfg
	band := 0.0
	if feat != nil {
		band = math.Min(feat.Band(cfg.React.ReactTo, "bass"), 1.2)
	}
	var sx, sy float64
	if cfg.React.Shake > 0 {
		a := cfg.React.Shake * band
		sx = a * (0.6*math.Sin(t*23.1) + 0.4*math.Sin(t*37.7+1.3))
		sy = a * (0.6*math.Sin(t*19.3+0.7) + 0.4*math.Sin(t*41.9+2.1))
	}
	beatScale := 1 + cfg.React.Scale*band
	inName, outName := cfg.InAnimation, cfg.OutAnimation
	if inName == "" {
		inName = cfg.Animation
	}
	if outName == "" {
		outName = cfg.Animation
	}

	inD := math.Max(cfg.InDuration, 0.01)
	outD := math.Max(cfg.OutDuration, 0.01)
	var items []Item
	for li := range e.lines {
		ll := &e.lines[li]
		if t < ll.start-inD || t > ll.end+outD {
			continue
		}
		dx, dy, lscale, alpha := 0.0, 0.0, 1.0, 1.0
		if t < ll.start {
			dx, dy, lscale, alpha = e.anim(inName, easeOut((t-(ll.start-inD))/inD), ll.side, false)
		} else if t > ll.end {
			dx, dy, lscale, alpha = e.anim(outName, 1-easeOut((t-ll.end)/outD), ll.side, true)
		}
		if alpha <= 0.001 {
			continue
		}
		cx, cy := e.anchor(ll)
		cx += sx + dx
		cy += sy + dy
		ls := lscale * beatScale
		for _, g := range ll.words {
			a, s, fill := alpha, ls, 1.0
			wordDur := math.Max(g.end-g.start, 0.05)
			switch cfg.Mode {
			case "words":
				// Cada palabra aparece cuando se canta
				p := (t - g.start) / math.Min(inD, 0.25)
				if p <= 0 {
					continue
				}
				a *= easeOut(p)
				s *= 1 + cfg.WordPop*(1-easeOut(p))
			case "line":
			default: // karaoke
				fill = clamp01((t - g.start) / wordDur)
			}
			if cfg.Mode != "words" && cfg.WordPop > 0 && t >= g.start {
				// Latido de la palabra al empezar a cantarse
				if k := (t - g.start) / 0.3; k < 1 {
					s *= 1 + cfg.WordPop*math.Sin(k*math.Pi)
				}
			}
			if len(items) == maxItems {
				return items
			}
			items = append(items, Item{
				AX: float32(g.ax), AY: float32(g.ay), AW: float32(g.aw), AH: float32(g.ah),
				CX: float32(cx + g.offX*ls), CY: float32(cy + g.offY*ls),
				Scale: float32(s), Alpha: float32(a), Fill: float32(fill), Pad: float32(g.pad),
			})
		}
	}
	return items
}

// Band devuelve el valor de la banda a la que reacciona la letra (para flash / parpadeo)
func (e *Engine) Band(feat *audio.FrameFeatures) float64 {
	if feat == nil {
		return 0
	}
	return math.Min(feat.Band(e.cfg.React.ReactTo, "bass"), 1)
}

const maxItems = 96

// buildSDF calcula la distancia con signo (px, + fuera) a partir de la cobertura del texto
func buildSDF(cov *image.Alpha, maxD float64) []byte {
	w, h := cov.Rect.Dx(), cov.Rect.Dy()
	inside := make([]float64, w*h)
	outside := make([]float64, w*h)
	const inf = 1e20
	for i, a := range cov.Pix {
		if a >= 128 {
			inside[i], outside[i] = inf, 0
		} else {
			inside[i], outside[i] = 0, inf
		}
	}
	edt2D(outside, w, h) // distancia² al píxel de texto más cercano
	edt2D(inside, w, h)  // distancia² al fondo más cercano
	out := make([]byte, w*h)
	for i := range out {
		var d float64
		a := float64(cov.Pix[i]) / 255
		if a > 0 && a < 1 {
			d = 0.5 - a // borde antialias: distancia subpíxel
		} else if outside[i] > 0 {
			d = math.Sqrt(outside[i]) - 0.5
		} else {
			d = -(math.Sqrt(inside[i]) - 0.5)
		}
		d = math.Max(-maxD, math.Min(maxD, d))
		out[i] = byte(math.Max(0, math.Min(255, math.Round(128-d*SDFScale))))
	}
	return out
}

// edt2D: transformada de distancia euclídea exacta (Felzenszwalb & Huttenlocher) in-place
func edt2D(g []float64, w, h int) {
	n := max(w, h)
	f := make([]float64, n)
	d := make([]float64, n)
	v := make([]int, n)
	z := make([]float64, n+1)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			f[y] = g[y*w+x]
		}
		edt1D(f[:h], d[:h], v, z)
		for y := 0; y < h; y++ {
			g[y*w+x] = d[y]
		}
	}
	for y := 0; y < h; y++ {
		copy(f[:w], g[y*w:(y+1)*w])
		edt1D(f[:w], d[:w], v, z)
		copy(g[y*w:(y+1)*w], d[:w])
	}
}

func edt1D(f, d []float64, v []int, z []float64) {
	n := len(f)
	k := 0
	v[0] = 0
	z[0], z[1] = math.Inf(-1), math.Inf(1)
	for q := 1; q < n; q++ {
		var s float64
		for {
			p := v[k]
			s = ((f[q] + float64(q*q)) - (f[p] + float64(p*p))) / float64(2*q-2*p)
			if s <= z[k] && k > 0 {
				k--
				continue
			}
			break
		}
		if s <= z[k] {
			// k == 0 y la nueva parábola domina desde -inf
			v[0] = q
			z[0], z[1] = math.Inf(-1), math.Inf(1)
			continue
		}
		k++
		v[k] = q
		z[k] = s
		z[k+1] = math.Inf(1)
	}
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float64(q) {
			k++
		}
		p := v[k]
		d[q] = float64((q-p)*(q-p)) + f[p]
	}
}
