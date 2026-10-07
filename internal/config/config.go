package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectConfig represents the complete configuration of a project
type ProjectConfig struct {
	Audio             AudioConfig             `yaml:"audio"`
	Background        BackgroundConfig        `yaml:"background"`
	BackgroundEffects BackgroundEffectsConfig `yaml:"background_effects"`
	Visualizer        VisualizerConfig        `yaml:"visualizer"`
	Particles         ParticlesConfig         `yaml:"particles"`
	Lyrics            LyricsConfig            `yaml:"lyrics"`
	Texts             []TextConfig            `yaml:"texts,omitempty"`
	Output            OutputConfig            `yaml:"output"`
	// How the studio shows the project (no effect on the video)
	Studio StudioView `yaml:"studio,omitempty"`

	// Changes over the course of the song (see timeline.go). Timeline is built on load.
	TimelineRaw yaml.Node `yaml:"timeline,omitempty"`
	Timeline    Timeline  `yaml:"-"`
	Markers     []Marker  `yaml:"-"`
	// Project folder (relative paths in the YAML are resolved against it)
	Dir string `yaml:"-"`
}

// StudioView: the studio's view of the project. Hidden: entries whose timeline track is hidden
// (track ids, "texts"); Collapsed: folded zones of the track list and timeline ("main",
// "layers", "effects").
type StudioView struct {
	Hidden    []string `yaml:"hidden,omitempty"`
	Collapsed []string `yaml:"collapsed,omitempty"`
}

type AudioConfig struct {
	Path        string  `yaml:"path"`
	Volume      float64 `yaml:"volume"`      // Volume multiplier (1.0 = normal)
	Sensitivity float64 `yaml:"sensitivity"` // Bar height multiplier (1.0 = normal); effects have their own
	// Adaptive normalization: each effect and each bar is rescaled to its local range
	// so they don't stay saturated (or dead) when a band is always high/low.
	Normalize       bool    `yaml:"normalize"`
	NormalizeWindow float64 `yaml:"normalize_window"` // seconds on each side of the window (def. 4)
}

type BackgroundConfig struct {
	Path      string       `yaml:"path"`       // Path to an image (.png, .jpg) or video (.mp4, .mkv)
	FitMode   string       `yaml:"fit_mode"`   // "cover", "contain", "stretch"
	Opacity   float64      `yaml:"opacity"`    // 0.0 to 1.0 (to fade the background image)
	Darken    float64      `yaml:"darken"`     // 0.0 to 1.0 (constant base darkening)
	Vignette  float64      `yaml:"vignette"`   // 0.0 to 1.0 (dark vignette at the edges)
	LoopVideo bool         `yaml:"loop_video"` // Whether the background video loops
	Motion    MotionConfig `yaml:"motion"`     // Camera motion over the image during its segment
}

// MotionConfig: slow camera motion over a background segment (Ken Burns style), so a still
// image is not static. It runs over the whole time the segment is visible (its transitions
// included) and the image is scaled up just enough that no edge ever shows.
type MotionConfig struct {
	Type   string  `yaml:"type"`   // none, zoom_in, zoom_out, pan_left, pan_right, pan_up, pan_down, ken_burns, drift, sway
	Amount float64 `yaml:"amount"` // strength 0..1: extra zoom / travelled fraction (default 0.15)
	Easing string  `yaml:"easing"` // linear (constant speed) or ease (starts and ends softly)
	Speed  float64 `yaml:"speed"`  // drift and sway: cycles per 10 s (default 1)
	// ken_burns: corner the camera moves towards while zooming in: auto (a different one per
	// segment), top_left, top_right, bottom_left, bottom_right
	Direction string `yaml:"direction,omitempty"`
}

type BackgroundEffectsConfig struct {
	BassZoom            BassZoomConfig            `yaml:"bass_zoom"`
	Shake               ShakeConfig               `yaml:"shake"`
	ChromaticAberration ChromaticAberrationConfig `yaml:"chromatic_aberration"`
	Ripple              RippleConfig              `yaml:"ripple"`
	Glitch              GlitchConfig              `yaml:"glitch"`
	ColorFlash          ColorFlashConfig          `yaml:"color_flash"`
}

