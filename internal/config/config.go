package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectConfig representa la configuración completa de un proyecto
type ProjectConfig struct {
	Audio             AudioConfig             `yaml:"audio"`
	Background        BackgroundConfig        `yaml:"background"`
	BackgroundEffects BackgroundEffectsConfig `yaml:"background_effects"`
	Visualizer        VisualizerConfig        `yaml:"visualizer"`
	Particles         ParticlesConfig         `yaml:"particles"`
	Output            OutputConfig            `yaml:"output"`
}

type AudioConfig struct {
	Path        string  `yaml:"path"`
	Volume      float64 `yaml:"volume"`       // Multiplicador de volumen (1.0 = normal)
	Sensitivity float64 `yaml:"sensitivity"`  // Multiplicador global de reactividad (1.0 = normal)
	// Normalización adaptativa: cada efecto y cada barra se reescala a su rango local
	// para que no se queden saturados (ni muertos) cuando una banda está siempre alta/baja.
	Normalize       bool    `yaml:"normalize"`
	NormalizeWindow float64 `yaml:"normalize_window"` // segundos a cada lado de la ventana (def. 4)
}

type BackgroundConfig struct {
	Path      string  `yaml:"path"`      // Ruta a imagen (.png, .jpg) o video (.mp4, .mkv)
	FitMode   string  `yaml:"fit_mode"`  // "cover", "contain", "stretch"
	Opacity   float64 `yaml:"opacity"`   // 0.0 a 1.0 (para atenuar la imagen de fondo)
	Darken    float64 `yaml:"darken"`    // 0.0 a 1.0 (oscurecimiento base constante)
	Vignette  float64 `yaml:"vignette"`  // 0.0 a 1.0 (viñeta oscura en bordes)
	LoopVideo bool    `yaml:"loop_video"`// Si el video de fondo se repite en bucle
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
	MaxZoom     float64 `yaml:"max_zoom"`     // ej. 1.08 = 8% de zoom máximo
	Sensitivity float64 `yaml:"sensitivity"`  // Multiplicador de reacción a graves
	DecaySpeed  float64 `yaml:"decay_speed"`  // Velocidad de amortiguación (0.1 a 0.5)
	ReactTo     string  `yaml:"react_to"`     // bass (def.), mid, treble, overall, transient, beat
}

// ShakeConfig: movimiento de cámara (traslación suave pseudoaleatoria) guiado por una banda
type ShakeConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Amplitude float64 `yaml:"amplitude"` // desplazamiento máximo en píxeles
	Speed     float64 `yaml:"speed"`     // velocidad del vaivén (1.0 = normal)
	ReactTo   string  `yaml:"react_to"`  // bass (def.), mid, treble, overall, transient, beat
}

type ChromaticAberrationConfig struct {
	Enabled    bool    `yaml:"enabled"`
	MaxShift   float64 `yaml:"max_shift"`   // Desplazamiento máximo en píxeles de R y B
	Mode       string  `yaml:"mode"`        // "horizontal" o "radial"
	ReactTo    string  `yaml:"react_to"`    // "bass", "treble", "transient", "overall"
}

type RippleConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Amplitude float64 `yaml:"amplitude"` // Amplitud máxima de onda en píxeles
	Frequency float64 `yaml:"frequency"` // Frecuencia espacial de la onda
	Speed     float64 `yaml:"speed"`     // Velocidad de propagación temporal
	ReactTo   string  `yaml:"react_to"`  // mid (def.), bass, treble, overall, transient, beat
}

type GlitchConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Threshold float64 `yaml:"threshold"` // Umbral de energía de transitorios (0.0 a 1.0)
	Intensity float64 `yaml:"intensity"` // Intensidad del desplazamiento de franjas
	ReactTo   string  `yaml:"react_to"`  // "" = golpes/transitorios (def.); o una banda que dispara al superar threshold
}

