package lyrics

import (
	"image"
	"math"
	"strings"

	"audioviz/internal/audio"
	"audioviz/internal/config"
)

// MaxStyles is how many different text styles a frame can show at once (lyrics + fixed texts);
// it matches the shader's style table.
const MaxStyles = 8

// Multi combines everything drawn with the text engine: the lyrics, in several styles across the
// song ("lyrics" track of the timeline), and the fixed texts (titles). Each lyric line uses the
// layout (font, position, animations) of the segment it starts in; colors and audio reaction
// follow the segment active at each instant. Each fixed text has its own engine and style.
type Multi struct {
	Atlas Atlas
	subs  []*Engine
	offs  []int                // vertical offset of each sub-atlas within the combined atlas
	nLyr  int                  // subs[:nLyr] are lyrics segments, subs[nLyr:] fixed texts
	texts []*config.TextConfig // the fixed text of each subs[nLyr+i] (its keyframes)
	draw  *Engine
	w, h  int
}

// NewMulti builds the lyrics (path and offset of the first segment; skipped when lyrics are off
// or have no file) and the fixed texts. Returns nil when there is nothing to draw.
func NewMulti(segs []config.Seg[config.LyricsConfig], texts []config.TextConfig, w, h int) (*Multi, error) {
	m := &Multi{w: w, h: h}
	var engines []*Engine
	anyOn := false
	for _, s := range segs {
		anyOn = anyOn || s.Cfg.Enabled
	}
	if anyOn && segs[0].Cfg.Path != "" {
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
		for i := range segs {
			if !segs[i].Cfg.Enabled || len(groups[i]) == 0 {
				continue
			}
			e, err := newEngine(&segs[i].Cfg, groups[i], w, h)
			if err != nil {
				return nil, err
			}
			engines = append(engines, e)
		}
	}
	m.nLyr = len(engines)
	for i := range texts {
		texts[i].Keys.Sort()
		e, err := textEngine(&texts[i], w, h)
		if err != nil {
			return nil, err
		}
		if e != nil {
			engines = append(engines, e)
			m.texts = append(m.texts, &texts[i])
		}
	}
	if len(engines) == 0 {
		return nil, nil
	}
	y := 0
	for _, e := range engines {
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

// textEngine lays out a fixed text as a single line from Start to End at its own position.
// Its in animation starts at Start and its out animation ends at End.
func textEngine(tc *config.TextConfig, w, h int) (*Engine, error) {
	if !tc.Enabled || strings.TrimSpace(tc.Text) == "" || tc.End <= tc.Start {
		return nil, nil
	}
	cfg := tc.LyricsConfig // own copy: the engine keeps a pointer
	cfg.Position = "custom"
	cfg.Mode = "line"
	inD, outD := math.Max(cfg.InDuration, 0.01), math.Max(cfg.OutDuration, 0.01)
	inName, outName := cfg.InAnimation, cfg.OutAnimation
	if inName == "" {
		inName = cfg.Animation
	}
	if outName == "" {
		outName = cfg.Animation
	}
	if inName == "none" { // appears at Start exactly
		inD = 0
	}
	if outName == "none" {
		outD = 0
	}
	start, end := tc.Start+inD, tc.End-outD
	if end < start { // shorter than its animations: split the time between them
		mid := (tc.Start + tc.End) / 2
		start, end = mid, mid
	}
	ln := Line{Start: start, End: end, WordTimed: true}
	for r, row := range strings.Split(strings.ReplaceAll(tc.Text, "\r\n", "\n"), "\n") {
		if r > 0 {
			ln.Words = append(ln.Words, Word{Text: BreakWord})
		}
		for _, wd := range strings.Fields(row) {
			ln.Words = append(ln.Words, Word{Text: wd, Start: start, End: end})
		}
	}
	return buildEngine(&cfg, []Line{ln}, w, h, true)
}

// Frame returns the words visible at t (combined atlas coordinates) and the style table they
// index: styles[0] is the lyrics style (from lyr, the lyrics config lerped at t; may be nil
// when there are no lyrics), then one entry per fixed text on screen (at most MaxStyles in all).
func (m *Multi) Frame(t float64, feat *audio.FrameFeatures, lyr *config.LyricsConfig) ([]Item, []Style) {
	var items []Item
	styles := []Style{{Text: -1}}
	if lyr != nil && m.nLyr > 0 {
		styles[0] = StyleFor(lyr, t, feat)
		styles[0].Z = lyr.ZIndex
		styles[0].Text = -1
	}
	for i, e := range m.subs {
		its := e.Frame(t, feat)
		if len(its) == 0 {
			continue
		}
		si := 0
		if i >= m.nLyr {
			if len(styles) == MaxStyles {
				continue // too many texts at once: the extra ones are not drawn
			}
			si = len(styles)
			cfg := e.cfg
			tc := m.texts[i-m.nLyr]
			var dx, dy, sc float32 = 0, 0, 1
			rot := tc.Rotation
			if len(tc.Keys) > 0 {
				// Animated text: style from its properties now; moved by the change of its
				// position, scaled and turned around it by the change of its size and rotation
				k := config.ApplyKeys(tc, tc.Keys, t-tc.Start)
				kc := k.LyricsConfig
				kc.Position, kc.Mode = cfg.Position, cfg.Mode
				cfg = &kc
				dx = float32((k.PositionX - tc.PositionX) * float64(m.w))
				dy = float32((k.PositionY - tc.PositionY) * float64(m.h))
				if tc.Size > 0 && k.Size > 0 {
					sc = float32(k.Size / tc.Size)
				}
				rot = k.Rotation
			}
			st := StyleFor(cfg, t, feat)
			st.Z = cfg.ZIndex
			ax, ay := float32(tc.PositionX*float64(m.w)), float32(tc.PositionY*float64(m.h))
			r := rot * math.Pi / 180
			st.Text, st.AnchorX, st.AnchorY, st.Rotation = i-m.nLyr, ax+dx, ay+dy, float32(r)
			styles = append(styles, st)
			if dx != 0 || dy != 0 || sc != 1 || rot != 0 {
				rc, rs := float32(math.Cos(r)), float32(math.Sin(r))
				for j := range its {
					ox, oy := (its[j].CX-ax)*sc, (its[j].CY-ay)*sc
					its[j].CX = ax + dx + ox*rc - oy*rs
					its[j].CY = ay + dy + ox*rs + oy*rc
					its[j].Scale *= sc
					its[j].Rot = float32(r)
				}
			}
		}
		for _, it := range its {
			it.AY += float32(m.offs[i])
			it.Style = si
			items = append(items, it)
		}
	}
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return items, styles
}

// TextBox is where a fixed text is on screen: its anchor (px), its rotation (rad) and the box of
// its letters around the anchor, before the rotation (px)
type TextBox struct {
	Text           int     `json:"i"`
	AX             float32 `json:"ax"`
	AY             float32 `json:"ay"`
	Rot            float32 `json:"rot"`
	X0, Y0, X1, Y1 float32
}

// Boxes are the fixed texts on screen in a frame (from Frame's items and styles)
func Boxes(items []Item, styles []Style) []TextBox {
	var bs []TextBox
	at := map[int]int{}
	for _, it := range items {
		st := &styles[it.Style]
		if st.Text < 0 {
			continue
		}
		c, s := math.Cos(float64(st.Rotation)), math.Sin(float64(st.Rotation))
		ox, oy := float64(it.CX-st.AnchorX), float64(it.CY-st.AnchorY)
		lx, ly := float32(ox*c+oy*s), float32(-ox*s+oy*c)
		hw, hh := (it.AW/2-it.Pad)*it.Scale, (it.AH/2-it.Pad)*it.Scale
		k, ok := at[it.Style]
		if !ok {
			k = len(bs)
			at[it.Style] = k
			bs = append(bs, TextBox{st.Text, st.AnchorX, st.AnchorY, st.Rotation, lx - hw, ly - hh, lx + hw, ly + hh})
			continue
		}
		b := &bs[k]
		b.X0, b.Y0, b.X1, b.Y1 = min(b.X0, lx-hw), min(b.Y0, ly-hh), max(b.X1, lx+hw), max(b.Y1, ly+hh)
	}
	return bs
}

// DrawCPU composites the texts onto the image (CPU render)
func (m *Multi) DrawCPU(img *image.RGBA, items []Item, styles []Style) {
	m.draw.DrawCPU(img, items, styles)
}
