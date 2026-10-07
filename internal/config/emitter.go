package config

import "gopkg.in/yaml.v3"

// Emitters: a layer of kind "emitter" has clips that throw particles (fire, sparks, smoke...)
// instead of showing a file. The clip places the emitter: x, y (or its motion path) is where the
// particles are born, rotation turns the emitter (its shape and its direction), scale makes
// everything bigger (sizes, speeds, the shape); opacity, blend, filters, masks and the background
// effects apply to the particles as to any clip. Lengths are px of a 1080-line frame (scaled with
// the output) and times are seconds.
//
//	layers:
//	  - name: "Fire"
//	    kind: emitter
//	    clips:
//	      - start: 0
//	        end: 30
//	        x: 0.5
//	        y: 0.9
//	        blend: add
//	        emitter: { preset: fire }

// EmitterConfig is what a clip of an emitter layer throws
type EmitterConfig struct {
	Preset string `yaml:"preset,omitempty"` // the preset it started from (fire, sparks, smoke, dust, light, explosion, snow)
	// How many: per second, at once at the clip's start, and the most alive at a time
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
	Max   int     `yaml:"max"`
	// Where they are born: point, circle, ring, line, box; width / height of the shape (px)
	Shape  string  `yaml:"shape"`
	Width  float64 `yaml:"width"`
	Height float64 `yaml:"height"`
	// How they leave: direction (degrees, 0 = up, clockwise; turned with the clip), the spread of
	// the cone around it (degrees; 360 = every way), speed and its random part (0..1)
	Direction float64 `yaml:"direction"`
	Spread    float64 `yaml:"spread"`
	Speed     float64 `yaml:"speed"`
	SpeedVar  float64 `yaml:"speed_var"`
	// Forces: gravity (px/s², down; negative rises), wind (px/s², to the right), drag (1/s: how
	// fast they slow down), turbulence (px of wandering) and its frequency (Hz)
	Gravity    float64 `yaml:"gravity"`
	Wind       float64 `yaml:"wind"`
	Drag       float64 `yaml:"drag"`
	Turbulence float64 `yaml:"turbulence"`
	TurbFreq   float64 `yaml:"turb_freq"`
	// Life (s) and its random part (0..1)
	Life    float64 `yaml:"life"`
	LifeVar float64 `yaml:"life_var"`
	// Size (px) at birth and at the end, and its random part (0..1)
	Size    float64 `yaml:"size"`
	SizeEnd float64 `yaml:"size_end"`
	SizeVar float64 `yaml:"size_var"`
	// Colors over the life: at birth, halfway and at the end (#rrggbbaa: the alpha fades them)
	ColorStart string `yaml:"color_start"`
	ColorMid   string `yaml:"color_mid"`
	ColorEnd   string `yaml:"color_end"`
	// Fade in / out: the part of the life (0..1) they take to appear and to vanish
	FadeIn  float64 `yaml:"fade_in"`
	FadeOut float64 `yaml:"fade_out"`
	// How they add up: "add" (light: fire, sparks, glows) or "soft" (matter: smoke, dust, snow)
	Mode string `yaml:"mode"`
	// Streak: stretched along their motion (s of motion; sparks), and how soft their edge is (0..1)
	Streak   float64      `yaml:"streak"`
	Softness float64      `yaml:"softness"`
	Seed     int          `yaml:"seed"`
	React    EmitterReact `yaml:"react"`
}

// EmitterReact: the music drives the emitter (each value: how much at full band)
type EmitterReact struct {
	ReactTo string  `yaml:"react_to"` // bass (default), mid, treble, overall, transient, beat
	Rate    float64 `yaml:"rate"`     // more particles (1 = twice as many at full band)
	Speed   float64 `yaml:"speed"`    // faster at birth
	Size    float64 `yaml:"size"`     // bigger at birth
	Burst   int     `yaml:"burst"`    // particles at once on each beat
}

