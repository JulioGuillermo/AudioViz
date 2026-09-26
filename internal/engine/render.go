package engine

import (
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
	"audioviz/internal/mux"
	"audioviz/internal/particles"
	"audioviz/internal/video"
	"audioviz/internal/visualizer"

	"github.com/fogleman/gg"
)

type RenderEngine struct {
	cfg        *config.ProjectConfig
	outW, outH int
}

func NewRenderEngine(cfg *config.ProjectConfig) *RenderEngine {
	return &RenderEngine{cfg: cfg}
}

// RenderOptions opciones auxiliares para previsualización o renderizado parcial
type RenderOptions struct {
	MaxDurationSec float64 // Si > 0, limita la duración del video (ej. 10s para preview)
	StartSec       float64 // Segundo inicial del preview
	SingleFrameAt  float64 // Si >= 0, exporta solo ese cuadro como imagen PNG
	SingleFrameOut string  // Ruta de salida de la imagen de cuadro único
}

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

func (e *RenderEngine) computeFX(feat *audio.FrameFeatures, frameIdx int) frameFX {
	eff := &e.cfg.BackgroundEffects
	fps := float64(e.cfg.Output.FPS)
	var fx frameFX
	fx.zoom = 1
	if eff.BassZoom.Enabled {
		fx.zoom = 1.0 + (eff.BassZoom.MaxZoom-1.0)*feat.Band(eff.BassZoom.ReactTo, "bass")*eff.BassZoom.Sensitivity
		if feat.BeatPulse > 0 {
			fx.zoom += (eff.BassZoom.MaxZoom - 1.0) * feat.BeatPulse * 0.5
		}
	}
	if sh := eff.Shake; sh.Enabled && sh.Amplitude > 0 {
		speed := sh.Speed
		if speed <= 0 {
			speed = 1
		}
		t := float64(frameIdx) / fps * speed
		amp := sh.Amplitude * feat.Band(sh.ReactTo, "bass")
		// Suma de senos de frecuencias no múltiplas: vaivén orgánico sin repetición visible
		fx.shakeX = amp * (0.6*math.Sin(t*7.3) + 0.4*math.Sin(t*13.1+1.7))
		fx.shakeY = amp * (0.6*math.Sin(t*8.9+0.5) + 0.4*math.Sin(t*11.7+2.3))
		// Zoom extra para que el desplazamiento nunca deje ver los bordes
		minDim := math.Max(math.Min(float64(e.outW), float64(e.outH)), 64)
		fx.zoom *= 1 + 2*sh.Amplitude/minDim
	}
	if eff.Ripple.Enabled {
		fx.rTim = float64(frameIdx) * 0.05 * eff.Ripple.Speed
		fx.rippleAmp = eff.Ripple.Amplitude * (0.3 + feat.Band(eff.Ripple.ReactTo, "mid")*0.7)
		fx.rippleFreq = eff.Ripple.Frequency
	}
	if eff.ChromaticAberration.Enabled {
		fx.chromaShift = eff.ChromaticAberration.MaxShift * feat.Band(eff.ChromaticAberration.ReactTo, "bass")
		fx.chromaMode = 1
		if eff.ChromaticAberration.Mode == "radial" {
			fx.chromaMode = 2
		}
	}
	if g := eff.Glitch; g.Enabled {
		if g.ReactTo == "" {
			fx.glitch = feat.SpectralFlux > g.Threshold || feat.IsBeat
		} else {
			fx.glitch = feat.Band(g.ReactTo, "") > g.Threshold
		}
	}
	if eff.ColorFlash.Enabled {
		v := feat.Band(eff.ColorFlash.ReactTo, "bass")
		fx.bright = eff.ColorFlash.BrightnessBoost * v
		fx.contrast = eff.ColorFlash.ContrastBoost * v
	}
	return fx
}

