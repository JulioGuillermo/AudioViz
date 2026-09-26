package engine

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/particles"
	"audioviz/internal/video"
	"audioviz/internal/visualizer"
	"github.com/fogleman/gg"
)

type RenderEngine struct {
	cfg *config.ProjectConfig
}

func NewRenderEngine(cfg *config.ProjectConfig) *RenderEngine {
	return &RenderEngine{cfg: cfg}
}

// RenderOptions opciones auxiliares para previsualización o renderizado parcial
type RenderOptions struct {
	MaxDurationSec float64 // Si > 0, limita la duración del video (ej. 10s para preview)
	SingleFrameAt  float64 // Si >= 0, exporta solo ese cuadro como imagen PNG
	SingleFrameOut string  // Ruta de salida de la imagen de cuadro único
}

func (e *RenderEngine) Execute(opts RenderOptions) error {
	startTime := time.Now()

	// 1. Probar medio de fondo para determinar dimensiones
	fmt.Printf("🔍 Inspeccionando medio de fondo: %s\n", e.cfg.Background.Path)
	probe, err := video.ProbeMedia(e.cfg.Background.Path)
	if err != nil {
		return fmt.Errorf("error al inspeccionar fondo: %w", err)
	}

	targetW := e.cfg.Output.Width
	targetH := e.cfg.Output.Height

	// Si width o height es 0, adaptar a las dimensiones del fondo (requisito del usuario)
	if targetW <= 0 || targetH <= 0 {
		targetW = probe.Width
		targetH = probe.Height
	}

	// Asegurar dimensiones pares para códec H.264
	targetW = (targetW / 2) * 2
	targetH = (targetH / 2) * 2

	fmt.Printf("📐 Resolución de video fijada en: %dx%d (Fondo: %s)\n", targetW, targetH,
		map[bool]string{true: "Video", false: "Imagen"}[probe.IsVideo])

	// 2. Decodificar audio
	fmt.Printf("🎵 Decodificando pista de audio: %s\n", e.cfg.Audio.Path)
	decodedAudio, err := audio.DecodeAudio(e.cfg.Audio.Path, e.cfg.Audio.Volume)
	if err != nil {
		return fmt.Errorf("error al decodificar audio: %w", err)
	}
	fmt.Printf("✅ Audio cargado: %.2f segundos (%d Hz, Estéreo)\n", decodedAudio.DurationSec, decodedAudio.SampleRate)

	// Calcular cuadros totales
	fps := e.cfg.Output.FPS
	fullFrames := int(decodedAudio.DurationSec * float64(fps))
	totalFrames := fullFrames

	if opts.SingleFrameAt >= 0 && opts.SingleFrameOut != "" {
		targetFrameIdx := int(opts.SingleFrameAt * float64(fps))
		if targetFrameIdx >= fullFrames {
			targetFrameIdx = fullFrames - 1
		}
		totalFrames = targetFrameIdx + 1
	} else if opts.MaxDurationSec > 0 {
		previewFrames := int(opts.MaxDurationSec * float64(fps))
		if previewFrames < totalFrames {
			totalFrames = previewFrames
		}
	}

	// 3. Analizar audio (FFT estéreo por cuadro con separación L y R)
	fmt.Printf("⚡ Analizando espectro de audio (FFT Estéreo, %d bandas, %d cuadros)...\n",
		e.cfg.Visualizer.Bars, totalFrames)
	analyzer := audio.NewAudioAnalyzer(
		decodedAudio,
		fps,
		e.cfg.Visualizer.Bars,
		e.cfg.Audio.Sensitivity,
		e.cfg.Visualizer.Smoothing,
		e.cfg.Visualizer.DecayRate,
	)
	allFeatures := analyzer.AnalyzeAllFrames(totalFrames)

	// Caso especial: Renderizado de un cuadro único para prueba/calibración
	if opts.SingleFrameAt >= 0 && opts.SingleFrameOut != "" {
		targetFrameIdx := int(opts.SingleFrameAt * float64(fps))
		if targetFrameIdx >= totalFrames {
			targetFrameIdx = totalFrames - 1
		}
		fmt.Printf("📸 Renderizando cuadro único en t=%.2fs (frame %d)...\n", opts.SingleFrameAt, targetFrameIdx)
		return e.renderSingleFrame(targetW, targetH, targetFrameIdx, allFeatures, opts.SingleFrameOut)
	}

	// 4. Inicializar Lector de Fondo
	var bgReader video.BackgroundReader
	if probe.IsVideo {
		bgReader, err = video.NewVideoBackgroundReader(e.cfg.Background.Path, targetW, targetH, fps, e.cfg.Background.LoopVideo)
	} else {
		bgReader, err = video.NewImageBackgroundReader(e.cfg.Background.Path, targetW, targetH, e.cfg.Background.FitMode)
	}
	if err != nil {
		return fmt.Errorf("error inicializando lector de fondo: %w", err)
	}
	defer bgReader.Close()

	// 5. Inicializar Escritor de Video (FFmpeg Pipe)
	fmt.Printf("🎬 Iniciando codificador de video FFmpeg (%s, CRF %d, Preset %s)...\n",
		e.cfg.Output.Codec, e.cfg.Output.CRF, e.cfg.Output.Preset)
	
	durationSec := float64(totalFrames) / float64(fps)
	writer, err := video.NewVideoWriter(
		e.cfg.Audio.Path,
		e.cfg.Output.Path,
		targetW,
		targetH,
		fps,
		e.cfg.Output.Codec,
		e.cfg.Output.Preset,
		e.cfg.Output.CRF,
		e.cfg.Output.AudioBitrate,
		durationSec,
	)
	if err != nil {
		return fmt.Errorf("error iniciando codificador de video: %w", err)
	}
	defer writer.Close()

	// 6. Inicializar Componentes Visuales
	viz, err := visualizer.NewVisualizer(&e.cfg.Visualizer, targetW, targetH)
	if err != nil {
		return fmt.Errorf("error creando visualizador: %w", err)
	}

	partSystem := particles.NewParticleSystem(&e.cfg.Particles, targetW, targetH)

	// 7. Bucle de Renderizado
	dt := 1.0 / float64(fps)
	fmt.Println("🚀 Renderizando video...")

	lastReport := time.Now()
	for i := 0; i < totalFrames; i++ {
		feat := &allFeatures[i]

		// A. Obtener cuadro base de fondo
		frame, err := bgReader.GetFrame(i)
		if err != nil {
			return fmt.Errorf("error leyendo cuadro de fondo %d: %w", i, err)
		}

		// B. Aplicar efectos de distorsión sobre el fondo
		frame = e.applyBackgroundEffects(frame, feat, i)

		// C. Componer sobre contexto gráfico de 2D (gg)
		dc := gg.NewContextForRGBA(frame)

		// D. Dibujar sistema de partículas reactivas
		partSystem.UpdateAndDraw(dc, feat, dt)

		// E. Dibujar espectrograma estéreo (Circular, Horizontal, etc.)
		viz.Draw(dc, feat)

		// F. Enviar cuadro procesado a FFmpeg stdin
		if err := writer.WriteFrame(frame); err != nil {
			return fmt.Errorf("error escribiendo cuadro %d a FFmpeg: %w", i, err)
		}

		// G. Reporte de progreso en terminal
		if time.Since(lastReport) >= 1*time.Second || i == totalFrames-1 {
			elapsed := time.Since(startTime).Seconds()
			framesDone := i + 1
			fpsActual := float64(framesDone) / elapsed
			pct := float64(framesDone) / float64(totalFrames) * 100.0
			remainingSec := float64(totalFrames-framesDone) / fpsActual
			fmt.Printf("\r⏳ [%5.1f%%] Cuadro %d/%d (%.1f FPS) | ETA: %.0fs    ",
				pct, framesDone, totalFrames, fpsActual, remainingSec)
			lastReport = time.Now()
		}
	}

	fmt.Printf("\n✨ Renderizado completado con éxito en %s!\n📁 Video guardado en: %s\n",
		time.Since(startTime).Round(time.Millisecond), e.cfg.Output.Path)
	return nil
}