// EmitterPresets: complete settings of each preset (a new emitter clip starts from one)
var EmitterPresets = map[string]EmitterConfig{
	"fire": {Rate: 650, Max: 9000, Shape: "line", Width: 200, Direction: 0, Spread: 12, Speed: 210, SpeedVar: 0.45,
		Gravity: -430, Drag: 0.9, Turbulence: 26, TurbFreq: 1.7, Life: 1.25, LifeVar: 0.35, Size: 52, SizeEnd: 8, SizeVar: 0.35,
		ColorStart: "#ffcf7048", ColorMid: "#ff561048", ColorEnd: "#5a060000", FadeIn: 0.12, FadeOut: 0.5, Mode: "add", Softness: 1,
		React: EmitterReact{ReactTo: "bass", Rate: 1, Speed: 0.6, Size: 0.35}},
	"sparks": {Rate: 90, Max: 4000, Shape: "point", Direction: 0, Spread: 70, Speed: 620, SpeedVar: 0.5, Gravity: 520, Drag: 0.4,
		Turbulence: 6, TurbFreq: 3, Life: 1.6, LifeVar: 0.5, Size: 4, SizeEnd: 1.5, SizeVar: 0.4,
		ColorStart: "#ffffffff", ColorMid: "#ffc040ff", ColorEnd: "#ff400000", FadeIn: 0, FadeOut: 0.35, Mode: "add", Streak: 0.03, Softness: 0.4,
		React: EmitterReact{ReactTo: "transient", Rate: 2, Speed: 0.6, Burst: 60}},
	"smoke": {Rate: 18, Max: 1500, Shape: "circle", Width: 90, Direction: 0, Spread: 18, Speed: 110, SpeedVar: 0.35, Gravity: -30, Wind: 35, Drag: 0.35,
		Turbulence: 70, TurbFreq: 0.35, Life: 7, LifeVar: 0.3, Size: 70, SizeEnd: 340, SizeVar: 0.35,
		ColorStart: "#4a4a4a00", ColorMid: "#5c5c5c48", ColorEnd: "#70707000", FadeIn: 0.15, FadeOut: 0.65, Mode: "soft", Softness: 1,
		React: EmitterReact{ReactTo: "bass", Rate: 0.5}},
	"dust": {Rate: 70, Max: 4000, Shape: "box", Width: 1920, Height: 1080, Direction: 0, Spread: 360, Speed: 14, SpeedVar: 0.8, Wind: 4,
		Turbulence: 30, TurbFreq: 0.25, Life: 8, LifeVar: 0.5, Size: 4.5, SizeEnd: 4.5, SizeVar: 0.6,
		ColorStart: "#ffe8c000", ColorMid: "#ffe8c0e0", ColorEnd: "#ffe8c000", FadeIn: 0.3, FadeOut: 0.3, Mode: "add", Softness: 0.6,
		React: EmitterReact{ReactTo: "bass", Size: 0.6}},
	"light": {Rate: 9, Max: 300, Shape: "box", Width: 1920, Height: 1080, Direction: 0, Spread: 360, Speed: 15, SpeedVar: 0.8,
		Turbulence: 40, TurbFreq: 0.15, Life: 7, LifeVar: 0.4, Size: 50, SizeEnd: 75, SizeVar: 0.7,
		ColorStart: "#ffd08000", ColorMid: "#ffb060a8", ColorEnd: "#ff80c000", FadeIn: 0.35, FadeOut: 0.4, Mode: "add", Softness: 0.45,
		React: EmitterReact{ReactTo: "bass", Size: 0.4}},
	"explosion": {Rate: 0, Burst: 900, Max: 9000, Shape: "circle", Width: 40, Direction: 0, Spread: 360, Speed: 1000, SpeedVar: 0.7, Gravity: 120, Drag: 2.6,
		Turbulence: 30, TurbFreq: 1.4, Life: 1.6, LifeVar: 0.5, Size: 44, SizeEnd: 8, SizeVar: 0.6,
		ColorStart: "#ffffe0c0", ColorMid: "#ff8a2090", ColorEnd: "#40100000", FadeIn: 0, FadeOut: 0.6, Mode: "add", Softness: 1,
		React: EmitterReact{ReactTo: "beat", Burst: 400}},
	"snow": {Rate: 70, Max: 6000, Shape: "line", Width: 2100, Direction: 180, Spread: 20, Speed: 70, SpeedVar: 0.5, Gravity: 20, Wind: 10, Drag: 0.3,
		Turbulence: 35, TurbFreq: 0.3, Life: 14, LifeVar: 0.2, Size: 5, SizeEnd: 5, SizeVar: 0.6,
		ColorStart: "#ffffffe0", ColorMid: "#ffffffe0", ColorEnd: "#ffffff00", FadeIn: 0.02, FadeOut: 0.1, Mode: "soft", Softness: 0.6},
}

// UnmarshalYAML fills the fields missing in the YAML from its preset (fire if none)
func (e *EmitterConfig) UnmarshalYAML(n *yaml.Node) error {
	var p struct {
		Preset string `yaml:"preset"`
	}
	if err := n.Decode(&p); err != nil {
		return err
	}
	type plain EmitterConfig
	d, ok := EmitterPresets[p.Preset]
	if !ok {
		d = DefaultEmitter()
	}
	pd := plain(d)
	if err := n.Decode(&pd); err != nil {
		return err
	}
	*e = EmitterConfig(pd)
	return nil
}

// DefaultEmitter: the fire preset
func DefaultEmitter() EmitterConfig { return EmitterPresets["fire"] }

// IsEmitter: a layer of particle emitters
func (l *LayerConfig) IsEmitter() bool { return l.Kind == "emitter" }
