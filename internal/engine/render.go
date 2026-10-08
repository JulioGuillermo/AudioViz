package engine

import (
	"context"
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

// NewRenderer creates the render backend ("auto", "gpu" or "cpu") for the session's resolution
func NewRenderer(s *Session, mode string, slots int) (*Renderer, error) {
	r := &Renderer{s: s}
	mode = strings.ToLower(mode)
	if mode != "cpu" {
		textCap := 1 << 20
		if s.lyr != nil {
			textCap = max(textCap, len(s.lyr.Atlas.Pix)*2)
		}
		g, err := gpu.New(s.W, s.H, slots, s.Cfg.Output.GPUDevice, textCap, effects.FluidBufWords(s.W, s.H), layerCap(s), s.EmitterCount())
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
	return r.G.Width == r.s.W && r.G.Height == r.s.H && need <= r.textCap && r.s.LayerPixels() <= r.G.LayerWords && r.s.EmitterCount() <= r.G.Emitters
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
	var vers []string
	off := 0
	// first the depth of the backgrounds A and B (a byte per pixel), then the clips
	dw := (r.s.W*r.s.H + 1) / 2 // 16 bits per pixel
	p.DepthI = [4]int32{}
	var depths [2][]uint16
	for k, d := range [2][]uint16{fs.DepthA, fs.DepthB} {
		if d != nil && off+dw <= g.LayerWords {
			depths[k] = d
			p.DepthI[k] = int32(off)
			p.DepthI[2] |= 1 << k
			off += dw
		}
	}
	vers = append(vers, "dA"+fs.VerDA, "dB"+fs.VerDB)
	type emitAt struct {
		e   *effects.EmitFrame
		off int
	}
	var emits []emitAt
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
		mwords := 0
		if l.Mask != nil {
			mwords = (len(l.Mask.Pix) + 3) / 4
		}
		if n == gpu.MaxLayers || off+px+mwords > g.LayerWords {
			break
		}
		vers = append(vers, l.Ver+"|"+l.MaskVer)
		lp := &p.Layers[n]
		flags := int32(l.Blend & 15)
		if l.Key {
			flags |= 1 << 4
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
				// its light field, made by the emitter pass (k-th accumulation)
				_, _, field, stride := effects.EmitAccLayout(r.s.W, r.s.H)
				xw, xh := effects.EmitLightGrid(r.s.W, r.s.H)
				mode := int32(1)
				if P.LightAdd {
					mode = 2
				}
				lp.Light0 = [4]int32{int32(len(emits)*stride + field), int32(xw), int32(xh), mode}
				lp.Light1 = [4]float32{float32(P.LightCol[0]), float32(P.LightCol[1]), float32(P.LightCol[2])}
				if P.LightD3 {
					lp.Light2 = [4]float32{float32(P.LightCX), float32(P.LightCY), float32(P.LightZ), float32(P.LightDepthK)}
					lp.Light3 = [4]float32{float32(P.LightShade), 1, float32(P.LightFrom), float32(P.LightTo)}
					lp.Light1[3] = float32(P.LightSoft)
					if P.LightRim {
						lp.Light4 = [4]float32{float32(P.LightRimFrom), float32(P.LightRimSoft), float32(P.LightRimW), float32(P.LightRimGain)}
						lp.Light5 = [4]float32{float32(P.LightRimEdge)}
					}
				}
			}
			emits = append(emits, emitAt{l.Emit, off})
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
				off += r.s.W * r.s.H // drawn there by the emitter pass
			} else {
				copy(buf[off*4:], l.Img.Pix)
				off += len(l.Img.Pix) / 4
			}
			if m := fs.Layers[i].Mask; m != nil {
				copy(buf[off*4:], m.Pix)
				off += (len(m.Pix) + 3) / 4
			}
		}
		for _, tm := range tms {
			copy(buf[off*4:], tm.img.Pix)
			off += (len(tm.img.Pix) + 3) / 4
		}
		r.layerVer[slot] = vers
	}
	// Stages beyond the clips drawn (n) go on top: render.comp clamps them to n
	p.LayerN = [4]int32{int32(n)}
	p.ZIdx[0], p.ZIdx[1] = int32(fs.ZParts), int32(fs.ZViz)
	p.FxFlash = [4]float32{float32(contrastFactor(fs.FX.contrast)), float32(fs.FX.bright)}
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
		cnt := max(0, min(e.I1-i0, gpu.MaxEmitParticles-base))
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
			f[o+4], f[o+5], f[o+6], f[o+7] = float32(b.Rot), float32(b.Speed), float32(b.Size), 0
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

