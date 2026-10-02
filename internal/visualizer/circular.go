package visualizer

import (
	"image/color"
	"math"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

type CircularVisualizer struct {
	cfg    *config.VisualizerConfig
	width  int
	height int
	cx     float64
	cy     float64
	radius float64
}

func NewCircularVisualizer(cfg *config.VisualizerConfig, width, height int) *CircularVisualizer {
	cx := float64(width) * cfg.CenterX
	cy := float64(height) * cfg.CenterY
	if cx <= 0 {
		cx = float64(width) / 2.0
	}
	if cy <= 0 {
		cy = float64(height) / 2.0
	}

	rad := cfg.Radius
	if rad <= 0 {
		minDim := math.Min(float64(width), float64(height))
		rad = minDim * 0.22
	}

	return &CircularVisualizer{
		cfg:    cfg,
		width:  width,
		height: height,
		cx:     cx,
		cy:     cy,
		radius: rad,
	}
}

func (cv *CircularVisualizer) Draw(dc *gg.Context, feat *audio.FrameFeatures) {
	if feat == nil {
		return
	}

	// The radius pulses slightly with the kick/bass
	baseRadius := cv.radius * (1.0 + feat.BassEnergy*0.08 + feat.BeatPulse*0.04)
	barCount := len(feat.LeftBars)
	if barCount == 0 {
		return
	}

	// Draw the central base ring with glow
	cv.drawCenterRing(dc, baseRadius, feat)

	// Draw stereo bars
	// Left channel: from -PI/2 (top) to +PI/2 (bottom) via the left (-PI)
	// Right channel: from -PI/2 (top) to +PI/2 (bottom) via the right (0)
	barWidth := cv.cfg.BarWidth
	if barWidth <= 0 {
		barWidth = 4.0
	}

	// Pass 1: outer glow if enabled
	if cv.cfg.Glow.Enabled {
		glowPasses := 3
		for p := glowPasses; p >= 1; p-- {
			extraWidth := float64(p) * (cv.cfg.Glow.Radius / float64(glowPasses))
			alpha := (cv.cfg.Glow.Intensity / float64(glowPasses)) * 0.4
			cv.renderBars(dc, feat, baseRadius, barWidth+extraWidth, alpha)
		}
	}

	// Pass 2: sharp main bars
	cv.renderBars(dc, feat, baseRadius, barWidth, 1.0)
}

func (cv *CircularVisualizer) renderBars(dc *gg.Context, feat *audio.FrameFeatures, baseRadius, barWidth, alpha float64) {
	barCount := len(feat.LeftBars)
	angleStep := math.Pi / float64(barCount)

	// Left channel (L): left semicircle
	for i := 0; i < barCount; i++ {
		magL := feat.LeftBars[i]
		h := cv.cfg.MinHeight + magL*(cv.cfg.MaxHeight-cv.cfg.MinHeight)

		// Angle: starts at the top pole (-PI/2) and turns left toward the bottom pole (+PI/2)
		angle := -math.Pi/2.0 - (float64(i)+0.5)*angleStep
		t := float64(i) / float64(barCount)

		col := cv.getColor(t, alpha)
		cv.drawRadialBar(dc, angle, baseRadius, h, barWidth, col)
	}

	// Right channel (R): right semicircle
	for i := 0; i < barCount; i++ {
		magR := feat.RightBars[i]
		h := cv.cfg.MinHeight + magR*(cv.cfg.MaxHeight-cv.cfg.MinHeight)

		// Angle: starts at the top pole (-PI/2) and turns right toward the bottom pole (+PI/2)
		angle := -math.Pi/2.0 + (float64(i)+0.5)*angleStep
		t := float64(i) / float64(barCount)

		col := cv.getColor(t, alpha)
		cv.drawRadialBar(dc, angle, baseRadius, h, barWidth, col)
	}
}

func (cv *CircularVisualizer) drawRadialBar(dc *gg.Context, angle, r0, length, width float64, col color.RGBA) {
	x0 := cv.cx + r0*math.Cos(angle)
	y0 := cv.cy + r0*math.Sin(angle)

	x1 := cv.cx + (r0+length)*math.Cos(angle)
	y1 := cv.cy + (r0+length)*math.Sin(angle)

	dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
	if pat := AlongPattern(cv.cfg, x0, y0, math.Cos(angle), math.Sin(angle), length, float64(col.A)/255); pat != nil {
		dc.SetStrokeStyle(pat)
	}
	dc.SetLineWidth(width)
	dc.SetLineCapRound()
	dc.DrawLine(x0, y0, x1, y1)
	dc.Stroke()
}

func (cv *CircularVisualizer) drawCenterRing(dc *gg.Context, radius float64, feat *audio.FrameFeatures) {
	// Subtle inner ring marking the base of the visualizer
	dc.Push()
	ringCol := cv.getColor(0.5, 0.4+feat.BassEnergy*0.3)
	dc.SetRGBA255(int(ringCol.R), int(ringCol.G), int(ringCol.B), int(ringCol.A))
	dc.SetLineWidth(2.0)
	dc.DrawCircle(cv.cx, cv.cy, radius-2.0)
	dc.Stroke()
	dc.Pop()
}

func (cv *CircularVisualizer) getColor(t, alpha float64) color.RGBA {
	var c color.RGBA
	if cv.cfg.ColorMode == "solid" {
		c, _ = ParseHexColor(cv.cfg.SolidColor)
	} else {
		c = ColorAtOffset(cv.cfg.Gradient, t)
	}
	c.A = uint8(float64(c.A) * alpha)
	return c
}