type BassZoomConfig struct {
	Enabled     bool    `yaml:"enabled"`
	MaxZoom     float64 `yaml:"max_zoom"`    // e.g. 1.08 = 8% maximum zoom
	Sensitivity float64 `yaml:"sensitivity"` // Bass reaction multiplier
	DecaySpeed  float64 `yaml:"decay_speed"` // Damping speed (0.1 to 0.5)
	ReactTo     string  `yaml:"react_to"`    // bass (def.), mid, treble, overall, transient, beat
}

// ShakeConfig: camera motion (smooth pseudo-random translation) driven by a band
type ShakeConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Amplitude float64 `yaml:"amplitude"` // maximum displacement in pixels
	Speed     float64 `yaml:"speed"`     // sway speed (1.0 = normal)
	ReactTo   string  `yaml:"react_to"`  // bass (def.), mid, treble, overall, transient, beat
}

type ChromaticAberrationConfig struct {
	Enabled  bool    `yaml:"enabled"`
	MaxShift float64 `yaml:"max_shift"` // Maximum R and B shift in pixels
	Mode     string  `yaml:"mode"`      // "horizontal" or "radial"
	ReactTo  string  `yaml:"react_to"`  // "bass", "treble", "transient", "overall"
}

type RippleConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Amplitude float64 `yaml:"amplitude"` // Maximum wave amplitude in pixels
	Frequency float64 `yaml:"frequency"` // Spatial frequency of the wave
	Speed     float64 `yaml:"speed"`     // Temporal propagation speed
	ReactTo   string  `yaml:"react_to"`  // mid (def.), bass, treble, overall, transient, beat
}

type GlitchConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Threshold float64 `yaml:"threshold"` // Transient energy threshold (0.0 to 1.0)
	Intensity float64 `yaml:"intensity"` // Strip displacement intensity
	ReactTo   string  `yaml:"react_to"`  // "" = hits/transients (def.); or a band that triggers when exceeding threshold
}

type ColorFlashConfig struct {
	Enabled         bool    `yaml:"enabled"`
	BrightnessBoost float64 `yaml:"brightness_boost"` // e.g. 0.20
	ContrastBoost   float64 `yaml:"contrast_boost"`   // e.g. 0.15
	ReactTo         string  `yaml:"react_to"`         // bass (def.), mid, treble, overall, transient, beat
}

type VisualizerConfig struct {
	Enabled         bool    `yaml:"enabled"`
	Type            string  `yaml:"type"`             // "circular", "horizontal", "vertical", "oscilloscope"
	StereoSymmetric bool    `yaml:"stereo_symmetric"` // true: L on the left, R on the right
	Bars            int     `yaml:"bars"`             // Bars per channel (e.g. 48 or 64)
	MinHeight       float64 `yaml:"min_height"`       // Minimum bar height
	MaxHeight       float64 `yaml:"max_height"`       // Maximum bar height at peak
	BarWidth        float64 `yaml:"bar_width"`        // Width or thickness of each bar
	BarGap          float64 `yaml:"bar_gap"`          // Gap between bars
	Roundness       float64 `yaml:"roundness"`        // Tip rounding
	Smoothing       float64 `yaml:"smoothing"`        // Weight of the new value when bars rise: higher = faster (0.75 default)
	DecayRate       float64 `yaml:"decay_rate"`       // Peak decay (0.05 to 0.3)

	// For type "circular":
	Radius   float64 `yaml:"radius"`    // Base radius of the circle
	CenterX  float64 `yaml:"center_x"`  // Relative position 0.0 to 1.0 (0.5 = center)
	CenterY  float64 `yaml:"center_y"`  // Relative position 0.0 to 1.0 (0.5 = center)
	DrawMode string  `yaml:"draw_mode"` // "bars", "smooth_curve", "filled"

	// For type "horizontal":
	PositionY    float64  `yaml:"position_y"`    // Relative Y position (e.g. 0.85 for bottom)
	MirrorBottom bool     `yaml:"mirror_bottom"` // Downward mirror
	CenterGap    *float64 `yaml:"center_gap"`    // Gap between L and R at the center (px, def. 2×bar_gap)

	// Colors and Glow
	ColorMode  string         `yaml:"color_mode"`  // "gradient", "solid"
	SolidColor string         `yaml:"solid_color"` // Hex e.g. "#00f0ff"
	Gradient   []GradientStop `yaml:"gradient"`
	// Gradient direction: "index" (def., across: one bar per color)
	// or "length" (along each bar: from base to tip)
	GradientAxis string `yaml:"gradient_axis"`
	// Only with gradient_axis: length. "bar" = last color always at the tip (moves with the bar);
	// "max" = last color is at max_height (short bars only show the first colors)
	GradientFit string     `yaml:"gradient_fit"`
	Glow        GlowConfig `yaml:"glow"`
}

