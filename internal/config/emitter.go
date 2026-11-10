package config

import (
	"math"

	"gopkg.in/yaml.v3"
)

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
	// Where they are born: point, circle, ring, line, box, text; width / height of the shape (px)
	Shape  string  `yaml:"shape"`
	Width  float64 `yaml:"width"`
	Height float64 `yaml:"height"`
	// Shape text: born on the letters of a fixed text (its index in texts), on their outline or
	// all over them (fill); they follow the text as it moves, turns and grows
	Text     int    `yaml:"text,omitempty"`
	TextPart string `yaml:"text_part,omitempty"`
	// They travel along the clip's motion path (at their speed), from where the emitter is on it
	FollowPath bool `yaml:"follow_path,omitempty"`
	// Their own route: they go along it while they live (at their speed), from its start moved to
	// where they are born (or from where it is drawn: RouteFixed); 0..1 of the frame, as a path
	Route      *MaskShape `yaml:"route,omitempty"`
	RouteFixed bool       `yaml:"route_fixed,omitempty"`
	// how much they follow it: 0 as if there were none (their launch and the forces), 1 only the
	// route (as far along it as their launch and the forces would take them; turbulence stays),
	// between a mix of both
	RouteStrength float64 `yaml:"route_strength"`
	// A floor they bounce on: px below the emitter (0 = none) or the scene's (FloorDepth: where
	// the depth below the emitter comes up to its own); how much speed they keep at each bounce
	// (0..1) and how much of their sideways speed (friction, 0..1)
	Floor      float64 `yaml:"floor,omitempty"`
	FloorDepth bool    `yaml:"floor_depth,omitempty"`
	Bounce     float64 `yaml:"bounce,omitempty"`
	Friction   float64 `yaml:"friction,omitempty"`
	// With a depth map: they go round what is in the scene at their depth (within CollideBand
	// of the emitter's depth: nearer things hide them, farther ones are behind), sliding off it
	Collide     bool    `yaml:"collide,omitempty"`
	CollideBand float64 `yaml:"collide_band,omitempty"`
	// how they come off it: 0 slide along it; more, they bounce off walls and ceilings keeping
	// that much speed (on what faces up they still slide: the floor above bounces them)
	CollideBounce float64 `yaml:"collide_bounce,omitempty"`
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
	// Look: "plain" (each particle as it is), "flame" (they melt into one flame: the color
	// comes from how much heat piles up, and rising noise cuts its edges into tongues), "smoke",
	// "water" (they melt into one liquid with a lit surface: WaterLook), "gas" (they are not
	// drawn: they feed a simulated gas, smoke and fire: GasLook) or "liquid" (they are born as
	// drops of a simulated liquid, drawn as the water look: LiquidLook)
	Look string `yaml:"look"`
	// Only its light (and its glow): the particles themselves are not drawn (a light source)
	Hidden bool      `yaml:"hidden,omitempty"`
	Flame  FlameLook `yaml:"flame"`
	Smoke  SmokeLook `yaml:"smoke,omitempty"`
	Water  WaterLook `yaml:"water,omitempty"`
	Gas    GasLook   `yaml:"gas,omitempty"`
	// Sim: where the gas or the liquid is simulated: "" / "auto" (the gas on the GPU unless
	// rendering on the CPU, the liquid on the CPU), "gpu" or "cpu" (they move a little
	// differently: changing it simulates it again)
	Sim    string     `yaml:"sim,omitempty"`
	Liquid LiquidLook `yaml:"liquid,omitempty"`
	Glow   EmitGlow   `yaml:"glow"`
	Light  EmitLight  `yaml:"light"`
}

