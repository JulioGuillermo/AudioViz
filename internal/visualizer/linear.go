package visualizer

import (
	"image/color"
	"math"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

type LinearVisualizer struct {
	cfg    *config.VisualizerConfig
	width  int
	height int
	baseY   float64
	pattern gg.Pattern
}

func NewLinearVisualizer(cfg *config.VisualizerConfig, width, height int) *LinearVisualizer {
	posY := cfg.PositionY
	if posY <= 0 {
		posY = 0.85
	}
	baseY := float64(height) * posY

	return &LinearVisualizer{
		cfg:    cfg,
		width:  width,
		height: height,
		baseY:  baseY,
	}
}

func (lv *LinearVisualizer) Draw(dc *gg.Context, feat *audio.FrameFeatures) {
	if feat == nil {
		return
	}

	barCount := len(feat.LeftBars)
	if barCount == 0 {
		return
	}

	halfW := float64(lv.width) / 2.0
	barWidth := lv.cfg.BarWidth
	barGap := lv.cfg.BarGap
	totalBarSpan := barWidth + barGap

	// Si no cabe en media pantalla, reajustar ancho automáticamente
	cg := CenterGap(lv.cfg)
	if avail := halfW*0.95 - cg/2; float64(barCount)*totalBarSpan > avail {
		totalBarSpan = avail / float64(barCount)
		barWidth = totalBarSpan * 0.75
		barGap = totalBarSpan * 0.25
	}

	// Glow si está activo
	if lv.cfg.Glow.Enabled {
		glowPasses := 3
		for p := glowPasses; p >= 1; p-- {
			extraWidth := float64(p) * (lv.cfg.Glow.Radius / float64(glowPasses))
			alpha := (lv.cfg.Glow.Intensity / float64(glowPasses)) * 0.4
			lv.renderBars(dc, feat, halfW, barWidth+extraWidth, barGap, alpha)
		}
	}

	// Barras nítidas principales
	lv.renderBars(dc, feat, halfW, barWidth, barGap, 1.0)
}

func (lv *LinearVisualizer) renderBars(dc *gg.Context, feat *audio.FrameFeatures, centerX, barWidth, barGap, alpha float64) {
	barCount := len(feat.LeftBars)
	totalSpan := barWidth + barGap

	// Canal Izquierdo (L): desde el centro hacia la izquierda
	for i := 0; i < barCount; i++ {
		magL := feat.LeftBars[i]
		h := lv.cfg.MinHeight + magL*(lv.cfg.MaxHeight-lv.cfg.MinHeight)
		x := centerX - CenterGap(lv.cfg)/2 - (float64(i)*totalSpan + barWidth)

		t := float64(i) / float64(barCount)
		col := lv.getColor(t, alpha)

		// Barra hacia arriba
		lv.pattern = AlongPattern(lv.cfg, x, lv.baseY, 0, -1, h, alpha)
		lv.drawBar(dc, x, lv.baseY, barWidth, -h, col)

		// Reflejo hacia abajo si está activo
		if lv.cfg.MirrorBottom {
			mirrorCol := col
			mirrorCol.A = uint8(float64(mirrorCol.A) * 0.35)
			lv.pattern = AlongPattern(lv.cfg, x, lv.baseY, 0, 0.5, h, alpha*0.35)
			lv.drawBar(dc, x, lv.baseY, barWidth, h*0.5, mirrorCol)
		}
	}

	// Canal Derecho (R): desde el centro hacia la derecha
	for i := 0; i < barCount; i++ {
		magR := feat.RightBars[i]
		h := lv.cfg.MinHeight + magR*(lv.cfg.MaxHeight-lv.cfg.MinHeight)
		x := centerX + CenterGap(lv.cfg)/2 + (float64(i) * totalSpan)

		t := float64(i) / float64(barCount)
		col := lv.getColor(t, alpha)

		// Barra hacia arriba
		lv.pattern = AlongPattern(lv.cfg, x, lv.baseY, 0, -1, h, alpha)
		lv.drawBar(dc, x, lv.baseY, barWidth, -h, col)

		// Reflejo hacia abajo si está activo
		if lv.cfg.MirrorBottom {
			mirrorCol := col
			mirrorCol.A = uint8(float64(mirrorCol.A) * 0.35)
			lv.pattern = AlongPattern(lv.cfg, x, lv.baseY, 0, 0.5, h, alpha*0.35)
			lv.drawBar(dc, x, lv.baseY, barWidth, h*0.5, mirrorCol)
		}
	}
}

func (lv *LinearVisualizer) drawBar(dc *gg.Context, x, y, width, height float64, col color.RGBA) {
	dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
	if lv.pattern != nil {
		dc.SetFillStyle(lv.pattern)
	}
	r := lv.cfg.Roundness
	if r > width/2 {
		r = width / 2
	}
	if math.Abs(height) < r*2 {
		r = math.Abs(height) / 2
	}

	if height < 0 {
		dc.DrawRoundedRectangle(x, y+height, width, -height, r)
	} else {
		dc.DrawRoundedRectangle(x, y, width, height, r)
	}
	dc.Fill()
}

func (lv *LinearVisualizer) getColor(t, alpha float64) color.RGBA {
	var c color.RGBA
	if lv.cfg.ColorMode == "solid" {
		c, _ = ParseHexColor(lv.cfg.SolidColor)
	} else {
		c = ColorAtOffset(lv.cfg.Gradient, t)
	}
	c.A = uint8(float64(c.A) * alpha)
	return c
}
