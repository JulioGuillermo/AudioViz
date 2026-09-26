package visualizer

import (
	"image/color"
	"math"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

type VerticalVisualizer struct {
	cfg    *config.VisualizerConfig
	width  int
	height  int
	pattern gg.Pattern
}

func NewVerticalVisualizer(cfg *config.VisualizerConfig, width, height int) *VerticalVisualizer {
	return &VerticalVisualizer{
		cfg:    cfg,
		width:  width,
		height: height,
	}
}

func (vv *VerticalVisualizer) Draw(dc *gg.Context, feat *audio.FrameFeatures) {
	if feat == nil {
		return
	}

	barCount := len(feat.LeftBars)
	if barCount == 0 {
		return
	}

	totalH := float64(vv.height) * 0.80
	startY := float64(vv.height)*0.10
	totalBarSpan := totalH / float64(barCount)
	barHeight := totalBarSpan * 0.70

	// Glow si está activo
	if vv.cfg.Glow.Enabled {
		glowPasses := 3
		for p := glowPasses; p >= 1; p-- {
			extraH := float64(p) * (vv.cfg.Glow.Radius / float64(glowPasses))
			alpha := (vv.cfg.Glow.Intensity / float64(glowPasses)) * 0.4
			vv.renderBars(dc, feat, startY, barHeight+extraH, totalBarSpan, alpha)
		}
	}

	vv.renderBars(dc, feat, startY, barHeight, totalBarSpan, 1.0)
}

func (vv *VerticalVisualizer) renderBars(dc *gg.Context, feat *audio.FrameFeatures, startY, barHeight, totalSpan, alpha float64) {
	barCount := len(feat.LeftBars)

	for i := 0; i < barCount; i++ {
		y := startY + float64(i)*totalSpan
		t := float64(i) / float64(barCount)
		col := vv.getColor(t, alpha)

		// Canal L: Borde izquierdo proyectándose hacia adentro (+X)
		magL := feat.LeftBars[i]
		lenL := vv.cfg.MinHeight + magL*(vv.cfg.MaxHeight-vv.cfg.MinHeight)
		vv.pattern = AlongPattern(vv.cfg, 0, y, 1, 0, lenL, alpha)
		vv.drawHBar(dc, 0, y, lenL, barHeight, col)

		// Canal R: Borde derecho proyectándose hacia adentro (-X)
		magR := feat.RightBars[i]
		lenR := vv.cfg.MinHeight + magR*(vv.cfg.MaxHeight-vv.cfg.MinHeight)
		vv.pattern = AlongPattern(vv.cfg, float64(vv.width), y, -1, 0, lenR, alpha)
		vv.drawHBar(dc, float64(vv.width)-lenR, y, lenR, barHeight, col)
	}
}

func (vv *VerticalVisualizer) drawHBar(dc *gg.Context, x, y, width, height float64, col color.RGBA) {
	dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
	if vv.pattern != nil {
		dc.SetFillStyle(vv.pattern)
	}
	r := vv.cfg.Roundness
	if r > height/2 {
		r = height / 2
	}
	if width < r*2 {
		r = width / 2
	}
	dc.DrawRoundedRectangle(x, y, math.Max(width, 2.0), height, r)
	dc.Fill()
}

func (vv *VerticalVisualizer) getColor(t, alpha float64) color.RGBA {
	var c color.RGBA
	if vv.cfg.ColorMode == "solid" {
		c, _ = ParseHexColor(vv.cfg.SolidColor)
	} else {
		c = ColorAtOffset(vv.cfg.Gradient, t)
	}
	c.A = uint8(float64(c.A) * alpha)
	return c
}
