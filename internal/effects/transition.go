package effects

import (
	"image"
	"math"

	"audioviz/internal/config"
)

// Layer is how a background image is placed on the frame: the source pixel for output pixel q
// is c + rotate(q - c, -Rot) / Scale + (OffX, OffY), with c the frame center. The GPU shader
// (layerUV in render.comp) uses exactly the same formula.
type Layer struct {
	Scale, OffX, OffY, Rot float64
}

// Identity is an image shown as it is
var Identity = Layer{Scale: 1}

func ease(p float64, mode string) float64 {
	p = math.Max(0, math.Min(1, p))
	if mode == "ease" {
		return p * p * (3 - 2*p)
	}
	return p
}

// coverScale is the zoom needed so that a view rotated by rot still covers the frame
func coverScale(rot float64, w, h int) float64 {
	c, s := math.Abs(math.Cos(rot)), math.Abs(math.Sin(rot))
	ar := float64(w) / float64(h)
	return math.Max(c+s/ar, c+s*ar)
}

// MotionAt places a background image for its motion at progress p (0..1 over the time the
// segment is visible) and tsec seconds into it. seed varies ken_burns between segments.
func MotionAt(m config.MotionConfig, p, tsec float64, w, h, seed int) Layer {
	a := m.Amount
	if a <= 0 {
		a = 0.15
	}
	speed := m.Speed
	if speed <= 0 {
		speed = 1
	}
	e := ease(p, m.Easing)
	W, H := float64(w), float64(h)
	// Half the room left on each side when zoomed in by s (how far it can pan without edges)
	slack := func(s float64) (float64, float64) { return (W - W/s) / 2, (H - H/s) / 2 }
	switch m.Type {
	case "zoom_in":
		return Layer{Scale: 1 + a*e}
	case "zoom_out":
		return Layer{Scale: 1 + a*(1-e)}
	case "pan_left", "pan_right", "pan_up", "pan_down":
		s := 1 + a
		sx, sy := slack(s)
		l := Layer{Scale: s}
		switch m.Type {
		case "pan_left": // the image moves left: the view travels right
			l.OffX = sx * (2*e - 1)
		case "pan_right":
			l.OffX = -sx * (2*e - 1)
		case "pan_up":
			l.OffY = sy * (2*e - 1)
		case "pan_down":
			l.OffY = -sy * (2*e - 1)
		}
		return l
	case "ken_burns": // zoom in while travelling from the opposite corner to the chosen one
		s := 1 + a*(0.5+0.5*e)
		sx, sy := slack(s)
		sx, sy = sx*(2*e-1), sy*(2*e-1)
		var d [2]float64
		switch m.Direction {
		case "top_left":
			d = [2]float64{-1, -1}
		case "top_right":
			d = [2]float64{1, -1}
		case "bottom_left":
			d = [2]float64{-1, 1}
		case "bottom_right":
			d = [2]float64{1, 1}
		default:
			d = [4][2]float64{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}}[((seed%4)+4)%4]
		}
		return Layer{Scale: s, OffX: d[0] * sx, OffY: d[1] * sy}
	case "drift": // slow floating around the image
		s := 1 + a
		sx, sy := slack(s)
		ph := 2 * math.Pi * speed / 10 * tsec
		return Layer{Scale: s, OffX: sx * math.Sin(ph), OffY: sy * math.Sin(ph*0.77+1.3)}
	case "sway": // gentle rotation back and forth
		maxRot := a * 0.25
		ph := 2 * math.Pi * speed / 10 * tsec
		return Layer{Scale: coverScale(maxRot, w, h) * (1 + a*0.1), Rot: maxRot * math.Sin(ph)}
	}
	return Identity
}

// Trans is the state of a background transition: A = previous image, B = new one
type Trans struct {
	Mode, Dir  int     // indices into config.TransitionModes / TransitionDirections
	K          float64 // progress 0..1
	Seed       uint32
	HasA, HasB bool // false = that side is black (no background)
	Burn       BurnParams
}

