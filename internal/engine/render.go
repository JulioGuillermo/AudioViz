package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/encode"
	"audioviz/internal/gpu"
	"audioviz/internal/lyrics"
	"audioviz/internal/mux"
	"audioviz/internal/visualizer"

	"github.com/fogleman/gg"
)

// frameFX holds a frame's background effect parameters (shared by CPU and GPU)
type frameFX struct {
	zoom                        float64
	rippleAmp, rippleFreq, rTim float64
	chromaShift                 float64
	chromaMode                  int // 0 no, 1 horizontal, 2 radial
	glitch                      bool
	shakeX, shakeY              float64
	bright, contrast            float64
	par                         effects.Parallax // the 2.5D camera (with a depth map)
	zoomDolly                   float64          // the zoom's share that is a camera moving in
	dof                         effects.DoF      // depth of field
	// background effects only on a depth range: from, to, soft, on
	rFlash, rChroma, rRipple, rGlitch [4]float64
	glitchAmt                         float64   // the glitch's intensity
	on                                [nFX]bool // the effects that work here (a segment picked: config.Picks)
}

// the background effects that can be bound to background segments (config.Picks)
const (
	fxZoom = iota
	fxShake
	fxChroma
	fxRipple
	fxGlitch
	fxFlash
	fxPar // the 2.5D camera: it moves only its image (and what is in depth on it)
	fxDoF // the depth of field: it blurs only its image (and what is in depth on it)
	nFX
)

// fxFor: the background effects on image side of the frame's transition (1 A, 2 B; 0 the frame
// as a whole)
func (fs *FrameState) fxFor(side int) *frameFX {
	if side == 0 {
		return &fs.FX
	}
	return &fs.FXS[side-1]
}

// RenderOptions are CLI options for preview or partial rendering
type RenderOptions struct {
	MaxDurationSec float64 // If > 0, limits the video duration (e.g. 10s for preview)
	StartSec       float64 // Preview start second
	SingleFrameAt  float64 // If >= 0, exports only that frame as a PNG image
	SingleFrameOut string  // Output path of the single-frame image
}

// Progress reports the progress of an export
type Progress struct {
	Done, Total int
	FPS         float64
	ETA         float64
	Stage       string
}

// ---------------------------------------------------------------- renderer GPU/CPU

// Renderer draws a session's frames on the GPU (Vulkan) or, if unavailable, on the CPU
type Renderer struct {
	G       *gpu.Renderer
	s       *Session
	verA    []string
	verB    []string
	lyrVer  []int
	textCap int
	// layerVer: per slot, the overlay images in its buffer (their Ver, in order)
	layerVer [][]string
}

// layerCap: room for the overlay images, with margin so that adding clips while editing does
// not recreate the renderer every time (0 when the project has no layers)
func layerCap(s *Session) int {
	need := s.LayerPixels()
	if need == 0 {
		return 0
	}
	return max(need+need/2, 2*s.W*s.H)
}

// roomFor: room for n clips drawn at once, with margin so that adding some while editing does not
// recreate the renderer every time (0 when there are none)
func roomFor(n int) int {
	if n == 0 {
		return 0
	}
	return max(n+n/2, 8)
}

// NewRenderer creates the render backend ("auto", "gpu" or "cpu") for the session's resolution
func NewRenderer(s *Session, mode string, slots int) (*Renderer, error) {
	r := &Renderer{s: s}
	mode = strings.ToLower(mode)
	if mode != "cpu" {
		textCap := 1 << 20
		if s.lyr != nil {
			textCap = max(textCap, len(s.lyr.Atlas.Pix)*2)
		}
		g, err := gpu.New(s.W, s.H, slots, s.Cfg.Output.GPUDevice, textCap, effects.FluidBufWords(s.W, s.H), layerCap(s),
			roomFor(s.LayerCount()), roomFor(s.EmitterCount()))
		if err != nil {
			if mode == "gpu" {
				return nil, fmt.Errorf("Vulkan GPU not available: %w", err)
			}
			s.Logf("⚠️  Vulkan GPU not available (%v), using CPU", err)
		} else {
			r.G, r.textCap = g, textCap
			r.verA, r.verB, r.lyrVer = make([]string, slots), make([]string, slots), make([]int, slots)
			r.layerVer = make([][]string, slots)
			s.Logf("🖥️  GPU rendering (Vulkan): %s", g.DeviceName)
		}
	}
	if r.G == nil {
		s.Logf("🖥️  CPU rendering (%d threads)", runtime.NumCPU())
	}
	return r, nil
}

// Fits reports whether the renderer suits the session as it is now (resolution and atlas)
func (r *Renderer) Fits() bool {
	if r.G == nil {
		return true
	}
	need := 0
	if r.s.lyr != nil {
		need = len(r.s.lyr.Atlas.Pix)
	}
	return r.G.Width == r.s.W && r.G.Height == r.s.H && need <= r.textCap && r.s.LayerPixels() <= r.G.LayerWords &&
		r.s.LayerCount() <= r.G.Layers && r.s.EmitterCount() <= r.G.Emitters
}

func (r *Renderer) Close() {
	if r.G != nil {
		r.G.Close()
		r.G = nil
	}
}

// submit prepares and launches frame fs in the slot (GPU only)
func (r *Renderer) submit(slot int, fs *FrameState, writeRGBA bool) error {
	g := r.G
	if fs.BgA != nil && r.verA[slot] != fs.VerA {
		copy(g.Background(slot), fs.BgA.Pix)
		r.verA[slot] = fs.VerA
	}
	if fs.BgB != nil && r.verB[slot] != fs.VerB {
		copy(g.Background2(slot), fs.BgB.Pix)
		r.verB[slot] = fs.VerB
	}
	if fs.Lyr != nil && r.lyrVer[slot] != r.s.LyrVer {
		copy(g.Text(slot, len(fs.Lyr.Atlas.Pix)), fs.Lyr.Atlas.Pix)
		r.lyrVer[slot] = r.s.LyrVer
	}
	p := g.Params(slot)
	r.s.fillParams(p, fs, g.PadW, g.PadH, writeRGBA)
	fluid := r.fluidData(slot, p, fs.Trans.Fluid)
	emit := r.layerData(slot, p, fs)
	return g.Submit(slot, fluid, emit)
}

