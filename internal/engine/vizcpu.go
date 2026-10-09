package engine

import (
	"image"
	"math"
	"runtime"
	"sync"

	"audioviz/internal/gpu"
)

// The visualizer on the CPU: the same as drawViz in render.comp, pixel by pixel, from the same
// parameters (fillParams), so that the CPU render matches the GPU's.

// drawVizCPU draws the visualizer onto the frame
func (s *Session) drawVizCPU(frame *image.RGBA, fs *FrameState) {
	if !fs.Viz.Enabled || fs.VizAlpha <= 0 {
		return
	}
	p := new(gpu.Params)
	s.fillParams(p, fs, s.W, s.H, false)
	if p.Flags[0] == 0 {
		return
	}
	v := vizCPU{p: p, W: float32(s.W)}
	W, H := s.W, s.H
	nw := runtime.NumCPU()
	var wg sync.WaitGroup
	for k := 0; k < nw; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for y := k; y < H; y += nw {
				for x := 0; x < W; x++ {
					o := y*frame.Stride + x*4
					col := [3]float32{float32(frame.Pix[o]) / 255, float32(frame.Pix[o+1]) / 255, float32(frame.Pix[o+2]) / 255}
					c := v.draw(col, float32(x)+0.5, float32(y)+0.5)
					for j := 0; j < 3; j++ {
						frame.Pix[o+j] = uint8(math.Round(float64(max(0, min(1, c[j]))) * 255))
					}
				}
			}
		}(k)
	}
	wg.Wait()
}

type vizCPU struct {
	p *gpu.Params
	W float32
}

// the accumulators of one pixel: glow and core (max), with their color
type vizAcc struct {
	gA, cA float32
	gC, cC [3]float32
}

func (v *vizCPU) gradientAt(t float32) [3]float32 {
	p := v.p
	if p.Flags2[1] == 1 {
		return [3]float32{p.SolidColor[0], p.SolidColor[1], p.SolidColor[2]}
	}
	n := int(p.Flags2[2])
	if n == 0 {
		return [3]float32{0, 240.0 / 255, 1}
	}
	g := func(i int) [3]float32 { return [3]float32{p.Grad[i][0], p.Grad[i][1], p.Grad[i][2]} }
	if n == 1 || t <= p.Grad[0][3] {
		return g(0)
	}
	if t >= p.Grad[n-1][3] {
		return g(n - 1)
	}
	for i := 0; i < n-1; i++ {
		a, b := p.Grad[i], p.Grad[i+1]
		if t >= a[3] && t <= b[3] {
			k := (t - a[3]) / max(b[3]-a[3], 1e-6)
			return [3]float32{a[0] + (b[0]-a[0])*k, a[1] + (b[1]-a[1])*k, a[2] + (b[2]-a[2])*k}
		}
	}
	return g(n - 1)
}

func (v *vizCPU) solidAlpha() float32 {
	if v.p.Flags2[1] == 1 {
		return v.p.SolidColor[3]
	}
	return 1
}

func over3(dst, src [3]float32, a float32) [3]float32 {
	a = max(0, min(1, a))
	return [3]float32{dst[0] + (src[0]-dst[0])*a, dst[1] + (src[1]-dst[1])*a, dst[2] + (src[2]-dst[2])*a}
}

// glowAlpha: as 3 concentric passes with opacity (intensity/3)·0.4
func (v *vizCPU) glowAlpha(d float32) float32 {
	R := max(v.p.Viz2[0], 1)
	a := v.p.Viz2[1] / 3 * 0.4
	k := max(0, min(3, 3*(1-d/(R*0.5))))
	return 1 - float32(math.Pow(float64(1-a), float64(k)))
}

func sdSegment(px, py, ax, ay, bx, by float32) float32 {
	pax, pay, bax, bay := px-ax, py-ay, bx-ax, by-ay
	h := max(0, min(1, (pax*bax+pay*bay)/max(bax*bax+bay*bay, 1e-6)))
	return float32(math.Hypot(float64(pax-bax*h), float64(pay-bay*h)))
}

