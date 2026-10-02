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

// frameFX son los parámetros de efectos de fondo de un cuadro (comunes a CPU y GPU)
type frameFX struct {
	zoom                        float64
	rippleAmp, rippleFreq, rTim float64
	chromaShift                 float64
	chromaMode                  int // 0 no, 1 horizontal, 2 radial
	glitch                      bool
	shakeX, shakeY              float64
	bright, contrast            float64
}

// RenderOptions opciones del CLI para previsualización o renderizado parcial
type RenderOptions struct {
	MaxDurationSec float64 // Si > 0, limita la duración del video (ej. 10s para preview)
	StartSec       float64 // Segundo inicial del preview
	SingleFrameAt  float64 // Si >= 0, exporta solo ese cuadro como imagen PNG
	SingleFrameOut string  // Ruta de salida de la imagen de cuadro único
}

// Progress informa del avance de una exportación
type Progress struct {
	Done, Total int
	FPS         float64
	ETA         float64
	Stage       string
}

// ---------------------------------------------------------------- renderer GPU/CPU

// Renderer dibuja cuadros de una sesión con la GPU (Vulkan) o, si no hay, con la CPU
type Renderer struct {
	G       *gpu.Renderer
	s       *Session
	verA    []string
	verB    []string
	lyrVer  []int
	textCap int
}

// NewRenderer crea el backend de render ("auto", "gpu" o "cpu") para la resolución de la sesión
func NewRenderer(s *Session, mode string, slots int) (*Renderer, error) {
	r := &Renderer{s: s}
	mode = strings.ToLower(mode)
	if mode != "cpu" {
		textCap := 1 << 20
		if s.lyr != nil {
			textCap = max(textCap, len(s.lyr.Atlas.Pix)*2)
		}
		g, err := gpu.New(s.W, s.H, slots, s.Cfg.Output.GPUDevice, textCap)
		if err != nil {
			if mode == "gpu" {
				return nil, fmt.Errorf("GPU Vulkan no disponible: %w", err)
			}
			s.Logf("⚠️  GPU Vulkan no disponible (%v), usando CPU", err)
		} else {
			r.G, r.textCap = g, textCap
			r.verA, r.verB, r.lyrVer = make([]string, slots), make([]string, slots), make([]int, slots)
			s.Logf("🖥️  Render por GPU (Vulkan): %s", g.DeviceName)
		}
	}
	if r.G == nil {
		s.Logf("🖥️  Render por CPU (%d hilos)", runtime.NumCPU())
	}
	return r, nil
}

// Fits indica si el renderer sirve para la sesión tal como está ahora (resolución y atlas)
func (r *Renderer) Fits() bool {
	if r.G == nil {
		return true
	}
	need := 0
	if r.s.lyr != nil {
		need = len(r.s.lyr.Atlas.Pix)
	}
	return r.G.Width == r.s.W && r.G.Height == r.s.H && need <= r.textCap
}

func (r *Renderer) Close() {
	if r.G != nil {
		r.G.Close()
		r.G = nil
	}
}

// submit prepara y lanza el cuadro fs en el slot (solo GPU)
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
	r.s.fillParams(g.Params(slot), fs, g.PadW, g.PadH, writeRGBA)
	return g.Submit(slot)
}

// Still renderiza un único cuadro como imagen RGBA
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

// Preview renderiza un cuadro a 1/scale de resolución directamente en la GPU (más rápido)
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
	defer func() { r.G.Params(0).Prev = [4]int32{} }() // tras esperar a la GPU
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

// ---------------------------------------------------------------- exportación

