package config

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// Overlay layers: images, GIFs or videos drawn over the background, as many as wanted. Each layer
// is a track of clips (a clip shows one file from Start to End). The list order is the drawing
// order: the first layer is drawn first (just above the background), the last one on top. The
// visualizer, the particles, the lyrics and each text have a z_index that places them among the
// layers: z = k draws them right above layer k (0 = right above the background); unset = above all.
//
//	layers:
//	  - name: "Logo"
//	    clips:
//	      - path: logo.png
//	        start: 0
//	        end: 30
//	        x: 0.85
//	        y: 0.12
//	        scale: 0.25
//	        blend: screen
//	        keys:
//	          rotation: [{ t: 0, v: 0 }, { t: 30, v: 360 }]

// LayerConfig is one layer (track) of the stack
type LayerConfig struct {
	ID    string       `yaml:"id,omitempty"` // stable id (the studio's view of its track)
	Name  string       `yaml:"name"`
	Kind  string       `yaml:"kind,omitempty"` // "" = images / GIFs / videos; "emitter" = particles (see EmitterConfig)
	Clips []ClipConfig `yaml:"clips"`
}

// ClipConfig is one file shown on a layer from Start to End
type ClipConfig struct {
	Path  string  `yaml:"path"`
	Start float64 `yaml:"start"` // s
	End   float64 `yaml:"end"`   // s
	Label string  `yaml:"label,omitempty"`
	// Placement: Fit gives the base size (contain: inside the frame, cover: covers it, stretch:
	// exactly the frame, none: the file's own pixels), then Scale; X, Y are the center (0..1 of
	// the frame); Rotation in degrees
	Fit      string  `yaml:"fit"`
	X        float64 `yaml:"x"`
	Y        float64 `yaml:"y"`
	Scale    float64 `yaml:"scale"`
	Rotation float64 `yaml:"rotation"`
	FlipX    bool    `yaml:"flip_x"`
	FlipY    bool    `yaml:"flip_y"`
	Opacity  float64 `yaml:"opacity"`
	FadeIn   float64 `yaml:"fade_in"`  // s of opacity ramp at the start
	FadeOut  float64 `yaml:"fade_out"` // s at the end
	Blend    string  `yaml:"blend"`    // normal, add, multiply, screen, overlay, lighten, darken
	// Filters (0 = unchanged): brightness and contrast -1..1, saturation -1 (gray) .. 1, hue in
	// degrees, blur in pixels
	Brightness float64      `yaml:"brightness"`
	Contrast   float64      `yaml:"contrast"`
	Saturation float64      `yaml:"saturation"`
	Hue        float64      `yaml:"hue"`
	Blur       float64      `yaml:"blur"`
	Chroma     ChromaKey    `yaml:"chroma"`
	React      LayerReact   `yaml:"react"`
	Video      ClipVideo    `yaml:"video"`
	Effects    LayerEffects `yaml:"effects"`
	// Masks: where the clip is seen (all of them apply)
	Masks []MaskConfig `yaml:"masks,omitempty"`
	// Motion along a path (instead of x, y)
	Motion Motion `yaml:"motion,omitempty"`
	// Particles (clips of an emitter layer; Path is not used)
	Emitter *EmitterConfig `yaml:"emitter,omitempty"`
	// Keyframes (times from Start): any of the numbers and colors above, e.g. x, scale, opacity
	Keys Keys `yaml:"keys,omitempty"`
}

// MaskConfig limits where a clip is seen. It belongs to the clip (moves, scales and turns with
// it): its coordinates are 0..1 of the clip's image.
type MaskConfig struct {
	ID      string      `yaml:"id,omitempty"`
	Kind    string      `yaml:"kind"`             // vector (shapes) or raster (a painted PNG)
	Mode    string      `yaml:"mode"`             // inside: the clip is only seen inside; outside: hidden inside
	Feather float64     `yaml:"feather"`          // soft edge, px of the clip's image
	Opacity float64     `yaml:"opacity"`          // strength 0..1
	Off     bool        `yaml:"off,omitempty"`    // turned off
	Shapes  []MaskShape `yaml:"shapes,omitempty"` // vector: closed shapes (their union)
	Path    string      `yaml:"path,omitempty"`   // raster: PNG, white / opaque = inside
	Name    string      `yaml:"name,omitempty"`
}

// MaskShape is a shape through its points ([x, y] a smooth node, [x, y, 1] a corner, see
// effects.NodeHandles). Open: a line with two ends, not an area (it does not mask until closed).
type MaskShape struct {
	Points [][]float64 `yaml:"points"`
	Open   bool        `yaml:"open,omitempty"`
	Group  string      `yaml:"group,omitempty"` // shapes with the same group are selected and moved together
}

// Motion moves a clip or a text along a path drawn on the screen: its center goes to the point at
// Progress of the path's length (0 = first node, 1 = last; a closed path loops past 1), so keys of
// progress with their curves give the motion. Orient turns it with the path's direction (added to
// its rotation; 0° = moving right).
type Motion struct {
	Path     *MaskShape `yaml:"path,omitempty"` // 0..1 of the frame, nodes as in a mask
	Progress float64    `yaml:"progress"`
	Orient   bool       `yaml:"orient,omitempty"`
}

