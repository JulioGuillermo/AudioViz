package audio

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

const FFTWindowSize = 2048 // 2048 samples = ~42.6ms at 48kHz (resolution ~23.4Hz)

// The bass bars (log-spaced: a few Hz wide) come from a longer window (~171 ms at 48 kHz,
// ~5.9 Hz per bin), blended into the short one between bassFull and bassUntil Hz; a bar narrower
// than two bins reads the spectrum at its centre (interpolated), so neighbours never share a bin
const (
	FFTBassSize = 8192
	bassFull    = 220.0
	bassUntil   = 450.0
)

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

	// The song separated into its instruments at this frame (nil until it is separated)
	Stem *StemFrame
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

// a bar: its frequencies (Hz) and how much of it comes from the long window (0..1)
type binRange struct {
	f0, f1 float64
	long   float64
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
	ranges := make([]binRange, barCount)
	for i := 0; i < barCount; i++ {
		// Exponential / logarithmic distribution
		fStart := minFreq * math.Pow(maxFreq/minFreq, float64(i)/float64(barCount))
		fEnd := minFreq * math.Pow(maxFreq/minFreq, float64(i+1)/float64(barCount))
		fc := math.Sqrt(fStart * fEnd)
		long := 1 - math.Max(0, math.Min(1, (fc-bassFull)/(bassUntil-bassFull)))
		ranges[i] = binRange{f0: fStart, f1: fEnd, long: long}
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
	fftB := newFFTPlan(FFTBassSize)
	hannB := make([]float64, FFTBassSize)
	for i := range hannB {
		hannB[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(FFTBassSize-1)))
	}
	hzB := float64(a.audio.SampleRate) / float64(FFTBassSize)

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
			bufB := make([]float32, FFTBassSize)
			reB := make([]float64, FFTBassSize)
			imB := make([]float64, FFTBassSize)
			bassL := make([]float64, FFTBassSize/2)
			bassR := make([]float64, FFTBassSize/2)
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

				// the long window, for the bass bars (scaled as the short one: a steady tone
				// gives the same magnitude)
				for ch, mags := range [2][]float64{bassL, bassR} {
					a.audio.ExtractChannelSamples(ch, centerSample, FFTBassSize, bufB)
					for i := range reB {
						reB[i] = float64(bufB[i]) * hannB[i]
						imB[i] = 0
					}
					fftB.transform(reB, imB)
					for i := range mags {
						mags[i] = math.Hypot(reB[i], imB[i]) * (float64(FFTWindowSize) / FFTBassSize)
					}
				}

				raws[frameIdx] = rawFrame{
					barsL:  a.extractBars(magsL, bassL, hzPerBin, hzB),
					barsR:  a.extractBars(magsR, bassR, hzPerBin, hzB),
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

func (a *AudioAnalyzer) extractBars(mags, bass []float64, hz, hzB float64) []float64 {
	bars := make([]float64, a.barCount)
	for i, r := range a.binFrequencies {
		avg := 0.0
		if r.long < 1 {
			avg = bandMag(mags, hz, r.f0, r.f1) * (1 - r.long)
		}
		if r.long > 0 {
			avg += bandMag(bass, hzB, r.f0, r.f1) * r.long
		}
		// Perceptual compression scale (soft logarithmic with normalization)
		bars[i] = math.Log10(1.0+avg*9.0) / 1.5
	}
	return bars
}

// bandMag: the spectrum's mean magnitude from f0 to f1 Hz (bins of hz Hz): the bins it covers, or
// where it is narrower than two bins, the spectrum at its centre between the two nearest bins
func bandMag(mags []float64, hz, f0, f1 float64) float64 {
	n := len(mags)
	if f1-f0 < 2*hz {
		x := math.Sqrt(f0*f1) / hz
		i := int(math.Floor(x))
		if i < 1 {
			return mags[min(1, n-1)]
		}
		if i >= n-1 {
			return mags[n-1]
		}
		t := x - float64(i)
		return mags[i]*(1-t) + mags[i+1]*t
	}
	b0 := max(1, int(math.Floor(f0/hz)))
	b1 := min(n, int(math.Ceil(f1/hz)))
	if b1 <= b0 {
		b1 = min(n, b0+1)
	}
	sum := 0.0
	for b := b0; b < b1; b++ {
		sum += mags[b]
	}
	return sum / float64(max(1, b1-b0))
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
