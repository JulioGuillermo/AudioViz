package audio

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

const FFTWindowSize = 2048 // 2048 samples = ~42.6ms at 48kHz (resolution ~23.4Hz)

// FrameFeatures holds the analyzed spectral and energy data for a specific video frame
type FrameFeatures struct {
	FrameIndex   int
	TimestampSec float64

	// Normalized spectral bars [0.0, 1.0], independent per channel
	LeftBars  []float64
	RightBars []float64

	// Aggregated reactivity indicators
	BassEnergy   float64 // 20 - 150 Hz
	MidEnergy    float64 // 150 - 3000 Hz
	TrebleEnergy float64 // 3000 - 16000 Hz
	OverallRMS   float64 // Overall amplitude level

	// Dynamic reactivity
	SpectralFlux float64 // Sudden change in spectral energy
	IsBeat       bool    // Drum hit / strong transient trigger
	BeatPulse    float64 // Damped hit pulse (1.0 at peak, decays to 0.0)
}

// AudioAnalyzer processes successive frames, keeping temporal smoothing state
type AudioAnalyzer struct {
	audio       *DecodedAudio
	fps         int
	barCount    int
	sensitivity float64
	smoothing   float64
	decayRate   float64

	// Adaptive normalization: each signal is rescaled to its local range (window ±WindowSec)
	Normalize bool
	WindowSec float64

	// Precomputed tables
	hannWindow     []float64
	binFrequencies []binRange

	// Previous temporal state for smoothing
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

// NewAudioAnalyzer initializes the analyzer, precomputing windows and frequencies
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

	// 1. Precompute Hann window
	hann := make([]float64, FFTWindowSize)
	for i := 0; i < FFTWindowSize; i++ {
		hann[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(FFTWindowSize-1)))
	}

	// 2. Precompute logarithmic frequency ranges
	// From 25 Hz to 18000 Hz
	minFreq := 25.0
	maxFreq := 18000.0
	hzPerBin := float64(audio.SampleRate) / float64(FFTWindowSize)
	maxBin := FFTWindowSize / 2

	ranges := make([]binRange, barCount)
	for i := 0; i < barCount; i++ {
		// Exponential / logarithmic distribution
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
		Normalize:      true,
		WindowSec:      4.0,
	}
}