type ColorFlashConfig struct {
	Enabled         bool    `yaml:"enabled"`
	BrightnessBoost float64 `yaml:"brightness_boost"` // ej. 0.20
	ContrastBoost   float64 `yaml:"contrast_boost"`   // ej. 0.15
	ReactTo         string  `yaml:"react_to"`         // bass (def.), mid, treble, overall, transient, beat
}

type VisualizerConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Type            string        `yaml:"type"`            // "circular", "horizontal", "vertical", "oscilloscope"
	StereoSymmetric bool          `yaml:"stereo_symmetric"`// true: L a la izquierda, R a la derecha
	Bars            int           `yaml:"bars"`            // Barras por canal (ej. 48 o 64)
	MinHeight       float64       `yaml:"min_height"`      // Altura mínima de barra
	MaxHeight       float64       `yaml:"max_height"`      // Altura máxima de barra en pico
	BarWidth        float64       `yaml:"bar_width"`       // Ancho o grosor de cada barra
	BarGap          float64       `yaml:"bar_gap"`         // Separación entre barras
	Roundness       float64       `yaml:"roundness"`       // Redondeo de las puntas
	Smoothing       float64       `yaml:"smoothing"`       // 0.0 (reactivo duro) a 0.95 (muy suave)
	DecayRate       float64       `yaml:"decay_rate"`      // Caída de picos (0.05 a 0.3)
	
	// Para tipo "circular":
	Radius          float64       `yaml:"radius"`          // Radio base del círculo
	CenterX         float64       `yaml:"center_x"`        // Posición relativa 0.0 a 1.0 (0.5 = centro)
	CenterY         float64       `yaml:"center_y"`        // Posición relativa 0.0 a 1.0 (0.5 = centro)
	DrawMode        string        `yaml:"draw_mode"`       // "bars", "smooth_curve", "filled"

	// Para tipo "horizontal":
	PositionY       float64       `yaml:"position_y"`      // Posición Y relativa (ej. 0.85 para abajo)
	MirrorBottom    bool          `yaml:"mirror_bottom"`   // Reflejo hacia abajo
	CenterGap       *float64      `yaml:"center_gap"`      // Separación entre L y R en el centro (px, def. 2×bar_gap)

	// Colores y Glow
	ColorMode       string        `yaml:"color_mode"`      // "gradient", "solid"
	SolidColor      string        `yaml:"solid_color"`     // Hex ej "#00f0ff"
	Gradient        []GradientStop `yaml:"gradient"`
	// Dirección del gradiente: "index" (def., a lo ancho: una barra de cada color)
	// o "length" (a lo largo de cada barra: de la base a la punta)
	GradientAxis string `yaml:"gradient_axis"`
	// Solo con gradient_axis: length. "bar" = el último color siempre en la punta (se mueve con la barra);
	// "max" = el último color está en max_height (las barras bajas solo muestran los primeros colores)
	GradientFit string `yaml:"gradient_fit"`
	Glow            GlowConfig    `yaml:"glow"`
}

type GradientStop struct {
	Offset float64 `yaml:"offset"` // 0.0 a 1.0
	Color  string  `yaml:"color"`  // Hex ej "#ff007f"
}

type GlowConfig struct {
	Enabled   bool    `yaml:"enabled"`
	Radius    float64 `yaml:"radius"`    // Radio del resplandor neón
	Intensity float64 `yaml:"intensity"` // Intensidad / opacidad del glow
}

type ParticlesConfig struct {
	Enabled bool `yaml:"enabled"`
	// Capas: cada una con su tipo, color y banda. Si está vacío se usa una única capa con los campos de abajo.
	Layers []ParticleLayer `yaml:"layers"`

	ParticleLayer `yaml:",inline"`
}