// Hash as in the shader (PCG): deterministic noise for dissolve and glitch
func Hash(x uint32) uint32 {
	x = x*747796405 + 2891336453
	w := ((x >> ((x >> 28) + 4)) ^ x) * 277803737
	return (w >> 22) ^ w
}

func hash3(a, b, c uint32) float64 {
	return float64(Hash(a+Hash(b+Hash(c)))) / 4294967296.0
}

func smoothstep(e0, e1, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-e0)/(e1-e0)))
	return t * t * (3 - 2*t)
}

type rgb [3]float64

func mixc(a, b rgb, k float64) rgb {
	return rgb{a[0] + (b[0]-a[0])*k, a[1] + (b[1]-a[1])*k, a[2] + (b[2]-a[2])*k}
}

// sampler reads a layer bilinearly (edge-clamped), black when there is no image
type sampler struct {
	img  *image.RGBA
	l    Layer
	w, h int
	cs   float64
	sn   float64
}

func newSampler(img *image.RGBA, l Layer, w, h int) sampler {
	return sampler{img: img, l: l, w: w, h: h, cs: math.Cos(-l.Rot), sn: math.Sin(-l.Rot)}
}

func (s *sampler) at(x, y float64) rgb {
	if s.img == nil {
		return rgb{}
	}
	cx, cy := float64(s.w)/2, float64(s.h)/2
	dx, dy := x-cx, y-cy
	if s.l.Rot != 0 {
		dx, dy = dx*s.cs-dy*s.sn, dx*s.sn+dy*s.cs
	}
	sc := s.l.Scale
	if sc <= 0 {
		sc = 1
	}
	x = cx + dx/sc + s.l.OffX
	y = cy + dy/sc + s.l.OffY
	x = math.Max(0, math.Min(float64(s.w-1), x))
	y = math.Max(0, math.Min(float64(s.h-1), y))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, s.w-1), min(y0+1, s.h-1)
	fx, fy := x-float64(x0), y-float64(y0)
	px := func(xx, yy int) rgb {
		o := yy*s.img.Stride + xx*4
		p := s.img.Pix[o : o+3 : o+3]
		return rgb{float64(p[0]), float64(p[1]), float64(p[2])}
	}
	return mixc(mixc(px(x0, y0), px(x1, y0), fx), mixc(px(x0, y1), px(x1, y1), fx), fy)
}

// Compose draws the background into dst: image a with layer la, and during a transition the
// new image b with layer lb mixed in by tr. It mirrors composite() in render.comp.
func Compose(dst, a, b *image.RGBA, la, lb Layer, tr Trans) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	if !tr.HasA {
		a = nil
	}
	if !tr.HasB {
		b = nil
	}
	sa, sb := newSampler(a, la, w, h), newSampler(b, lb, w, h)
	parallelFor(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				var c rgb
				if tr.Mode == 0 {
					c = sa.at(float64(x), float64(y))
				} else {
					c = transitionPixel(&sa, &sb, float64(x), float64(y), w, h, tr)
				}
				o := y*dst.Stride + x*4
				dst.Pix[o] = uint8(math.Max(0, math.Min(255, c[0]+0.5)))
				dst.Pix[o+1] = uint8(math.Max(0, math.Min(255, c[1]+0.5)))
				dst.Pix[o+2] = uint8(math.Max(0, math.Min(255, c[2]+0.5)))
				dst.Pix[o+3] = 255
			}
		}
	})
}

// dirVec: unit vector where the new image moves (left, right, up, down)
func dirVec(d int) (float64, float64) {
	switch d {
	case 1:
		return 1, 0
	case 2:
		return 0, -1
	case 3:
		return 0, 1
	}
	return -1, 0
}

