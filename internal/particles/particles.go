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
	X, Y     float64
	VX, VY   float64
	Life     float64
	MaxLife  float64
	Size     float64
	BaseSize float64
	Phase    float64 // fase para la turbulencia
	Active   bool
}

type layer struct {
	cfg        config.ParticleLayer
	particles  []*Particle
	col, col2  color.RGBA
	hasEnd     bool
	minDensity float64
	energy     float64
}

// ParticleSystem simula una o varias capas de partículas, cada una reactiva a su banda
type ParticleSystem struct {
	// Draining: deja de emitir; las partículas vivas terminan su vida (cambio de tramo)
	Draining bool
	// Alpha multiplica la opacidad de todas las partículas (fundidos entre tramos)
	Alpha   float64
	enabled bool
	width   int
	height  int
	layers  []*layer
	rng     *rand.Rand
	time    float64
}

func NewParticleSystem(cfg *config.ParticlesConfig, width, height int) *ParticleSystem {
	return newSystem(cfg, width, height, false, 1337)
}

// NewEmptyParticleSystem crea un sistema sin partículas vivas: van naciendo poco a poco
func NewEmptyParticleSystem(cfg *config.ParticlesConfig, width, height int, seed int64) *ParticleSystem {
	return newSystem(cfg, width, height, true, seed)
}

func newSystem(cfg *config.ParticlesConfig, width, height int, empty bool, seed int64) *ParticleSystem {
	ps := &ParticleSystem{Alpha: 1, enabled: cfg.Enabled, width: width, height: height, rng: rand.New(rand.NewSource(seed))}
	if !cfg.Enabled {
		return ps
	}
	layers := cfg.Layers
	if len(layers) == 0 {
		layers = []config.ParticleLayer{cfg.ParticleLayer}
	}
	for _, lc := range layers {
		if lc.Count <= 0 {
			lc.Count = 100
		}
		l := &layer{cfg: lc, col: parseColor(lc.Color), minDensity: 1}
		if lc.MinDensity != nil {
			l.minDensity = math.Max(0, math.Min(1, *lc.MinDensity))
		}
		if lc.ColorEnd != "" {
			l.col2, l.hasEnd = parseColor(lc.ColorEnd), true
		}
		l.particles = make([]*Particle, lc.Count)
		for i := range l.particles {
			p := &Particle{}
			ps.respawn(l, p, true)
			// Arrancar con la densidad mínima para no tener un estallido inicial
			p.Active = !empty && ps.rng.Float64() < math.Max(l.minDensity, 0.3)
			l.particles[i] = p
		}
		ps.layers = append(ps.layers, l)
	}
	return ps
}

func (ps *ParticleSystem) respawn(l *layer, p *Particle, randomLife bool) {
	c := &l.cfg
	minSize := c.MinSize
	if minSize <= 0 {
		minSize = 2.0
	}
	maxSize := c.MaxSize
	if maxSize <= minSize {
		maxSize = minSize + 4
	}
	minLife, maxLife := c.MinLife, c.MaxLife
	if minLife <= 0 {
		minLife = 1
	}
	if maxLife <= minLife {
		maxLife = minLife + 2
	}

	p.Active = true
	p.BaseSize = minSize + ps.rng.Float64()*(maxSize-minSize)
	p.Size = p.BaseSize
	p.MaxLife = minLife + ps.rng.Float64()*(maxLife-minLife)
	p.Life = p.MaxLife
	if randomLife {
		p.Life = ps.rng.Float64() * p.MaxLife
	}
	p.Phase = ps.rng.Float64() * 2 * math.Pi

	speed := c.Speed
	if speed <= 0 {
		speed = 1.0
	}
	w, h := float64(ps.width), float64(ps.height)

	switch c.Type {
	case "ambient_dust":
		p.X = ps.rng.Float64() * w
		p.Y = ps.rng.Float64() * h
		angle := ps.rng.Float64() * 2 * math.Pi
		sp := (0.3 + ps.rng.Float64()*0.7) * speed
		p.VX, p.VY = math.Cos(angle)*sp, math.Sin(angle)*sp
		if c.Direction != nil {
			a := *c.Direction * math.Pi / 180
			p.VX += math.Cos(a) * speed * 0.8
			p.VY -= math.Sin(a) * speed * 0.8
		}
	case "rising_sparks":
		if c.Direction == nil {
			p.X = ps.rng.Float64() * w
			p.Y = h + ps.rng.Float64()*20.0
			if randomLife {
				p.Y = h - ps.rng.Float64()*h*0.6
			}
			p.VX = (ps.rng.Float64()*2.0 - 1.0) * speed * 0.8
			p.VY = -(1.5 + ps.rng.Float64()*2.5) * speed
			break
		}
		// Dirección libre: nacen en el borde opuesto y viajan hacia direction (± spread)
		spread := c.Spread
		if spread <= 0 {
			spread = 25
		}
		ang := (*c.Direction + (ps.rng.Float64()*2-1)*spread) * math.Pi / 180
		dx, dy := math.Cos(ang), -math.Sin(ang) // eje Y de pantalla hacia abajo
		sp := (1.5 + ps.rng.Float64()*2.5) * speed
		p.VX, p.VY = dx*sp, dy*sp
		mainA := *c.Direction * math.Pi / 180
		mx, my := math.Cos(mainA), -math.Sin(mainA)
		// Punto de nacimiento: a lo largo del borde por el que "entran"
		if math.Abs(mx) > math.Abs(my) {
			p.Y = ps.rng.Float64() * h
			p.X = -10 - ps.rng.Float64()*20
			if mx < 0 {
				p.X = w + 10 + ps.rng.Float64()*20
			}
		} else {
			p.X = ps.rng.Float64() * w
			p.Y = -10 - ps.rng.Float64()*20
			if my < 0 {
				p.Y = h + 10 + ps.rng.Float64()*20
			}
		}
		if randomLife {
			p.X, p.Y = ps.rng.Float64()*w, ps.rng.Float64()*h
		}
	default: // radial_burst
		cx, cy := w/2, h/2
		startR := ps.rng.Float64() * 40.0
		angle := ps.rng.Float64() * 2 * math.Pi
		p.X, p.Y = cx+math.Cos(angle)*startR, cy+math.Sin(angle)*startR
		sp := (1.0 + ps.rng.Float64()*4.0) * speed
		p.VX, p.VY = math.Cos(angle)*sp, math.Sin(angle)*sp
	}
}

