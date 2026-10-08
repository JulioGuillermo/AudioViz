package config

import (
	"fmt"
	"sort"

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
	At         float64
	Label      string
	Transition Transition
	Cfg        T
	Keys       Keys // keyframes of its properties (times from At); see keys.go
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
		Transition *Transition `yaml:"transition"`
		Keys       Keys        `yaml:"keys"`
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
				case "at", "label", "transition", "keys":
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
		seg := Seg[T]{At: *p.h.At, Label: p.h.Label, Transition: tr, Cfg: cfg, Keys: p.h.Keys}
		if seg.At <= 0 {
			// A segment at 0 replaces the base (or only gives it a name or keyframes)
			segs[0].Cfg, segs[0].Label, segs[0].Keys = cfg, p.h.Label, p.h.Keys
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
	add("transition", s.Transition)
	if len(s.Keys) > 0 {
		add("keys", s.Keys)
	}
	n.Content = append(n.Content, body.Content...)
	return n, nil
}

func trackNodes[T any](segs []Seg[T]) ([]*yaml.Node, error) {
	var out []*yaml.Node
	// The first segment is the top-level block; its name and keyframes go in an entry at 0
	if s := segs[0]; s.Label != "" || len(s.Keys) > 0 {
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
		if len(s.Keys) > 0 {
			add("keys", s.Keys)
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
