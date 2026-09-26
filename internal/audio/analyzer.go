package audio

import (
	"math"
	"math/cmplx"
)

const FFTWindowSize = 2048 // 2048 samples = ~42.6ms at 48kHz (resolution ~23.4Hz)

// FrameFeatures contiene los datos espectrales y de energía analizados para un cuadro de video específico
type FrameFeatures struct {
	FrameIndex   int
	TimestampSec float64

	// Barras espectrales normalizadas [0.0, 1.0] independientes para cada canal
	LeftBars     []float64
	RightBars    []float64

	// Indicadores agregados de reactividad
	BassEnergy   float64 // 20 - 150 Hz
	MidEnergy    float64 // 150 - 3000 Hz
	TrebleEnergy float64 // 3000 - 16000 Hz
	OverallRMS   float64 // Nivel global de amplitud
	
	// Reactividad dinámica
	SpectralFlux float64 // Cambio repentino de energía espectral
	IsBeat       bool    // Disparo de golpe de batería / transitorio fuerte
	BeatPulse    float64 // Pulso amortiguado de golpe (1.0 en pico, decae a 0.0)
}

// AudioAnalyzer procesa cuadros sucesivos manteniendo estado de amortiguación temporal
type AudioAnalyzer struct {
	audio          *DecodedAudio
	fps            int
	barCount       int
	sensitivity    float64
	smoothing      float64
	decayRate      float64

	// Tablas precalculadas
	hannWindow     []float64
	binFrequencies []binRange

	// Estado temporal previo para suavizado
	prevLeftBars   []float64
	prevRightBars  []float64
	prevBassEnergy float64
	prevFlux       float64
	beatPulseVal   float64
}

type binRange struct {
	startBin int
	endBin   int
}

// NewAudioAnalyzer inicializa el analizador con precomputación de ventanas y frecuencias
func NewAudioAnalyzer(audio *DecodedAudio, fps int, barCount int, sensitivity float64, smoothing float64, decayRate float64) *AudioAnalyzer {
	if smoothing <= 0 {
		smoothing = 0.75
	}
	if decayRate <= 0 {
		decayRate = 0.12
	}
	if barCount <= 0 {
		barCount = 48
	}
	if sensitivity <= 0 {
		sensitivity = 1.0
	}

	// 1. Precomputar ventana Hann
	hann := make([]float64, FFTWindowSize)
	for i := 0; i < FFTWindowSize; i++ {
		hann[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(FFTWindowSize-1)))
	}

	// 2. Precomputar rangos de frecuencia logarítmicos
	// Desde 25 Hz hasta 18000 Hz
	minFreq := 25.0
	maxFreq := 18000.0
	hzPerBin := float64(audio.SampleRate) / float64(FFTWindowSize)
	maxBin := FFTWindowSize / 2

	ranges := make([]binRange, barCount)
	for i := 0; i < barCount; i++ {
		// Distribución exponencial / logarítmica
		fStart := minFreq * math.Pow(maxFreq/minFreq, float64(i)/float64(barCount))
		fEnd := minFreq * math.Pow(maxFreq/minFreq, float64(i+1)/float64(barCount))

		bStart := int(math.Floor(fStart / hzPerBin))
		bEnd := int(math.Ceil(fEnd / hzPerBin))

		if bStart < 1 {
			bStart = 1
		}
		if bEnd <= bStart {
			bEnd = bStart + 1
		}
		if bEnd > maxBin {
			bEnd = maxBin
		}
		ranges[i] = binRange{startBin: bStart, endBin: bEnd}
	}

	return &AudioAnalyzer{
		audio:          audio,
		fps:            fps,
		barCount:       barCount,
		sensitivity:    sensitivity,
		smoothing:      smoothing,
		decayRate:      decayRate,
		hannWindow:     hann,
		binFrequencies: ranges,
		prevLeftBars:   make([]float64, barCount),
		prevRightBars:  make([]float64, barCount),
	}
}