type GradientStop struct {
	Offset float64 `yaml:"offset"` // 0.0 to 1.0
	Color  string  `yaml:"color"`  // Hex e.g. "#ff007f"
}

type GlowConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Radius    float64 `yaml:"radius"`    // Neon glow radius
	Intensity float64 `yaml:"intensity"` // Glow intensity / opacity
}

type ParticlesConfig struct {
	Enabled bool `yaml:"enabled"`
	// Layers: each with its own type, color and band. If empty, a single layer with the fields below is used.
	Layers []ParticleLayer `yaml:"layers"`

	ParticleLayer `yaml:",inline"`
}

type ParticleLayer struct {
	Type       string  `yaml:"type"`        // "radial_burst", "ambient_dust", "rising_sparks"
	Count      int     `yaml:"count"`       // Maximum number of particles in the layer
	MinSize    float64 `yaml:"min_size"`    // Minimum size
	MaxSize    float64 `yaml:"max_size"`    // Maximum size
	Color      string  `yaml:"color"`       // Hex "#ffffff" (color at birth)
	ColorEnd   string  `yaml:"color_end"`   // Optional hex: color at death (interpolated over lifetime)
	Speed      float64 `yaml:"speed"`       // Base speed
	AudioBoost float64 `yaml:"audio_boost"` // Speed multiplier driven by audio
	Glow       bool    `yaml:"glow"`        // Soft halo
	ReactTo    string  `yaml:"react_to"`    // bass (def.), mid, treble, overall, transient, beat
	// Reactive density: minimum fraction of live particles during silence (0..1).
	// 1 = always all of them (classic behaviour); 0.1 = almost none if the band is silent.
	MinDensity *float64 `yaml:"min_density"` // unset = 1
	Turbulence float64  `yaml:"turbulence"`  // Lateral wiggle (0 = straight; 0.5-1 = flame)
	MinLife    float64  `yaml:"min_life"`    // Minimum lifetime in seconds (def. 1)
	MaxLife    float64  `yaml:"max_life"`    // Maximum lifetime in seconds (def. 3)
	// Movement direction in degrees (0 = right, 90 = up, 180 = left, 270 = down).
	// rising_sparks: def. 90, spawning at the opposite edge; ambient_dust: common drift in that direction.
	Direction *float64 `yaml:"direction"`
	Spread    float64  `yaml:"spread"` // spread in degrees around direction (def. 25)
}

