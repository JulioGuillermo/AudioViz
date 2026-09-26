package particles

import (
	"image/color"
	"math"
	"math/rand"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

type Particle struct {
	X        float64
	Y        float64
	VX       float64
	VY       float64
	Life     float64
	MaxLife  float64
	Size     float64
	BaseSize float64
	Color    color.RGBA
}

type ParticleSystem struct {
	cfg       *config.ParticlesConfig
	width     int
	height    int
	particles []*Particle
	rng       *rand.Rand
	baseCol   color.RGBA
}

func NewParticleSystem(cfg *config.ParticlesConfig, width, height int) *ParticleSystem {
	if !cfg.Enabled {
		return &ParticleSystem{cfg: cfg, width: width, height: height}
	}

	count := cfg.Count
	if count <= 0 {
		count = 100
	}

	rng := rand.New(rand.NewSource(1337))
	col := parseColor(cfg.Color)

	ps := &ParticleSystem{
		cfg:       cfg,
		width:     width,
		height:    height,
		particles: make([]*Particle, count),
		rng:       rng,
		baseCol:   col,
	}

	// Inicializar partículas
	for i := 0; i < count; i++ {
		p := &Particle{}
		ps.respawn(p, true)
		ps.particles[i] = p
	}

	return ps
}

func (ps *ParticleSystem) respawn(p *Particle, randomLife bool) {
	minSize := ps.cfg.MinSize
	if minSize <= 0 {
		minSize = 2.0
	}
	maxSize := ps.cfg.MaxSize
	if maxSize <= minSize {
		maxSize = 6.0
	}

	p.BaseSize = minSize + ps.rng.Float64()*(maxSize-minSize)
	p.Size = p.BaseSize
	p.MaxLife = 1.0 + ps.rng.Float64()*2.0
	if randomLife {
		p.Life = ps.rng.Float64() * p.MaxLife
	} else {
		p.Life = p.MaxLife
	}
	p.Color = ps.baseCol

	speed := ps.cfg.Speed
	if speed <= 0 {
		speed = 1.0
	}

	switch ps.cfg.Type {
	case "ambient_dust":
		p.X = ps.rng.Float64() * float64(ps.width)
		p.Y = ps.rng.Float64() * float64(ps.height)
		angle := ps.rng.Float64() * 2 * math.Pi
		sp := (0.3 + ps.rng.Float64()*0.7) * speed
		p.VX = math.Cos(angle) * sp
		p.VY = math.Sin(angle) * sp

	case "rising_sparks":
		p.X = ps.rng.Float64() * float64(ps.width)
		p.Y = float64(ps.height) + ps.rng.Float64()*20.0
		p.VX = (ps.rng.Float64()*2.0 - 1.0) * speed * 0.8
		p.VY = -(1.5 + ps.rng.Float64()*2.5) * speed

	case "radial_burst":
		fallthrough
	default:
		// Emisión radial desde el centro
		cx := float64(ps.width) / 2.0
		cy := float64(ps.height) / 2.0
		// Radio aleatorio cercano al centro
		startR := ps.rng.Float64() * 40.0
		angle := ps.rng.Float64() * 2 * math.Pi
		p.X = cx + math.Cos(angle)*startR
		p.Y = cy + math.Sin(angle)*startR

		sp := (1.0 + ps.rng.Float64()*4.0) * speed
		p.VX = math.Cos(angle) * sp
		p.VY = math.Sin(angle) * sp
	}
}

func (ps *ParticleSystem) UpdateAndDraw(dc *gg.Context, feat *audio.FrameFeatures, dt float64) {
	ps.Update(feat, dt)
	ps.Each(func(x, y, size, alpha float64) {
		col := ps.baseCol
		col.A = uint8(alpha * 255.0)
		if ps.cfg.Glow {
			dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(float64(col.A)*0.3))
			dc.DrawCircle(x, y, size*2.0)
			dc.Fill()
		}
		dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
		dc.DrawCircle(x, y, size)
		dc.Fill()
	})
}

// Update avanza la simulación un cuadro
func (ps *ParticleSystem) Update(feat *audio.FrameFeatures, dt float64) {
	if !ps.cfg.Enabled || len(ps.particles) == 0 {
		return
	}

	audioBoost := 1.0
	if feat != nil {
		audioBoost += feat.BassEnergy * ps.cfg.AudioBoost
		if feat.IsBeat {
			audioBoost += 2.0
		}
	}

	for _, p := range ps.particles {
		p.Life -= dt
		if p.Life <= 0 {
			ps.respawn(p, false)
		}

		// Movimiento guiado por audio
		p.X += p.VX * audioBoost
		p.Y += p.VY * audioBoost

		// Pulso de tamaño con el bajo
		pulseFactor := 1.0
		if feat != nil {
			pulseFactor = 1.0 + feat.BassEnergy*0.5
		}
		p.Size = p.BaseSize * pulseFactor
	}
}

// Each recorre las partículas visibles: posición, radio y alfa (0..1)
func (ps *ParticleSystem) Each(fn func(x, y, size, alpha float64)) {
	if !ps.cfg.Enabled {
		return
	}
	for _, p := range ps.particles {
		// Transparencia según vida restante (entrada y salida suave)
		alpha := math.Sin(p.Life/p.MaxLife*math.Pi) * 0.8
		if alpha <= 0 {
			continue
		}
		fn(p.X, p.Y, p.Size, alpha)
	}
}

// Color devuelve el color base de las partículas
func (ps *ParticleSystem) Color() color.RGBA { return ps.baseCol }

func parseColor(s string) color.RGBA {
	if s == "" {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	if s[0] == '#' {
		s = s[1:]
	}
	if len(s) == 6 {
		var r, g, b uint8
		_, _ = parseHexByte(s[0:2], &r)
		_, _ = parseHexByte(s[2:4], &g)
		_, _ = parseHexByte(s[4:6], &b)
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	return color.RGBA{R: 255, G: 255, B: 255, A: 255}
}

func parseHexByte(s string, out *uint8) (bool, error) {
	var val uint8
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d uint8
		if c >= '0' && c <= '9' {
			d = c - '0'
		} else if c >= 'a' && c <= 'f' {
			d = c - 'a' + 10
		} else if c >= 'A' && c <= 'F' {
			d = c - 'A' + 10
		} else {
			return false, nil
		}
		val = val*16 + d
	}
	*out = val
	return true, nil
}
