package lyrics

import (
	"image"
	"math"
	"strconv"
	"strings"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
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
	// Ev works out the texts' expressions (the engine's; nil: none)
	Ev   config.Evaluator
	w, h int
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
	return buildEngine(&cfg, []Line{ln}, w, h, true, tc.Motion.Bend && tc.Motion.On())
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
			mo := tc.Motion
			if len(tc.Keys) > 0 || (len(tc.Exprs) > 0 && m.Ev != nil) {
				// Animated text: style from its properties now; moved by the change of its
				// position, scaled and turned around it by the change of its size and rotation
				k := config.Animate(tc, tc.Keys, tc.Exprs, m.Ev, config.ExprCtx{T: t, U: t - tc.Start, Dur: tc.End - tc.Start, ID: "text:" + strconv.Itoa(i-m.nLyr)})
				kc := k.LyricsConfig
				kc.Position, kc.Mode = cfg.Position, cfg.Mode
				cfg = &kc
				dx = float32((k.PositionX - tc.PositionX) * float64(m.w))
				dy = float32((k.PositionY - tc.PositionY) * float64(m.h))
				if tc.Size > 0 && k.Size > 0 {
					sc = float32(k.Size / tc.Size)
				}
				rot = k.Rotation
				mo = k.Motion
			}
			bend := mo.On() && mo.Bend && tc.Motion.Bend // laid out letter by letter
			var ztr *effects.PathTrack
			var rib *effects.Ribbon
			var zs0, ztx, zty float32
			fsx, fsy := float32(1), float32(1) // turned round: its width and height (Motion.Flip)
			if mo.On() && !bend {
				// along its path: the anchor goes to the path's point
				x, y, a := effects.PathAt(mo.Path, float64(m.w), float64(m.h), mo.Progress)
				pa := a
				ztr = effects.Track(mo.Path, "z", float64(m.w), float64(m.h))
				if mo.Orient && effects.IsRibbon(mo.Flip) {
					pts, L := effects.SamplePath(mo.Path, float64(m.w), float64(m.h))
					rib = effects.NewRibbon(pts, L, !mo.Path.Open)
				}
				if ztr != nil || rib != nil {
					_, L := effects.NodeLens(mo.Path, float64(m.w), float64(m.h))
					p := mo.Progress
					if mo.Path.Open {
						p = math.Max(0, math.Min(1, p))
					} else {
						p -= math.Floor(p)
					}
					zs0, ztx, zty = float32(p*L), float32(math.Cos(a)), float32(math.Sin(a))
				}
				dx, dy = float32(x-tc.PositionX*float64(m.w)), float32(y-tc.PositionY*float64(m.h))
				if mo.Orient {
					fr, kx, ky := effects.FlipTurn(math.Cos(pa), math.Sin(pa), mo.Flip)
					rot += fr * 180 / math.Pi
					fsx, fsy = float32(kx), float32(ky)
					if rib != nil {
						fsy = 1 // it twists per pixel (Style.Ribbon)
					}
				}
			}
			if bend {
				// bent along its path: each letter at its distance from the anchor along it, at
				// its height across it, turned with it (plus the text's own rotation)
				pts, L := effects.SamplePath(mo.Path, float64(m.w), float64(m.h))
				closed := !mo.Path.Open
				ax, ay := float32(tc.PositionX*float64(m.w)), float32(tc.PositionY*float64(m.h))
				s0 := float32(mo.Progress) * L
				r0 := float32(rot * math.Pi / 180)
				// readable going back: the whole text turns round like a card where its middle is
				// (its letters laid the other way along the path, so that it reads well)
				var rdir, rk float32 = 1, 1
				wrap := effects.IsWrap(mo.Flip)
				if mo.Flip == "readable" {
					_, _, tx0, _ := effects.PathPoint(pts, L, closed, s0)
					rk = float32(math.Min(1, math.Abs(float64(tx0))/0.25))
					if tx0 < 0 {
						rdir = -1
					}
				}
				for j := range its {
					ox, oy := (its[j].CX-ax)*sc, (its[j].CY-ay)*sc
					if mo.Flip == "readable" {
						ox *= rk * rdir
					}
					px, py, tx, ty := effects.PathPoint(pts, L, closed, s0+ox)
					fr, kx, ky := effects.FlipTurn(float64(tx), float64(ty), mo.Flip)
					if mo.Flip == "readable" {
						// upright as the text's middle is, all narrowed alike
						fr, kx, ky = math.Atan2(float64(ty*rdir), float64(tx*rdir)), float64(rk), 1
						oy *= rdir
					}
					// a ribbon: its lines across the path closer where it twists (its letters twist
					// per pixel when drawn: Style.Ribbon)
					oy *= float32(ky)
					if effects.IsRibbon(mo.Flip) {
						kx, ky = 1, 1
					}
					its[j].CX, its[j].CY = px-ty*oy, py+tx*oy
					its[j].Rot = float32(fr) + r0
					its[j].Flip, its[j].FlipY = 1-float32(kx), 1-float32(ky)
					if wrap {
						// round a cylinder: its height upright (the text's own rotation), its
						// length along the path
						ux, uy := -math.Sin(float64(r0)), math.Cos(float64(r0))
						wr, wk, wsh := effects.WrapFrame(float64(tx), float64(ty), ux, uy)
						its[j].CX, its[j].CY = px+float32(ux)*oy, py+float32(uy)*oy
						its[j].Rot, its[j].Flip, its[j].FlipY, its[j].Shear = float32(wr), 0, 1-float32(wk), float32(wsh)
					}
					its[j].Scale *= sc
					its[j].PS, its[j].PTX, its[j].PTY = s0+ox, tx, ty
				}
				st := StyleFor(cfg, t, feat)
				st.ZTrack = effects.Track(mo.Path, "z", float64(m.w), float64(m.h))
				if effects.IsRibbon(mo.Flip) {
					st.Ribbon = effects.NewRibbon(pts, L, closed)
				}
				st.Wrap = wrap
				st.Z = cfg.ZIndex
				px, py, tx, ty := effects.PathPoint(pts, L, closed, s0)
				st.Text, st.AnchorX, st.AnchorY = i-m.nLyr, px, py
				st.Rotation = float32(math.Atan2(float64(ty), float64(tx))) + r0
				styles = append(styles, st)
				for _, it := range its {
					it.AY += float32(m.offs[i])
					it.Style = si
					items = append(items, it)
				}
				continue
			}
			st := StyleFor(cfg, t, feat)
			st.Z = cfg.ZIndex
			ax, ay := float32(tc.PositionX*float64(m.w)), float32(tc.PositionY*float64(m.h))
			r := rot * math.Pi / 180
			st.Text, st.AnchorX, st.AnchorY, st.Rotation = i-m.nLyr, ax+dx, ay+dy, float32(r)
			st.ZTrack, st.Ribbon = ztr, rib
			styles = append(styles, st)
			if dx != 0 || dy != 0 || sc != 1 || rot != 0 || fsx != 1 || fsy != 1 {
				rc, rs := float32(math.Cos(r)), float32(math.Sin(r))
				for j := range its {
					ox, oy := (its[j].CX-ax)*sc*fsx, (its[j].CY-ay)*sc*fsy
					its[j].CX = ax + dx + ox*rc - oy*rs
					its[j].CY = ay + dy + ox*rs + oy*rc
					its[j].Scale *= sc
					its[j].Rot = float32(r)
					its[j].Flip, its[j].FlipY = 1-fsx, 1-fsy
				}
			}
			if ztr != nil || rib != nil {
				for j := range its {
					// along the path from its anchor (on the path) in the path's direction
					its[j].PS = zs0 + (its[j].CX-ax-dx)*ztx + (its[j].CY-ay-dy)*zty
					its[j].PTX, its[j].PTY = ztx, zty
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

// LightSources: how much of the text of style si there is in each cell (div px) of a gw×gh grid
// over the frame (px² covered by its letters × its opacity): where its light comes from
func (m *Multi) LightSources(items []Item, si, div, gw, gh int) []float32 {
	src := make([]float32, gw*gh)
	const step = 6.0 // px between the samples of its letters
	for i := range items {
		it := &items[i]
		if it.Style != si || it.Alpha <= 0 {
			continue
		}
		sc := float64(it.Scale)
		hw, hh := float64(it.AW)/2, float64(it.AH)/2
		rc, rs := 1.0, 0.0
		if it.Rot != 0 {
			rc, rs = math.Cos(float64(it.Rot)), math.Sin(float64(it.Rot))
		}
		ex := (hw*math.Abs(rc) + hh*math.Abs(rs)) * sc
		ey := (hw*math.Abs(rs) + hh*math.Abs(rc)) * sc
		cx, cy := float64(it.CX), float64(it.CY)
		for y := cy - ey; y <= cy+ey; y += step {
			for x := cx - ex; x <= cx+ex; x += step {
				dx, dy := x-cx, y-cy
				lx, ly := (dx*rc+dy*rs)/sc, (-dx*rs+dy*rc)/sc
				if math.Abs(lx) > hw || math.Abs(ly) > hh {
					continue
				}
				d := m.draw.sdfAt(float64(it.AX)+hw+lx, float64(it.AY)+hh+ly, it) * sc
				cov := clampF(0.5-d/3, 0, 1)
				if cov <= 0 {
					continue
				}
				gx, gy := int(x)/div, int(y)/div
				if gx < 0 || gy < 0 || gx >= gw || gy >= gh {
					continue
				}
				src[gy*gw+gx] += float32(cov * step * step * float64(it.Alpha))
			}
		}
	}
	return src
}

// TextPoints: points on the letters of fixed text i as drawn at time t (x, y pairs, px around
// its anchor then, before its rotation then): its outline, or all of it (fill); at most max,
// evenly picked
func (m *Multi) TextPoints(i int, t float64, fill bool, max int) []float32 {
	items, styles := m.Frame(t, &audio.FrameFeatures{}, nil)
	si := -1
	for k := range styles {
		if styles[k].Text == i {
			si = k
		}
	}
	if si < 0 {
		return nil
	}
	st := &styles[si]
	ax, ay := float64(st.AnchorX), float64(st.AnchorY)
	cr, sr := math.Cos(-float64(st.Rotation)), math.Sin(-float64(st.Rotation))
	var pts []float32
	const step = 2.0
	for k := range items {
		it := &items[k]
		if it.Style != si {
			continue
		}
		sc := float64(it.Scale)
		hw, hh := float64(it.AW)/2, float64(it.AH)/2
		rc, rs := 1.0, 0.0
		if it.Rot != 0 {
			rc, rs = math.Cos(float64(it.Rot)), math.Sin(float64(it.Rot))
		}
		ex := (hw*math.Abs(rc) + hh*math.Abs(rs)) * sc
		ey := (hw*math.Abs(rs) + hh*math.Abs(rc)) * sc
		cx, cy := float64(it.CX), float64(it.CY)
		for y := cy - ey; y <= cy+ey; y += step {
			for x := cx - ex; x <= cx+ex; x += step {
				dx, dy := x-cx, y-cy
				lx, ly := (dx*rc+dy*rs)/sc, (-dx*rs+dy*rc)/sc
				if math.Abs(lx) > hw || math.Abs(ly) > hh {
					continue
				}
				d := m.draw.sdfAt(float64(it.AX)+hw+lx, float64(it.AY)+hh+ly, it) * sc
				if (fill && d > 0) || (!fill && math.Abs(d) > 1) {
					continue
				}
				px, py := x-ax, y-ay
				pts = append(pts, float32(px*cr-py*sr), float32(px*sr+py*cr))
			}
		}
	}
	if n := len(pts) / 2; n > max {
		out := make([]float32, 0, max*2)
		for j := 0; j < max; j++ {
			k := j * n / max
			out = append(out, pts[2*k], pts[2*k+1])
		}
		pts = out
	}
	return pts
}

// TextPointsOf: Multi.TextPoints of a fixed text laid out on its own, shown or not (an emitter
// can be born on the letters of a hidden text: only the fire shows them)
func TextPointsOf(tc config.TextConfig, t float64, fill bool, max, w, h int) []float32 {
	tc.Enabled = true
	m, err := NewMulti(nil, []config.TextConfig{tc}, w, h)
	if err != nil || m == nil {
		return nil
	}
	return m.TextPoints(0, t, fill, max)
}
