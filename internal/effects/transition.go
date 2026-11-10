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
	case "ken_burns": // zoom in while travelling from the opposite corner (or side) to the chosen one
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
		case "top":
			d = [2]float64{0, -1}
		case "bottom":
			d = [2]float64{0, 1}
		case "left":
			d = [2]float64{-1, 0}
		case "right":
			d = [2]float64{1, 0}
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
	Fluid      *FluidFrame // fluid / liquid: the particles at this instant
	// Side: the mix (0), or only the old image (1) or the new one (2) as the transition shows it
	// (the frame drawn once per image: MixSides)
	Side int
}

// pick: the transition's mix of a and b (w: how much of b), or only the side drawn
func (tr *Trans) pick(a, b rgb, w float64) rgb {
	switch tr.Side {
	case 1:
		return a
	case 2:
		return b
	}
	return mixc(a, b, w)
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
	// the 2.5D camera with this image's depth (nil: none), and how far it moves the image at
	// the pixel being drawn (as gParA / gParB in render.comp: found at the pixel, applied to
	// whatever point of the image the transition looks at)
	par      *Parallax
	depth    []uint16
	n        int
	pdx, pdy float64
}

// moveAt: the camera's move of this image at frame pixel (x, y)
func (s *sampler) moveAt(x, y float64) {
	if s.par == nil || s.img == nil {
		return
	}
	px, py, _ := s.par.Find(float32(x), float32(y), s.n, s.w, s.h, func(x, y float32) float32 {
		return layerDepth(s.depth, s.l, x, y, s.w, s.h)
	})
	s.pdx, s.pdy = x-float64(px), y-float64(py)
}

func newSampler(img *image.RGBA, l Layer, w, h int) sampler {
	return sampler{img: img, l: l, w: w, h: h, cs: math.Cos(-l.Rot), sn: math.Sin(-l.Rot)}
}

func (s *sampler) at(x, y float64) rgb { return s.raw(x-s.pdx, y-s.pdy) }

// atCam: the image at background point (x, y) seen by its camera (as camAt in fluid.comp: the
// fluid's pieces carry the images as they were moving)
func (s *sampler) atCam(x, y float64) rgb {
	if s.par == nil || s.img == nil {
		return s.raw(x, y)
	}
	px, py, _ := s.par.Find(float32(x), float32(y), s.n, s.w, s.h, func(x, y float32) float32 {
		return layerDepth(s.depth, s.l, x, y, s.w, s.h)
	})
	return s.raw(float64(px), float64(py))
}

// raw: the image at point (x, y) of the frame through its layer (motion)
func (s *sampler) raw(x, y float64) rgb {
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
	ComposeParallax(dst, a, b, la, lb, tr, Parallax{}, Parallax{}, nil, nil)
}