// layerData uploads the images of the overlay clips (only when they changed for this slot) and
// fills their parameters. Clips that do not fit in the buffer are left out. An emitter clip's
// image is made by the emitter pass in its place (emitData); true if there is one.
func (r *Renderer) layerData(slot int, p *gpu.Params, fs *FrameState) bool {
	g := r.G
	n := 0
	buf := g.LayerData(slot)
	lps := g.LayerParams(slot)
	var vers []string
	off := 0
	// first the depth of the backgrounds A and B (a byte per pixel), then the clips
	dw := (r.s.W*r.s.H + 1) / 2 // 16 bits per pixel
	p.DepthI, p.NormI = [4]int32{}, [4]int32{}
	var depths [4][]uint16 // A, B, then their normals (packed the same way)
	for k, d := range [2][]uint16{fs.DepthA, fs.DepthB} {
		if d != nil && off+dw <= g.LayerWords {
			depths[k] = d
			p.DepthI[k] = int32(off)
			p.DepthI[2] |= 1 << k
			off += dw
		}
	}
	for k, nm := range [2][]uint16{fs.NormA, fs.NormB} {
		if nm != nil && depths[k] != nil && off+dw <= g.LayerWords {
			depths[2+k] = nm
			p.NormI[k] = int32(off)
			p.NormI[2] |= 1 << k
			off += dw
		}
	}
	vers = append(vers, "dA"+fs.VerDA, "dB"+fs.VerDB, fmt.Sprint("n", p.NormI[2]))
	type emitAt struct {
		e   *effects.EmitFrame
		off int
		gas int // where its gas grid is (the gas look)
	}
	var emits []emitAt
	type trackAt struct {
		off int
		t   *effects.PathTrack
	}
	var tracks []trackAt
	type gasAt struct {
		off int
		g   *effects.GasFrame
	}
	var gases []gasAt
	for i := range fs.Layers {
		l := &fs.Layers[i]
		iw, ih := r.s.W, r.s.H
		if l.Img != nil {
			iw, ih = l.Img.Bounds().Dx(), l.Img.Bounds().Dy()
		}
		if l.Emit != nil && len(emits) == g.Emitters {
			break // no accumulation left for it
		}
		px := iw * ih
		lens := l.Emit != nil && l.Emit.P.Water && l.Emit.P.WaterLens > 0
		zplane := l.Emit != nil && l.Emit.P.ZTrack != nil && len(l.Emit.P.ZTrack.S) <= 256
		planes := 1
		if lens {
			planes++ // its lens after its image (x, y as halves)
		}
		if zplane {
			planes++ // its particles' depth (a float per pixel)
		}
		px *= planes
		// the gas look: its grid after its image (written every frame)
		gwords := 0
		if l.Emit != nil && l.Emit.P.Gas != nil {
			gwords = len(l.Emit.P.Gas.Cells) + len(l.Emit.P.Gas.Light)
		}
		px += gwords
		mwords := 0
		if l.Mask != nil {
			mwords = (len(l.Mask.Pix) + 3) / 4
		}
		// its depth along its path after its mask: n, then n lengths, values and easings
		twords := 0
		if l.ZTrack != nil {
			twords = 1 + 3*len(l.ZTrack.S)
		}
		if n == g.Layers || off+px+mwords+twords > g.LayerWords {
			break
		}
		vers = append(vers, fmt.Sprintf("%s|%s|%d|%d", l.Ver, l.MaskVer, twords, gwords))
		lp := &lps[n]
		flags := int32(l.Blend & 15)
		if l.Key {
			flags |= 1 << 4
		}
		if lens {
			flags |= 1 << 13
		}
		if zplane {
			flags |= 1 << 14
		}
		w := &l.W
		if w.GlitchBG {
			flags |= 1 << 5
		} else if len(w.Glitch) > 0 {
			flags |= 2<<5 | int32(min(len(w.Glitch), 8))<<8
			for j, gs := range w.Glitch {
				if j == 8 {
					break
				}
				lp.Glitch[j] = [4]float32{float32(gs.Y0), float32(gs.Y1), float32(gs.ShiftX)}
			}
		}
		lp.Info = [4]int32{int32(off), int32(iw), int32(ih), flags}
		lp.Light0, lp.Light1 = [4]int32{}, [4]float32{}
		lp.Depth, lp.Light2, lp.Light3, lp.Light4, lp.Light5 = [4]float32{}, [4]float32{}, [4]float32{}, [4]float32{}, [4]float32{}
		if l.Behind {
			lp.Depth = [4]float32{1, l.DepthZ, l.DepthSoft}
		}
		if l.Emit != nil {
			if P := &l.Emit.P; P.LightOn {
				// its light field, made by the emitter pass (k-th, after a batch's accumulations)
				stride, field := effects.EmitAccLayout(r.s.W, r.s.H)
				xw, xh := effects.EmitLightGrid(r.s.W, r.s.H)
				mode := int32(1)
				if P.LightAdd {
					mode = 2
				}
				lp.Light0 = [4]int32{int32(min(g.Emitters, gpu.EmitBatch)*stride + len(emits)*field), int32(xw), int32(xh), mode}
				lp.Light1 = [4]float32{float32(P.LightCol[0]), float32(P.LightCol[1]), float32(P.LightCol[2])}
				if P.LightD3 {
					lp.Light2 = [4]float32{float32(P.LightCX), float32(P.LightCY), float32(P.LightZ), float32(P.LightDepthK)}
					lp.Light3 = [4]float32{float32(P.LightShade), 1, float32(P.LightFrom), float32(P.LightTo)}
					lp.Light5 = [4]float32{0.04, float32(P.LightGloss), float32(P.LightShadow)}
					lp.Light1[3] = float32(P.LightSoft)
					if P.LightRim {
						lp.Light4 = [4]float32{float32(P.LightRimFrom), float32(P.LightRimSoft), float32(P.LightRimW), float32(P.LightRimGain)}
						lp.Light5[0] = float32(P.LightRimEdge)
					}
				}
			}
			ea := emitAt{e: l.Emit, off: off}
			if gwords > 0 {
				ea.gas = off + px - gwords
				gases = append(gases, gasAt{ea.gas, l.Emit.P.Gas})
			}
			emits = append(emits, ea)
		}
		lp.Xf = [4]float32{float32(l.CX), float32(l.CY), float32(l.SX), float32(l.SY)}
		lp.Xf2 = [4]float32{float32(l.Rot), float32(l.Opacity), float32(l.Blur), float32(l.Hue)}
		lp.Adj = [4]float32{float32(l.Brightness), float32(l.Contrast), float32(l.Saturate)}
		lp.Key = [4]float32{float32(l.KeyColor[0]), float32(l.KeyColor[1]), float32(l.KeyColor[2]), float32(l.KeyTol)}
		lp.Key2 = [4]float32{float32(l.KeySoft)}
		lp.Warp0 = [4]float32{float32(w.Zoom), float32(w.RippleAmp), float32(w.RippleFreq), float32(w.RippleTm)}
		lp.Warp1 = [4]float32{float32(w.ShakeX), float32(w.ShakeY), float32(w.Chroma), float32(w.ChromaMode)}
		lp.Warp2 = [4]float32{float32(w.FlashCF), float32(w.FlashBright)}
		off += px
		lp.Mask = [4]int32{}
		if l.Mask != nil {
			lp.Mask = [4]int32{int32(off), int32(l.Mask.Rect.Dx()), int32(l.Mask.Rect.Dy()), 1}
			off += mwords
		}
		lp.ZTrack = [4]float32{}
		if t := l.ZTrack; t != nil {
			// written every frame (below): it is small and moves with the path
			lp.ZTrack = [4]float32{math.Float32frombits(uint32(off + 1)), l.ZS0, l.ZTX, l.ZTY}
			tracks = append(tracks, trackAt{off, t})
			off += twords
		}
		n++
	}
	// The texts' screen masks after the clips (each style with a mask: its offset in TStyles)
	type tmask struct {
		si  int
		img *image.Alpha
	}
	var tms []tmask
	for si := 0; si < min(len(fs.TextStyles), gpu.MaxTextStyles); si++ {
		st := &fs.TextStyles[si]
		p.TStyles[si].Mask = [4]int32{}
		if st.Mask == nil {
			continue
		}
		words := (len(st.Mask.Pix) + 3) / 4
		if off+words > g.LayerWords {
			break
		}
		p.TStyles[si].Mask = [4]int32{int32(off), int32(st.Mask.Rect.Dx()), int32(st.Mask.Rect.Dy()), 1}
		off += words
		vers = append(vers, "t"+st.MaskVer)
		tms = append(tms, tmask{si, st.Mask})
	}
	if !slices.Equal(vers, r.layerVer[slot]) {
		off = 0
		for _, d := range depths {
			if d != nil {
				b := buf[off*4:]
				for i, v := range d {
					b[2*i], b[2*i+1] = byte(v), byte(v>>8)
				}
				off += dw
			}
		}
		for i := 0; i < n; i++ {
			l := &fs.Layers[i]
			if l.Emit != nil {
				off += r.s.W * r.s.H // drawn there by the emitter pass (and its lens, its depth)
				if l.Emit.P.Water && l.Emit.P.WaterLens > 0 {
					off += r.s.W * r.s.H
				}
				if t := l.Emit.P.ZTrack; t != nil && len(t.S) <= 256 {
					off += r.s.W * r.s.H
				}
				if g := l.Emit.P.Gas; g != nil {
					off += len(g.Cells) + len(g.Light)
				}
			} else {
				copy(buf[off*4:], l.Img.Pix)
				off += len(l.Img.Pix) / 4
			}
			if m := fs.Layers[i].Mask; m != nil {
				copy(buf[off*4:], m.Pix)
				off += (len(m.Pix) + 3) / 4
			}
			if t := fs.Layers[i].ZTrack; t != nil {
				off += 1 + 3*len(t.S)
			}
		}
		for _, tm := range tms {
			copy(buf[off*4:], tm.img.Pix)
			off += (len(tm.img.Pix) + 3) / 4
		}
		r.layerVer[slot] = vers
	}
	for _, tr := range tracks {
		putTrack(buf, tr.off, tr.t)
	}
	for _, ga := range gases {
		b := buf[ga.off*4:]
		for i, v := range ga.g.Cells {
			binary.LittleEndian.PutUint32(b[i*4:], v)
		}
		b = b[len(ga.g.Cells)*4:]
		for i, v := range ga.g.Light {
			binary.LittleEndian.PutUint32(b[i*4:], v)
		}
	}
	// the texts' depth along their paths and their twist as ribbons, after everything (written
	// every frame)
	for si := 0; si < min(len(fs.TextStyles), gpu.MaxTextStyles); si++ {
		p.TStyles[si].ZTrack = [4]float32{}
		st := &fs.TextStyles[si]
		if t := st.ZTrack; t != nil {
			if tw := 1 + 3*len(t.S); off+tw <= g.LayerWords {
				putTrack(buf, off, t)
				p.TStyles[si].ZTrack[0] = math.Float32frombits(uint32(off + 1))
				p.TStyles[si].ZTrack[1] = float32(st.ZMargin)
				off += tw
			}
		}
		if st.Wrap {
			p.TStyles[si].ZTrack[3] = 1
		}
		if rb := st.Ribbon; rb != nil {
			// as ribbonAt in render.comp reads it: n, length, closed, then n × (K, O)
			if tw := 3 + 2*len(rb.K); off+tw <= g.LayerWords {
				put := func(i int, v float32) { binary.LittleEndian.PutUint32(buf[(off+i)*4:], math.Float32bits(v)) }
				put(0, float32(len(rb.K)))
				put(1, rb.L)
				put(2, 0)
				if rb.Closed {
					put(2, 1)
				}
				for k := range rb.K {
					put(3+2*k, rb.K[k])
					put(4+2*k, rb.O[k])
				}
				p.TStyles[si].ZTrack[2] = math.Float32frombits(uint32(off + 1))
				off += tw
			}
		}
	}
	// the clips' properties per pixel (their expressions' programs: a table of two words per
	// effects.ClipPxProps, its program's offset + 1 and -, then the programs; written every frame)
	for k := 0; k < n; k++ {
		l := &fs.Layers[k]
		lp := &lps[k]
		lp.Xpr, lp.Xps = [4]int32{3: int32(l.Side)}, [4]float32{}
		x0 := l.FirstX()
		if x0 == nil {
			continue
		}
		put := func(i int, u uint32) { binary.LittleEndian.PutUint32(buf[i*4:], u) }
		tab := 2 * effects.NClipPx
		need := tab
		for _, x := range l.X {
			if x != nil {
				need += effects.PixExprWords(x) + 1
			}
		}
		if off+need > g.LayerWords {
			continue
		}
		o := off + tab
		for j, x := range l.X {
			put(off+2*j, 0)
			put(off+2*j+1, 0)
			if x == nil {
				continue
			}
			effects.PackPixExpr(put, o, x)
			put(off+2*j, uint32(o+1))
			put(off+2*j+1, math.Float32bits(float32(l.XV[j]))) // its value (the expression's value)
			o += effects.PixExprWords(x)
		}
		lp.Xpr = [4]int32{int32(off + 1), int32(x0.Seed), int32(x0.Frame), int32(l.Side)}
		lp.Xps = [4]float32{l.XS0, l.XTX, l.XTY, float32(l.OpK)}
		off += need
	}
	// the texts' properties per letter / pixel (their expressions' programs: a table of two words
	// per lyrics.TextPxProps, its program's offset + 1 and per letter, then the programs), after
	// everything (every frame)
	for si := 0; si < min(len(fs.TextStyles), gpu.MaxTextStyles); si++ {
		ts := &p.TStyles[si]
		ts.Xpr = [4]int32{}
		st := &fs.TextStyles[si]
		x0 := st.FirstX()
		if x0 == nil {
			continue
		}
		put := func(i int, u uint32) { binary.LittleEndian.PutUint32(buf[i*4:], u) }
		tab := 2 * lyrics.NTextPx
		need := tab
		for _, x := range st.X {
			if x != nil {
				need += effects.PixExprWords(x)
			}
		}
		if off+need > g.LayerWords {
			continue
		}
		o := off + tab
		for k, x := range st.X {
			put(off+2*k, 0)
			put(off+2*k+1, 0)
			if x == nil {
				continue
			}
			effects.PackPixExpr(put, o, x)
			put(off+2*k, uint32(o+1))
			if x.Letter {
				put(off+2*k+1, 1)
			}
			o += effects.PixExprWords(x)
		}
		ts.Xpr = [4]int32{int32(off + 1), int32(math.Float32bits(float32(st.Opacity))), int32(x0.Seed), int32(x0.Frame)}
		off += need
	}
	// Stages beyond the clips drawn (n) go on top: render.comp clamps them to n
	p.LayerN = [4]int32{int32(n)}
	p.ZIdx[0], p.ZIdx[1] = int32(fs.ZParts), int32(fs.ZViz)
	fa := fs.fxFor(1) // (image A's, or the frame's: fillParams)
	p.FxFlash = [4]float32{float32(contrastFactor(fa.contrast)), float32(fa.bright)}
	if g.Emitters == 0 {
		return false
	}
	u, f := g.EmitData(slot)
	u[0], u[1], u[2] = uint32(len(emits)), uint32(r.s.W), uint32(r.s.H)
	base, bo := 0, 4+g.Emitters*gpu.EmitHeaderWords
	for k, em := range emits {
		e := em.e
		h := 4 + k*gpu.EmitHeaderWords
		bs := e.Births
		if len(bs) > gpu.MaxEmitBirths {
			bs = bs[len(bs)-gpu.MaxEmitBirths:]
		}
		i0 := e.I0
		if len(bs) > 0 {
			i0 = max(i0, int(math.Floor(bs[0].N-0.5)))
		}
		cnt := max(0, e.I1-i0)
		P := &e.P
		flags := uint32(0)
		if P.Soft {
			flags = 1
		}
		u[h], u[h+1], u[h+2], u[h+3] = uint32(base), uint32(i0), uint32(cnt), uint32(bo)
		u[h+4], u[h+5], u[h+6], u[h+7] = uint32(len(bs)), uint32(em.off), uint32(P.Shape), flags
		v := []float64{P.W, P.H, P.Dir, P.Spread, P.Speed, P.SpeedVar, P.Gravity, P.Wind, P.Drag, P.Turb, P.TurbFreq, P.Life,
			P.LifeVar, P.Size, P.SizeEnd, P.SizeVar}
		for j, x := range v {
			f[h+8+j] = float32(x)
		}
		for c := 0; c < 3; c++ {
			for j := 0; j < 4; j++ {
				f[h+24+c*4+j] = float32(P.Col[c][j])
			}
		}
		f[h+36], f[h+37], f[h+38], f[h+39] = float32(P.FadeIn), float32(P.FadeOut), float32(P.Streak), float32(P.Softness)
		u[h+40] = P.Seed
		u[h+42] = uint32(max(P.Div, 1))
		u[h+43] = 0
		if P.Flame {
			u[h+43] = 1
		} else if P.Smoke {
			u[h+43] = 2
		} else if P.Water {
			u[h+43] = 3
		}
		for c := 0; c < 3; c++ {
			for j := 0; j < 3; j++ {
				f[h+44+c*3+j] = float32(P.FlameCol[c][j])
			}
		}
		f[h+53], f[h+54], f[h+55], f[h+56], f[h+57] = float32(P.Heat), float32(P.Noise), float32(P.NoiseScale), float32(P.NoiseSpeed), float32(P.Edge)
		f[h+58], f[h+59] = float32(P.GlowI), float32(P.GlowR)
		f[h+60], f[h+61], f[h+62] = float32(P.GlowCol[0]), float32(P.GlowCol[1]), float32(P.GlowCol[2])
		u[h+63] = 0
		if P.GlowOwn {
			u[h+63] = 1
		}
		u[h+64] = 0
		if P.LightOn {
			u[h+64] = 1
		}
		f[h+65] = float32(P.LightR)
		f[h+66] = float32(P.LightEnd)
		f[h+41] = float32(e.T)
		for j, b := range bs {
			o := bo + j*gpu.EmitBirthWords
			f[o], f[o+1], f[o+2], f[o+3] = float32(b.T), float32(b.N), float32(b.X), float32(b.Y)
			f[o+4], f[o+5], f[o+6], f[o+7] = float32(b.Rot), float32(b.Speed), float32(b.Size), float32(b.Scale)
			f[o+8], f[o+9] = float32(b.PathP), 0
		}
		// its extra data: the points on a text's letters, its motion path
		xo := 4 + g.Emitters*(gpu.EmitHeaderWords+gpu.MaxEmitBirths*gpu.EmitBirthWords) + k*gpu.EmitExtraWords
		np := min(len(P.Pts)/2, effects.MaxEmitPts)
		copy(f[xo:], P.Pts[:np*2])
		u[h+67], u[h+68] = uint32(xo), uint32(np)
		po := xo + effects.MaxEmitPts*2
		ns := 0
		if P.Follow {
			ns = min(len(P.Path)/3, effects.MaxPathSamples)
			copy(f[po:], P.Path[:ns*3])
		}
		u[h+69], u[h+70], f[h+71] = uint32(po), uint32(ns), P.PathLen
		fl := uint32(0)
		if P.PathClosed {
			fl |= 1
		}
		if P.FloorOn {
			fl |= 2
		}
		if P.Route {
			fl |= 4
		}
		if P.Route && !P.RouteFixed {
			fl |= 8
		}
		u[h+72] = fl
		f[h+73], f[h+74], f[h+75] = float32(P.Floor), float32(P.Bounce), float32(P.Friction)
		f[h+76], f[h+77], f[h+78], f[h+79] = float32(P.SmokeWisp), float32(P.SmokeScale), float32(P.SmokeSpeed), float32(P.SmokeWarp)
		// what they go round in the scene: its signed distance grid
		co := po + effects.MaxPathSamples*3
		u[h+80], u[h+81], u[h+82], u[h+83] = uint32(co), 0, 0, 0
		f[h+84] = float32(P.CBounce)
		f[h+86], f[h+87], f[h+88], f[h+89] = float32(P.WaterTh), float32(P.WaterClear), float32(P.WaterShine), float32(P.WaterBump)
		f[h+90], f[h+91], f[h+92] = float32(P.WaterRipple), float32(P.WaterScale), float32(P.WaterSpeed)
		f[h+93] = float32(P.RouteK)
		f[h+94] = 0
		if P.Water {
			f[h+94] = float32(P.WaterLens)
		}
		// the depth along their path: after the collision grid
		zo := co + effects.MaxCollideCells
		u[h+95] = 0
		if t := P.ZTrack; t != nil && len(t.S) <= 256 {
			n := len(t.S)
			f[zo] = float32(n)
			for j := 0; j < n; j++ {
				f[zo+1+j], f[zo+1+n+j], f[zo+1+2*n+j] = t.S[j], t.V[j], float32(t.E[j])
			}
			u[h+95] = uint32(zo + 1)
		}
		// the expressions worked out per particle: after the depth track
		eo := zo + 1 + 3*256
		u[h+96], u[h+97], u[h+98] = 0, uint32(P.Frame), uint32(max(P.Count, 1))
		if len(P.Exprs) > 0 {
			effects.PackEmitExprs(u, f, eo, P.Exprs)
			u[h+96] = uint32(eo + 1)
		}
		// the scene's depth on screen: each particle hidden where it is nearer (after the expressions)
		oo := eo + gpu.EmitExprWords
		u[h+99] = 0
		if P.Occl != nil && P.OGW*P.OGH <= effects.MaxCollideCells && P.OGW >= 2 && P.OGH >= 2 {
			copy(f[oo:], P.Occl)
			u[h+99], u[h+100], u[h+101], u[h+102] = uint32(oo+1), uint32(P.OGW), uint32(P.OGH), uint32(P.ODiv)
			f[h+103] = P.OSoft
		}
		u[h+85] = 0
		if P.Hidden {
			u[h+85] = 1
		}
		// the gas look (effects.GasFrame, gasPixel): its grid in the overlay buffer (+1; 0: none
		// yet), its size, cells, the first wisps' weight; how it looks
		if P.GasOn {
			u[h+43] = 4
			u[h+104], u[h+105], u[h+106], u[h+119] = 0, 0, 0, 0
			if gf := P.Gas; gf != nil && em.gas > 0 {
				u[h+104], u[h+105], u[h+106] = uint32(em.gas+1), uint32(gf.GW), uint32(gf.GH)
				f[h+107], f[h+108] = gf.CS, gf.W1
				if len(gf.Light) > 0 {
					u[h+119] = uint32(em.gas + len(gf.Cells) + 1) // its flames' light sources
				}
			}
			f[h+109], f[h+110], f[h+111] = float32(P.GasOpacity), float32(P.GasWisp), float32(P.GasWispScale)
			for j := 0; j < 3; j++ {
				f[h+112+j], f[h+115+j] = float32(P.GasLightCol[j]), float32(P.GasShadow[j])
			}
			f[h+118] = float32(P.GasFireLight)
		}
		if P.CSDF != nil && P.CGW*P.CGH <= effects.MaxCollideCells {
			copy(f[co:], P.CSDF)
			u[h+81], u[h+82], u[h+83] = uint32(P.CGW), uint32(P.CGH), uint32(P.CDiv)
		}
		bo += gpu.MaxEmitBirths * gpu.EmitBirthWords
		base += cnt
	}
	return len(emits) > 0
}

