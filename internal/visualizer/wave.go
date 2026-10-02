package visualizer

import (
	"image/color"
	"math"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

type WaveVisualizer struct {
	cfg    *config.VisualizerConfig
	width  int
	height int
	baseY  float64
}

func NewWaveVisualizer(cfg *config.VisualizerConfig, width, height int) *WaveVisualizer {
	posY := cfg.PositionY
	if posY <= 0 {
		posY = 0.5
	}
	return &WaveVisualizer{
		cfg:    cfg,
		width:  width,
		height: height,
		baseY:  float64(height) * posY,
	}
}

func (wv *WaveVisualizer) Draw(dc *gg.Context, feat *audio.FrameFeatures) {
	if feat == nil || len(feat.LeftBars) == 0 {
		return
	}

	colL := color.RGBA{R: 0, G: 240, B: 255, A: 255} // Cyan for L
	colR := color.RGBA{R: 255, G: 0, B: 127, A: 255} // Magenta for R

	if len(wv.cfg.Gradient) >= 2 {
		colL, _ = ParseHexColor(wv.cfg.Gradient[0].Color)
		colR, _ = ParseHexColor(wv.cfg.Gradient[len(wv.cfg.Gradient)-1].Color)
	}

	lineWidth := wv.cfg.BarWidth
	if lineWidth <= 0 {
		lineWidth = 3.0
	}

	// Glow if enabled
	if wv.cfg.Glow.Enabled {
		glowPasses := 3
		for p := glowPasses; p >= 1; p-- {
			extraW := float64(p) * (wv.cfg.Glow.Radius / float64(glowPasses))
			alpha := (wv.cfg.Glow.Intensity / float64(glowPasses)) * 0.4
			wv.drawWaveLine(dc, feat.LeftBars, -1.0, lineWidth+extraW, colL, alpha)
			wv.drawWaveLine(dc, feat.RightBars, 1.0, lineWidth+extraW, colR, alpha)
		}
	}

	// Main lines
	wv.drawWaveLine(dc, feat.LeftBars, -1.0, lineWidth, colL, 1.0)
	wv.drawWaveLine(dc, feat.RightBars, 1.0, lineWidth, colR, 1.0)
}

func (wv *WaveVisualizer) drawWaveLine(dc *gg.Context, bars []float64, dir float64, width float64, col color.RGBA, alpha float64) {
	n := len(bars)
	if n < 2 {
		return
	}

	dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(float64(col.A)*alpha))
	dc.SetLineWidth(width)

	stepX := float64(wv.width) / float64(n-1)
	amp := wv.cfg.MaxHeight

	dc.MoveTo(0, wv.baseY)
	for i := 0; i < n; i++ {
		x := float64(i) * stepX
		// Modulated amplitude
		y := wv.baseY + dir*bars[i]*amp*math.Sin(float64(i)*0.4)
		dc.LineTo(x, y)
	}
	dc.Stroke()
}