// EmitLight: the emitter lights up what is drawn behind it (the background and the layers below
// it), from where its particles are: how strongly, how far (px), its color (empty = the
// particles' own; the flame's body with the flame look), how ("soft": it lights and tints what is
// there, like a real light; "add": it adds its color, brighter, like neon) and how much it
// flickers (0..1)
type EmitLight struct {
	Enabled   bool    `yaml:"enabled"`
	Intensity float64 `yaml:"intensity"`
	Reach     float64 `yaml:"reach"`
	End       float64 `yaml:"end"` // px where it has faded out completely (0 = 2.5 × reach)
	Color     string  `yaml:"color"`
	Mode      string  `yaml:"mode"`
	Flicker   float64 `yaml:"flicker"`
	// The music (the emitter's Reacts to band): React adds to its strength (1 = twice as strong
	// at full band), Quiet dims it where the band is low (1 = dark in silence)
	// ReactTo: its own band ("" = the emitter's, Audio reaction → Reacts to)
	React   float64 `yaml:"react,omitempty"`
	Quiet   float64 `yaml:"quiet,omitempty"`
	ReactTo string  `yaml:"react_to,omitempty"`
	// With a background that has depth: how fast it fades with the difference of depth (0 = not
	// at all, flat as in 2D) and how much the surfaces are shaded by where they face (0..1)
	Depth float64 `yaml:"depth"`
	Shade float64 `yaml:"shade"`
	// With depth: it only lights from From (what is nearer is in front of it) to To (what is
	// farther is too far), 0 = the farthest of the scene … 1 = the nearest, with a soft edge
	From      float64 `yaml:"from"`
	To        float64 `yaml:"to"`
	RangeSoft float64 `yaml:"range_soft"`
	// With depth, rim light: farther than RimFrom px (soft by RimSoft px) it no longer lights the
	// surfaces but only the edges of things that face it (where the scene drops behind them
	// towards the light by RimEdge or more), RimWidth px wide, RimGain times as bright
	Rim      bool    `yaml:"rim,omitempty"`
	RimFrom  float64 `yaml:"rim_from,omitempty"`
	RimSoft  float64 `yaml:"rim_soft,omitempty"`
	RimWidth float64 `yaml:"rim_width,omitempty"`
	RimGain  float64 `yaml:"rim_gain,omitempty"`
	RimEdge  float64 `yaml:"rim_edge,omitempty"`
	// With depth: specular shine on the surfaces that face it (0..1) and how dark the shadows
	// that nearer things cast away from it (0..1)
	Gloss   float64 `yaml:"gloss,omitempty"`
	Shadows float64 `yaml:"shadows,omitempty"`
}

// FlameLook: the colors of the flame from cool (its edges) to hot (its core), how fast it gets
// hot, and its tongues: how far the noise moves it (px), the size of its whirls (px), how fast
// they rise (px/s) and how much they eat its edges (0..1)
// SmokeLook (look "smoke"): soft matter torn into wisps by noise: how much (0..1), the size of the
// wisps (px), how fast the pattern rises (px/s) and how far the noise moves the smoke (px)
type SmokeLook struct {
	Wisp  float64 `yaml:"wisp"`
	Scale float64 `yaml:"scale"`
	Speed float64 `yaml:"speed"`
	Warp  float64 `yaml:"warp"`
}

// WaterLook (look "water"): the particles melt into one liquid with a surface: where it starts
// (how much of them piles up, 0.1..2), how clear it is (0 opaque … 1 you see through its body;
// its edges stay), its shine (highlights), how rounded it looks (bump), and ripples moving it
// (px, their size in px and speed px/s)
type WaterLook struct {
	Threshold   float64 `yaml:"threshold"`
	Clear       float64 `yaml:"clear"`
	Shine       float64 `yaml:"shine"`
	Bump        float64 `yaml:"bump"`
	Ripple      float64 `yaml:"ripple"`
	RippleScale float64 `yaml:"ripple_scale"`
	RippleSpeed float64 `yaml:"ripple_speed"`
	// a lens: what is under it is seen through it, bent by its surface (px; 0 = none): drops
	// magnify what is behind them, and turn it upside down when strong
	Lens float64 `yaml:"lens,omitempty"`
}