// ComposeParallax is Compose seen by the 2.5D cameras pa (image a) and pb (image b), each image
// with its depth (da, db; nil: that one doesn't move). In the fluid transitions the pieces carry
// the images as their cameras show them (as fluid.comp).
func ComposeParallax(dst, a, b *image.RGBA, la, lb Layer, tr Trans, pa, pb Parallax, da, db []uint16) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	if !tr.HasA {
		a = nil
	}
	if !tr.HasB {
		b = nil
	}
	sa, sb := newSampler(a, la, w, h), newSampler(b, lb, w, h)
	if pa.On() && da != nil {
		sa.par, sa.depth, sa.n = &pa, da, pa.Steps(w, h)
	}
	if pb.On() && db != nil {
		sb.par, sb.depth, sb.n = &pb, db, pb.Steps(w, h)
	}
	if tr.Fluid != nil {
		tr.Fluid.Render(dst, &sa, &sb)
		return
	}
	parallelFor(h, func(y0, y1 int) {
		sa, sb := sa, sb // each block moves its own copies
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				var c rgb
				// (each image's camera move where the transition reads it)
				ax, ay, bx, by := float64(x), float64(y), float64(x), float64(y)
				if da != nil && db != nil && (sa.par != nil || sb.par != nil) {
					ax, ay, bx, by, _ = TransPick(tr, float64(x), float64(y), w, h)
				}
				sa.moveAt(ax, ay)
				sb.moveAt(bx, by)
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

// TransPick: the transition's geometry at frame pixel (x, y), for the depth (as transPick in
// render.comp): where the old image is read (ax, ay) and the new one (bx, by), and how much of the
// new one is in the mix (from 0.5 it is the one seen there)
func TransPick(tr Trans, x, y float64, w, h int) (ax, ay, bx, by, wb float64) {
	ax, ay, bx, by = x, y, x, y
	W, H := float64(w), float64(h)
	k := math.Max(0, math.Min(1, tr.K))
	ke := k * k * (3 - 2*k)
	cx, cy := W/2, H/2
	mode := ""
	if tr.Mode >= 0 && tr.Mode < len(config.TransitionModes) {
		mode = config.TransitionModes[tr.Mode]
	}
	wb = k // fade
	switch mode {
	case "cut":
		wb = 0
	case "dissolve":
		n := hash3(uint32(int(math.Floor(x*0.5))), uint32(int(math.Floor(y*0.5))), tr.Seed)
		wb = smoothstep(n, n+0.1, k*1.1)
	case "dip_black", "flash":
		wb = smoothstep(0.3, 0.7, k) // (the depth glides while the frame is nearly black / white)
	case "wipe":
		var u float64
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
		wb = 1 - smoothstep(e-f, e+f, u)
	case "slide", "push":
		dx, dy := dirVec(tr.Dir)
		bx, by = x+dx*W*(1-ke), y+dy*H*(1-ke)
		if mode == "push" {
			ax, ay = x-dx*W*ke, y-dy*H*ke
		}
		wb = 0
		if bx >= 0 && bx < W && by >= 0 && by < H {
			wb = 1
		}
	case "zoom_in":
		s, sb := 1+1.5*ke, 1+0.2*(1-ke)
		ax, ay = cx+(x-cx)/s, cy+(y-cy)/s
		bx, by = cx+(x-cx)/sb, cy+(y-cy)/sb
		wb = smoothstep(0.3, 1, k)
	case "zoom_out":
		s := 1 + 1.5*(1-ke)
		bx, by = cx+(x-cx)/s, cy+(y-cy)/s
		wb = smoothstep(0, 0.7, k)
	case "circle":
		diag := math.Hypot(W, H) / 2
		f := 0.01 * diag
		r := ke * (diag + f)
		wb = 1 - smoothstep(r-f, r+f, math.Hypot(x-cx, y-cy))
	case "clock":
		ang := math.Atan2(x-cx, -(y - cy))
		if ang < 0 {
			ang += 2 * math.Pi
		}
		e := ke * (2*math.Pi + 0.04)
		wb = 1 - smoothstep(e-0.04, e, ang)
	case "glitch":
		g := math.Sin(math.Pi * k)
		band := uint32(int(math.Floor(y)) / max(1, h/24))
		step := uint32(k * 16)
		wb = 0
		if hash3(band, step, tr.Seed) < k {
			wb = 1
		}
		shift := (hash3(band, step+97, tr.Seed) - 0.5) * W * 0.08 * g
		ax, bx = x+shift, x+shift
	case "pixelate":
		bs := 1 + math.Floor(math.Sin(math.Pi*k)*48*(H/1080))
		ax, ay = (math.Floor(x/bs)+0.5)*bs, (math.Floor(y/bs)+0.5)*bs
		bx, by = ax, ay
		wb = smoothstep(0.4, 0.6, k)
	case "fluid", "liquid":
		// how much of the new image the pieces bring here (FluidFrame.Render)
		if f := tr.Fluid; f != nil && len(f.New) == w*h {
			xi, yi := min(max(int(x), 0), w-1), min(max(int(y), 0), h-1)
			wb = float64(f.New[yi*w+xi])
		} else if k >= 0.5 {
			wb = 1
		} else {
			wb = 0
		}
	case "burn":
		wb = smoothstep(0, 1.5/H, burnFront(tr, x, y, h)) // the hole
	}
	return
}

// TransW: how much of the new image the transition shows at (x, y), as transitionPixel mixes
// them (as transW in render.comp)
func TransW(tr Trans, x, y float64, w, h int) float64 {
	k := math.Max(0, math.Min(1, tr.K))
	mode := ""
	if tr.Mode >= 0 && tr.Mode < len(config.TransitionModes) {
		mode = config.TransitionModes[tr.Mode]
	}
	switch mode {
	case "dip_black", "flash":
		if k < 0.5 {
			return 0
		}
		return 1
	case "fluid", "liquid":
		if f := tr.Fluid; f == nil || len(f.New) != w*h {
			return k // (no fluid drawn: a fade)
		}
	}
	_, _, _, _, wb := TransPick(tr, x, y, w, h)
	return wb
}

// MixSides: the frame drawn once per image of transition tr (a: with what is on the old image,
// b: on the new one) mixed as the transition mixes them, at each pixel through the zoom and the
// shake on both (as shade in render.comp); into a
func MixSides(a, b *image.RGBA, tr Trans, zoom, shakeX, shakeY float64) {
	w, h := a.Bounds().Dx(), a.Bounds().Dy()
	cx, cy := float64(w)/2, float64(h)/2
	parallelFor(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				qx, qy := float64(x), float64(y)
				if zoom > 1.001 {
					qx, qy = cx+(qx-cx)/zoom, cy+(qy-cy)/zoom
				}
				k := TransW(tr, qx-shakeX, qy-shakeY, w, h)
				o := y*a.Stride + x*4
				for c := 0; c < 3; c++ {
					va, vb := float64(a.Pix[o+c]), float64(b.Pix[o+c])
					a.Pix[o+c] = uint8(math.Max(0, math.Min(255, va+(vb-va)*k+0.5)))
				}
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
		return tr.pick(sa.at(x, y), sb.at(x, y), smoothstep(n, n+0.1, k*1.1))
	case "dip_black", "flash":
		mid := rgb{}
		if config.TransitionModes[tr.Mode] == "flash" {
			mid = white
		}
		// (each image on its half: the old one goes to black / white, the new one comes from it)
		switch {
		case tr.Side == 1 && k < 0.5:
			return mixc(sa.at(x, y), mid, smoothstep(0, 1, 2*k))
		case tr.Side == 2 && k >= 0.5:
			return mixc(mid, sb.at(x, y), smoothstep(0, 1, 2*k-1))
		case tr.Side != 0:
			return mid
		}
		if k < 0.5 {
			return mixc(sa.at(x, y), mid, smoothstep(0, 1, 2*k))
		}
		return mixc(mid, sb.at(x, y), smoothstep(0, 1, 2*k-1))
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
		return tr.pick(sa.at(x, y), sb.at(x, y), 1-smoothstep(e-f, e+f, u))
	case "slide", "push":
		dx, dy := dirVec(tr.Dir)
		// B comes from the side opposite to its movement
		bx, by := x+dx*W*(1-ke), y+dy*H*(1-ke)
		if tr.Side == 2 || (tr.Side == 0 && inside(bx, by)) {
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
		return tr.pick(sa.at(cx+(x-cx)/s, cy+(y-cy)/s), sb.at(cx+(x-cx)/sb2, cy+(y-cy)/sb2), smoothstep(0.3, 1, k))
	case "zoom_out": // the new image arrives from very close
		cx, cy := W/2, H/2
		s := 1 + 1.5*(1-ke)
		return tr.pick(sa.at(x, y), sb.at(cx+(x-cx)/s, cy+(y-cy)/s), smoothstep(0, 0.7, k))
	case "circle":
		diag := math.Hypot(W, H) / 2
		f := 0.01 * diag
		r := ke * (diag + f)
		d := math.Hypot(x-W/2, y-H/2)
		return tr.pick(sa.at(x, y), sb.at(x, y), 1-smoothstep(r-f, r+f, d))
	case "clock":
		ang := math.Atan2(x-W/2, -(y - H/2))
		if ang < 0 {
			ang += 2 * math.Pi
		}
		e := ke * (2*math.Pi + 0.04)
		return tr.pick(sa.at(x, y), sb.at(x, y), 1-smoothstep(e-0.04, e, ang))
	case "glitch":
		g := math.Sin(math.Pi * k)
		band := uint32(int(y) / max(1, h/24))
		step := uint32(k * 16)
		useB := hash3(band, step, tr.Seed) < k
		if tr.Side != 0 {
			useB = tr.Side == 2
		}
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
		return tr.pick(sa.at(px, py), sb.at(px, py), smoothstep(0.4, 0.6, k))
	}
	return tr.pick(sa.at(x, y), sb.at(x, y), k) // fade
}
