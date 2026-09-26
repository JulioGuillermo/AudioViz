package visualizer

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"audioviz/internal/config"
	"github.com/fogleman/gg"
)

// ParseHexColor convierte una cadena hex ("#ff007f" o "ff007f" o "#fff") a color.RGBA
func ParseHexColor(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = fmt.Sprintf("%c%c%c%c%c%c", s[0], s[0], s[1], s[1], s[2], s[2])
	}
	if len(s) != 6 && len(s) != 8 {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}, fmt.Errorf("formato hex inválido: %s", s)
	}

	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}, err
	}

	if len(s) == 6 {
		return color.RGBA{
			R: uint8(val >> 16),
			G: uint8((val >> 8) & 0xff),
			B: uint8(val & 0xff),
			A: 255,
		}, nil
	}

	return color.RGBA{
		R: uint8(val >> 24),
		G: uint8((val >> 16) & 0xff),
		B: uint8((val >> 8) & 0xff),
		A: uint8(val & 0xff),
	}, nil
}

// ColorAtOffset interpola linealmente el color en la posición t [0.0, 1.0] según la paleta de gradientes
func ColorAtOffset(stops []config.GradientStop, t float64) color.RGBA {
	if len(stops) == 0 {
		return color.RGBA{R: 0, G: 240, B: 255, A: 255}
	}
	if len(stops) == 1 || t <= stops[0].Offset {
		c, _ := ParseHexColor(stops[0].Color)
		return c
	}
	if t >= stops[len(stops)-1].Offset {
		c, _ := ParseHexColor(stops[len(stops)-1].Color)
		return c
	}

	for i := 0; i < len(stops)-1; i++ {
		s0 := stops[i]
		s1 := stops[i+1]
		if t >= s0.Offset && t <= s1.Offset {
			span := s1.Offset - s0.Offset
			localT := (t - s0.Offset) / span
			c0, _ := ParseHexColor(s0.Color)
			c1, _ := ParseHexColor(s1.Color)

			return color.RGBA{
				R: uint8(float64(c0.R)*(1-localT) + float64(c1.R)*localT),
				G: uint8(float64(c0.G)*(1-localT) + float64(c1.G)*localT),
				B: uint8(float64(c0.B)*(1-localT) + float64(c1.B)*localT),
				A: uint8(float64(c0.A)*(1-localT) + float64(c1.A)*localT),
			}
		}
	}

	c, _ := ParseHexColor(stops[len(stops)-1].Color)
	return c
}

// RenderGlow aplica un resplandor neón dibujando pases concéntricos con opacidad decreciente
func RenderGlow(dc *gg.Context, drawFunc func(ctx *gg.Context, alpha float64), glowRadius float64, intensity float64) {
	if glowRadius <= 1.0 || intensity <= 0.05 {
		// Solo dibujo principal
		drawFunc(dc, 1.0)
		return
	}

	passes := 4
	for p := passes; p >= 1; p-- {
		factor := float64(p) / float64(passes)
		alpha := (intensity / float64(passes)) * (1.0 - factor*0.5)
		if alpha > 1.0 {
			alpha = 1.0
		}
		dc.Push()
		drawFunc(dc, alpha)
		dc.Pop()
	}

	// Dibujo central nítido
	dc.Push()
	drawFunc(dc, 1.0)
	dc.Pop()
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func lerp(a, b, t float64) float64 {
	return a*(1.0-t) + b*t
}