type ParticleLayer struct {
	Type       string  `yaml:"type"`        // "radial_burst", "ambient_dust", "rising_sparks"
	Count      int     `yaml:"count"`       // Número máximo de partículas de la capa
	MinSize    float64 `yaml:"min_size"`    // Tamaño mínimo
	MaxSize    float64 `yaml:"max_size"`    // Tamaño máximo
	Color      string  `yaml:"color"`       // Hex "#ffffff" (color al nacer)
	ColorEnd   string  `yaml:"color_end"`   // Hex opcional: color al morir (se interpola con la vida)
	Speed      float64 `yaml:"speed"`       // Velocidad base
	AudioBoost float64 `yaml:"audio_boost"` // Multiplicador de velocidad con el audio
	Glow       bool    `yaml:"glow"`        // Halo suave
	ReactTo    string  `yaml:"react_to"`    // bass (def.), mid, treble, overall, transient, beat
	// Densidad reactiva: fracción mínima de partículas vivas en silencio (0..1).
	// 1 = siempre todas (comportamiento clásico); 0.1 = casi ninguna si la banda está en silencio.
	MinDensity *float64 `yaml:"min_density"` // sin definir = 1
	Turbulence float64 `yaml:"turbulence"` // Serpenteo lateral (0 = recto; 0.5-1 = llama)
	MinLife    float64 `yaml:"min_life"`   // Vida mínima en segundos (def. 1)
	MaxLife    float64 `yaml:"max_life"`   // Vida máxima en segundos (def. 3)
}

type OutputConfig struct {
	Path        string `yaml:"path"`
	Width       int    `yaml:"width"`       // 0 = automático (según imagen/video de fondo)
	Height      int    `yaml:"height"`      // 0 = automático
	FPS         int    `yaml:"fps"`         // ej. 30 o 60
	Codec       string `yaml:"codec"`       // obsoleto: siempre H.264 (x264 integrado)
	Preset      string `yaml:"preset"`      // preset x264: "ultrafast" ... "veryslow"
	Renderer    string `yaml:"renderer"`    // "auto" (GPU Vulkan si hay), "gpu", "cpu"
	GPUDevice   int    `yaml:"gpu_device"`  // -1 = automático
	CRF         int    `yaml:"crf"`         // 18-23
	AudioBitrate string `yaml:"audio_bitrate"` // "320k"
}

// DefaultConfig retorna la configuración base recomendada
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
		Output: OutputConfig{
			Path:         "output.mp4",
			Width:        0, // 0 = inferir de fondo
			Height:       0, // 0 = inferir de fondo
			FPS:          60,
			Preset:       "fast",
			Renderer:     "auto",
			GPUDevice:    -1,
			CRF:          18,
			AudioBitrate: "320k",
		},
	}
}

// LoadConfig carga un archivo de configuración YAML y aplica valores predeterminados
func LoadConfig(filePath string) (*ProjectConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error leyendo archivo de proyecto '%s': %w", filePath, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error parseando YAML de proyecto: %w", err)
	}

	// Resolver rutas relativas al archivo de proyecto
	projectDir := filepath.Dir(filePath)
	if cfg.Audio.Path != "" && !filepath.IsAbs(cfg.Audio.Path) {
		cfg.Audio.Path = filepath.Clean(filepath.Join(projectDir, cfg.Audio.Path))
	}
	if cfg.Background.Path != "" && !filepath.IsAbs(cfg.Background.Path) {
		cfg.Background.Path = filepath.Clean(filepath.Join(projectDir, cfg.Background.Path))
	}
	if cfg.Output.Path != "" && !filepath.IsAbs(cfg.Output.Path) {
		cfg.Output.Path = filepath.Clean(filepath.Join(projectDir, cfg.Output.Path))
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate comprueba consistencia básica de la configuración
func (c *ProjectConfig) Validate() error {
	if c.Audio.Path == "" {
		return fmt.Errorf("el campo 'audio.path' es obligatorio en el archivo de proyecto")
	}
	if c.Background.Path == "" {
		return fmt.Errorf("el campo 'background.path' es obligatorio en el archivo de proyecto")
	}
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