func (e *RenderEngine) applyBackgroundEffects(frame *image.RGBA, feat *audio.FrameFeatures, frameIdx int) *image.RGBA {
	eff := &e.cfg.BackgroundEffects

	// 1. Bass Pulse / Zoom
	if eff.BassZoom.Enabled {
		zoom := 1.0 + (eff.BassZoom.MaxZoom-1.0)*feat.BassEnergy*eff.BassZoom.Sensitivity
		if feat.BeatPulse > 0 {
			zoom += (eff.BassZoom.MaxZoom - 1.0) * feat.BeatPulse * 0.5
		}
		frame = effects.ApplyBassZoom(frame, zoom)
	}

	// 2. Ripple (Deformación de onda)
	if eff.Ripple.Enabled {
		timeVal := float64(frameIdx) * 0.05 * eff.Ripple.Speed
		amp := eff.Ripple.Amplitude * (0.3 + feat.MidEnergy*0.7)
		frame = effects.ApplyRipple(frame, amp, eff.Ripple.Frequency, timeVal)
	}

	// 3. Chromatic Aberration (Separación RGB)
	if eff.ChromaticAberration.Enabled {
		var reactVal float64
		switch eff.ChromaticAberration.ReactTo {
		case "treble":
			reactVal = feat.TrebleEnergy
		case "transient":
			reactVal = feat.SpectralFlux * 5.0
		case "overall":
			reactVal = feat.OverallRMS * 3.0
		case "bass":
			fallthrough
		default:
			reactVal = feat.BassEnergy
		}
		shift := eff.ChromaticAberration.MaxShift * reactVal
		frame = effects.ApplyChromaticAberration(frame, shift, eff.ChromaticAberration.Mode)
	}

	// 4. Glitch / VHS
	if eff.Glitch.Enabled {
		if feat.SpectralFlux > eff.Glitch.Threshold || feat.IsBeat {
			frame = effects.ApplyGlitch(frame, eff.Glitch.Intensity, int64(frameIdx))
		}
	}

	// 5. Ajustes de color, brillo, contraste, viñeta y oscuridad
	brightBoost := 0.0
	contrastBoost := 0.0
	if eff.ColorFlash.Enabled {
		brightBoost = eff.ColorFlash.BrightnessBoost * feat.BassEnergy
		contrastBoost = eff.ColorFlash.ContrastBoost * feat.BassEnergy
	}
	effects.ApplyColorAndContrast(
		frame,
		brightBoost,
		contrastBoost,
		e.cfg.Background.Darken,
		e.cfg.Background.Opacity,
		e.cfg.Background.Vignette,
	)

	return frame
}

func (e *RenderEngine) renderSingleFrame(w, h, frameIdx int, allFeat []audio.FrameFeatures, outPath string) error {
	bgReader, err := video.NewImageBackgroundReader(e.cfg.Background.Path, w, h, e.cfg.Background.FitMode)
	if err != nil {
		return err
	}
	defer bgReader.Close()

	frame, err := bgReader.GetFrame(frameIdx)
	if err != nil {
		return err
	}

	feat := &allFeat[frameIdx]
	frame = e.applyBackgroundEffects(frame, feat, frameIdx)

	dc := gg.NewContextForRGBA(frame)

	partSystem := particles.NewParticleSystem(&e.cfg.Particles, w, h)
	// Simular unos pasos para que las partículas no estén en reposo inicial
	for s := 0; s < 30; s++ {
		partSystem.UpdateAndDraw(dc, feat, 1.0/60.0)
	}

	viz, err := visualizer.NewVisualizer(&e.cfg.Visualizer, w, h)
	if err != nil {
		return err
	}
	viz.Draw(dc, feat)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil && filepath.Dir(outPath) != "." {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := png.Encode(f, frame); err != nil {
		return err
	}

	fmt.Printf("✅ Cuadro guardado exitosamente en: %s\n", outPath)
	return nil
}