// fluidData uploads the fluid / liquid frame (particles, bins, coarse level) for the fluid pass;
// false when there is none (the main pass then shows a fade)
func (r *Renderer) fluidData(slot int, p *gpu.Params, fr *effects.FluidFrame) bool {
	p.FluidI, p.FluidN, p.FluidF = [4]int32{}, [4]int32{}, [4]float32{}
	if fr == nil {
		return false
	}
	oStart := len(fr.Parts)
	oCoarse := oStart + len(fr.BinStart)
	if oCoarse+len(fr.Coarse) > r.G.FluidWords {
		return false
	}
	u, f := r.G.FluidData(slot)
	copy(f, fr.Parts)
	copy(u[oStart:], fr.BinStart)
	copy(f[oCoarse:], fr.Coarse)
	p.FluidI = [4]int32{int32(oStart), 0, int32(oCoarse), int32(fr.Bin)}
	p.FluidN = [4]int32{int32(fr.NBX), int32(fr.NBY), int32(fr.CW), int32(fr.CH)}
	p.FluidF = [4]float32{fr.HBW, fr.HBH, float32(fr.K)}
	if effects.FluidDebug {
		p.FluidF[3] = 1
	}
	return true
}

// Still renders a single frame as an RGBA image
func (r *Renderer) Still(fs *FrameState) (*image.RGBA, error) {
	if r.G == nil {
		return r.s.cpuFrame(fs), nil
	}
	if err := r.submit(0, fs, true); err != nil {
		return nil, err
	}
	if err := r.G.Wait(0); err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, r.G.Width, r.G.Height))
	copy(out.Pix, r.G.RGBA(0))
	return out, nil
}

