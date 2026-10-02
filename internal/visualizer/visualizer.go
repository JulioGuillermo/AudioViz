package visualizer

import (
	"fmt"
	"strings"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

// Visualizer is the common interface for rendering any type of spectrogram
type Visualizer interface {
	Draw(dc *gg.Context, feat *audio.FrameFeatures)
}

// NewVisualizer creates a visualizer instance for the specified type
func NewVisualizer(cfg *config.VisualizerConfig, width, height int) (Visualizer, error) {
	if !cfg.Enabled {
		return &noopVisualizer{}, nil
	}

	switch strings.ToLower(cfg.Type) {
	case "circular", "circle", "radial":
		return NewCircularVisualizer(cfg, width, height), nil
	case "horizontal", "linear", "bottom":
		return NewLinearVisualizer(cfg, width, height), nil
	case "vertical", "edges":
		return NewVerticalVisualizer(cfg, width, height), nil
	case "oscilloscope", "wave", "waveform":
		return NewWaveVisualizer(cfg, width, height), nil
	default:
		return nil, fmt.Errorf("unknown visualizer type: '%s'. Options: circular, horizontal, vertical, oscilloscope", cfg.Type)
	}
}

type noopVisualizer struct{}

func (n *noopVisualizer) Draw(dc *gg.Context, feat *audio.FrameFeatures) {}