// GasLook (look "gas"): the particles are not drawn, they feed a gas simulated over the frame
// (effects.GasSim) that then moves by itself. The particles stay the source: where, how many,
// how fast and how big they are, and their color (the smoke's). Lengths in px (of a 1080-line
// frame), times in s.
type GasLook struct {
	// the simulation's cells (px): smaller is finer and slower
	Detail float64 `yaml:"detail"`
	// how fast the gas lives (1 = real time, 2 = twice as fast, 0.5 = slow motion); its sources
	// keep their pace
	Speed float64 `yaml:"speed,omitempty"`
	// what each particle gives off per second: smoke, heat (it rises), fuel (it burns: heat and
	// soot), and how much it pushes the gas its way (0..1)
	Smoke float64 `yaml:"smoke"`
	Heat  float64 `yaml:"heat"`
	Fuel  float64 `yaml:"fuel"`
	Push  float64 `yaml:"push"`
	// how fast heat rises and smoke sinks (px/s² per unit), the swirls (0..3), the wind (px/s,
	// to the right), the air's turbulence (px/s) and the size of its whirls (px)
	Rise       float64 `yaml:"rise"`
	Weight     float64 `yaml:"weight"`
	Swirl      float64 `yaml:"swirl"`
	Wind       float64 `yaml:"wind"`
	Turbulence float64 `yaml:"turbulence"`
	TurbScale  float64 `yaml:"turb_scale"`
	// how fast smoke vanishes, heat is lost, fuel burns (1/s); the smoke a unit of fuel makes and
	// its color
	Fade float64 `yaml:"fade"`
	Cool float64 `yaml:"cool"`
	Burn float64 `yaml:"burn"`
	// how much the smoke spreads out by itself (px²/s): fuller, softer
	Spread    float64 `yaml:"spread"`
	Soot      float64 `yaml:"soot"`
	SootColor string  `yaml:"soot_color"`
	// how it looks: how thick (per unit of smoke), its wisps (how much 0..1, their size px), the
	// light (where it comes from: degrees, 0 = from above, clockwise; its color), the color in
	// its shadow, how dark its own shadow gets (0..2) and how much its fire lights it
	Opacity     float64 `yaml:"opacity"`
	Wisps       float64 `yaml:"wisps"`
	WispScale   float64 `yaml:"wisp_scale"`
	LightAngle  float64 `yaml:"light_angle"`
	LightColor  string  `yaml:"light_color"`
	ShadowColor string  `yaml:"shadow_color"`
	Shadow      float64 `yaml:"shadow"`
	FireLight   float64 `yaml:"fire_light"`
}

// DefaultGas: cool smoke rising slowly
func DefaultGas() GasLook {
	return GasLook{Detail: 7, Smoke: 0.2, Heat: 0.4, Push: 0.1, Rise: 250, Swirl: 2.5, Turbulence: 120, TurbScale: 140,
		Fade: 0.12, Cool: 3, Burn: 2.5, Soot: 0.6, SootColor: "#2a2623", Spread: 300,
		Opacity: 1.6, Wisps: 0.4, WispScale: 60, LightColor: "#fff2e0", ShadowColor: "#66707e", Shadow: 0.6, FireLight: 1}
}

// LiquidLook (look "liquid"): the particles are born as drops of a liquid simulated over the
// frame (effects.LiquidSim): the emitter's gravity and wind pull it, it keeps its volume (it
// piles up into pools, splashes and flows round things), its floor and, with collide, the scene
// at its depth hold it. Drawn with the water look (Water). The drops live until they leave the
// frame (at most Max of them: the oldest go).
type LiquidLook struct {
	// the simulation's cells (px of a 1080-line frame): smaller is finer and slower
	Detail float64 `yaml:"detail"`
	// how lively: 1 splashes and keeps every ripple, 0 calm and thick like honey
	Splash float64 `yaml:"splash"`
	// how fast it lives (1 = real time)
	Speed float64 `yaml:"speed,omitempty"`
}

// DefaultLiquid: water at real speed
func DefaultLiquid() LiquidLook { return LiquidLook{Detail: 6, Splash: 0.95, Speed: 1} }

// DefaultWater: the water look's settings
func DefaultWater() WaterLook {
	return WaterLook{Threshold: 0.5, Clear: 0.7, Shine: 1, Bump: 1, Ripple: 3, RippleScale: 40, RippleSpeed: 50}
}