// Update avanza la simulación un cuadro
func (ps *ParticleSystem) Update(feat *audio.FrameFeatures, dt float64) {
	if !ps.enabled {
		return
	}
	ps.time += dt
	for _, l := range ps.layers {
		c := &l.cfg
		energy := 0.0
		if feat != nil {
			energy = math.Min(feat.Band(c.ReactTo, "bass"), 1)
		}
		l.energy = energy

		audioBoost := 1.0 + energy*c.AudioBoost
		if feat != nil && feat.IsBeat && (c.ReactTo == "" || c.ReactTo == "bass" || c.ReactTo == "beat") {
			audioBoost += 2.0
		}

		// Densidad objetivo según la energía de la banda
		n := len(l.particles)
		target := int(float64(n) * (l.minDensity + (1-l.minDensity)*energy))
		active := 0
		for _, p := range l.particles {
			if p.Active {
				active++
			}
		}
		spawnBudget := n/15 + 1
		if ps.Draining {
			spawnBudget = 0
		}

		for _, p := range l.particles {
			if !p.Active {
				if active < target && spawnBudget > 0 {
					ps.respawn(l, p, false)
					active++
					spawnBudget--
				}
				continue
			}
			p.Life -= dt
			if p.Life <= 0 {
				if active <= target && spawnBudget > 0 {
					ps.respawn(l, p, false)
					spawnBudget--
				} else {
					p.Active = false
					active--
					continue
				}
			}
			vx := p.VX
			if c.Turbulence > 0 {
				vx += math.Sin(ps.time*3.0+p.Phase+p.Y*0.01) * c.Turbulence * 1.5
			}
			p.X += vx * audioBoost
			p.Y += p.VY * audioBoost
			p.Size = p.BaseSize * (1.0 + energy*0.5)
		}
	}
}

// Each recorre las partículas visibles: posición, radio, alfa (0..1), color y halo
func (ps *ParticleSystem) Each(fn func(x, y, size, alpha float64, col color.RGBA, glow bool)) {
	if !ps.enabled || ps.Alpha <= 0 {
		return
	}
	for _, l := range ps.layers {
		intensity := 1.0
		if l.minDensity < 1 {
			intensity = 0.45 + 0.55*l.energy
		}
		for _, p := range l.particles {
			if !p.Active {
				continue
			}
			lr := p.Life / p.MaxLife
			alpha := math.Sin(lr*math.Pi) * 0.8 * intensity * ps.Alpha
			if alpha <= 0 {
				continue
			}
			col := l.col
			if l.hasEnd {
				t := 1 - lr
				col = color.RGBA{
					R: uint8(float64(l.col.R)*(1-t) + float64(l.col2.R)*t),
					G: uint8(float64(l.col.G)*(1-t) + float64(l.col2.G)*t),
					B: uint8(float64(l.col.B)*(1-t) + float64(l.col2.B)*t),
					A: 255,
				}
			}
			fn(p.X, p.Y, p.Size, alpha, col, l.cfg.Glow)
		}
	}
}

// Draw dibuja con gg (render por CPU)
func (ps *ParticleSystem) Draw(dc *gg.Context) {
	ps.Each(func(x, y, size, alpha float64, col color.RGBA, glow bool) {
		a := alpha * 255
		if glow {
			dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(a*0.3))
			dc.DrawCircle(x, y, size*2.0)
			dc.Fill()
		}
		dc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(a))
		dc.DrawCircle(x, y, size)
		dc.Fill()
	})
}

func parseColor(s string) color.RGBA {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		for _, ch := range s[i*2 : i*2+2] {
			var d uint8
			switch {
			case ch >= '0' && ch <= '9':
				d = uint8(ch - '0')
			case ch >= 'a' && ch <= 'f':
				d = uint8(ch-'a') + 10
			case ch >= 'A' && ch <= 'F':
				d = uint8(ch-'A') + 10
			}
			v[i] = v[i]*16 + d
		}
	}
	return color.RGBA{R: v[0], G: v[1], B: v[2], A: 255}
}

// Alive indica si queda alguna partícula viva
func (ps *ParticleSystem) Alive() bool {
	for _, l := range ps.layers {
		for _, p := range l.particles {
			if p.Active {
				return true
			}
		}
	}
	return false
}