// View is a studio view of the scene's depth instead of the frame (gpu.Params.ViewI): mode and
// its values
type View struct {
	Mode int
	P, Q [4]float32
}

// PreviewView renders a view of the scene's depth on the GPU (false: no GPU; the caller draws it)
func (r *Renderer) PreviewView(fs *FrameState, scale int, v View) (*image.RGBA, bool, error) {
	if r.G == nil {
		return nil, false, nil
	}
	p := r.G.Params(0)
	p.ViewI, p.ViewP, p.ViewQ = [4]int32{int32(v.Mode)}, v.P, v.Q
	defer func() { p.ViewI = [4]int32{} }()
	img, err := r.Preview(fs, scale)
	return img, true, err
}

// Preview renders a frame at 1/scale resolution directly on the GPU (faster)
func (r *Renderer) Preview(fs *FrameState, scale int) (*image.RGBA, error) {
	if r.G == nil || scale <= 1 {
		img, err := r.Still(fs)
		if err != nil || scale <= 1 {
			return img, err
		}
		return downscale(img, scale), nil
	}
	pw, ph := (r.s.W+scale-1)/scale, (r.s.H+scale-1)/scale
	r.G.Params(0).Prev = [4]int32{int32(scale), int32(pw), int32(ph)}
	defer func() { r.G.Params(0).Prev = [4]int32{} }() // after waiting for the GPU
	if err := r.submit(0, fs, true); err != nil {
		return nil, err
	}
	if err := r.G.Wait(0); err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, pw, ph))
	copy(out.Pix, r.G.RGBA(0)[:pw*ph*4])
	return out, nil
}