// On: it has a path to follow
func (m *Motion) On() bool { return m.Path != nil && len(m.Path.Points) >= 2 }

// ChromaKey makes a color (green screen) transparent
type ChromaKey struct {
	Enabled   bool    `yaml:"enabled"`
	Color     string  `yaml:"color"`     // the key color (default #00ff00)
	Tolerance float64 `yaml:"tolerance"` // 0..1 how far from the color is still removed
	Softness  float64 `yaml:"softness"`  // 0..1 soft edge
}

// LayerReact: the clip pulses with a band of the music
type LayerReact struct {
	ReactTo string  `yaml:"react_to"` // bass (default), mid, treble, overall, transient, beat
	Opacity float64 `yaml:"opacity"`  // 0..1: how much the opacity follows the band (1 = only visible on hits)
	Scale   float64 `yaml:"scale"`    // extra scale at full band (0.1 = +10 %)
}

// ClipVideo: what part of a video (or GIF) is shown and how fast
type ClipVideo struct {
	Trim  float64 `yaml:"trim"`  // s of the file skipped at the start
	Speed float64 `yaml:"speed"` // 1 = normal
	Loop  bool    `yaml:"loop"`
}

// LayerEffects: for each background effect, whether the clip moves with the background
// ("inherit", default), has its own settings ("own") or ignores it ("off")
type LayerEffects struct {
	Zoom      LayerEffect[BassZoomConfig]            `yaml:"bass_zoom"`
	Shake     LayerEffect[ShakeConfig]               `yaml:"shake"`
	Chromatic LayerEffect[ChromaticAberrationConfig] `yaml:"chromatic_aberration"`
	Ripple    LayerEffect[RippleConfig]              `yaml:"ripple"`
	Glitch    LayerEffect[GlitchConfig]              `yaml:"glitch"`
	Flash     LayerEffect[ColorFlashConfig]          `yaml:"color_flash"`
}

// LayerEffect is the mode of one effect on a clip and, for "own", its settings
type LayerEffect[T any] struct {
	Mode string `yaml:"mode"` // inherit (default), own, off
	Own  T      `yaml:"own"`
}

// Blends are the blend modes of a clip
var Blends = []string{"normal", "add", "multiply", "screen", "overlay", "lighten", "darken"}

// BlendIndex is the position of the blend mode in Blends (unknown = normal)
func (c *ClipConfig) BlendIndex() int {
	for i, b := range Blends {
		if b == c.Blend {
			return i
		}
	}
	return 0
}

// EffectMode: 0 inherit, 1 own, 2 off
func EffectMode(m string) int {
	switch m {
	case "own":
		return 1
	case "off":
		return 2
	}
	return 0
}

// DefaultClip is a new clip: centered, inside the frame, the background's effects
func DefaultClip() ClipConfig {
	d := DefaultConfig().BackgroundEffects
	return ClipConfig{Fit: "contain", X: 0.5, Y: 0.5, Scale: 1, Opacity: 1, Blend: "normal",
		Chroma: ChromaKey{Color: "#00ff00", Tolerance: 0.3, Softness: 0.1},
		React:  LayerReact{ReactTo: "bass"},
		Video:  ClipVideo{Speed: 1, Loop: true},
		Effects: LayerEffects{Zoom: LayerEffect[BassZoomConfig]{Own: d.BassZoom}, Shake: LayerEffect[ShakeConfig]{Own: d.Shake},
			Chromatic: LayerEffect[ChromaticAberrationConfig]{Own: d.ChromaticAberration}, Ripple: LayerEffect[RippleConfig]{Own: d.Ripple},
			Glitch: LayerEffect[GlitchConfig]{Own: d.Glitch}, Flash: LayerEffect[ColorFlashConfig]{Own: d.ColorFlash}}}
}

// UnmarshalYAML fills the fields missing in the YAML with DefaultClip
func (c *ClipConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain ClipConfig
	d := plain(DefaultClip())
	if err := n.Decode(&d); err != nil {
		return err
	}
	*c = ClipConfig(d)
	c.Keys.Sort()
	return nil
}

// SortClips orders each layer's clips by start
func (l *LayerConfig) SortClips() {
	sort.SliceStable(l.Clips, func(i, j int) bool { return l.Clips[i].Start < l.Clips[j].Start })
}

// ClipAt is the clip of the layer visible at t (nil if none)
func (l *LayerConfig) ClipAt(t float64) *ClipConfig {
	for i := range l.Clips {
		c := &l.Clips[i]
		if t >= c.Start && t < c.End {
			return c
		}
	}
	return nil
}

// UnmarshalYAML fills the fields missing in the YAML: a vector mask, the clip seen inside, full strength
func (m *MaskConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain MaskConfig
	d := plain(MaskConfig{Kind: "vector", Mode: "inside", Opacity: 1})
	if err := n.Decode(&d); err != nil {
		return err
	}
	*m = MaskConfig(d)
	return nil
}