// LyricsConfig: synced lyrics (.srt / .lrc / LRC with per-word timing) drawn animated
type LyricsConfig struct {
	Enabled bool    `yaml:"enabled"`
	Path    string  `yaml:"path"`
	Offset  float64 `yaml:"offset"` // seconds added to all timestamps (+ = later)

	Font      string  `yaml:"font"`      // .ttf/.otf; empty = built-in font (Go Bold)
	Size      float64 `yaml:"size"`      // size in px
	Uppercase bool    `yaml:"uppercase"` // convert to uppercase
	// Position: top, bottom, center, left, right, side (alternates left/right per line)
	// or custom (uses position_x / position_y)
	Position    string  `yaml:"position"`
	Margin      float64 `yaml:"margin"`       // px margin to the edges (positions top/bottom/left/right/side)
	PositionX   float64 `yaml:"position_x"`   // relative center (0-1), only with position: custom
	PositionY   float64 `yaml:"position_y"`   // relative center (0-1), custom; or height for left/right/side
	MaxWidth    float64 `yaml:"max_width"`    // maximum relative width before wrapping into several rows
	LineSpacing float64 `yaml:"line_spacing"` // line spacing (1.0 = font height)
	WordSpacing float64 `yaml:"word_spacing"` // extra space between words, fraction of size (def. 0.12)
	Align       string  `yaml:"align"`        // center, left, right

	Mode string `yaml:"mode"` // karaoke, words, line
	// In/out animations for each line: fade, slide_up, slide_down, slide_left, slide_right,
	// slide_side (from its side), pop, zoom, none
	InAnimation   string  `yaml:"in_animation"`
	OutAnimation  string  `yaml:"out_animation"`
	Animation     string  `yaml:"animation"`      // shortcut: same for in and out
	InDuration    float64 `yaml:"in_duration"`    // s
	OutDuration   float64 `yaml:"out_duration"`   // s
	SlideDistance float64 `yaml:"slide_distance"` // px for animation: slide
	WordPop       float64 `yaml:"word_pop"`       // word scale boost while sung (0.15 = +15 %)

	Color         string         `yaml:"color"`          // color of the sung / visible word
	UpcomingColor string         `yaml:"upcoming_color"` // color of words not yet sung (karaoke), supports alpha #rrggbbaa
	Gradient      []GradientStop `yaml:"gradient"`       // optional: vertical gradient from top (0) to bottom (1) instead of color
	OutlineColor  string         `yaml:"outline_color"`
	OutlineWidth  float64        `yaml:"outline_width"` // px, 0 = no outline
	Glow          TextGlowConfig `yaml:"glow"`
	Shadow        ShadowConfig   `yaml:"shadow"`
	React         TextReact      `yaml:"react"`
}

// TextConfig: a fixed text (title, credits...) shown from Start to End at a chosen position.
// It has every style option of the lyrics (font, size, colors, gradient, outline, glow, shadow,
// animations, audio reaction); position_x / position_y place it (0..1, center of the block,
// or its left/right edge with align: left/right). Path, offset, position and mode are not used.
type TextConfig struct {
	Text         string  `yaml:"text"`  // several rows with line breaks
	Start        float64 `yaml:"start"` // s: starts entering
	End          float64 `yaml:"end"`   // s: has fully left
	Label        string  `yaml:"label,omitempty"`
	LyricsConfig `yaml:",inline"`
}

// DefaultText is a new text: white, centered, fading in and out
func DefaultText() TextConfig {
	l := DefaultConfig().Lyrics
	l.Enabled = true
	l.Size = 96
	l.Position = "custom"
	l.Mode = "line"
	l.InAnimation, l.OutAnimation = "fade", "fade"
	l.InDuration, l.OutDuration = 0.6, 0.6
	l.WordPop = 0
	l.React = TextReact{ReactTo: "bass"}
	return TextConfig{Start: 0, End: 5, LyricsConfig: l}
}

// UnmarshalYAML fills the fields missing in the YAML with DefaultText
func (t *TextConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain TextConfig
	d := plain(DefaultText())
	if err := n.Decode(&d); err != nil {
		return err
	}
	*t = TextConfig(d)
	return nil
}