func sdRoundBox(px, py, lx, ly, hx, hy, r float32) float32 {
	cx, cy := (lx+hx)*0.5, (ly+hy)*0.5
	bx, by := (hx-lx)*0.5, (hy-ly)*0.5
	r = min(r, min(bx, by))
	qx, qy := float32(math.Abs(float64(px-cx)))-bx+r, float32(math.Abs(float64(py-cy)))-by+r
	return float32(math.Hypot(float64(max(qx, 0)), float64(max(qy, 0)))) + min(max(qx, qy), 0) - r
}

// gradT: the place in the gradient: by bar, or along the bar
func (v *vizCPU) gradT(i, N int, dist, h float32) float32 {
	p := v.p
	if p.GradInfo[0] < 0.5 {
		return float32(i) / float32(N)
	}
	L := h
	if p.GradInfo[1] > 0.5 {
		L = p.GradInfo[2]
	}
	return max(0, min(1, dist/max(L, 1)))
}

func (v *vizCPU) addShape(a *vizAcc, d float32, color [3]float32, alpha float32) {
	alpha *= v.p.BgMix[1]
	if cov := max(0, min(1, 0.5-d)) * alpha; cov > a.cA {
		a.cA, a.cC = cov, color
	}
	if v.p.Flags3[1] != 0 {
		if g := v.glowAlpha(d) * alpha; g > a.gA {
			a.gA, a.gC = g, color
		}
	}
}

func (v *vizCPU) circularBars(a *vizAcc, px, py float32) {
	p := v.p
	N := int(p.Flags[1])
	cx, cy, r0, w := p.Viz0[0], p.Viz0[1], p.Viz0[2], p.Viz1[0]
	stepA := float32(math.Pi) / float32(N)
	dx, dy := px-cx, py-cy
	ang := float32(math.Atan2(float64(dy), float64(dx)))
	const halfPi, twoPi = 1.5707963, 6.2831853
	for ch := 0; ch < 2; ch++ {
		t := ang + halfPi
		if ch == 0 {
			t = -halfPi - ang
		}
		t = float32(math.Mod(float64(t+twoPi), twoPi))
		base := int(math.Floor(float64(t/stepA - 0.5)))
		for k := -2; k <= 2; k++ {
			i := base + k
			if i < 0 || i >= N {
				continue
			}
			h := p.Bars[i][1]
			an := -halfPi + (float32(i)+0.5)*stepA
			if ch == 0 {
				h = p.Bars[i][0]
				an = -halfPi - (float32(i)+0.5)*stepA
			}
			ux, uy := float32(math.Cos(float64(an))), float32(math.Sin(float64(an)))
			dist := sdSegment(px, py, cx+ux*r0, cy+uy*r0, cx+ux*(r0+h), cy+uy*(r0+h)) - w*0.5
			along := dx*ux + dy*uy - r0
			v.addShape(a, dist, v.gradientAt(v.gradT(i, N, along, h)), v.solidAlpha())
		}
	}
}

func (v *vizCPU) horizontalBars(a *vizAcc, px, py float32) {
	p := v.p
	N := int(p.Flags[1])
	cx, baseY, bw, span, rr := p.Viz0[0], p.Viz0[3], p.Viz1[0], p.Viz1[1], p.Viz1[2]
	hg := p.Viz3[2] * 0.5
	for ch := 0; ch < 2; ch++ {
		rel := px - cx - hg
		if ch == 0 {
			rel = cx - hg - px
		}
		base := int(math.Floor(float64(rel / span)))
		for k := -1; k <= 1; k++ {
			i := base + k
			if i < 0 || i >= N {
				continue
			}
			h, x0 := p.Bars[i][1], cx+hg+float32(i)*span
			if ch == 0 {
				h, x0 = p.Bars[i][0], cx-hg-(float32(i)*span+bw)
			}
			r := min(rr, min(bw*0.5, h*0.5))
			v.addShape(a, sdRoundBox(px, py, x0, baseY-h, x0+bw, baseY, r), v.gradientAt(v.gradT(i, N, baseY-py, h)), v.solidAlpha())
			if p.Flags3[0] != 0 {
				hm := h * 0.5
				rm := min(rr, min(bw*0.5, hm*0.5))
				v.addShape(a, sdRoundBox(px, py, x0, baseY, x0+bw, baseY+hm, rm), v.gradientAt(v.gradT(i, N, (py-baseY)*2, h)), v.solidAlpha()*0.35)
			}
		}
	}
}