func downscale(src *image.RGBA, f int) *image.RGBA {
	w, h := src.Bounds().Dx()/f, src.Bounds().Dy()/f
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	var wg sync.WaitGroup
	nw := runtime.NumCPU()
	for k := 0; k < nw; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			n := uint32(f * f)
			for y := k; y < h; y += nw {
				for x := 0; x < w; x++ {
					var cr, cg, cb uint32
					for dy := 0; dy < f; dy++ {
						o := (y*f+dy)*src.Stride + x*f*4
						for dx := 0; dx < f; dx++ {
							cr += uint32(src.Pix[o])
							cg += uint32(src.Pix[o+1])
							cb += uint32(src.Pix[o+2])
							o += 4
						}
					}
					d := dst.Pix[y*dst.Stride+x*4:]
					d[0], d[1], d[2], d[3] = uint8(cr/n), uint8(cg/n), uint8(cb/n), 255
				}
			}
		}(k)
	}
	wg.Wait()
	return dst
}

// ---------------------------------------------------------------- export

// ExportRange exports frames [start, end) to the output MP4
func (s *Session) ExportRange(ctx context.Context, mode string, start, end int, outPath string, progress func(Progress)) error {
	if s.Audio == nil {
		return fmt.Errorf("the project has no audio")
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	cfg := s.Cfg
	r, err := NewRenderer(s, mode, 3)
	if err != nil {
		return err
	}
	defer r.Close()

	preset := cfg.Output.Preset
	if preset == "" {
		preset = "fast"
	}
	crf := float64(cfg.Output.CRF)
	if crf <= 0 {
		crf = 18
	}
	fps := s.FPS
	enc, err := encode.NewX264(s.W, s.H, fps, preset, crf)
	if err != nil {
		return err
	}
	defer enc.Close()
	if dir := filepath.Dir(outPath); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	mp4, err := mux.NewMP4Writer(outPath, s.W, s.H, fps)
	if err != nil {
		return err
	}
	var sps, pps []byte
	for _, nal := range mux.SplitAnnexB(enc.Headers()) {
		switch nal[0] & 0x1F {
		case 7:
			sps = nal
		case 8:
			pps = nal
		}
	}
	mp4.SetAVCConfig(sps, pps)

	// Audio: the exact segment, to lossless FLAC in parallel with rendering
	var audioWG sync.WaitGroup
	audioWG.Add(1)
	go func() {
		defer audioWG.Done()
		d := s.Audio
		s0 := int(float64(start) / float64(fps) * float64(d.SampleRate))
		s1 := min(int(float64(end)/float64(fps)*float64(d.SampleRate)), len(d.PCM)/2)
		st := audio.EncodeFLAC(d.PCM[s0*2:s1*2], d.SampleRate, d.BitsPerSample, 0)
		mp4.SetAudio(&mux.AudioTrack{SampleRate: d.SampleRate, Channels: 2, BitsPerSample: d.BitsPerSample,
			StreamInfo: st.StreamInfo, Frames: st.Frames, FrameSamples: st.FrameSamples})
	}()
	emit := func(p *encode.Packet) error {
		if p == nil {
			return nil
		}
		return mp4.WriteVideoAnnexB(p.Data, p.PTS, p.DTS, p.Key)
	}

	total := end - start
	t0 := time.Now()
	last := time.Time{}
	done := 0
	report := func() {
		if time.Since(last) < 250*time.Millisecond && done != total {
			return
		}
		last = time.Now()
		f := float64(done) / math.Max(time.Since(t0).Seconds(), 1e-3)
		progress(Progress{Done: done, Total: total, FPS: f, ETA: float64(total-done) / math.Max(f, 0.01), Stage: "render"})
	}

	if r.G != nil {
		g := r.G
		inFlight := make([]int, g.Slots)
		for i := range inFlight {
			inFlight[i] = -1
		}
		collect := func(slot int) error {
			if inFlight[slot] < 0 {
				return nil
			}
			if err := g.Wait(slot); err != nil {
				return err
			}
			p, err := enc.Encode(g.PlaneY(slot), g.PlaneUV(slot), g.PadW, int64(inFlight[slot]-start))
			if err != nil {
				return err
			}
			inFlight[slot] = -1
			done++
			report()
			return emit(p)
		}
		for i := start; i < end; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			slot := (i - start) % g.Slots
			if err := collect(slot); err != nil {
				return err
			}
			fs, err := s.Prepare(i)
			if err != nil {
				return err
			}
			if err := r.submit(slot, fs, false); err != nil {
				return err
			}
			inFlight[slot] = i
		}
		for k := 0; k < g.Slots; k++ {
			if err := collect((end - start + k) % g.Slots); err != nil {
				return err
			}
		}
	} else {
		stride := (s.W + 15) / 16 * 16
		ph := (s.H + 15) / 16 * 16
		yp := encode.Alloc(stride * ph)
		uvp := encode.Alloc(stride * ph / 2)
		defer encode.Free(yp)
		defer encode.Free(uvp)
		Y := unsafe.Slice((*byte)(yp), stride*ph)
		UV := unsafe.Slice((*byte)(uvp), stride*ph/2)
		for i := start; i < end; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			fs, err := s.Prepare(i)
			if err != nil {
				return err
			}
			rgbaToNV12(s.cpuFrame(fs), Y, UV, stride)
			p, err := enc.Encode(yp, uvp, stride, int64(i-start))
			if err != nil {
				return err
			}
			if err := emit(p); err != nil {
				return err
			}
			done++
			report()
		}
	}
	if err := enc.Flush(emit); err != nil {
		return err
	}
	progress(Progress{Done: done, Total: total, Stage: "mux"})
	audioWG.Wait()
	return mp4.Close()
}