// ExportRange exporta los cuadros [start, end) al MP4 de salida
func (s *Session) ExportRange(ctx context.Context, mode string, start, end int, outPath string, progress func(Progress)) error {
	if s.Audio == nil {
		return fmt.Errorf("el proyecto no tiene audio")
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

	// Audio: el tramo exacto, a FLAC sin pérdidas en paralelo al render
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

// Execute es el flujo del CLI: un PNG suelto o el video (completo o un tramo)
func Execute(cfg *config.ProjectConfig, opts RenderOptions) error {
	startTime := time.Now()
	logf := func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
	if cfg.Audio.Path == "" {
		return fmt.Errorf("el campo 'audio.path' es obligatorio")
	}
	s, err := Open(cfg, logf)
	if err != nil {
		return err
	}
	defer s.Close()
	logf("📐 Resolución: %dx%d @ %d fps", s.W, s.H, s.FPS)

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
	logf("🚀 Renderizando %d cuadros (x264 preset %s, CRF %d)...", end-start, cfg.Output.Preset, cfg.Output.CRF)
	t0 := time.Now()
	err = s.ExportRange(context.Background(), cfg.Output.Renderer, start, end, cfg.Output.Path, func(p Progress) {
		if p.Stage == "mux" {
			fmt.Printf("\n📦 Escribiendo MP4...\n")
			return
		}
		fmt.Printf("\r⏳ [%5.1f%%] Cuadro %d/%d (%.1f FPS) | ETA: %.0fs    ", float64(p.Done)/float64(p.Total)*100, p.Done, p.Total, p.FPS, p.ETA)
	})
	if err != nil {
		return err
	}
	logf("✨ Completado en %s (%.1f FPS medios)\n📁 %s", time.Since(startTime).Round(time.Millisecond),
		float64(end-start)/time.Since(t0).Seconds(), cfg.Output.Path)
	return nil
}

// ---------------------------------------------------------------- CPU

func (s *Session) cpuFrame(fs *FrameState) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, s.W, s.H))
	if fs.BgA != nil {
		copy(frame.Pix, fs.BgA.Pix)
		if fs.BgB != nil && fs.BgMix > 0 {
			m := fs.BgMix
			for i := range frame.Pix {
				frame.Pix[i] = uint8(float64(frame.Pix[i])*(1-m) + float64(fs.BgB.Pix[i])*m + 0.5)
			}
		}
	}
	fx := fs.FX
	frame = effects.ApplyBassZoom(frame, fx.zoom, fx.shakeX, fx.shakeY)
	if fx.rippleAmp > 0 {
		frame = effects.ApplyRipple(frame, fx.rippleAmp, fx.rippleFreq, fx.rTim)
	}
	if fx.chromaMode != 0 {
		mode := "horizontal"
		if fx.chromaMode == 2 {
			mode = "radial"
		}
		frame = effects.ApplyChromaticAberration(frame, fx.chromaShift, mode)
	}
	if fx.glitch {
		frame = effects.ApplyGlitch(frame, fs.GlitchAmount, int64(fs.Idx))
	}
	effects.ApplyColorAndContrast(frame, fx.bright, fx.contrast, fs.BG.Darken, fs.BG.Opacity, fs.BG.Vignette)

	dc := gg.NewContextForRGBA(frame)
	for _, ps := range fs.Parts {
		ps.Draw(dc)
	}
	if viz, err := visualizer.NewVisualizer(&fs.Viz, s.W, s.H); err == nil && fs.VizAlpha > 0 {
		if fs.VizAlpha >= 0.999 {
			viz.Draw(dc, fs.VizFeat)
		} else {
			layer := image.NewRGBA(frame.Bounds())
			copy(layer.Pix, frame.Pix)
			viz.Draw(gg.NewContextForRGBA(layer), fs.VizFeat)
			a := fs.VizAlpha
			for i := range frame.Pix {
				frame.Pix[i] = uint8(float64(frame.Pix[i])*(1-a) + float64(layer.Pix[i])*a + 0.5)
			}
		}
	}
	if fs.Lyr != nil {
		fs.Lyr.DrawCPU(frame, fs.Lyr.Frame(fs.T, fs.Feat), lyrics.StyleFor(&fs.LyrSt, fs.T, fs.Feat))
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
	fmt.Printf("✅ Cuadro guardado en: %s\n", path)
	return nil
}

// ---------------------------------------------------------------- parámetros GPU

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

	// Fondo
	c := fx.contrast * 128.0
	cf := (259.0 * (c + 255.0)) / (255.0 * (259.0 - c))
	p.ZoomRipple = [4]float32{float32(fx.zoom), float32(fx.rippleAmp), float32(fx.rippleFreq), float32(fx.rTim)}
	p.ColorAdj = [4]float32{float32(cf), float32(-fs.BG.Darken + fx.bright), float32(fs.BG.Opacity), float32(fs.BG.Vignette)}
	if fs.BgA == nil {
		p.ColorAdj[2] = 0 // sin fondo: negro
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
	if fs.BgB != nil {
		p.BgMix[0] = float32(fs.BgMix)
	}

	// Partículas
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

	// Visualizador
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

	// Letra
	p.TextInfo = [4]int32{}
	if fs.Lyr != nil {
		items := fs.Lyr.Frame(fs.T, fs.Feat)
		st := lyrics.StyleFor(&fs.LyrSt, fs.T, fs.Feat)
		n := min(len(items), gpu.MaxTextItems)
		for i := 0; i < n; i++ {
			it := items[i]
			p.TItemA[i] = [4]float32{it.AX, it.AY, it.AW, it.AH}
			p.TItemB[i] = [4]float32{it.CX, it.CY, it.Scale, it.Alpha}
			p.TItemC[i] = [4]float32{it.Fill, it.Pad}
		}
		f4 := func(c [4]float64) [4]float32 { return [4]float32{float32(c[0]), float32(c[1]), float32(c[2]), float32(c[3])} }
		ng := min(len(st.Grad), gpu.MaxGrad)
		for i := 0; i < ng; i++ {
			g := st.Grad[i]
			p.TextGrad[i] = [4]float32{float32(g.C[0]), float32(g.C[1]), float32(g.C[2]), float32(g.Off)}
		}
		chroma := int32(0)
		if st.Chroma > 0.3 {
			chroma = 1
		}
		p.TextInfo = [4]int32{int32(n), int32(fs.Lyr.Atlas.W), chroma, int32(ng)}
		p.TextStyle = [4]float32{float32(st.OutlineW), float32(st.GlowR), float32(st.GlowI), float32(st.Chroma)}
		p.TextColor, p.TextUpcoming, p.TextOutline, p.TextGlowC = f4(st.Color), f4(st.Upcoming), f4(st.Outline), f4(st.GlowC)
		p.TextShadow = [4]float32{float32(st.ShadowDX), float32(st.ShadowDY), float32(st.ShadowOp), float32(st.Blur)}
		p.TextShadowC = f4(st.ShadowC)
	}
}