func (e *RenderEngine) Execute(opts RenderOptions) error {
	startTime := time.Now()
	cfg := e.cfg

	fmt.Printf("🔍 Inspeccionando fondo: %s\n", cfg.Background.Path)
	probe, err := video.ProbeMedia(cfg.Background.Path)
	if err != nil {
		return fmt.Errorf("error al inspeccionar fondo: %w", err)
	}
	targetW, targetH := cfg.Output.Width, cfg.Output.Height
	if targetW <= 0 || targetH <= 0 {
		targetW, targetH = probe.Width, probe.Height
	}
	targetW, targetH = targetW/2*2, targetH/2*2
	e.outW, e.outH = targetW, targetH
	fmt.Printf("📐 Resolución: %dx%d\n", targetW, targetH)

	fmt.Printf("🎵 Decodificando audio: %s\n", cfg.Audio.Path)
	decoded, err := audio.DecodeAudio(cfg.Audio.Path, cfg.Audio.Volume)
	if err != nil {
		return err
	}
	fmt.Printf("✅ Audio: %.2f s, %d Hz, %d bits\n", decoded.DurationSec, decoded.SampleRate, decoded.BitsPerSample)

	fps := cfg.Output.FPS
	fullFrames := int(decoded.DurationSec * float64(fps))
	startFrame := 0
	endFrame := fullFrames
	if opts.SingleFrameAt >= 0 {
		endFrame = min(int(opts.SingleFrameAt*float64(fps))+1, fullFrames)
	} else {
		if opts.StartSec > 0 {
			startFrame = min(int(opts.StartSec*float64(fps)), fullFrames-1)
		}
		if opts.MaxDurationSec > 0 {
			endFrame = min(startFrame+int(opts.MaxDurationSec*float64(fps)), fullFrames)
		}
	}

	fmt.Printf("⚡ Analizando espectro (%d bandas, %d cuadros, normalización %v)...\n", cfg.Visualizer.Bars, fullFrames, cfg.Audio.Normalize)
	t0 := time.Now()
	analyzer := audio.NewAudioAnalyzer(decoded, fps, cfg.Visualizer.Bars, cfg.Audio.Sensitivity, cfg.Visualizer.Smoothing, cfg.Visualizer.DecayRate)
	analyzer.Normalize = cfg.Audio.Normalize
	analyzer.WindowSec = cfg.Audio.NormalizeWindow
	// Se analiza la canción entera para que la normalización y el suavizado sean idénticos
	// en un preview y en el render completo
	allFeatures := analyzer.AnalyzeAllFrames(fullFrames)
	fmt.Printf("   análisis en %v\n", time.Since(t0).Round(time.Millisecond))

	bgReader, err := video.NewBackgroundReader(cfg.Background.Path, targetW, targetH, cfg.Background.FitMode, cfg.Background.LoopVideo)
	if err != nil {
		return err
	}
	defer bgReader.Close()

	// Backend de render
	var g *gpu.Renderer
	mode := strings.ToLower(cfg.Output.Renderer)
	if mode != "cpu" {
		slots := 3
		if opts.SingleFrameAt >= 0 {
			slots = 1
		}
		g, err = gpu.New(targetW, targetH, slots, cfg.Output.GPUDevice)
		if err != nil {
			if mode == "gpu" {
				return fmt.Errorf("GPU Vulkan no disponible: %w", err)
			}
			fmt.Printf("⚠️  GPU Vulkan no disponible (%v), usando CPU\n", err)
			g = nil
		} else {
			defer g.Close()
			fmt.Printf("🖥️  Render por GPU (Vulkan): %s\n", g.DeviceName)
		}
	}
	if g == nil {
		fmt.Printf("🖥️  Render por CPU (%d hilos)\n", runtime.NumCPU())
	}

	sc := newScene(cfg, targetW, targetH)

	if opts.SingleFrameAt >= 0 {
		idx := endFrame - 1
		fmt.Printf("📸 Renderizando cuadro t=%.2fs (frame %d)...\n", float64(idx)/float64(fps), idx)
		// Simular partículas desde un poco antes para que no estén en reposo
		for i := max(0, idx-60); i < idx; i++ {
			sc.parts.Update(&allFeatures[i], 1.0/float64(fps))
		}
		sc.parts.Update(&allFeatures[idx], 1.0/float64(fps))
		var img *image.RGBA
		if g != nil {
			img, err = e.gpuStill(g, sc, bgReader, &allFeatures[idx], idx)
		} else {
			img, err = e.cpuFrame(sc, bgReader, &allFeatures[idx], idx)
		}
		if err != nil {
			return err
		}
		return savePNG(opts.SingleFrameOut, img)
	}

	// Codificador H.264 (x264 integrado) + muxer MP4
	preset := cfg.Output.Preset
	if preset == "" {
		preset = "fast"
	}
	crf := float64(cfg.Output.CRF)
	if crf <= 0 {
		crf = 18
	}
	enc, err := encode.NewX264(targetW, targetH, fps, preset, crf)
	if err != nil {
		return err
	}
	defer enc.Close()
	mp4, err := mux.NewMP4Writer(cfg.Output.Path, targetW, targetH, fps)
	if err != nil {
		return err
	}
	hdrs := mux.SplitAnnexB(enc.Headers())
	var sps, pps []byte
	for _, nal := range hdrs {
		switch nal[0] & 0x1F {
		case 7:
			sps = nal
		case 8:
			pps = nal
		}
	}
	mp4.SetAVCConfig(sps, pps)

	// Audio: el tramo exacto del video, codificado a FLAC sin pérdidas en paralelo al render
	var audioWG sync.WaitGroup
	audioWG.Add(1)
	go func() {
		defer audioWG.Done()
		s0 := int(float64(startFrame) / float64(fps) * float64(decoded.SampleRate))
		s1 := int(float64(endFrame) / float64(fps) * float64(decoded.SampleRate))
		s1 = min(s1, len(decoded.PCM)/2)
		st := audio.EncodeFLAC(decoded.PCM[s0*2:s1*2], decoded.SampleRate, decoded.BitsPerSample, 0)
		mp4.SetAudio(&mux.AudioTrack{SampleRate: decoded.SampleRate, Channels: 2, BitsPerSample: decoded.BitsPerSample,
			StreamInfo: st.StreamInfo, Frames: st.Frames, FrameSamples: st.FrameSamples})
	}()

	emit := func(p *encode.Packet) error {
		if p == nil {
			return nil
		}
		return mp4.WriteVideoAnnexB(p.Data, p.PTS, p.DTS, p.Key)
	}

	// Avanzar partículas hasta el inicio del preview
	for i := 0; i < startFrame; i++ {
		sc.parts.Update(&allFeatures[i], 1.0/float64(fps))
	}

	total := endFrame - startFrame
	fmt.Printf("🚀 Renderizando %d cuadros (x264 preset %s, CRF %.0f)...\n", total, preset, crf)
	renderStart := time.Now()
	lastReport := time.Now()
	progress := func(done int) {
		if time.Since(lastReport) < time.Second && done != total {
			return
		}
		lastReport = time.Now()
		el := time.Since(renderStart).Seconds()
		f := float64(done) / el
		fmt.Printf("\r⏳ [%5.1f%%] Cuadro %d/%d (%.1f FPS) | ETA: %.0fs    ", float64(done)/float64(total)*100, done, total, f, float64(total-done)/math.Max(f, 0.01))
	}

	if g != nil {
		err = e.renderGPU(g, sc, bgReader, allFeatures, startFrame, endFrame, enc, emit, progress)
	} else {
		err = e.renderCPU(sc, bgReader, allFeatures, startFrame, endFrame, enc, emit, progress)
	}
	if err != nil {
		return err
	}
	if err := enc.Flush(emit); err != nil {
		return err
	}
	audioWG.Wait()
	fmt.Printf("\n📦 Escribiendo MP4...\n")
	if err := mp4.Close(); err != nil {
		return err
	}
	fmt.Printf("✨ Completado en %s (%.1f FPS medios)\n📁 %s\n", time.Since(startTime).Round(time.Millisecond),
		float64(total)/time.Since(renderStart).Seconds(), cfg.Output.Path)
	return nil
}