type FlameLook struct {
	ColorCool  string  `yaml:"color_cool"`
	ColorMid   string  `yaml:"color_mid"`
	ColorHot   string  `yaml:"color_hot"`
	Heat       float64 `yaml:"heat"`
	Noise      float64 `yaml:"noise"`
	NoiseScale float64 `yaml:"noise_scale"`
	NoiseSpeed float64 `yaml:"noise_speed"`
	Edge       float64 `yaml:"edge"`
}

// EmitGlow: a halo of light around the particles: strength (0 = none), reach (px) and its color
// (empty = the particles' own)
type EmitGlow struct {
	Intensity float64 `yaml:"intensity"`
	Radius    float64 `yaml:"radius"`
	Color     string  `yaml:"color"`
}

// DefaultFlame: a natural fire (red edges, orange body, white-yellow core)
func DefaultFlame() FlameLook {
	return FlameLook{ColorCool: "#b8200a", ColorMid: "#ff8a1e", ColorHot: "#fff4d0", Heat: 1.1, Noise: 18, NoiseScale: 70, NoiseSpeed: 240, Edge: 0.5}
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
	// fire with the flame look and a glow: one flame instead of single particles
	"fire_real": {Rate: 480, Max: 9000, Shape: "circle", Width: 120, Direction: 0, Spread: 8, Speed: 200, SpeedVar: 0.4,
		Gravity: -440, Drag: 0.9, Turbulence: 18, TurbFreq: 1.6, Life: 1.25, LifeVar: 0.35, Size: 56, SizeEnd: 12, SizeVar: 0.35,
		ColorStart: "#ffffff50", ColorMid: "#ffffff48", ColorEnd: "#ffffff00", FadeIn: 0.25, FadeOut: 0.55, Mode: "add", Softness: 1,
		React: EmitterReact{ReactTo: "bass", Rate: 0.8, Speed: 0.5, Size: 0.3},
		Look:  "flame", Flame: FlameLook{ColorCool: "#a8180600", ColorMid: "#ff7616", ColorHot: "#fff1c4", Heat: 0.95, Noise: 24, NoiseScale: 55,
			NoiseSpeed: 280, Edge: 0.6}, Glow: EmitGlow{Intensity: 0.9, Radius: 110, Color: "#ff5a12"}},
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
	"lamp": {Rate: 60, Max: 300, Shape: "point", Direction: 0, Spread: 360, Speed: 0, Life: 0.5, LifeVar: 0.2,
		Size: 40, SizeEnd: 40, ColorStart: "#ffd8a0ff", ColorMid: "#ffd8a0ff", ColorEnd: "#ffd8a000", FadeIn: 0.1, FadeOut: 0.3,
		Mode: "add", Softness: 1, Hidden: true, Glow: EmitGlow{Intensity: 0.7, Radius: 90}},
	// water: a jet that melts into one liquid, falls and splashes on a floor
	"water": {Rate: 320, Max: 4000, Shape: "circle", Width: 14, Direction: 0, Spread: 9, Speed: 950, SpeedVar: 0.12, Gravity: 1500, Drag: 0.1,
		Turbulence: 12, TurbFreq: 1, Life: 1.7, LifeVar: 0.2, Size: 34, SizeEnd: 26, SizeVar: 0.3,
		ColorStart: "#6cbcffd8", ColorMid: "#4a98ecd8", ColorEnd: "#2f74c800", FadeIn: 0, FadeOut: 0.15, Mode: "soft", Softness: 1,
		Floor: 320, Bounce: 0.25, Friction: 0.6, Look: "water", Water: DefaultWater(),
		React: EmitterReact{ReactTo: "bass", Speed: 0.25}},
	// a splash of water on each beat
	"splash": {Rate: 0, Burst: 260, Max: 4000, Shape: "point", Direction: 0, Spread: 130, Speed: 720, SpeedVar: 0.45, Gravity: 1700, Drag: 0.3,
		Turbulence: 8, TurbFreq: 1.5, Life: 1.3, LifeVar: 0.3, Size: 24, SizeEnd: 12, SizeVar: 0.5,
		ColorStart: "#8ccaffd8", ColorMid: "#5aa4f0d0", ColorEnd: "#3a80d000", FadeIn: 0, FadeOut: 0.3, Mode: "soft", Softness: 1,
		Look: "water", Water: WaterLook{Threshold: 0.45, Clear: 0.5, Shine: 1.2, Bump: 1, Ripple: 2, RippleScale: 30, RippleSpeed: 40},
		React: EmitterReact{ReactTo: "beat", Burst: 200}},
	// clear water: almost no color, it bends what is behind it like glass (a lens)
	"water_clear": {Rate: 320, Max: 4000, Shape: "circle", Width: 14, Direction: 0, Spread: 9, Speed: 950, SpeedVar: 0.12, Gravity: 1500, Drag: 0.1,
		Turbulence: 12, TurbFreq: 1, Life: 1.7, LifeVar: 0.2, Size: 38, SizeEnd: 30, SizeVar: 0.3,
		ColorStart: "#cfe8ffd8", ColorMid: "#c4e2ffd8", ColorEnd: "#b8dcff00", FadeIn: 0, FadeOut: 0.15, Mode: "soft", Softness: 1,
		Floor: 320, Bounce: 0.25, Friction: 0.6, Look: "water",
		Water: WaterLook{Threshold: 0.5, Clear: 1, Shine: 1.3, Bump: 0.8, Ripple: 3, RippleScale: 40, RippleSpeed: 50, Lens: 40},
		React: EmitterReact{ReactTo: "bass", Speed: 0.25}},
	// rain: fast streaked drops of water
	"rain": {Rate: 420, Max: 6000, Shape: "line", Width: 2200, Direction: 185, Spread: 2, Speed: 1500, SpeedVar: 0.2, Gravity: 500, Wind: 60,
		Life: 1.4, LifeVar: 0.2, Size: 7, SizeEnd: 7, SizeVar: 0.4, Streak: 0.025,
		ColorStart: "#d4eaffb0", ColorMid: "#d4eaffb0", ColorEnd: "#d4eaff00", FadeIn: 0.05, FadeOut: 0.1, Mode: "soft", Softness: 0.8,
		Look: "water", Water: WaterLook{Threshold: 0.3, Clear: 0.6, Shine: 1.4, Bump: 0.6, Ripple: 0, RippleScale: 40, RippleSpeed: 0},
		React: EmitterReact{ReactTo: "bass", Rate: 0.5}},
	// a simulated gas: realistic smoke rising and curling
	"smoke_real": {Rate: 200, Max: 3000, Shape: "circle", Width: 200, Direction: 0, Spread: 25, Speed: 70, SpeedVar: 0.3, Drag: 1.5,
		Turbulence: 10, TurbFreq: 1, Life: 0.6, LifeVar: 0.3, Size: 110, SizeEnd: 140, SizeVar: 0.3,
		ColorStart: "#c0bcb8ff", ColorMid: "#c0bcb8ff", ColorEnd: "#c0bcb800", FadeIn: 0.1, FadeOut: 0.4, Mode: "soft", Softness: 1,
		Look: "gas", Gas: func() GasLook { g := DefaultGas(); g.Speed = 1.5; return g }(), React: EmitterReact{ReactTo: "bass", Rate: 0.6}},
	// a simulated fire: fuel burning into flames and the smoke they make
	"fire_gas": {Rate: 450, Max: 4000, Shape: "circle", Width: 220, Direction: 0, Spread: 30, Speed: 170, SpeedVar: 0.3, Drag: 2,
		Turbulence: 8, TurbFreq: 2, Life: 0.35, LifeVar: 0.3, Size: 48, SizeEnd: 34, SizeVar: 0.3,
		ColorStart: "#5c5650ff", ColorMid: "#5c5650ff", ColorEnd: "#5c565000", FadeIn: 0.05, FadeOut: 0.4, Mode: "soft", Softness: 1,
		Look: "gas", Flame: FlameLook{ColorCool: "#8a1606", ColorMid: "#ff6a14", ColorHot: "#fff2c8", Heat: 1.2, NoiseScale: 55,
			NoiseSpeed: 280, Edge: 0.7},
		Gas: GasLook{Detail: 5, Speed: 1.5, Smoke: 0.02, Heat: 0.2, Fuel: 0.8, Push: 0.3, Rise: 300, Swirl: 3, Turbulence: 140, TurbScale: 60,
			Fade: 0.15, Cool: 3.5, Burn: 4, Soot: 0.5, SootColor: "#24201d", Spread: 100,
			Opacity: 1.6, Wisps: 0.7, WispScale: 25, LightColor: "#ffe8d0", ShadowColor: "#4a4c54", Shadow: 1, FireLight: 1.2},
		Glow:  EmitGlow{Intensity: 0.6, Radius: 130, Color: "#ff6a18"},
		React: EmitterReact{ReactTo: "bass", Rate: 0.8, Speed: 0.4}},
	// a bank of storm clouds: heavy smoke from the top of the frame that sinks in lobes
	"clouds_gas": {Rate: 600, Max: 9000, Shape: "line", Width: 2400, Direction: 180, Spread: 20, Speed: 60, SpeedVar: 0.3, Drag: 1.5,
		Turbulence: 10, TurbFreq: 1, Life: 0.8, LifeVar: 0.3, Size: 120, SizeEnd: 140, SizeVar: 0.3,
		ColorStart: "#d8d0c8ff", ColorMid: "#d8d0c8ff", ColorEnd: "#d8d0c800", FadeIn: 0.1, FadeOut: 0.4, Mode: "soft", Softness: 1,
		Look: "gas",
		Gas: GasLook{Detail: 5, Smoke: 0.2, Push: 0.2, Rise: 250, Weight: 120, Swirl: 3, Turbulence: 40, TurbScale: 300,
			Fade: 0.01, Cool: 3, Burn: 2.5, Soot: 0.6, SootColor: "#2a2623", Spread: 100,
			Opacity: 1.6, Wisps: 0.5, WispScale: 40, LightColor: "#fff0e2", ShadowColor: "#9aa4b0", Shadow: 0.45, FireLight: 1},
		React: EmitterReact{ReactTo: "bass", Rate: 0.3}},
	// simulated water: a jet that fills a pool on a floor
	"water_sim": {Rate: 260, Max: 9000, Shape: "circle", Width: 16, Direction: 0, Spread: 8, Speed: 900, SpeedVar: 0.1, Gravity: 1500,
		Turbulence: 0, Life: 1, Size: 30, SizeEnd: 30, SizeVar: 0.2,
		ColorStart: "#5aa8f2d8", ColorMid: "#5aa8f2d8", ColorEnd: "#5aa8f2d8", Mode: "soft", Softness: 1,
		Floor: 330, Look: "liquid", Water: WaterLook{Threshold: 0.6, Clear: 0.65, Shine: 1.1, Bump: 1, Ripple: 2, RippleScale: 40, RippleSpeed: 40},
		Liquid: DefaultLiquid(), React: EmitterReact{ReactTo: "bass", Speed: 0.2}},
	// simulated clear water: almost no color, it bends what is behind it like glass (a lens)
	"water_sim_clear": {Rate: 260, Max: 9000, Shape: "circle", Width: 16, Direction: 0, Spread: 8, Speed: 900, SpeedVar: 0.1, Gravity: 1500,
		Life: 1, Size: 32, SizeEnd: 32, SizeVar: 0.2,
		ColorStart: "#d8ecffd8", ColorMid: "#d8ecffd8", ColorEnd: "#d8ecffd8", Mode: "soft", Softness: 1,
		Floor: 330, Look: "liquid", Water: WaterLook{Threshold: 0.6, Clear: 1, Shine: 1.3, Bump: 0.9, Ripple: 2, RippleScale: 40, RippleSpeed: 40, Lens: 40},
		Liquid: DefaultLiquid(), React: EmitterReact{ReactTo: "bass", Speed: 0.2}},
	// fog low over the ground, drifting: a soft simulated gas that stays down
	"fog_gas": {Rate: 260, Max: 4000, Shape: "box", Width: 2400, Height: 160, Direction: 90, Spread: 40, Speed: 20, SpeedVar: 0.5, Drag: 1,
		Life: 1, LifeVar: 0.3, Size: 170, SizeEnd: 190, SizeVar: 0.3,
		ColorStart: "#c8d0d8ff", ColorMid: "#c8d0d8ff", ColorEnd: "#c8d0d800", FadeIn: 0.2, FadeOut: 0.4, Mode: "soft", Softness: 1,
		Look: "gas",
		Gas: GasLook{Detail: 8, Smoke: 0.05, Push: 0.05, Rise: 0, Weight: 30, Swirl: 1, Wind: 30, Turbulence: 25, TurbScale: 400,
			Fade: 0.12, Cool: 3, Burn: 2.5, Soot: 0.6, SootColor: "#2a2623", Spread: 800,
			Opacity: 1.1, Wisps: 0.35, WispScale: 120, LightColor: "#eef2f6", ShadowColor: "#a2adba", Shadow: 0.25, FireLight: 1},
		React: EmitterReact{ReactTo: "bass", Rate: 0.2}},
	// a simulated splash of water on each beat, falling onto a floor
	"splash_sim": {Rate: 0, Burst: 220, Max: 9000, Shape: "circle", Width: 60, Direction: 0, Spread: 110, Speed: 700, SpeedVar: 0.45, Gravity: 1600,
		Life: 1, Size: 26, SizeEnd: 26, SizeVar: 0.3,
		ColorStart: "#7cc0ffd8", ColorMid: "#7cc0ffd8", ColorEnd: "#7cc0ffd8", Mode: "soft", Softness: 1,
		Floor: 160, Look: "liquid", Water: WaterLook{Threshold: 0.55, Clear: 0.55, Shine: 1.2, Bump: 1, Ripple: 2, RippleScale: 30, RippleSpeed: 40},
		Liquid: DefaultLiquid(), React: EmitterReact{ReactTo: "beat", Burst: 160}},
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

func init() {
	// every preset can turn on the flame look with sensible colors, and has a glow reach ready
	for k, e := range EmitterPresets {
		if e.Flame == (FlameLook{}) {
			e.Flame = DefaultFlame()
		}
		if e.Glow.Radius == 0 {
			e.Glow.Radius = 60
		}
		e.RouteStrength = 1
		if e.Water == (WaterLook{}) {
			e.Water = DefaultWater()
		}
		if e.Look == "" {
			e.Look = "plain"
		}
		if e.Gas == (GasLook{}) {
			e.Gas = DefaultGas()
		}
		if e.Liquid == (LiquidLook{}) {
			e.Liquid = DefaultLiquid()
		}
		if e.Light.Reach == 0 {
			e.Light = EmitLight{Intensity: 0.8, Reach: 450, Mode: "soft", Flicker: 0.25, Depth: 2.5, Shade: 0.6, From: 1, To: 0, RangeSoft: 0.05}
			if k == "fire" || k == "fire_real" || k == "explosion" || k == "lamp" || k == "fire_gas" {
				e.Light.Enabled = true
			}
		}
		if e.Light.RimWidth == 0 {
			e.Light.RimFrom, e.Light.RimSoft, e.Light.RimWidth, e.Light.RimGain, e.Light.RimEdge = 500, 120, 10, 2, 0.04
		}
		EmitterPresets[k] = e
	}
}

// DefaultEmitter: the fire preset
func DefaultEmitter() EmitterConfig { return EmitterPresets["fire"] }

// IsEmitter: a layer of particle emitters
func (l *LayerConfig) IsEmitter() bool { return l.Kind == "emitter" }

// LightMusic: how a light's strength follows the music at band level b (0..~1.5): react adds to
// it (1 = twice as strong at full band), quiet takes it away where the band is low (1 = dark in
// silence, full at full band)
func LightMusic(react, quiet, b float64) float64 {
	q := math.Max(0, math.Min(1, quiet))
	return math.Max(0, (1-q*(1-math.Min(b, 1)))*(1+react*b))
}