// MarshalYAML leaves out the lyrics fields that a fixed text does not use
func (t TextConfig) MarshalYAML() (any, error) {
	type plain TextConfig
	var n yaml.Node
	if err := n.Encode(plain(t)); err != nil {
		return nil, err
	}
	unused := map[string]bool{"path": true, "offset": true, "position": true, "margin": true,
		"mode": true, "word_pop": true, "upcoming_color": true}
	var kept []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if unused[k] || (k == "animation" && t.Animation == "") {
			continue
		}
		kept = append(kept, n.Content[i], n.Content[i+1])
	}
	n.Content = kept
	return &n, nil
}

type TextGlowConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Color     string  `yaml:"color"`     // empty = text color
	Radius    float64 `yaml:"radius"`    // px (max. ~24)
	Intensity float64 `yaml:"intensity"` // 0-1+
}

type ShadowConfig struct {
	Enabled bool    `yaml:"enabled"`
	Color   string  `yaml:"color"`
	OffsetX float64 `yaml:"offset_x"`
	OffsetY float64 `yaml:"offset_y"`
	Opacity float64 `yaml:"opacity"`
	Blur    float64 `yaml:"blur"` // blur in px
}

// TextReact: how the lyrics react to the audio
type TextReact struct {
	ReactTo    string  `yaml:"react_to"`  // bass (def.), mid, treble, overall, transient, beat
	Scale      float64 `yaml:"scale"`     // pulse: +max scale (0.08 = +8 %)
	Glow       float64 `yaml:"glow"`      // extra glow intensity with the band
	Shake      float64 `yaml:"shake"`     // shake in px
	Chromatic  float64 `yaml:"chromatic"` // RGB split in px
	Flicker    float64 `yaml:"flicker"`   // flame-like brightness flicker (0-1)
	Flash      float64 `yaml:"flash"`     // how much it shifts toward flash_color with the band (0-1)
	FlashColor string  `yaml:"flash_color"`
}

type OutputConfig struct {
	Path         string `yaml:"path"`
	Width        int    `yaml:"width"`         // 0 = automatic (from background image/video)
	Height       int    `yaml:"height"`        // 0 = automatic
	FPS          int    `yaml:"fps"`           // e.g. 30 or 60
	Codec        string `yaml:"codec"`         // deprecated: always H.264 (built-in x264)
	Preset       string `yaml:"preset"`        // x264 preset: "ultrafast" ... "veryslow"
	Renderer     string `yaml:"renderer"`      // "auto" (Vulkan GPU if available), "gpu", "cpu"
	GPUDevice    int    `yaml:"gpu_device"`    // -1 = automatic
	CRF          int    `yaml:"crf"`           // 18-23
	AudioBitrate string `yaml:"audio_bitrate"` // "320k"
}