// ---------------------------------------------------------------- GPU

func (e *RenderEngine) renderGPU(g *gpu.Renderer, sc *scene, bg video.BackgroundReader, feats []audio.FrameFeatures,
	start, end int, enc *encode.X264, emit func(*encode.Packet) error, progress func(int)) error {
	fps := float64(e.cfg.Output.FPS)
	bgVersion := make([]int, g.Slots)
	for i := range bgVersion {
		bgVersion[i] = -1
	}
	inFlight := make([]int, g.Slots) // cuadro en cada slot, -1 libre
	for i := range inFlight {
		inFlight[i] = -1
	}
	done := 0
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
		progress(done)
		return emit(p)
	}

	for i := start; i < end; i++ {
		slot := (i - start) % g.Slots
		if err := collect(slot); err != nil {
			return err
		}
		feat := &feats[i]
		sc.parts.Update(feat, 1/fps)
		img, ver := bg.FrameAt(float64(i) / fps)
		if ver != bgVersion[slot] {
			copy(g.Background(slot), img.Pix)
			bgVersion[slot] = ver
		}
		sc.fillParams(g.Params(slot), e.computeFX(feat, i), feat, i, g.PadW, g.PadH, false)
		if err := g.Submit(slot); err != nil {
			return err
		}
		inFlight[slot] = i
	}
	for k := 0; k < g.Slots; k++ {
		if err := collect((end - start + k) % g.Slots); err != nil {
			return err
		}
	}
	return nil
}