// drawVizCPU draws the visualizer onto the frame (faded with VizAlpha)
func (s *Session) drawVizCPU(frame *image.RGBA, dc *gg.Context, fs *FrameState) {
	viz, err := visualizer.NewVisualizer(&fs.Viz, s.W, s.H)
	if err != nil || fs.VizAlpha <= 0 {
		return
	}
	if fs.VizAlpha >= 0.999 {
		viz.Draw(dc, fs.VizFeat)
		return
	}
	layer := image.NewRGBA(frame.Bounds())
	copy(layer.Pix, frame.Pix)
	viz.Draw(gg.NewContextForRGBA(layer), fs.VizFeat)
	a := fs.VizAlpha
	for i := range frame.Pix {
		frame.Pix[i] = uint8(float64(frame.Pix[i])*(1-a) + float64(layer.Pix[i])*a + 0.5)
	}
}

func (s *Session) cpuFrame(fs *FrameState) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, s.W, s.H))
	par := fs.FX.par
	if !par.On() || (fs.DepthA == nil && fs.DepthB == nil) {
		par = effects.Parallax{}
	}
	if fs.Trans.Mode == 0 && fs.LayerA == effects.Identity && !par.On() {
		if fs.BgA != nil {
			copy(frame.Pix, fs.BgA.Pix)
		}
	} else {
		effects.ComposeParallax(frame, fs.BgA, fs.BgB, fs.LayerA, fs.LayerB, fs.Trans, par, fs.DepthA, fs.DepthB)
	}
	fx := fs.FX
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
		frame = effects.MaskedBlend(frame, effects.ApplyGlitch(frame, fs.GlitchAmount, int64(fs.Idx)), effects.RangeMaskField(z, fx.rGlitch))
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
		if st > 0 {
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
			s.drawVizCPU(frame, dc, fs)
		}
		var its []lyrics.Item
		for _, it := range items {
			if fs.Stage(styles[it.Style].Z) == st {
				its = append(its, it)
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

func (s *Session) fillParams(p *gpu.Params, fs *FrameState, padW, padH int, writeRGBA bool) {
	v := &fs.Viz
	feat := fs.VizFeat
	fx := fs.FX
	W, H := float64(s.W), float64(s.H)
	b2i := func(b bool) int32 {
		if b {
			return 1
		}
		return 0
	}

	p.Dims = [4]int32{int32(s.W), int32(s.H), int32(padW), int32(padH)}

	// Background
	c := fx.contrast * 128.0
	cf := (259.0 * (c + 255.0)) / (255.0 * (259.0 - c))
	p.ZoomRipple = [4]float32{float32(fx.zoom), float32(fx.rippleAmp), float32(fx.rippleFreq), float32(fx.rTim)}
	// the 2.5D camera (with a depth map; the fluid transitions don't move)
	p.Parallax, p.ParallaxN = [4]float32{}, [4]int32{}
	p.Dof = [4]float32{float32(fx.dof.From), float32(fx.dof.To), float32(fx.dof.Soft), float32(fx.dof.Blur)}
	if fx.dof.On() {
		p.ParallaxN[1] = effects.DoFTaps // the gather's samples
	}
	for i, r := range [4][4]float64{fx.rFlash, fx.rChroma, fx.rRipple, fx.rGlitch} {
		p.Ranges[i] = [4]float32{float32(r[0]), float32(r[1]), float32(r[2]), float32(r[3])}
	}
	if par := fx.par; par.On() && (fs.DepthA != nil || fs.DepthB != nil) && fs.Trans.Fluid == nil {
		p.Parallax = [4]float32{float32(par.SX), float32(par.SY), float32(par.Dolly), float32(par.Focus)}
		p.ParallaxN[0] = int32(par.Steps(s.W, s.H))
	}
	p.ColorAdj = [4]float32{float32(cf), float32(-fs.BG.Darken + fx.bright), float32(fs.BG.Opacity), float32(fs.BG.Vignette)}
	if fs.BgA == nil && fs.BgB == nil {
		p.ColorAdj[2] = 0 // no background: black
	}
	p.Chroma = [4]float32{float32(fx.chromaShift)}
	nGlitch := 0
	if fx.glitch {
		for _, sl := range effects.GlitchSlices(s.W, s.H, fs.GlitchAmount, int64(fs.Idx)) {
			if nGlitch == gpu.MaxGlitch {
				break
			}
			p.Glitch[nGlitch] = [4]float32{float32(sl.Y0), float32(sl.Y1), float32(sl.ShiftX)}
			nGlitch++
		}
	}
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
	p.Shake = [4]float32{float32(fx.shakeX), float32(fx.shakeY)}

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
	p.ZIdx[2] = 0 // stages with texts (mask)
	if fs.Lyr != nil {
		items, styles := fs.TextItems, fs.TextStyles
		n := min(len(items), gpu.MaxTextItems)
		for i := 0; i < n; i++ {
			it := items[i]
			p.TItemA[i] = [4]float32{it.AX, it.AY, it.AW, it.AH}
			p.TItemB[i] = [4]float32{it.CX, it.CY, it.Scale, it.Alpha}
			p.TItemC[i] = [4]float32{it.Fill, it.Pad, float32(it.Style), it.Rot}
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
			stg := min(fs.Stage(st.Z), gpu.MaxLayers)
			ts.GradN = [4]int32{int32(ng), int32(stg)}
			p.ZIdx[2] |= 1 << stg
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
}