// Execute is the CLI flow: a single PNG or the video (full or a segment)
func Execute(cfg *config.ProjectConfig, opts RenderOptions) error {
	startTime := time.Now()
	logf := func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
	if cfg.Audio.Path == "" {
		return fmt.Errorf("the 'audio.path' field is required")
	}
	s, err := Open(cfg, logf)
	if err != nil {
		return err
	}
	defer s.Close()
	logf("📐 Resolution: %dx%d @ %d fps", s.W, s.H, s.FPS)

	if opts.SingleFrameAt >= 0 {
		idx := min(int(opts.SingleFrameAt*float64(s.FPS)), s.Frames-1)
		r, err := NewRenderer(s, cfg.Output.Renderer, 1)
		if err != nil {
			return err
		}
		defer r.Close()
		fs, err := s.Prepare(idx)
		if err != nil {
			return err
		}
		img, err := r.Still(fs)
		if err != nil {
			return err
		}
		return savePNG(opts.SingleFrameOut, img)
	}

	start, end := 0, s.Frames
	if opts.StartSec > 0 {
		start = min(int(opts.StartSec*float64(s.FPS)), s.Frames-1)
	}
	if opts.MaxDurationSec > 0 {
		end = min(start+int(opts.MaxDurationSec*float64(s.FPS)), s.Frames)
	}
	logf("🚀 Rendering %d frames (x264 preset %s, CRF %d)...", end-start, cfg.Output.Preset, cfg.Output.CRF)
	t0 := time.Now()
	err = s.ExportRange(context.Background(), cfg.Output.Renderer, start, end, cfg.Output.Path, func(p Progress) {
		if p.Stage == "mux" {
			fmt.Printf("\n📦 Writing MP4...\n")
			return
		}
		fmt.Printf("\r⏳ [%5.1f%%] Frame %d/%d (%.1f FPS) | ETA: %.0fs    ", float64(p.Done)/float64(p.Total)*100, p.Done, p.Total, p.FPS, p.ETA)
	})
	if err != nil {
		return err
	}
	logf("✨ Completed in %s (%.1f FPS average)\n📁 %s", time.Since(startTime).Round(time.Millisecond),
		float64(end-start)/time.Since(t0).Seconds(), cfg.Output.Path)
	return nil
}

// ---------------------------------------------------------------- CPU

func (s *Session) cpuFrame(fs *FrameState) *image.RGBA {
	if !fs.Sided() {
		return s.cpuSide(fs, 0)
	}
	// once per image of the transition (some clip or effect is only on one of them), mixed as
	// the transition mixes them
	a, b := s.cpuSide(fs, 1), s.cpuSide(fs, 2)
	effects.MixSides(a, b, fs.Trans, fs.FX.zoom, fs.FX.shakeX, fs.FX.shakeY)
	return a
}

// cpuSide: the frame (side 0), or only one image of the transition with the clips and effects on
// it (1 the old one, 2 the new one)
func (s *Session) cpuSide(fs *FrameState, side int) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, s.W, s.H))
	fx := fs.fxFor(side)
	tr := fs.Trans
	tr.Side = side
	// (each image with its own camera, as render.comp: the same as fx's but on the other image of
	// a frame drawn once per image, and that one is seen only in a fluid's pieces)
	pa, pb := fs.FXS[0].par, fs.FXS[1].par
	if fs.DepthA == nil && fs.DepthB == nil {
		pa, pb = effects.Parallax{}, effects.Parallax{}
	}
	if tr.Mode == 0 && fs.LayerA == effects.Identity && !pa.On() {
		if fs.BgA != nil {
			copy(frame.Pix, fs.BgA.Pix)
		}
	} else {
		effects.ComposeParallax(frame, fs.BgA, fs.BgB, fs.LayerA, fs.LayerB, tr, pa, pb, fs.DepthA, fs.DepthB)
	}
	frame = effects.ApplyBassZoom(frame, fx.zoom, fx.shakeX, fx.shakeY)
	// the scene's depth at each pixel, for the effects on a depth range and the depth of field
	var z []float32
	if fs.Scene.On() && (fx.dof.On() || fx.rFlash[3] > 0 || fx.rChroma[3] > 0 || fx.rRipple[3] > 0 || fx.rGlitch[3] > 0) {
		z = effects.DepthField(fs.Scene, s.W, s.H)
	}
	if fx.rippleAmp > 0 {
		frame = effects.MaskedBlend(frame, effects.ApplyRipple(frame, fx.rippleAmp, fx.rippleFreq, fx.rTim), effects.RangeMaskField(z, fx.rRipple))
	}
	frame = effects.ApplyDoF(frame, z, fx.dof)
	if fx.chromaMode != 0 {
		mode := "horizontal"
		if fx.chromaMode == 2 {
			mode = "radial"
		}
		frame = effects.MaskedBlend(frame, effects.ApplyChromaticAberration(frame, fx.chromaShift, mode), effects.RangeMaskField(z, fx.rChroma))
	}
	if fx.glitch {
		frame = effects.MaskedBlend(frame, effects.ApplyGlitch(frame, fx.glitchAmt, int64(fs.Idx)), effects.RangeMaskField(z, fx.rGlitch))
	}
	if m := effects.RangeMaskField(z, fx.rFlash); m != nil {
		// the flash only on its depths: the colors without it, then with it, mixed
		base := image.NewRGBA(frame.Bounds())
		copy(base.Pix, frame.Pix)
		effects.ApplyColorAndContrast(base, 0, 0, fs.BG.Darken, fs.BG.Opacity, fs.BG.Vignette)
		effects.ApplyColorAndContrast(frame, fx.bright, fx.contrast, fs.BG.Darken, fs.BG.Opacity, fs.BG.Vignette)
		frame = effects.MaskedBlend(base, frame, m)
	} else {
		effects.ApplyColorAndContrast(frame, fx.bright, fx.contrast, fs.BG.Darken, fs.BG.Opacity, fs.BG.Vignette)
	}

	// Overlay clips in their order; the particles, the visualizer and the texts go at their stage
	items, styles := fs.TextItems, fs.TextStyles
	dc := gg.NewContextForRGBA(frame)
	for st := 0; st <= len(fs.Layers); st++ {
		if st > 0 && (side == 0 || fs.Layers[st-1].Side == 0 || fs.Layers[st-1].Side == side) {
			l := &fs.Layers[st-1]
			if l.Emit != nil && l.Img == nil {
				l.Img, l.Light = effects.DrawEmitter(l.Emit, s.W, s.H)
			}
			effects.DrawLayer(frame, l)
		}
		if fs.ZParts == st {
			for _, ps := range fs.Parts {
				ps.Draw(dc)
			}
		}
		if fs.ZViz == st {
			s.drawVizCPU(frame, fs)
		}
		var its []lyrics.Item
		for _, it := range items {
			if fs.Stage(styles[it.Style].Z) == st {
				its = append(its, it)
			}
		}
		// the texts of this stage that light the scene: on what is drawn so far, then the texts
		for _, tl := range fs.TextLights {
			if fs.Stage(styles[tl.Style].Z) == st {
				effects.LightOnto(frame, tl.Light, fs.Scene)
			}
		}
		if len(its) > 0 {
			fs.Lyr.DrawCPU(frame, its, styles)
		}
	}
	return frame
}

func rgbaToNV12(img *image.RGBA, Y, UV []byte, stride int) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var wg sync.WaitGroup
	rows := h / 2
	nw := runtime.NumCPU()
	for k := 0; k < nw; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for cy := k * rows / nw; cy < (k+1)*rows/nw; cy++ {
				for cx := 0; cx < w/2; cx++ {
					var sr, sg, sb float64
					for dy := 0; dy < 2; dy++ {
						for dx := 0; dx < 2; dx++ {
							x, y := cx*2+dx, cy*2+dy
							o := y*img.Stride + x*4
							r, g, b := float64(img.Pix[o])/255, float64(img.Pix[o+1])/255, float64(img.Pix[o+2])/255
							Y[y*stride+x] = uint8(16 + 219*(0.2126*r+0.7152*g+0.0722*b) + 0.5)
							sr, sg, sb = sr+r, sg+g, sb+b
						}
					}
					sr, sg, sb = sr/4, sg/4, sb/4
					UV[cy*stride+cx*2] = uint8(128 + 224*(-0.1146*sr-0.3854*sg+0.5*sb) + 0.5)
					UV[cy*stride+cx*2+1] = uint8(128 + 224*(0.5*sr-0.4542*sg-0.0458*sb) + 0.5)
				}
			}
		}(k)
	}
	wg.Wait()
}

