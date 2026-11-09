package config

import (
	"fmt"
	"slices"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Timeline: each track is a list of segments. Segment 0 is the top-level YAML block
// (background, visualizer, ...); the following ones come from `timeline:` and
// only need the fields that change (they are applied on top of the previous segment).
//
//	timeline:
//	  visualizer:
//	    - at: 86.4
//	      label: "Chorus"
//	      transition: { mode: fade, duration: 1.5 }
//	      type: circular
//	      radius: 220

// Transition describes how a segment is entered
type Transition struct {
	// "cut" (instant), "fade" (interpolate/crossfade) or, for the background, one of the visual
	// transitions in TransitionModes. Other tracks treat every non-cut mode as a fade.
	Mode      string  `yaml:"mode"`
	Duration  float64 `yaml:"duration"`            // seconds (not for cut)
	Direction string  `yaml:"direction,omitempty"` // left, right, up, down: wipe, slide and push
	Burn      *Burn   `yaml:"burn,omitempty"`      // options of the burn transition
	Fluid     *Fluid  `yaml:"fluid,omitempty"`     // options of the fluid transition
}

// Fluid: every bit of the old image travels to a place of similar color in the new one, pushing
// the others on its way (they cannot overlap), like the particles of a fluid
type Fluid struct {
	ColorWeight float64 `yaml:"color_weight,omitempty" json:"color_weight,omitempty"` // 0..1 0 = stay near, 1 = go to the most similar color anywhere (default 0.6)
	Interaction float64 `yaml:"interaction,omitempty" json:"interaction,omitempty"`   // 0..1 how solid the particles are (how much they push each other, default 0.5)
	Stagger     float64 `yaml:"stagger,omitempty" json:"stagger,omitempty"`           // 0..1 how spread out in time the departures are (default 0.5)
	Viscosity   float64 `yaml:"viscosity,omitempty" json:"viscosity,omitempty"`       // liquid: 0..1 how much neighbors move together (default 0.6)
	Seed        int     `yaml:"seed,omitempty" json:"seed,omitempty"`                 // 0 = a different one per segment
}

// Burn: the old image burns like paper from one or more ignition points, revealing the new one
type Burn struct {
	Origins   string       `yaml:"origins,omitempty" json:"origins,omitempty"`     // random (default) or points
	Count     int          `yaml:"count,omitempty" json:"count,omitempty"`         // random: ignition points, 1..8 (default 3)
	Seed      int          `yaml:"seed,omitempty" json:"seed,omitempty"`           // random: 0 = a different one per segment
	Points    [][2]float64 `yaml:"points,omitempty" json:"points,omitempty"`       // points: x, y in 0..1 of the frame (up to 8)
	Edge      float64      `yaml:"edge,omitempty" json:"edge,omitempty"`           // 0..1: width of the charred edge and the ember (default 0.5)
	Roughness float64      `yaml:"roughness,omitempty" json:"roughness,omitempty"` // 0..1: how irregular the fire front is (default 0.5)
	Color     string       `yaml:"color,omitempty" json:"color,omitempty"`         // ember color (default #ff7a1f)
}

// MaxBurnPoints: ignition points the renderer takes
const MaxBurnPoints = 8

// TransitionModes are the background transitions (the renderer numbers them in this order)
var TransitionModes = []string{"cut", "fade", "dissolve", "dip_black", "flash", "wipe", "slide", "push",
	"zoom_in", "zoom_out", "circle", "clock", "glitch", "pixelate", "burn", "fluid", "liquid"}

// Directions for wipe, slide and push: where the new image moves
var TransitionDirections = []string{"left", "right", "up", "down"}

// Animated says whether entering the segment takes time (any mode but cut, with a duration)
func (t Transition) Animated() bool { return t.Mode != "" && t.Mode != "cut" && t.Duration > 0 }

// ModeIndex is the position of the mode in TransitionModes (unknown modes count as fade)
func (t Transition) ModeIndex() int {
	for i, m := range TransitionModes {
		if m == t.Mode {
			return i
		}
	}
	if t.Mode == "" {
		return 0
	}
	return 1
}

// DirectionIndex is the position of the direction in TransitionDirections (default left)
func (t Transition) DirectionIndex() int {
	for i, d := range TransitionDirections {
		if d == t.Direction {
			return i
		}
	}
	return 0
}

// Seg is a segment of a track
type Seg[T any] struct {
	At    float64
	Label string
	ID    string   // a name that stays (background segments: what clips and effects bind to)
	Bind  []string // background effects: the background segments it applies to (ids; none: all)
	// bound background effects: not held through the background's transitions (Picks):
	// cut on its image when the transition starts (yaml hold: false)
	NoHold     bool
	Transition Transition
	Cfg        T
	Keys       Keys  // keyframes of its properties (times from At); see keys.go
	Exprs      Exprs // the advanced settings (expressions) of its properties; see exprs.go
}

// CfgAtEv is CfgAt with its expressions too (worked out by ev; dur: the segment's length, for
// progress)
func (s *Seg[T]) CfgAtEv(t, dur float64, ev Evaluator, id string) *T {
	if len(s.Keys) == 0 && (len(s.Exprs) == 0 || ev == nil) {
		return &s.Cfg
	}
	return Animate(&s.Cfg, s.Keys, s.Exprs, ev, ExprCtx{T: t, U: t - s.At, Dur: dur, ID: id})
}

// SegDur: how long segment i lasts: until the next one, the last one until end (the song's)
func SegDur[T any](segs []Seg[T], i int, end float64) float64 {
	if i+1 < len(segs) {
		return segs[i+1].At - segs[i].At
	}
	return max(end-segs[i].At, 0)
}

// SegAtEv: the config of the segment active at t, with its keys and expressions
func SegAtEv[T any](segs []Seg[T], t, end float64, ev Evaluator, id string) *T {
	i := Active(segs, t)
	return segs[i].CfgAtEv(t, SegDur(segs, i, end), ev, id+strconv.Itoa(i))
}

// BlendEv is Blend with the segments' expressions too (worked out by ev; id names the track;
// end: the song's, for the last segment's length)
func BlendEv[T any](segs []Seg[T], t, end float64, ev Evaluator, id string) (prev, cur *T, k float64) {
	i := Active(segs, t)
	cur = segs[i].CfgAtEv(t, SegDur(segs, i, end), ev, id+strconv.Itoa(i))
	prev, k = cur, 1
	if i > 0 {
		s := segs[i]
		if s.Transition.Animated() && t < s.At+s.Transition.Duration {
			prev = segs[i-1].CfgAtEv(t, SegDur(segs, i-1, end), ev, id+strconv.Itoa(i-1))
			k = (t - s.At) / s.Transition.Duration
		}
	}
	return
}

// BgBind is which background segments are on screen: A, and during a transition (Trans, from
// T0 to T1) B coming in over it (their ids), and the ids the background track has (Known)
type BgBind struct {
	A, B   string
	Trans  bool
	T0, T1 float64
	Known  map[string]bool
}

// tied: whether ids name background segment id (one the track still has)
func (b *BgBind) tied(ids []string, id string) bool {
	return id != "" && b.Known[id] && slices.Contains(ids, id)
}

// Pick is which segment of a background effect's track works on an image: none (Off), the one
// active now with its own transition (Active), or segment Seg held (Held)
type Pick struct {
	Mode int
	Seg  int
}

const (
	PickOff = iota
	PickActive
	PickHeld
)

// Picks: which segment of a background effect's track works at t on the frame as a whole (both:
// the one active now unless it is tied to only one image, or to none on screen) and, during a
// transition, on each image. On an image: the one active now if it is tied to that image's
// segment; else, held through the transition, the one tied to it that was active until the
// transition started (A, the old one) or is when it ends (B, the new one), unless it says
// hold: false; else the one active now if it is tied to nothing
func Picks[T any](segs []Seg[T], t float64, b *BgBind) (both, a, bb Pick) {
	if len(segs) == 0 {
		return
	}
	i := Active(segs, t)
	side := b.BindSide(segs[i].Bind)
	if side == 0 {
		both = Pick{Mode: PickActive, Seg: i}
	}
	if !b.Trans {
		return both, both, both
	}
	untied := true
	for _, id := range segs[i].Bind {
		if id != "" && b.Known[id] {
			untied = false
		}
	}
	on := func(id string, j int) Pick {
		if b.tied(segs[i].Bind, id) {
			return Pick{Mode: PickActive, Seg: i}
		}
		if j != i && !segs[j].NoHold && b.tied(segs[j].Bind, id) {
			return Pick{Mode: PickHeld, Seg: j}
		}
		if untied {
			return Pick{Mode: PickActive, Seg: i}
		}
		return Pick{}
	}
	// (A: the one active until the transition starts, also when the next one starts with it)
	before := max(sort.Search(len(segs), func(k int) bool { return segs[k].At >= b.T0 })-1, 0)
	return both, on(b.A, before), on(b.B, Active(segs, b.T1))
}

// BindSide: where something bound to these background segments shows (ids it no longer has
// count for nothing; none left: it belongs to all): -1 nowhere, 0 everywhere, 1 only on image A
// of the transition, 2 only on B
func (b *BgBind) BindSide(ids []string) int {
	inA, inB, has := false, false, false
	for _, id := range ids {
		if id == "" || !b.Known[id] {
			continue
		}
		has = true
		inA = inA || id == b.A
		inB = inB || (b.Trans && id == b.B)
	}
	switch {
	case !has || (inA && (inB || !b.Trans)):
		return 0
	case inA:
		return 1
	case inB:
		return 2
	}
	return -1
}

// BgBindAt: the background segments on screen at t (as the frame's transition: the one coming
// in is B until it has fully entered)
func (tl *Timeline) BgBindAt(t float64) BgBind {
	segs := tl.Background
	b := BgBind{Known: map[string]bool{}}
	for _, s := range segs {
		if s.ID != "" {
			b.Known[s.ID] = true
		}
	}
	if len(segs) == 0 {
		return b
	}
	i := Active(segs, t)
	b.A = segs[i].ID
	if i > 0 && segs[i].Transition.Animated() && t < segs[i].At+segs[i].Transition.Duration {
		b.Trans, b.A, b.B = true, segs[i-1].ID, segs[i].ID
		b.T0, b.T1 = segs[i].At, segs[i].At+segs[i].Transition.Duration
	}
	return b
}

// Marker is a named mark on the timeline (song sections)
type Marker struct {
	At    float64 `yaml:"at"`
	Label string  `yaml:"label"`
}

type Timeline struct {
	Background []Seg[BackgroundConfig]
	BassZoom   []Seg[BassZoomConfig]
	Shake      []Seg[ShakeConfig]
	Chromatic  []Seg[ChromaticAberrationConfig]
	Ripple     []Seg[RippleConfig]
	Glitch     []Seg[GlitchConfig]
	ColorFlash []Seg[ColorFlashConfig]
	Parallax   []Seg[ParallaxConfig]
	DoF        []Seg[DepthOfFieldConfig]
	Visualizer []Seg[VisualizerConfig]
	Particles  []Seg[ParticlesConfig]
	Lyrics     []Seg[LyricsConfig]
}

// TrackNames are the track keys in `timeline:` (and in the studio API)
var TrackNames = []string{"background", "bass_zoom", "shake", "chromatic_aberration", "ripple",
	"glitch", "color_flash", "parallax", "depth_of_field", "visualizer", "particles", "lyrics"}

// Active returns the index of the segment active at t (the last one with At <= t)
func Active[T any](segs []Seg[T], t float64) int {
	i := sort.Search(len(segs), func(i int) bool { return segs[i].At > t }) - 1
	return max(i, 0)
}

// Blend returns (previous, current, k): k = 1 outside a transition; during a fade, k goes from 0 to 1
func Blend[T any](segs []Seg[T], t float64) (prev, cur *T, k float64) {
	i := Active(segs, t)
	cur = segs[i].CfgAt(t)
	prev, k = cur, 1
	if i > 0 {
		s := segs[i]
		if s.Transition.Animated() && t < s.At+s.Transition.Duration {
			prev = segs[i-1].CfgAt(t)
			k = (t - s.At) / s.Transition.Duration
		}
	}
	return
}

// deepCopy clones a config block (including slices) via YAML
func deepCopy[T any](v T) T {
	b, _ := yaml.Marshal(v)
	var out T
	_ = yaml.Unmarshal(b, &out)
	return out
}

// buildTrack builds a track's segments: base at t=0 plus the timeline nodes applied in sequence
func buildTrack[T any](base T, nodes []yaml.Node, fix func(*T)) ([]Seg[T], error) {
	fix(&base)
	segs := []Seg[T]{{At: 0, Cfg: base, Transition: Transition{Mode: "cut"}}}
	type head struct {
		At         *float64    `yaml:"at"`
		Label      string      `yaml:"label"`
		ID         string      `yaml:"id"`
		Bind       []string    `yaml:"bind"`
		Hold       *bool       `yaml:"hold"`
		Transition *Transition `yaml:"transition"`
		Keys       Keys        `yaml:"keys"`
		Exprs      Exprs       `yaml:"exprs"`
	}
	var parsed []struct {
		h    head
		node yaml.Node
	}
	for _, n := range nodes {
		var h head
		if err := n.Decode(&h); err != nil {
			return nil, err
		}
		if h.At == nil {
			return nil, fmt.Errorf("timeline segment without 'at' (line %d)", n.Line)
		}
		parsed = append(parsed, struct {
			h    head
			node yaml.Node
		}{h, n})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return *parsed[i].h.At < *parsed[j].h.At })
	for _, p := range parsed {
		prev := segs[len(segs)-1]
		cfg := deepCopy(prev.Cfg)
		// Strip at/label/transition and apply the rest onto the copy of the previous segment
		body := p.node
		if body.Kind == yaml.MappingNode {
			var kept []*yaml.Node
			for i := 0; i+1 < len(body.Content); i += 2 {
				switch body.Content[i].Value {
				case "at", "label", "id", "bind", "hold", "transition", "keys":
					continue
				}
				kept = append(kept, body.Content[i], body.Content[i+1])
			}
			body.Content = kept
		}
		if err := body.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("segment at %.2fs: %w", *p.h.At, err)
		}
		fix(&cfg)
		tr := Transition{Mode: "cut"}
		if p.h.Transition != nil {
			tr = *p.h.Transition
			if tr.Mode == "" {
				tr.Mode = "fade"
			}
		}
		p.h.Keys.Sort()
		noHold := p.h.Hold != nil && !*p.h.Hold
		seg := Seg[T]{At: *p.h.At, Label: p.h.Label, ID: p.h.ID, Bind: p.h.Bind, NoHold: noHold, Transition: tr, Cfg: cfg, Keys: p.h.Keys, Exprs: p.h.Exprs}
		if seg.At <= 0 {
			// A segment at 0 replaces the base (or only gives it a name, id or keyframes)
			segs[0].Cfg, segs[0].Label, segs[0].Keys, segs[0].Exprs = cfg, p.h.Label, p.h.Keys, p.h.Exprs
			segs[0].ID, segs[0].Bind, segs[0].NoHold = p.h.ID, p.h.Bind, noHold
			continue
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

func noFix[T any](*T) {}

// BuildTimeline builds all tracks from the base config and the `timeline:` block
func (c *ProjectConfig) BuildTimeline() error {
	raw := map[string][]yaml.Node{}
	if c.TimelineRaw.Kind != 0 {
		if err := c.TimelineRaw.Decode(&raw); err != nil {
			return fmt.Errorf("invalid 'timeline' block: %w", err)
		}
	}
	for k := range raw {
		known := k == "markers"
		for _, n := range TrackNames {
			known = known || k == n
		}
		if !known {
			return fmt.Errorf("unknown timeline track '%s' (valid: %v)", k, TrackNames)
		}
	}
	var err error
	tl := &c.Timeline
	be := &c.BackgroundEffects
	if tl.Background, err = buildTrack(c.Background, raw["background"], noFix[BackgroundConfig]); err != nil {
		return err
	}
	if tl.BassZoom, err = buildTrack(be.BassZoom, raw["bass_zoom"], noFix[BassZoomConfig]); err != nil {
		return err
	}
	if tl.Shake, err = buildTrack(be.Shake, raw["shake"], noFix[ShakeConfig]); err != nil {
		return err
	}
	if tl.Chromatic, err = buildTrack(be.ChromaticAberration, raw["chromatic_aberration"], noFix[ChromaticAberrationConfig]); err != nil {
		return err
	}
	if tl.Ripple, err = buildTrack(be.Ripple, raw["ripple"], noFix[RippleConfig]); err != nil {
		return err
	}
	if tl.Glitch, err = buildTrack(be.Glitch, raw["glitch"], noFix[GlitchConfig]); err != nil {
		return err
	}
	if tl.Parallax, err = buildTrack(be.Parallax, raw["parallax"], noFix[ParallaxConfig]); err != nil {
		return err
	}
	if tl.DoF, err = buildTrack(be.DepthOfField, raw["depth_of_field"], noFix[DepthOfFieldConfig]); err != nil {
		return err
	}
	if tl.ColorFlash, err = buildTrack(be.ColorFlash, raw["color_flash"], noFix[ColorFlashConfig]); err != nil {
		return err
	}
	if tl.Visualizer, err = buildTrack(c.Visualizer, raw["visualizer"], FixVisualizer); err != nil {
		return err
	}
	if tl.Particles, err = buildTrack(c.Particles, raw["particles"], FixParticles); err != nil {
		return err
	}
	if tl.Lyrics, err = buildTrack(c.Lyrics, raw["lyrics"], noFix[LyricsConfig]); err != nil {
		return err
	}
	c.Markers = nil
	for _, n := range raw["markers"] {
		var m Marker
		if err := n.Decode(&m); err != nil {
			return err
		}
		c.Markers = append(c.Markers, m)
	}
	sort.Slice(c.Markers, func(i, j int) bool { return c.Markers[i].At < c.Markers[j].At })
	return nil
}

// FixVisualizer normalizes a visualizer block (bar limits, lowercase type)
func FixVisualizer(v *VisualizerConfig) {
	if v.Bars <= 0 {
		v.Bars = 48
	}
	if v.Bars > 256 {
		v.Bars = 256
	}
	v.Type = lower(v.Type)
}

// FixParticles ensures there is at least one layer
func FixParticles(p *ParticlesConfig) {
	if len(p.Layers) == 0 {
		p.Layers = []ParticleLayer{p.ParticleLayer}
	}
}

// ---------------------------------------------------------------- serialization

// segYAML is the shape of a segment when saving (header + all fields of the block)
func segNode[T any](s Seg[T]) (*yaml.Node, error) {
	var body yaml.Node
	if err := body.Encode(s.Cfg); err != nil {
		return nil, err
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v any) {
		var vn yaml.Node
		_ = vn.Encode(v)
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &vn)
	}
	add("at", s.At)
	if s.Label != "" {
		add("label", s.Label)
	}
	if s.ID != "" {
		add("id", s.ID)
	}
	if len(s.Bind) > 0 {
		add("bind", s.Bind)
	}
	if s.NoHold {
		add("hold", false)
	}
	add("transition", s.Transition)
	if len(s.Keys) > 0 {
		add("keys", s.Keys)
	}
	if len(s.Exprs) > 0 {
		add("exprs", s.Exprs)
	}
	n.Content = append(n.Content, body.Content...)
	return n, nil
}

func trackNodes[T any](segs []Seg[T]) ([]*yaml.Node, error) {
	var out []*yaml.Node
	// The first segment is the top-level block; its name, id and keyframes go in an entry at 0
	if s := segs[0]; s.Label != "" || s.ID != "" || len(s.Bind) > 0 || s.NoHold || len(s.Keys) > 0 || len(s.Exprs) > 0 {
		n := &yaml.Node{Kind: yaml.MappingNode}
		add := func(k string, v any) {
			var vn yaml.Node
			_ = vn.Encode(v)
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &vn)
		}
		add("at", 0)
		if s.Label != "" {
			add("label", s.Label)
		}
		if s.ID != "" {
			add("id", s.ID)
		}
		if len(s.Bind) > 0 {
			add("bind", s.Bind)
		}
		if s.NoHold {
			add("hold", false)
		}
		if len(s.Keys) > 0 {
			add("keys", s.Keys)
		}
		if len(s.Exprs) > 0 {
			add("exprs", s.Exprs)
		}
		out = append(out, n)
	}
	for _, s := range segs[1:] {
		n, err := segNode(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// SyncFromTimeline copies segment 0 of each track into the base blocks and regenerates `timeline:`
// so that Save writes a YAML consistent with c.Timeline
func (c *ProjectConfig) SyncFromTimeline() error {
	tl := &c.Timeline
	if len(tl.Background) > 0 {
		c.Background = tl.Background[0].Cfg
	}
	be := &c.BackgroundEffects
	if len(tl.BassZoom) > 0 {
		be.BassZoom = tl.BassZoom[0].Cfg
	}
	if len(tl.Shake) > 0 {
		be.Shake = tl.Shake[0].Cfg
	}
	if len(tl.Chromatic) > 0 {
		be.ChromaticAberration = tl.Chromatic[0].Cfg
	}
	if len(tl.Ripple) > 0 {
		be.Ripple = tl.Ripple[0].Cfg
	}
	if len(tl.Glitch) > 0 {
		be.Glitch = tl.Glitch[0].Cfg
	}
	if len(tl.ColorFlash) > 0 {
		be.ColorFlash = tl.ColorFlash[0].Cfg
	}
	if len(tl.Parallax) > 0 {
		be.Parallax = tl.Parallax[0].Cfg
	}
	if len(tl.DoF) > 0 {
		be.DepthOfField = tl.DoF[0].Cfg
	}
	if len(tl.Visualizer) > 0 {
		c.Visualizer = tl.Visualizer[0].Cfg
	}
	if len(tl.Particles) > 0 {
		c.Particles = tl.Particles[0].Cfg
	}
	if len(tl.Lyrics) > 0 {
		c.Lyrics = tl.Lyrics[0].Cfg
	}

	root := &yaml.Node{Kind: yaml.MappingNode}
	put := func(name string, nodes []*yaml.Node, err error) error {
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return nil
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Content: nodes}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name}, seq)
		return nil
	}
	steps := []func() error{
		func() error { n, e := trackNodes(tl.Background); return put("background", n, e) },
		func() error { n, e := trackNodes(tl.BassZoom); return put("bass_zoom", n, e) },
		func() error { n, e := trackNodes(tl.Shake); return put("shake", n, e) },
		func() error { n, e := trackNodes(tl.Chromatic); return put("chromatic_aberration", n, e) },
		func() error { n, e := trackNodes(tl.Ripple); return put("ripple", n, e) },
		func() error { n, e := trackNodes(tl.Glitch); return put("glitch", n, e) },
		func() error { n, e := trackNodes(tl.ColorFlash); return put("color_flash", n, e) },
		func() error { n, e := trackNodes(tl.Parallax); return put("parallax", n, e) },
		func() error { n, e := trackNodes(tl.DoF); return put("depth_of_field", n, e) },
		func() error { n, e := trackNodes(tl.Visualizer); return put("visualizer", n, e) },
		func() error { n, e := trackNodes(tl.Particles); return put("particles", n, e) },
		func() error { n, e := trackNodes(tl.Lyrics); return put("lyrics", n, e) },
	}
	for _, s := range steps {
		if len(tl.Background) == 0 {
			break
		}
		if err := s(); err != nil {
			return err
		}
	}
	if len(c.Markers) > 0 {
		var mn yaml.Node
		if err := mn.Encode(c.Markers); err != nil {
			return err
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "markers"}, &mn)
	}
	c.TimelineRaw = yaml.Node{}
	if len(root.Content) > 0 {
		c.TimelineRaw = *root
	}
	return nil
}