func (e *RenderEngine) gpuStill(g *gpu.Renderer, sc *scene, bg video.BackgroundReader, feat *audio.FrameFeatures, idx int) (*image.RGBA, error) {
	img, _ := bg.FrameAt(float64(idx) / float64(e.cfg.Output.FPS))
	copy(g.Background(0), img.Pix)
	sc.fillParams(g.Params(0), e.computeFX(feat, idx), feat, idx, g.PadW, g.PadH, true)
	if err := g.Submit(0); err != nil {
		return nil, err
	}
	if err := g.Wait(0); err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, g.Width, g.Height))
	copy(out.Pix, g.RGBA(0))
	return out, nil
}

// ---------------------------------------------------------------- CPU

func (e *RenderEngine) cpuFrame(sc *scene, bg video.BackgroundReader, feat *audio.FrameFeatures, idx int) (*image.RGBA, error) {
	src, _ := bg.FrameAt(float64(idx) / float64(e.cfg.Output.FPS))
	frame := image.NewRGBA(src.Bounds())
	copy(frame.Pix, src.Pix)
	fx := e.computeFX(feat, idx)
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
		frame = effects.ApplyGlitch(frame, e.cfg.BackgroundEffects.Glitch.Intensity, int64(idx))
	}
	effects.ApplyColorAndContrast(frame, fx.bright, fx.contrast, e.cfg.Background.Darken, e.cfg.Background.Opacity, e.cfg.Background.Vignette)

	dc := gg.NewContextForRGBA(frame)
	sc.parts.Draw(dc)
	sc.viz.Draw(dc, feat)
	return frame, nil
}