func savePNG(path string, img *image.RGBA) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Printf("✅ Frame saved to: %s\n", path)
	return nil
}

// ---------------------------------------------------------------- GPU parameters

func rgbaF(hex string) [4]float32 {
	c, _ := visualizer.ParseHexColor(hex)
	return [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, float32(c.A) / 255}
}

// fxSet: a set of background effects as render.comp reads them (gpu.FXSet)
func (s *Session) fxSet(set *gpu.FXSet, fx *frameFX, fs *FrameState) {
	c := fx.contrast * 128.0
	cf := (259.0 * (c + 255.0)) / (255.0 * (259.0 - c))
	*set = gpu.FXSet{}
	set.ZoomRipple = [4]float32{float32(fx.zoom), float32(fx.rippleAmp), float32(fx.rippleFreq), float32(fx.rTim)}
	set.Shake = [4]float32{float32(fx.shakeX), float32(fx.shakeY)}
	// the 2.5D camera (with a depth map)
	set.Dof = [4]float32{float32(fx.dof.From), float32(fx.dof.To), float32(fx.dof.Soft), float32(fx.dof.Blur)}
	if fx.dof.On() {
		set.N[3] = effects.DoFTaps // the gather's samples
	}
	for i, r := range [4][4]float64{fx.rFlash, fx.rChroma, fx.rRipple, fx.rGlitch} {
		set.Ranges[i] = [4]float32{float32(r[0]), float32(r[1]), float32(r[2]), float32(r[3])}
	}
	if par := fx.par; par.On() && (fs.DepthA != nil || fs.DepthB != nil) {
		set.Parallax = [4]float32{float32(par.SX), float32(par.SY), float32(par.Dolly), float32(par.Focus)}
		set.N[2] = int32(par.Steps(s.W, s.H))
	}
	set.ColorAdj = [4]float32{float32(cf), float32(-fs.BG.Darken + fx.bright), float32(fs.BG.Opacity), float32(fs.BG.Vignette)}
	if fs.BgA == nil && fs.BgB == nil {
		set.ColorAdj[2] = 0 // no background: black
	}
	set.FxFlash = [4]float32{float32(contrastFactor(fx.contrast)), float32(fx.bright)}
	set.Chroma = [4]float32{float32(fx.chromaShift)}
	set.N[1] = int32(fx.chromaMode)
	if fx.glitch {
		n := 0
		for _, sl := range effects.GlitchSlices(s.W, s.H, fx.glitchAmt, int64(fs.Idx)) {
			if n == gpu.MaxGlitch {
				break
			}
			set.Glitch[n] = [4]float32{float32(sl.Y0), float32(sl.Y1), float32(sl.ShiftX)}
			n++
		}
		set.N[0] = int32(n)
	}
}

