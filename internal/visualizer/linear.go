package visualizer

import (
	"image/color"
	"math"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

type LinearVisualizer struct {
	cfg     *config.VisualizerConfig
	width   int
	height  int
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

	// If it doesn't fit in half the screen, readjust the width automatically
	cg := CenterGap(lv.cfg)
	if avail := halfW*0.95 - cg/2; float64(barCount)*totalBarSpan > avail {
		totalBarSpan = avail / float64(barCount)
		barWidth = totalBarSpan * 0.75
		barGap = totalBarSpan * 0.25
	}

	// Glow if enabled: each bar grown by a third of the reach more at each pass, where it is (as
	// glowAlpha in render.comp)
	if lv.cfg.Glow.Enabled {
		glowPasses := 3
		for p := glowPasses; p >= 1; p-- {
			grow := float64(p) * (lv.cfg.Glow.Radius / float64(glowPasses)) / 2
			alpha := (lv.cfg.Glow.Intensity / float64(glowPasses)) * 0.4
			lv.renderBars(dc, feat, halfW, barWidth, totalBarSpan, grow, alpha)
		}
	}

	// Sharp main bars
	lv.renderBars(dc, feat, halfW, barWidth, totalBarSpan, 0, 1.0)
}

// renderBars draws the bars barWidth wide every totalSpan px, grown by grow px all round (the
// glow's passes)
func (lv *LinearVisualizer) renderBars(dc *gg.Context, feat *audio.FrameFeatures, centerX, barWidth, totalSpan, grow, alpha float64) {
	barCount := len(feat.LeftBars)

	// Left channel (L): from the center to the left
	for i := 0; i < barCount; i++ {
		magL := feat.LeftBars[i]
		h := lv.cfg.MinHeight + magL*(lv.cfg.MaxHeight-lv.cfg.MinHeight)
		x := centerX - CenterGap(lv.cfg)/2 - (float64(i)*totalSpan + barWidth)

		t := float64(i) / float64(barCount)
		col := lv.getColor(t, alpha)

		lv.barPair(dc, x, barWidth, h, grow, col, alpha)
	}

	// Right channel (R): from the center to the right
	for i := 0; i < barCount; i++ {
		magR := feat.RightBars[i]
		h := lv.cfg.MinHeight + magR*(lv.cfg.MaxHeight-lv.cfg.MinHeight)
		x := centerX + CenterGap(lv.cfg)/2 + (float64(i) * totalSpan)

		t := float64(i) / float64(barCount)
		col := lv.getColor(t, alpha)

		lv.barPair(dc, x, barWidth, h, grow, col, alpha)
	}
}

// barPair: a bar upward from the base (and its reflection downward), grown by grow px all round
func (lv *LinearVisualizer) barPair(dc *gg.Context, x, barWidth, h, grow float64, col color.RGBA, alpha float64) {
	lv.pattern = AlongPattern(lv.cfg, x, lv.baseY, 0, -1, h, alpha)
	lv.drawBarGrown(dc, x, lv.baseY, barWidth, -h, grow, col)
	if lv.cfg.MirrorBottom {
		mirrorCol := col
		mirrorCol.A = uint8(float64(mirrorCol.A) * 0.35)
		lv.pattern = AlongPattern(lv.cfg, x, lv.baseY, 0, 0.5, h, alpha*0.35)
		lv.drawBarGrown(dc, x, lv.baseY, barWidth, h*0.5, grow, mirrorCol)
	}
}

// drawBarGrown: drawBar grown by g px all round (rounder by as much)
func (lv *LinearVisualizer) drawBarGrown(dc *gg.Context, x, y, width, height, g float64, col color.RGBA) {
	if g <= 0 {
		lv.drawBar(dc, x, y, width, height, col)
		return
	}
	r := math.Min(lv.cfg.Roundness, width/2)
	if math.Abs(height) < r*2 {
		r = math.Abs(height) / 2
	}
	top, bot := y, y+height
	if height < 0 {
		top, bot = y+height, y
	}
	dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
	if lv.pattern != nil {
		dc.SetFillStyle(lv.pattern)
	}
	dc.DrawRoundedRectangle(x-g, top-g, width+2*g, bot-top+2*g, r+g)
	dc.Fill()
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
