package studio

import (
	"fmt"
	"slices"

	"audioviz/internal/config"

	"gopkg.in/yaml.v3"
)

// JSON shape of the project for the UI: same keys as the YAML, but each track
// as an explicit list of segments (the first one, at at=0, is the base block).

type TransJSON struct {
	Mode      string        `json:"mode"`
	Duration  float64       `json:"duration"`
	Direction string        `json:"direction,omitempty"`
	Burn      *config.Burn  `json:"burn,omitempty"`
	Fluid     *config.Fluid `json:"fluid,omitempty"`
}

type SegJSON struct {
	At         float64        `json:"at"`
	Label      string         `json:"label"`
	Transition TransJSON      `json:"transition"`
	Cfg        map[string]any `json:"cfg"`
	Keys       config.Keys    `json:"keys,omitempty"` // keyframes: property path → keys (times from At)
}

type MarkerJSON struct {
	At    float64 `json:"at"`
	Label string  `json:"label"`
}

type ViewJSON struct {
	Hidden    []string `json:"hidden"`
	Collapsed []string `json:"collapsed"`
}

type LayerJSON struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Kind  string           `json:"kind,omitempty"` // "" media, "emitter" particles
	Clips []map[string]any `json:"clips"`          // config.ClipConfig, YAML keys
}

type ProjectJSON struct {
	Audio   map[string]any       `json:"audio"`
	Output  map[string]any       `json:"output"`
	Tracks  map[string][]SegJSON `json:"tracks"`
	Markers []MarkerJSON         `json:"markers"`
	Texts   []map[string]any     `json:"texts"`  // fixed texts (config.TextConfig, YAML keys)
	View    ViewJSON             `json:"view"`   // studio view: hidden tracks, folded zones
	Layers  []LayerJSON          `json:"layers"` // overlay layers, bottom to top (config.LayerConfig)
	// Values of a new clip (read-only for the UI)
	ClipDefaults map[string]any `json:"clipDefaults,omitempty"`
	// The emitter presets (read-only for the UI)
	EmitterPresets map[string]map[string]any `json:"emitterPresets,omitempty"`
	// Values of a new text (read-only for the UI; ignored when the project comes back)
	TextDefaults map[string]any `json:"textDefaults,omitempty"`
}

// toMap converts a config block into a map with the YAML keys
func toMap(v any) map[string]any {
	b, _ := yaml.Marshal(v)
	m := map[string]any{}
	_ = yaml.Unmarshal(b, &m)
	return normalize(m).(map[string]any)
}

// normalize converts nested map[any]any (yaml) into map[string]any (json)
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalize(x)
		}
		return t
	case map[any]any:
		m := map[string]any{}
		for k, x := range t {
			m[fmt.Sprint(k)] = normalize(x)
		}
		return m
	case []any:
		for i := range t {
			t[i] = normalize(t[i])
		}
		return t
	}
	return v
}

// fromMap applies a map onto dst (which already holds the default values)
func fromMap(m map[string]any, dst any) error {
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, dst)
}

func segsToJSON[T any](segs []config.Seg[T]) []SegJSON {
	out := make([]SegJSON, len(segs))
	for i, s := range segs {
		out[i] = SegJSON{At: s.At, Label: s.Label, Transition: TransJSON{s.Transition.Mode, s.Transition.Duration, s.Transition.Direction, s.Transition.Burn, s.Transition.Fluid}, Cfg: toMap(s.Cfg), Keys: s.Keys}
	}
	return out
}

