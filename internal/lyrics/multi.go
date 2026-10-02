package lyrics

import (
	"image"

	"audioviz/internal/audio"
	"audioviz/internal/config"
)

// Multi reúne varios estilos de letra a lo largo de la canción (pista "lyrics" de la línea de tiempo).
// Cada línea usa la maquetación (fuente, posición, animaciones) del tramo en el que empieza;
// los colores y la reacción al audio siguen al tramo activo en cada instante.
type Multi struct {
	Atlas Atlas
	subs  []*Engine
	offs  []int // desplazamiento vertical de cada sub-atlas dentro del atlas combinado
	draw  *Engine
}

// NewMulti carga la letra (ruta y desfase del primer tramo) y la reparte entre los tramos
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

// Frame devuelve las palabras visibles en t (coordenadas del atlas combinado)
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

// DrawCPU compone la letra sobre la imagen (render por CPU)
func (m *Multi) DrawCPU(img *image.RGBA, items []Item, st Style) { m.draw.DrawCPU(img, items, st) }