func (e *RenderEngine) renderCPU(sc *scene, bg video.BackgroundReader, feats []audio.FrameFeatures,
	start, end int, enc *encode.X264, emit func(*encode.Packet) error, progress func(int)) error {
	w, h := sc.w, sc.h
	stride := (w + 15) / 16 * 16
	ph := (h + 15) / 16 * 16
	yp := encode.Alloc(stride * ph)
	uvp := encode.Alloc(stride * ph / 2)
	defer encode.Free(yp)
	defer encode.Free(uvp)
	Y := unsafe.Slice((*byte)(yp), stride*ph)
	UV := unsafe.Slice((*byte)(uvp), stride*ph/2)
	fps := float64(e.cfg.Output.FPS)
	for i := start; i < end; i++ {
		sc.parts.Update(&feats[i], 1/fps)
		frame, err := e.cpuFrame(sc, bg, &feats[i], i)
		if err != nil {
			return err
		}
		rgbaToNV12(frame, Y, UV, stride)
		p, err := enc.Encode(yp, uvp, stride, int64(i-start))
		if err != nil {
			return err
		}
		if err := emit(p); err != nil {
			return err
		}
		progress(i - start + 1)
	}
	return nil
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

// ---------------------------------------------------------------- escena

type scene struct {
	cfg   *config.ProjectConfig
	w, h  int
	parts *particles.ParticleSystem
	viz   visualizer.Visualizer
}

func newScene(cfg *config.ProjectConfig, w, h int) *scene {
	viz, _ := visualizer.NewVisualizer(&cfg.Visualizer, w, h)
	return &scene{cfg: cfg, w: w, h: h, parts: particles.NewParticleSystem(&cfg.Particles, w, h), viz: viz}
}

func rgbaF(hex string) [4]float32 {
	c, _ := visualizer.ParseHexColor(hex)
	return [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, float32(c.A) / 255}
}

func (s *scene) fillParams(p *gpu.Params, fx frameFX, feat *audio.FrameFeatures, frameIdx, padW, padH int, writeRGBA bool) {
	cfg := s.cfg
	v := &cfg.Visualizer
	W, H := float64(s.w), float64(s.h)
	b2i := func(b bool) int32 {
		if b {
			return 1
		}
		return 0
	}

	p.Dims = [4]int32{int32(s.w), int32(s.h), int32(padW), int32(padH)}

	// Fondo
	c := fx.contrast * 128.0
	cf := (259.0 * (c + 255.0)) / (255.0 * (259.0 - c))
	p.ZoomRipple = [4]float32{float32(fx.zoom), float32(fx.rippleAmp), float32(fx.rippleFreq), float32(fx.rTim)}
	p.ColorAdj = [4]float32{float32(cf), float32(-cfg.Background.Darken + fx.bright), float32(cfg.Background.Opacity), float32(cfg.Background.Vignette)}
	p.Chroma = [4]float32{float32(fx.chromaShift)}
	nGlitch := 0
	if fx.glitch {
		for _, sl := range effects.GlitchSlices(s.w, s.h, cfg.BackgroundEffects.Glitch.Intensity, int64(frameIdx)) {
			if nGlitch == gpu.MaxGlitch {
				break
			}
			p.Glitch[nGlitch] = [4]float32{float32(sl.Y0), float32(sl.Y1), float32(sl.ShiftX)}
			nGlitch++
		}
	}

	// Partículas
	np := 0
	s.parts.Each(func(x, y, size, alpha float64, col color.RGBA, glow bool) {
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
	p.Shake = [4]float32{float32(fx.shakeX), float32(fx.shakeY)}

	// Visualizador
	vizType := int32(0)
	if v.Enabled {
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

	chromaMode := int32(fx.chromaMode)
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
	p.Flags2 = [4]int32{chromaMode, colorMode, int32(ng), b2i(writeRGBA)}
	p.Flags3 = [4]int32{b2i(v.MirrorBottom), b2i(v.Glow.Enabled), 0, 1}
	p.GradInfo = [4]float32{float32(b2i(v.GradientAxis == "length")), float32(b2i(v.GradientFit == "max")), float32(v.MaxHeight)}

	bw := v.BarWidth
	glowR, glowI := v.Glow.Radius, v.Glow.Intensity
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
		base := rad * (1.0 + feat.BassEnergy*0.08 + feat.BeatPulse*0.04)
		p.Viz0 = [4]float32{float32(cx), float32(cy), float32(base), 0}
		p.Viz1 = [4]float32{float32(bw), 0, 0, float32(0.4 + feat.BassEnergy*0.3)}
	case 2:
		posY := v.PositionY
		if posY <= 0 {
			posY = 0.85
		}
		halfW := W / 2
		gap := v.BarGap
		span := bw + gap
		if float64(n)*span > halfW*0.95 {
			span = halfW * 0.95 / float64(n)
			bw = span * 0.75
		}
		p.Viz0 = [4]float32{float32(halfW), 0, 0, float32(H * posY)}
		p.Viz1 = [4]float32{float32(bw), float32(span), float32(v.Roundness), 0}
	case 3:
		span := H * 0.8 / float64(n)
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
	p.Viz2[0] = float32(glowR)
	p.Viz2[1] = float32(glowI)
}