func transitionPixel(sa, sb *sampler, x, y float64, w, h int, tr Trans) rgb {
	W, H := float64(w), float64(h)
	k := math.Max(0, math.Min(1, tr.K))
	ke := k * k * (3 - 2*k)
	white := rgb{255, 255, 255}
	inside := func(x, y float64) bool { return x >= 0 && x < W && y >= 0 && y < H }
	switch config.TransitionModes[tr.Mode] {
	case "dissolve":
		n := hash3(uint32(int(x)/2), uint32(int(y)/2), tr.Seed)
		return mixc(sa.at(x, y), sb.at(x, y), smoothstep(n, n+0.1, k*1.1))
	case "dip_black":
		if k < 0.5 {
			return mixc(sa.at(x, y), rgb{}, smoothstep(0, 1, 2*k))
		}
		return mixc(rgb{}, sb.at(x, y), smoothstep(0, 1, 2*k-1))
	case "flash":
		if k < 0.5 {
			return mixc(sa.at(x, y), white, smoothstep(0, 1, 2*k))
		}
		return mixc(white, sb.at(x, y), smoothstep(0, 1, 2*k-1))
	case "wipe":
		var u float64 // 0 where the new image appears first
		switch tr.Dir {
		case 1:
			u = x / W
		case 2:
			u = 1 - y/H
		case 3:
			u = y / H
		default:
			u = 1 - x/W
		}
		const f = 0.02
		e := ke*(1+2*f) - f
		return mixc(sa.at(x, y), sb.at(x, y), 1-smoothstep(e-f, e+f, u))
	case "slide", "push":
		dx, dy := dirVec(tr.Dir)
		// B comes from the side opposite to its movement
		bx, by := x+dx*W*(1-ke), y+dy*H*(1-ke)
		if inside(bx, by) {
			return sb.at(bx, by)
		}
		if config.TransitionModes[tr.Mode] == "push" {
			return sa.at(x-dx*W*ke, y-dy*H*ke)
		}
		return sa.at(x, y)
	case "zoom_in": // fly into the old image while the new one settles
		cx, cy := W/2, H/2
		s := 1 + 1.5*ke
		sb2 := 1 + 0.2*(1-ke)
		return mixc(sa.at(cx+(x-cx)/s, cy+(y-cy)/s), sb.at(cx+(x-cx)/sb2, cy+(y-cy)/sb2), smoothstep(0.3, 1, k))
	case "zoom_out": // the new image arrives from very close
		cx, cy := W/2, H/2
		s := 1 + 1.5*(1-ke)
		return mixc(sa.at(x, y), sb.at(cx+(x-cx)/s, cy+(y-cy)/s), smoothstep(0, 0.7, k))
	case "circle":
		diag := math.Hypot(W, H) / 2
		f := 0.01 * diag
		r := ke * (diag + f)
		d := math.Hypot(x-W/2, y-H/2)
		return mixc(sa.at(x, y), sb.at(x, y), 1-smoothstep(r-f, r+f, d))
	case "clock":
		ang := math.Atan2(x-W/2, -(y - H/2))
		if ang < 0 {
			ang += 2 * math.Pi
		}
		e := ke * (2*math.Pi + 0.04)
		return mixc(sa.at(x, y), sb.at(x, y), 1-smoothstep(e-0.04, e, ang))
	case "glitch":
		g := math.Sin(math.Pi * k)
		band := uint32(int(y) / max(1, h/24))
		step := uint32(k * 16)
		useB := hash3(band, step, tr.Seed) < k
		shift := (hash3(band, step+97, tr.Seed) - 0.5) * W * 0.08 * g
		split := 8 * g * (H / 1080)
		s := sa
		if useB {
			s = sb
		}
		xs := x + shift
		return rgb{s.at(xs+split, y)[0], s.at(xs, y)[1], s.at(xs-split, y)[2]}
	case "burn":
		return burnPixel(sa, sb, x, y, w, h, tr)
	case "pixelate":
		bs := 1 + math.Floor(math.Sin(math.Pi*k)*48*(H/1080))
		px, py := (math.Floor(x/bs)+0.5)*bs, (math.Floor(y/bs)+0.5)*bs
		return mixc(sa.at(px, py), sb.at(px, py), smoothstep(0.4, 0.6, k))
	}
	return mixc(sa.at(x, y), sb.at(x, y), k) // fade
}