func segsFromJSON[T any](name string, in []SegJSON, def T, fix func(*T)) ([]config.Seg[T], error) {
	if len(in) == 0 {
		fix(&def)
		return []config.Seg[T]{{Cfg: def, Transition: config.Transition{Mode: "cut"}}}, nil
	}
	out := make([]config.Seg[T], 0, len(in))
	for i, s := range in {
		cfg := def
		// Clean copy of the default value (slices must not be shared)
		b, _ := yaml.Marshal(def)
		_ = yaml.Unmarshal(b, &cfg)
		if err := fromMap(s.Cfg, &cfg); err != nil {
			return nil, fmt.Errorf("%s segment %d: %w", name, i+1, err)
		}
		fix(&cfg)
		at := s.At
		if i == 0 {
			at = 0
		}
		mode := s.Transition.Mode
		if !slices.Contains(config.TransitionModes, mode) {
			mode = "cut"
		}
		s.Keys.Sort()
		out = append(out, config.Seg[T]{At: at, Label: s.Label, Cfg: cfg, Keys: s.Keys,
			Transition: config.Transition{Mode: mode, Duration: s.Transition.Duration, Direction: s.Transition.Direction, Burn: s.Transition.Burn, Fluid: s.Transition.Fluid}})
	}
	// Sort by time (the first one always at 0)
	for i := 1; i < len(out); i++ {
		for j := i; j > 1 && out[j].At < out[j-1].At; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func noFix[T any](*T) {}

// ProjectToJSON prepares the project for the UI
func ProjectToJSON(c *config.ProjectConfig) *ProjectJSON {
	tl := &c.Timeline
	p := &ProjectJSON{
		Audio:  toMap(c.Audio),
		Output: toMap(c.Output),
		Tracks: map[string][]SegJSON{
			"background":           segsToJSON(tl.Background),
			"bass_zoom":            segsToJSON(tl.BassZoom),
			"shake":                segsToJSON(tl.Shake),
			"chromatic_aberration": segsToJSON(tl.Chromatic),
			"ripple":               segsToJSON(tl.Ripple),
			"glitch":               segsToJSON(tl.Glitch),
			"color_flash":          segsToJSON(tl.ColorFlash),
			"visualizer":           segsToJSON(tl.Visualizer),
			"particles":            segsToJSON(tl.Particles),
			"lyrics":               segsToJSON(tl.Lyrics),
		},
		Markers: []MarkerJSON{},
	}
	for _, m := range c.Markers {
		p.Markers = append(p.Markers, MarkerJSON{m.At, m.Label})
	}
	p.View = ViewJSON{Hidden: append([]string{}, c.Studio.Hidden...), Collapsed: append([]string{}, c.Studio.Collapsed...)}
	p.TextDefaults = toMap(config.DefaultText())
	p.ClipDefaults = toMap(config.DefaultClip())
	p.EmitterPresets = map[string]map[string]any{}
	for k, e := range config.EmitterPresets {
		e.Preset = k
		p.EmitterPresets[k] = toMap(e)
	}
	p.Layers = []LayerJSON{}
	for _, l := range c.Layers {
		lj := LayerJSON{ID: l.ID, Name: l.Name, Kind: l.Kind, Clips: []map[string]any{}}
		for _, cl := range l.Clips {
			lj.Clips = append(lj.Clips, toMap(cl))
		}
		p.Layers = append(p.Layers, lj)
	}
	p.Texts = []map[string]any{}
	for _, t := range c.Texts {
		p.Texts = append(p.Texts, toMap(t))
	}
	return p
}

// ProjectFromJSON rebuilds the full config from what the UI sends
func ProjectFromJSON(p *ProjectJSON, dir string) (*config.ProjectConfig, error) {
	c := config.DefaultConfig()
	c.Dir = dir
	if err := fromMap(p.Audio, &c.Audio); err != nil {
		return nil, fmt.Errorf("audio: %w", err)
	}
	if err := fromMap(p.Output, &c.Output); err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	d := config.DefaultConfig()
	tl := &c.Timeline
	var err error
	if tl.Background, err = segsFromJSON("background", p.Tracks["background"], d.Background, noFix[config.BackgroundConfig]); err != nil {
		return nil, err
	}
	be := &d.BackgroundEffects
	if tl.BassZoom, err = segsFromJSON("bass_zoom", p.Tracks["bass_zoom"], be.BassZoom, noFix[config.BassZoomConfig]); err != nil {
		return nil, err
	}
	if tl.Shake, err = segsFromJSON("shake", p.Tracks["shake"], be.Shake, noFix[config.ShakeConfig]); err != nil {
		return nil, err
	}
	if tl.Chromatic, err = segsFromJSON("chromatic_aberration", p.Tracks["chromatic_aberration"], be.ChromaticAberration, noFix[config.ChromaticAberrationConfig]); err != nil {
		return nil, err
	}
	if tl.Ripple, err = segsFromJSON("ripple", p.Tracks["ripple"], be.Ripple, noFix[config.RippleConfig]); err != nil {
		return nil, err
	}
	if tl.Glitch, err = segsFromJSON("glitch", p.Tracks["glitch"], be.Glitch, noFix[config.GlitchConfig]); err != nil {
		return nil, err
	}
	if tl.ColorFlash, err = segsFromJSON("color_flash", p.Tracks["color_flash"], be.ColorFlash, noFix[config.ColorFlashConfig]); err != nil {
		return nil, err
	}
	if tl.Visualizer, err = segsFromJSON("visualizer", p.Tracks["visualizer"], d.Visualizer, config.FixVisualizer); err != nil {
		return nil, err
	}
	if tl.Particles, err = segsFromJSON("particles", p.Tracks["particles"], d.Particles, config.FixParticles); err != nil {
		return nil, err
	}
	if tl.Lyrics, err = segsFromJSON("lyrics", p.Tracks["lyrics"], d.Lyrics, noFix[config.LyricsConfig]); err != nil {
		return nil, err
	}
	// The lyrics path and offset are global: the first segment sets them
	for i := range tl.Lyrics {
		tl.Lyrics[i].Cfg.Path, tl.Lyrics[i].Cfg.Offset = tl.Lyrics[0].Cfg.Path, tl.Lyrics[0].Cfg.Offset
	}
	for _, m := range p.Markers {
		c.Markers = append(c.Markers, config.Marker{At: m.At, Label: m.Label})
	}
	c.Studio = config.StudioView{Hidden: p.View.Hidden, Collapsed: p.View.Collapsed}
	for i, m := range p.Texts {
		t := config.DefaultText()
		if err := fromMap(m, &t); err != nil {
			return nil, fmt.Errorf("text %d: %w", i+1, err)
		}
		c.Texts = append(c.Texts, t)
	}
	for i, lj := range p.Layers {
		l := config.LayerConfig{ID: lj.ID, Name: lj.Name, Kind: lj.Kind}
		for j, m := range lj.Clips {
			var cl config.ClipConfig
			if err := fromMap(m, &cl); err != nil {
				return nil, fmt.Errorf("layer %d, clip %d: %w", i+1, j+1, err)
			}
			l.Clips = append(l.Clips, cl)
		}
		l.SortClips()
		c.Layers = append(c.Layers, l)
	}
	if err := c.SyncFromTimeline(); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}