// DefaultConfig returns the recommended base configuration
func DefaultConfig() ProjectConfig {
	return ProjectConfig{
		Audio: AudioConfig{
			Volume:          1.0,
			Sensitivity:     1.0,
			Normalize:       true,
			NormalizeWindow: 4.0,
		},
		Background: BackgroundConfig{
			FitMode:   "cover",
			Opacity:   0.88,
			Darken:    0.15,
			Vignette:  0.25,
			LoopVideo: true,
			Motion:    MotionConfig{Type: "none", Amount: 0.15, Easing: "linear", Speed: 1},
		},
		BackgroundEffects: BackgroundEffectsConfig{
			BassZoom: BassZoomConfig{
				Enabled:     true,
				MaxZoom:     1.06,
				Sensitivity: 1.2,
				DecaySpeed:  0.15,
			},
			ChromaticAberration: ChromaticAberrationConfig{
				Enabled:  true,
				MaxShift: 10.0,
				Mode:     "radial",
				ReactTo:  "bass",
			},
			Ripple: RippleConfig{
				Enabled:   false,
				Amplitude: 6.0,
				Frequency: 0.04,
				Speed:     1.5,
			},
			Glitch: GlitchConfig{
				Enabled:   true,
				Threshold: 0.70,
				Intensity: 0.4,
			},
			ColorFlash: ColorFlashConfig{
				Enabled:         true,
				BrightnessBoost: 0.15,
				ContrastBoost:   0.10,
			},
		},
		Visualizer: VisualizerConfig{
			Enabled:         true,
			Type:            "circular",
			StereoSymmetric: true,
			Bars:            56,
			MinHeight:       4.0,
			MaxHeight:       200.0,
			BarWidth:        5.0,
			BarGap:          4.0,
			Roundness:       2.5,
			Smoothing:       0.75,
			DecayRate:       0.12,
			Radius:          220.0,
			CenterX:         0.5,
			CenterY:         0.5,
			DrawMode:        "bars",
			PositionY:       0.85,
			MirrorBottom:    true,
			ColorMode:       "gradient",
			SolidColor:      "#00f0ff",
			Gradient: []GradientStop{
				{Offset: 0.0, Color: "#00f0ff"}, // Cyan
				{Offset: 0.5, Color: "#9b51e0"}, // Purple
				{Offset: 1.0, Color: "#ff007f"}, // Magenta
			},
			Glow: GlowConfig{
				Enabled:   true,
				Radius:    12.0,
				Intensity: 0.65,
			},
		},
		Particles: ParticlesConfig{
			Enabled: true,
			ParticleLayer: ParticleLayer{
				Type:       "radial_burst",
				Count:      120,
				MinSize:    1.5,
				MaxSize:    5.0,
				Color:      "#ffffff",
				Speed:      1.5,
				AudioBoost: 3.0,
				Glow:       true,
			},
		},
		Lyrics: LyricsConfig{
			Size:          56,
			Position:      "bottom",
			Margin:        60,
			PositionX:     0.5,
			PositionY:     0.5,
			MaxWidth:      0.9,
			LineSpacing:   1.15,
			WordSpacing:   0.12,
			Align:         "center",
			Mode:          "karaoke",
			InAnimation:   "slide_up",
			OutAnimation:  "fade",
			InDuration:    0.3,
			OutDuration:   0.35,
			SlideDistance: 30,
			WordPop:       0.12,
			Color:         "#ffffff",
			UpcomingColor: "#ffffff66",
			OutlineColor:  "#000000",
			OutlineWidth:  2,
			Glow:          TextGlowConfig{Enabled: true, Radius: 12, Intensity: 0.6},
			Shadow:        ShadowConfig{Enabled: true, Color: "#000000", OffsetX: 3, OffsetY: 4, Opacity: 0.6, Blur: 4},
			React:         TextReact{ReactTo: "bass", Scale: 0.05},
		},
		Output: OutputConfig{
			Path:         "output.mp4",
			Width:        0, // 0 = infer from background
			Height:       0, // 0 = infer from background
			FPS:          60,
			Preset:       "fast",
			Renderer:     "auto",
			GPUDevice:    -1,
			CRF:          18,
			AudioBitrate: "320k",
		},
	}
}

// LoadConfig loads a YAML configuration file and applies default values
func LoadConfig(filePath string) (*ProjectConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error reading project file '%s': %w", filePath, err)
	}
	abs, _ := filepath.Abs(filePath)
	return ParseConfig(data, filepath.Dir(abs))
}

// ParseConfig parses a project YAML whose relative paths are rooted at dir
func ParseConfig(data []byte, dir string) (*ProjectConfig, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error parsing project YAML: %w", err)
	}
	cfg.Dir = dir
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.BuildTimeline(); err != nil {
		return nil, err
	}
	cfg.mapPaths(func(p string) string {
		if p == "" || isRooted(p) {
			return p
		}
		return filepath.Clean(filepath.Join(dir, p))
	})
	return &cfg, nil
}

