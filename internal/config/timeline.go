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
	Mode     string  `yaml:"mode"`     // "cut" (instant) or "fade" (interpolate/crossfade)
	Duration float64 `yaml:"duration"` // seconds (fade only)
}

// Seg is a segment of a track
type Seg[T any] struct {
	At         float64
	Label      string
	Transition Transition
	Cfg        T
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
	Visualizer []Seg[VisualizerConfig]
	Particles  []Seg[ParticlesConfig]
	Lyrics     []Seg[LyricsConfig]
}

// TrackNames are the track keys in `timeline:` (and in the studio API)
var TrackNames = []string{"background", "bass_zoom", "shake", "chromatic_aberration", "ripple",
	"glitch", "color_flash", "visualizer", "particles", "lyrics"}

// Active returns the index of the segment active at t (the last one with At <= t)
func Active[T any](segs []Seg[T], t float64) int {
	i := sort.Search(len(segs), func(i int) bool { return segs[i].At > t }) - 1
	return max(i, 0)
}

// Blend returns (previous, current, k): k = 1 outside a transition; during a fade, k goes from 0 to 1
func Blend[T any](segs []Seg[T], t float64) (prev, cur *T, k float64) {
	i := Active(segs, t)
	cur = &segs[i].Cfg
	prev, k = cur, 1
	if i > 0 {
		s := segs[i]
		if s.Transition.Mode == "fade" && s.Transition.Duration > 0 && t < s.At+s.Transition.Duration {
			prev = &segs[i-1].Cfg
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
				case "at", "label", "transition":
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
		seg := Seg[T]{At: *p.h.At, Label: p.h.Label, Transition: tr, Cfg: cfg}
		if seg.At <= 0 {
			// A segment at 0 replaces the base
			segs[0].Cfg, segs[0].Label = cfg, p.h.Label
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
	n.Content = append(n.Content, body.Content...)
	return n, nil
}

func trackNodes[T any](segs []Seg[T]) ([]*yaml.Node, error) {
	var out []*yaml.Node
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