// AnalyzeAllFrames pre-analyzes all video frames. The FFTs (independent per frame)
// are computed in parallel on all cores; temporal smoothing is applied afterwards in order.
func (a *AudioAnalyzer) AnalyzeAllFrames(totalVideoFrames int) []FrameFeatures {
	results := make([]FrameFeatures, totalVideoFrames)
	samplesPerVideoFrame := float64(a.audio.SampleRate) / float64(a.fps)
	hzPerBin := float64(a.audio.SampleRate) / float64(FFTWindowSize)

	raws := make([]rawFrame, totalVideoFrames)
	fft := newFFTPlan(FFTWindowSize)

	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	var next atomic.Int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bufL := make([]float32, FFTWindowSize)
			bufR := make([]float32, FFTWindowSize)
			re := make([]float64, FFTWindowSize)
			im := make([]float64, FFTWindowSize)
			magsL := make([]float64, FFTWindowSize/2)
			magsR := make([]float64, FFTWindowSize/2)
			for {
				frameIdx := int(next.Add(1) - 1)
				if frameIdx >= totalVideoFrames {
					return
				}
				centerSample := int(float64(frameIdx) * samplesPerVideoFrame)
				a.audio.ExtractChannelSamples(0, centerSample, FFTWindowSize, bufL)
				a.audio.ExtractChannelSamples(1, centerSample, FFTWindowSize, bufR)

				for ch, buf := range [2][]float32{bufL, bufR} {
					for i := range re {
						re[i] = float64(buf[i]) * a.hannWindow[i]
						im[i] = 0
					}
					fft.transform(re, im)
					mags := magsL
					if ch == 1 {
						mags = magsR
					}
					for i := range mags {
						mags[i] = math.Hypot(re[i], im[i])
					}
				}

				raws[frameIdx] = rawFrame{
					barsL:  a.extractBars(magsL),
					barsR:  a.extractBars(magsR),
					bass:   a.calculateBandEnergy(magsL, magsR, 20.0, 150.0, hzPerBin),
					mid:    a.calculateBandEnergy(magsL, magsR, 150.0, 3000.0, hzPerBin),
					treble: a.calculateBandEnergy(magsL, magsR, 3000.0, 16000.0, hzPerBin),
					rmsAll: (ComputeRMS(bufL) + ComputeRMS(bufR)) * 0.5,
				}
			}
		}()
	}
	wg.Wait()

	series := func(get func(r *rawFrame) *float64) []*float64 {
		out := make([]*float64, totalVideoFrames)
		for i := range raws {
			out[i] = get(&raws[i])
		}
		return out
	}
	if a.Normalize && totalVideoFrames > 1 {
		win := int(a.WindowSec * float64(a.fps))
		// Energies: each against its own local range
		normalizeSeries(win, series(func(r *rawFrame) *float64 { return &r.bass }))
		normalizeSeries(win, series(func(r *rawFrame) *float64 { return &r.mid }))
		normalizeSeries(win, series(func(r *rawFrame) *float64 { return &r.treble }))
		normalizeSeries(win, series(func(r *rawFrame) *float64 { return &r.rmsAll }))
		bl, br := raws2bars(raws)
		a.normalizeBars(win, bl, br)
	} else {
		for i := range raws {
			r := &raws[i]
			r.bass, r.mid, r.treble = math.Min(r.bass, 1), math.Min(r.mid, 1), math.Min(r.treble, 1)
			for b := range r.barsL {
				r.barsL[b], r.barsR[b] = math.Min(r.barsL[b], 1), math.Min(r.barsR[b], 1)
			}
		}
	}

	for frameIdx := 0; frameIdx < totalVideoFrames; frameIdx++ {
		raw := &raws[frameIdx]
		rawBarsL, rawBarsR := raw.barsL, raw.barsR

		// Temporal smoothing with ballistics (fast Attack, smooth Decay)
		smoothBarsL := make([]float64, a.barCount)
		smoothBarsR := make([]float64, a.barCount)

		for b := 0; b < a.barCount; b++ {
			// Channel L
			valL := rawBarsL[b] * a.sensitivity
			prevL := a.prevLeftBars[b]
			if valL >= prevL {
				smoothBarsL[b] = prevL*(1.0-a.smoothing) + valL*a.smoothing
			} else {
				smoothBarsL[b] = math.Max(0, prevL*(1.0-a.decayRate))
			}
			a.prevLeftBars[b] = smoothBarsL[b]

			// Channel R
			valR := rawBarsR[b] * a.sensitivity
			prevR := a.prevRightBars[b]
			if valR >= prevR {
				smoothBarsR[b] = prevR*(1.0-a.smoothing) + valR*a.smoothing
			} else {
				smoothBarsR[b] = math.Max(0, prevR*(1.0-a.decayRate))
			}
			a.prevRightBars[b] = smoothBarsR[b]
		}

		// Spectral Flux y Beat Detection
		bassEnergy := raw.bass
		flux := math.Max(0, bassEnergy-a.prevBassEnergy)
		a.prevBassEnergy = bassEnergy

		isBeat := false
		beatThresh := 0.08
		if a.Normalize {
			beatThresh = 0.15 // the normalized range is larger
		}
		if flux > beatThresh && flux > a.prevFlux*1.2 {
			isBeat = true
			a.beatPulseVal = 1.0
		} else {
			a.beatPulseVal = math.Max(0, a.beatPulseVal*0.82)
		}
		a.prevFlux = flux

		results[frameIdx] = FrameFeatures{
			FrameIndex:   frameIdx,
			TimestampSec: float64(frameIdx) / float64(a.fps),
			LeftBars:     smoothBarsL,
			RightBars:    smoothBarsR,
			BassEnergy:   bassEnergy,
			MidEnergy:    raw.mid,
			TrebleEnergy: raw.treble,
			OverallRMS:   raw.rmsAll,
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
			// Perceptual compression scale (soft logarithmic with normalization)
			bars[i] = math.Log10(1.0+avg*9.0) / 1.5
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
	return math.Log10(1.0 + avg*8.0)
}

type rawFrame struct {
	barsL, barsR              []float64
	bass, mid, treble, rmsAll float64
}

func raws2bars(raws []rawFrame) (l, r [][]float64) {
	l = make([][]float64, len(raws))
	r = make([][]float64, len(raws))
	for i := range raws {
		l[i], r[i] = raws[i].barsL, raws[i].barsR
	}
	return
}

// rollingRange computes, for each frame, the floor (p10) and ceiling (p97) of x in a centered window
// of ±win frames. It is evaluated on a grid and interpolated to keep it cheap.
func rollingRange(x []float64, win int) (lo, hi []float64) {
	n := len(x)
	lo, hi = make([]float64, n), make([]float64, n)
	step := win / 12
	if step < 1 {
		step = 1
	}
	var grid []int
	for t := 0; t < n; t += step {
		grid = append(grid, t)
	}
	if grid[len(grid)-1] != n-1 {
		grid = append(grid, n-1)
	}
	glo, ghi := make([]float64, len(grid)), make([]float64, len(grid))
	buf := make([]float64, 0, 2*win+1)
	for gi, t := range grid {
		s, e := t-win, t+win+1
		if s < 0 {
			s = 0
		}
		if e > n {
			e = n
		}
		buf = append(buf[:0], x[s:e]...)
		sort.Float64s(buf)
		glo[gi] = buf[int(float64(len(buf)-1)*0.10)]
		ghi[gi] = buf[int(float64(len(buf)-1)*0.97)]
	}
	for gi := 0; gi+1 < len(grid); gi++ {
		t0, t1 := grid[gi], grid[gi+1]
		for t := t0; t <= t1; t++ {
			f := 0.0
			if t1 > t0 {
				f = float64(t-t0) / float64(t1-t0)
			}
			lo[t] = glo[gi]*(1-f) + glo[gi+1]*f
			hi[t] = ghi[gi]*(1-f) + ghi[gi+1]*f
		}
	}
	if len(grid) == 1 {
		lo[0], hi[0] = glo[0], ghi[0]
	}
	return
}

// softKnee compresses smoothly above 0.85 instead of hard clipping
func softKnee(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v <= 0.85 {
		return v
	}
	return 0.85 + 0.15*math.Tanh((v-0.85)/0.15)
}

// normalizeSeries rescales a signal in-place to its local range [p10, p97].
// This way a constantly loud bass keeps "breathing" instead of pinning the effect at maximum.
func normalizeSeries(win int, ptrs []*float64) {
	x := make([]float64, len(ptrs))
	gmax := 0.0
	for i, p := range ptrs {
		x[i] = *p
	}
	lo, hi := rollingRange(x, win)
	for _, h := range hi {
		gmax = math.Max(gmax, h)
	}
	minSpan := math.Max(gmax*0.25, 1e-4) // avoids amplifying noise in near-silent passages
	for i, p := range ptrs {
		*p = softKnee((x[i] - lo[i]) / math.Max(hi[i]-lo[i], minSpan))
	}
}

// normalizeBars normalizes each band against its own local range (shared between L and R to
// preserve stereo balance), with a minimum relative to the full spectrum to avoid inflating
// nearly empty bands.
func (a *AudioAnalyzer) normalizeBars(win int, left, right [][]float64) {
	n := len(left)
	nb := a.barCount
	los := make([][]float64, nb)
	his := make([][]float64, nb)
	var wg sync.WaitGroup
	for b := 0; b < nb; b++ {
		wg.Add(1)
		go func(b int) {
			defer wg.Done()
			x := make([]float64, n)
			for t := 0; t < n; t++ {
				x[t] = math.Max(left[t][b], right[t][b])
			}
			los[b], his[b] = rollingRange(x, win)
		}(b)
	}
	wg.Wait()
	for t := 0; t < n; t++ {
		maxHi := 0.0
		for b := 0; b < nb; b++ {
			maxHi = math.Max(maxHi, his[b][t])
		}
		minSpan := math.Max(maxHi*0.35, 1e-4)
		for b := 0; b < nb; b++ {
			span := math.Max(his[b][t]-los[b][t], minSpan)
			left[t][b] = softKnee((left[t][b] - los[b][t]) / span)
			right[t][b] = softKnee((right[t][b] - los[b][t]) / span)
		}
	}
}

// fftPlan is an iterative in-place radix-2 FFT with precomputed tables (no per-call allocations)
type fftPlan struct {
	n      int
	rev    []int
	cosTab []float64
	sinTab []float64
}

func newFFTPlan(n int) *fftPlan {
	p := &fftPlan{n: n, rev: make([]int, n), cosTab: make([]float64, n/2), sinTab: make([]float64, n/2)}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := 0; i < n; i++ {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		p.rev[i] = r
	}
	for i := 0; i < n/2; i++ {
		angle := -2.0 * math.Pi * float64(i) / float64(n)
		p.cosTab[i] = math.Cos(angle)
		p.sinTab[i] = math.Sin(angle)
	}
	return p
}

func (p *fftPlan) transform(re, im []float64) {
	n := p.n
	for i := 0; i < n; i++ {
		if j := p.rev[i]; j > i {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		step := n / size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				wr, wi := p.cosTab[k*step], p.sinTab[k*step]
				a, b := start+k, start+k+half
				tr := wr*re[b] - wi*im[b]
				ti := wr*im[b] + wi*re[b]
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
			}
		}
	}
}