func (s *Session) fillParams(p *gpu.Params, fs *FrameState, padW, padH int, writeRGBA bool) {
	v := &fs.Viz
	feat := fs.VizFeat
	fx := fs.fxFor(1)
	W, H := float64(s.W), float64(s.H)
	b2i := func(b bool) int32 {
		if b {
			return 1
		}
		return 0
	}

	p.Dims = [4]int32{int32(s.W), int32(s.H), int32(padW), int32(padH)}

	// Background: its effects (image A's in a transition drawn once per image; B's in FXB)
	var set gpu.FXSet
	s.fxSet(&set, fx, fs)
	p.ZoomRipple, p.Shake, p.Chroma, p.ColorAdj = set.ZoomRipple, set.Shake, set.Chroma, set.ColorAdj
	p.Parallax, p.Dof, p.Ranges, p.Glitch = set.Parallax, set.Dof, set.Ranges, set.Glitch
	p.ParallaxN = [4]int32{set.N[2], set.N[3]}
	nGlitch := int(set.N[0])
	p.BgMix = [4]float32{0, float32(fs.VizAlpha)}
	// Background layers (motion) and transition (see composite() in render.comp)
	layer := func(l effects.Layer) [4]float32 {
		return [4]float32{float32(l.Scale), float32(l.OffX), float32(l.OffY), float32(l.Rot)}
	}
	tr := fs.Trans
	has := int32(0)
	if tr.HasA {
		has |= 1
	}
	if tr.HasB {
		has |= 2
	}
	p.LayerA, p.LayerB = layer(fs.LayerA), layer(fs.LayerB)
	p.Trans = [4]int32{int32(tr.Mode), int32(tr.Dir), has, int32(tr.Seed & 0x7fffffff)}
	p.TransP = [4]float32{float32(tr.K)}
	p.FXB, p.MixZS = gpu.FXSet{}, [4]float32{}
	if fs.Sided() {
		// drawn once per image (render.comp shade): B's effects, and the zoom and shake on both
		// for the mix
		p.TransP[1] = 1
		s.fxSet(&p.FXB, fs.fxFor(2), fs)
		p.MixZS = [4]float32{float32(fs.FX.zoom), float32(fs.FX.shakeX), float32(fs.FX.shakeY)}
	}
	if b := tr.Burn; len(b.Pts) > 0 {
		p.BurnP = [4]float32{float32(b.Edge), float32(b.Rough), float32(b.MaxD), float32(len(b.Pts))}
		p.BurnCol = [4]float32{float32(b.Color[0]), float32(b.Color[1]), float32(b.Color[2])}
		for i, q := range b.Pts {
			if i < len(p.BurnPts) {
				p.BurnPts[i] = [4]float32{float32(q[0]), float32(q[1])}
			}
		}
	}

	// Particles
	np := 0
	for _, ps := range fs.Parts {
		ps.Each(func(x, y, size, alpha float64, col color.RGBA, glow bool) {
			if np < gpu.MaxParts {
				p.Parts[np] = [4]float32{float32(x), float32(y), float32(size), float32(alpha)}
				g := float32(0)
				if glow {
					g = 1
				}
				p.PartCol[np] = [4]float32{float32(col.R) / 255, float32(col.G) / 255, float32(col.B) / 255, g}
				np++
			}
		})
	}

	// Visualizer
	vizType := int32(0)
	if v.Enabled && fs.VizAlpha > 0 {
		switch v.Type {
		case "circular", "circle", "radial":
			vizType = 1
		case "horizontal", "linear", "bottom":
			vizType = 2
		case "vertical", "edges":
			vizType = 3
		case "oscilloscope", "wave", "waveform":
			vizType = 4
		}
	}
	n := min(len(feat.LeftBars), gpu.MaxBars)
	for i := 0; i < n; i++ {
		l, r := feat.LeftBars[i], feat.RightBars[i]
		p.Bars[i] = [4]float32{float32(v.MinHeight + l*(v.MaxHeight-v.MinHeight)), float32(v.MinHeight + r*(v.MaxHeight-v.MinHeight)), float32(l), float32(r)}
	}

	colorMode := int32(0)
	if v.ColorMode == "solid" {
		colorMode = 1
	}
	ng := min(len(v.Gradient), gpu.MaxGrad)
	for i := 0; i < ng; i++ {
		c := rgbaF(v.Gradient[i].Color)
		c[3] = float32(v.Gradient[i].Offset)
		p.Grad[i] = c
	}
	p.SolidColor = rgbaF(v.SolidColor)
	p.WaveColL = [4]float32{0, 240.0 / 255, 1, 1}
	p.WaveColR = [4]float32{1, 0, 127.0 / 255, 1}
	if ng >= 2 {
		p.WaveColL = rgbaF(v.Gradient[0].Color)
		p.WaveColR = rgbaF(v.Gradient[ng-1].Color)
	}

	p.Flags = [4]int32{vizType, int32(n), int32(np), int32(nGlitch)}
	p.Flags2 = [4]int32{int32(fx.chromaMode), colorMode, int32(ng), b2i(writeRGBA)}
	p.Flags3 = [4]int32{b2i(v.MirrorBottom), b2i(v.Glow.Enabled), 0, 1}
	p.GradInfo = [4]float32{float32(b2i(v.GradientAxis == "length")), float32(b2i(v.GradientFit == "max")), float32(v.MaxHeight)}
	p.Viz0, p.Viz1, p.Viz2, p.Viz3 = [4]float32{}, [4]float32{}, [4]float32{}, [4]float32{}

	bw := v.BarWidth
	switch vizType {
	case 1:
		cx, cy := W*v.CenterX, H*v.CenterY
		if cx <= 0 {
			cx = W / 2
		}
		if cy <= 0 {
			cy = H / 2
		}
		rad := v.Radius
		if rad <= 0 {
			rad = math.Min(W, H) * 0.22
		}
		if bw <= 0 {
			bw = 4
		}
		base := rad * (1.0 + fs.Feat.BassEnergy*0.08 + fs.Feat.BeatPulse*0.04)
		p.Viz0 = [4]float32{float32(cx), float32(cy), float32(base), 0}
		p.Viz1 = [4]float32{float32(bw), 0, 0, float32(0.4 + fs.Feat.BassEnergy*0.3)}
	case 2:
		posY := v.PositionY
		if posY <= 0 {
			posY = 0.85
		}
		halfW := W / 2
		cg := visualizer.CenterGap(v)
		span := bw + v.BarGap
		if avail := halfW*0.95 - cg/2; float64(n)*span > avail {
			span = avail / float64(n)
			bw = span * 0.75
		}
		p.Viz0 = [4]float32{float32(halfW), 0, 0, float32(H * posY)}
		p.Viz1 = [4]float32{float32(bw), float32(span), float32(v.Roundness), 0}
		p.Viz3 = [4]float32{0, 0, float32(cg), 0}
	case 3:
		span := H * 0.8 / float64(max(n, 1))
		p.Viz1 = [4]float32{float32(bw), float32(span), float32(v.Roundness), 0}
		p.Viz3 = [4]float32{float32(H * 0.1), float32(span * 0.7), 0, 0}
	case 4:
		posY := v.PositionY
		if posY <= 0 {
			posY = 0.5
		}
		if bw <= 0 {
			bw = 3
		}
		p.Viz0 = [4]float32{0, 0, 0, float32(H * posY)}
		p.Viz1 = [4]float32{float32(bw), 0, 0, 0}
		p.Viz2[2] = float32(v.MaxHeight)
		p.Viz2[3] = float32(W / float64(max(n-1, 1)))
	}
	p.Viz2[0] = float32(v.Glow.Radius)
	p.Viz2[1] = float32(v.Glow.Intensity)

	// Lyrics
	p.TextInfo = [4]int32{}
	p.ZIdx[2] = 0 // the text styles drawn (mask; each one's stage in its GradN[1])
	if fs.Lyr != nil {
		items, styles := fs.TextItems, fs.TextStyles
		n := min(len(items), gpu.MaxTextItems)
		for i := 0; i < n; i++ {
			it := items[i]
			p.TItemA[i] = [4]float32{it.AX, it.AY, it.AW, it.AH}
			p.TItemB[i] = [4]float32{it.CX, it.CY, it.Scale, it.Alpha}
			p.TItemC[i] = [4]float32{it.Fill, it.Pad, float32(it.Style), it.Rot}
			fl := it.Flip
			if it.Shear != 0 {
				fl = it.Shear // wrapped round a cylinder (its style's ztrack.w): its slant
			}
			p.TItemD[i] = [4]float32{it.PS, float32(math.Atan2(float64(it.PTY), float64(it.PTX))), fl, it.FlipY}
			// where it is in its text (its properties per letter / pixel)
			p.TItemG[i] = [4]float32{it.TX, it.TY, it.TSX, it.TSY}
			p.TItemH[i] = [4]float32{float32(it.Index), float32(it.Count)}
		}
		f4 := func(c [4]float64) [4]float32 {
			return [4]float32{float32(c[0]), float32(c[1]), float32(c[2]), float32(c[3])}
		}
		chroma := int32(0)
		for si := 0; si < min(len(styles), gpu.MaxTextStyles); si++ {
			st := &styles[si]
			ts := &p.TStyles[si]
			ch := st.Chroma
			if ch <= 0.3 { // same threshold as the CPU (lyrics.chromaOf)
				ch = 0
			} else {
				chroma = 1
			}
			ng := min(len(st.Grad), gpu.MaxGrad)
			for i := 0; i < ng; i++ {
				g := st.Grad[i]
				ts.Grad[i] = [4]float32{float32(g.C[0]), float32(g.C[1]), float32(g.C[2]), float32(g.Off)}
			}
			// GradN[1]: the stage among the overlay clips of this lyrics / text style
			stg := fs.Stage(st.Z)
			ts.GradN = [4]int32{int32(ng), int32(stg)}
			if st.GlowOn {
				ts.GradN[2] |= 1
			}
			if st.ShadowOn {
				ts.GradN[2] |= 2
			}
			p.ZIdx[2] |= 1 << si
			ts.Info = [4]float32{float32(st.OutlineW), float32(st.GlowR), float32(st.GlowI), float32(ch)}
			ts.Color, ts.Upcoming, ts.Outline, ts.GlowC = f4(st.Color), f4(st.Upcoming), f4(st.Outline), f4(st.GlowC)
			ts.Shadow = [4]float32{float32(st.ShadowDX), float32(st.ShadowDY), float32(st.ShadowOp), float32(st.Blur)}
			ts.ShadowC = f4(st.ShadowC)
			ts.Depth = [4]float32{0, float32(st.DepthZ), float32(st.DepthSoft), float32(st.DepthBlur)}
			if st.Behind && st.Scene.On() {
				ts.Depth[0] = 1
			}
		}
		p.TextInfo = [4]int32{int32(n), int32(fs.Lyr.Atlas.W), chroma}
	}
	// texts lighting the scene
	for i := range p.TLightI {
		p.TLightI[i] = [4]int32{}
	}
	i := 0
	for _, tl := range fs.TextLights {
		if i == gpu.MaxTextLights || tl.Style >= gpu.MaxTextStyles {
			continue // (a text the GPU does not draw)
		}
		l := tl.Light
		if l.GW*l.GH > 4*gpu.TextLightVec4 {
			continue
		}
		add := float32(0)
		if l.Add {
			add = 1
		}
		p.TLightI[i] = [4]int32{int32(l.GW), int32(l.GH), int32(l.Div), int32(tl.Style)}
		p.TLightC[i] = [4]float32{l.Col[0], l.Col[1], l.Col[2], add}
		p.TLightD[i], p.TLightE[i], p.TLightF[i], p.TLightG[i] = [4]float32{}, [4]float32{}, [4]float32{}, [4]float32{}
		if l.D3 {
			p.TLightD[i] = [4]float32{l.CX, l.CY, l.LZ, l.DepthK}
			p.TLightE[i] = [4]float32{l.Shade, 1, l.From, l.To}
			p.TLightG[i] = [4]float32{l.RimEdge, l.Soft, l.Gloss, l.Shadow}
			if l.Rim {
				p.TLightF[i] = [4]float32{l.RimFrom, l.RimSoft, l.RimW, l.RimGain}
			}
		}
		for c, v := range l.V {
			p.TLightV[i*gpu.TextLightVec4+c/4][c%4] = v
		}
		i++
	}
}

// putTrack writes a path track (effects.PathTrack) as trackAt in render.comp reads it, at word off
// of buf: n, then n lengths, values and easings (floats)
func putTrack(buf []byte, off int, t *effects.PathTrack) {
	put := func(i int, v float32) { binary.LittleEndian.PutUint32(buf[(off+i)*4:], math.Float32bits(v)) }
	n := len(t.S)
	put(0, float32(n))
	for k := 0; k < n; k++ {
		put(1+k, t.S[k])
		put(1+n+k, t.V[k])
		put(1+2*n+k, float32(t.E[k]))
	}
}