func (v *vizCPU) verticalBars(a *vizAcc, px, py float32) {
	p := v.p
	N := int(p.Flags[1])
	startY, barH, span, rr := p.Viz3[0], p.Viz3[1], p.Viz1[1], p.Viz1[2]
	base := int(math.Floor(float64((py - startY) / span)))
	for k := -1; k <= 1; k++ {
		i := base + k
		if i < 0 || i >= N {
			continue
		}
		y0 := startY + float32(i)*span
		lenL, lenR := max(p.Bars[i][0], 2), max(p.Bars[i][1], 2)
		v.addShape(a, sdRoundBox(px, py, 0, y0, lenL, y0+barH, min(rr, min(barH, lenL)*0.5)), v.gradientAt(v.gradT(i, N, px, lenL)), v.solidAlpha())
		v.addShape(a, sdRoundBox(px, py, v.W-lenR, y0, v.W, y0+barH, min(rr, min(barH, lenR)*0.5)), v.gradientAt(v.gradT(i, N, v.W-px, lenR)), v.solidAlpha())
	}
}

func (v *vizCPU) wavePoint(i, ch int) (float32, float32) {
	p := v.p
	mag, dir := p.Bars[i][3], float32(1)
	if ch == 0 {
		mag, dir = p.Bars[i][2], -1
	}
	return float32(i) * p.Viz2[3], p.Viz0[3] + dir*mag*p.Viz2[2]*float32(math.Sin(float64(float32(i)*0.4)))
}

func (v *vizCPU) waveLines(a *vizAcc, px, py float32) {
	p := v.p
	N := int(p.Flags[1])
	w := p.Viz1[0]
	base := int(math.Floor(float64(px / p.Viz2[3])))
	for ch := 0; ch < 2; ch++ {
		col := [3]float32{p.WaveColR[0], p.WaveColR[1], p.WaveColR[2]}
		if ch == 0 {
			col = [3]float32{p.WaveColL[0], p.WaveColL[1], p.WaveColL[2]}
		}
		if p.GradInfo[0] > 0.5 {
			col = v.gradientAt(max(0, min(1, float32(math.Abs(float64(py-p.Viz0[3])))/max(p.Viz2[2], 1))))
		}
		for k := -2; k <= 1; k++ {
			i := base + k
			if i < 0 || i+1 >= N {
				continue
			}
			ax, ay := v.wavePoint(i, ch)
			bx, by := v.wavePoint(i+1, ch)
			v.addShape(a, sdSegment(px, py, ax, ay, bx, by)-w*0.5, col, 1)
		}
	}
}

// draw: the visualizer over col at pixel center (px, py), as drawViz
func (v *vizCPU) draw(col [3]float32, px, py float32) [3]float32 {
	p := v.p
	var a vizAcc
	switch p.Flags[0] {
	case 1:
		if p.Flags3[3] != 0 {
			// the base ring of the circular visualizer
			rd := float32(math.Abs(math.Hypot(float64(px-p.Viz0[0]), float64(py-p.Viz0[1]))-float64(p.Viz0[2]-2))) - 1
			col = over3(col, v.gradientAt(0.5), max(0, min(1, 0.5-rd))*p.Viz1[3]*p.BgMix[1])
		}
		v.circularBars(&a, px, py)
	case 2:
		v.horizontalBars(&a, px, py)
	case 3:
		v.verticalBars(&a, px, py)
	case 4:
		v.waveLines(&a, px, py)
	}
	col = over3(col, a.gC, a.gA)
	return over3(col, a.cC, a.cA)
}