// AnalyzeAllFrames preanaliza todos los cuadros del video secuencialmente para garantizar suavizado temporal perfecto
func (a *AudioAnalyzer) AnalyzeAllFrames(totalVideoFrames int) []FrameFeatures {
	results := make([]FrameFeatures, totalVideoFrames)
	samplesPerVideoFrame := float64(a.audio.SampleRate) / float64(a.fps)

	bufL := make([]float32, FFTWindowSize)
	bufR := make([]float32, FFTWindowSize)
	fftInL := make([]complex128, FFTWindowSize)
	fftInR := make([]complex128, FFTWindowSize)

	for frameIdx := 0; frameIdx < totalVideoFrames; frameIdx++ {
		centerSample := int(float64(frameIdx) * samplesPerVideoFrame)
		timestamp := float64(frameIdx) / float64(a.fps)

		// Extraer muestras para canal L (0) y canal R (1)
		a.audio.ExtractChannelSamples(0, centerSample, FFTWindowSize, bufL)
		a.audio.ExtractChannelSamples(1, centerSample, FFTWindowSize, bufR)

		rmsL := ComputeRMS(bufL)
		rmsR := ComputeRMS(bufR)
		overallRMS := (rmsL + rmsR) * 0.5

		// Aplicar ventana Hann y llenar buffers complejos
		for i := 0; i < FFTWindowSize; i++ {
			fftInL[i] = complex(float64(bufL[i])*a.hannWindow[i], 0)
			fftInR[i] = complex(float64(bufR[i])*a.hannWindow[i], 0)
		}

		// Ejecutar FFT Cooley-Tukey
		fftOutL := fftCooleyTukey(fftInL)
		fftOutR := fftCooleyTukey(fftInR)

		// Magnitudes para L y R
		magsL := make([]float64, FFTWindowSize/2)
		magsR := make([]float64, FFTWindowSize/2)
		for i := 0; i < FFTWindowSize/2; i++ {
			magsL[i] = cmplx.Abs(fftOutL[i])
			magsR[i] = cmplx.Abs(fftOutR[i])
		}

		// Extraer barras logarítmicas
		rawBarsL := a.extractBars(magsL)
		rawBarsR := a.extractBars(magsR)

		// Suavizado temporal con balística (Attack rápido, Decay suave)
		smoothBarsL := make([]float64, a.barCount)
		smoothBarsR := make([]float64, a.barCount)

		for b := 0; b < a.barCount; b++ {
			// Canal L
			valL := rawBarsL[b] * a.sensitivity
			prevL := a.prevLeftBars[b]
			if valL >= prevL {
				smoothBarsL[b] = prevL*(1.0-a.smoothing) + valL*a.smoothing
			} else {
				smoothBarsL[b] = math.Max(0, prevL*(1.0-a.decayRate))
			}
			a.prevLeftBars[b] = smoothBarsL[b]

			// Canal R
			valR := rawBarsR[b] * a.sensitivity
			prevR := a.prevRightBars[b]
			if valR >= prevR {
				smoothBarsR[b] = prevR*(1.0-a.smoothing) + valR*a.smoothing
			} else {
				smoothBarsR[b] = math.Max(0, prevR*(1.0-a.decayRate))
			}
			a.prevRightBars[b] = smoothBarsR[b]
		}

		// Bandas de frecuencia específicas
		hzPerBin := float64(a.audio.SampleRate) / float64(FFTWindowSize)
		bassEnergy := a.calculateBandEnergy(magsL, magsR, 20.0, 150.0, hzPerBin)
		midEnergy := a.calculateBandEnergy(magsL, magsR, 150.0, 3000.0, hzPerBin)
		trebleEnergy := a.calculateBandEnergy(magsL, magsR, 3000.0, 16000.0, hzPerBin)

		// Spectral Flux y Beat Detection
		flux := math.Max(0, bassEnergy-a.prevBassEnergy)
		a.prevBassEnergy = bassEnergy

		isBeat := false
		if flux > 0.08 && flux > a.prevFlux*1.2 {
			isBeat = true
			a.beatPulseVal = 1.0
		} else {
			a.beatPulseVal = math.Max(0, a.beatPulseVal*0.82)
		}
		a.prevFlux = flux

		results[frameIdx] = FrameFeatures{
			FrameIndex:   frameIdx,
			TimestampSec: timestamp,
			LeftBars:     smoothBarsL,
			RightBars:    smoothBarsR,
			BassEnergy:   bassEnergy,
			MidEnergy:    midEnergy,
			TrebleEnergy: trebleEnergy,
			OverallRMS:   overallRMS,
			SpectralFlux: flux,
			IsBeat:       isBeat,
			BeatPulse:    a.beatPulseVal,
		}
	}

	return results
}

func (a *AudioAnalyzer) extractBars(magnitudes []float64) []float64 {
	bars := make([]float64, a.barCount)
	for i, r := range a.binFrequencies {
		var sum float64
		count := 0
		for b := r.startBin; b < r.endBin && b < len(magnitudes); b++ {
			sum += magnitudes[b]
			count++
		}
		if count > 0 {
			avg := sum / float64(count)
			// Escala de compresión perceptual (logarítmica suave con normalización)
			norm := math.Log10(1.0 + avg*9.0) / 1.5
			if norm > 1.0 {
				norm = 1.0
			}
			bars[i] = norm
		}
	}
	return bars
}

func (a *AudioAnalyzer) calculateBandEnergy(magsL, magsR []float64, fMin, fMax, hzPerBin float64) float64 {
	bStart := int(fMin / hzPerBin)
	bEnd := int(fMax / hzPerBin)
	if bStart < 1 {
		bStart = 1
	}
	if bEnd > len(magsL) {
		bEnd = len(magsL)
	}
	if bEnd <= bStart {
		return 0
	}

	var sum float64
	for b := bStart; b < bEnd; b++ {
		sum += (magsL[b] + magsR[b]) * 0.5
	}
	avg := sum / float64(bEnd-bStart)
	val := math.Log10(1.0 + avg*8.0)
	if val > 1.0 {
		val = 1.0
	}
	return val
}

// fftCooleyTukey implementa la Transformada Rápida de Fourier Radix-2 en Go puro
func fftCooleyTukey(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		return x
	}

	even := make([]complex128, n/2)
	odd := make([]complex128, n/2)
	for i := 0; i < n/2; i++ {
		even[i] = x[i*2]
		odd[i] = x[i*2+1]
	}

	fftEven := fftCooleyTukey(even)
	fftOdd := fftCooleyTukey(odd)

	res := make([]complex128, n)
	for k := 0; k < n/2; k++ {
		angle := -2.0 * math.Pi * float64(k) / float64(n)
		twiddle := cmplx.Rect(1, angle) * fftOdd[k]
		res[k] = fftEven[k] + twiddle
		res[k+n/2] = fftEven[k] - twiddle
	}
	return res
}
