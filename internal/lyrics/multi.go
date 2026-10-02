package lyrics

import (
	"image"

	"audioviz/internal/audio"
	"audioviz/internal/config"
)

// Multi combines several lyrics styles across the song ("lyrics" track of the timeline).
// Each line uses the layout (font, position, animations) of the segment it starts in;
// colors and audio reaction follow the segment active at each instant.
type Multi struct {
	Atlas Atlas
	subs  []*Engine
	offs  []int // vertical offset of each sub-atlas within the combined atlas
	draw  *Engine
}

// NewMulti loads the lyrics (path and offset of the first segment) and distributes them among the segments
func NewMulti(segs []config.Seg[config.LyricsConfig], w, h int) (*Multi, error) {
	base := &segs[0].Cfg
	lines, err := Load(base.Path, base.Offset)
	if err != nil {
		return nil, err
	}
	groups := make([][]Line, len(segs))
	for _, l := range lines {
		i := config.Active(segs, l.Start)
		groups[i] = append(groups[i], l)
	}
	m := &Multi{}
	y := 0
	for i := range segs {
		if !segs[i].Cfg.Enabled || len(groups[i]) == 0 {
			continue
		}
		e, err := newEngine(&segs[i].Cfg, groups[i], w, h)
		if err != nil {
			return nil, err
		}
		m.subs = append(m.subs, e)
		m.offs = append(m.offs, y)
		y += e.Atlas.H
	}
	m.Atlas = Atlas{W: 2048, H: max(y, 1)}
	m.Atlas.Pix = make([]byte, m.Atlas.W*m.Atlas.H)
	for i, e := range m.subs {
		copy(m.Atlas.Pix[m.offs[i]*m.Atlas.W:], e.Atlas.Pix)
	}
	m.draw = &Engine{Atlas: m.Atlas}
	return m, nil
}

// Frame returns the words visible at t (combined atlas coordinates)
func (m *Multi) Frame(t float64, feat *audio.FrameFeatures) []Item {
	var items []Item
	for i, e := range m.subs {
		for _, it := range e.Frame(t, feat) {
			it.AY += float32(m.offs[i])
			items = append(items, it)
		}
	}
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return items
}

// DrawCPU composites the lyrics onto the image (CPU render)
func (m *Multi) DrawCPU(img *image.RGBA, items []Item, st Style) { m.draw.DrawCPU(img, items, st) }