// mapPaths applies fn to every file path in the project (base and segments)
func (c *ProjectConfig) mapPaths(fn func(string) string) {
	c.Audio.Path = fn(c.Audio.Path)
	c.Output.Path = fn(c.Output.Path)
	c.Background.Path = fn(c.Background.Path)
	c.Lyrics.Path, c.Lyrics.Font = fn(c.Lyrics.Path), fn(c.Lyrics.Font)
	for i := range c.Timeline.Background {
		c.Timeline.Background[i].Cfg.Path = fn(c.Timeline.Background[i].Cfg.Path)
	}
	for i := range c.Timeline.Lyrics {
		l := &c.Timeline.Lyrics[i].Cfg
		l.Path, l.Font = fn(l.Path), fn(l.Font)
	}
	for i := range c.Texts {
		c.Texts[i].Font = fn(c.Texts[i].Font)
	}
}

// Marshal serializes the project to YAML with paths relative to c.Dir when they are inside it
func (c *ProjectConfig) Marshal() ([]byte, error) {
	cp := *c
	if err := cp.SyncFromTimeline(); err != nil {
		return nil, err
	}
	// Deep copy of the tracks so the original's paths are not altered
	b, err := yaml.Marshal(&cp)
	if err != nil {
		return nil, err
	}
	tmp := DefaultConfig()
	if err := yaml.Unmarshal(b, &tmp); err != nil {
		return nil, err
	}
	rel := func(p string) string {
		if p == "" || c.Dir == "" || !isRooted(p) {
			return p
		}
		if r, err := filepath.Rel(c.Dir, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
		return p
	}
	tmp.Dir = c.Dir
	if err := tmp.BuildTimeline(); err != nil {
		return nil, err
	}
	tmp.mapPaths(rel)
	if err := tmp.SyncFromTimeline(); err != nil {
		return nil, err
	}
	return yaml.Marshal(&tmp)
}

// Save writes the project to path (atomically)
func (c *ProjectConfig) Save(path string) error {
	b, err := c.Marshal()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte("# AudioViz project (hand-editable; see docs/index.md)\n"), b...), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func lower(s string) string { return strings.ToLower(s) }

// Validate checks basic configuration consistency
func (c *ProjectConfig) Validate() error {
	if c.Output.Path == "" {
		c.Output.Path = "output.mp4"
	}
	if c.Output.FPS <= 0 {
		c.Output.FPS = 60
	}
	if c.Visualizer.Bars <= 0 {
		c.Visualizer.Bars = 48
	}
	if c.Audio.Volume <= 0 {
		c.Audio.Volume = 1.0
	}
	if c.Audio.Sensitivity <= 0 {
		c.Audio.Sensitivity = 1.0
	}
	if c.Audio.NormalizeWindow <= 0 {
		c.Audio.NormalizeWindow = 4.0
	}
	if c.Visualizer.Bars > 256 {
		c.Visualizer.Bars = 256
	}
	if len(c.Particles.Layers) == 0 {
		c.Particles.Layers = []ParticleLayer{c.Particles.ParticleLayer}
	}
	if c.Output.Renderer == "" {
		c.Output.Renderer = "auto"
	}
	c.Visualizer.Type = strings.ToLower(c.Visualizer.Type)
	return nil
}

// isRooted: an absolute path, or (Windows) one starting with a separator, which means the root
// of the current drive. Such paths are not resolved against the project folder.
func isRooted(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
}

// RewritePaths applies fn to every file path of the project (audio, output, backgrounds and
// lyrics of every segment, fonts of the lyrics and texts). Empty paths are left alone.
func (c *ProjectConfig) RewritePaths(fn func(string) string) {
	c.mapPaths(func(p string) string {
		if p == "" {
			return p
		}
		return fn(p)
	})
}

// Files lists the existing files the project uses (absolute paths, without duplicates): audio,
// backgrounds of every segment, lyrics and fonts. The output video is not included.
func (c *ProjectConfig) Files() []string {
	seen := map[string]bool{}
	var out []string
	out0 := c.Output.Path
	c.RewritePaths(func(p string) string {
		if p != out0 && !seen[p] {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				seen[p] = true
				out = append(out, p)
			}
		}
		return p
	})
	return out
}
