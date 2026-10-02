package lyricsgen

import (
	"math"
	"runtime"
	"sync"
)

// High-quality resampling (Kaiser-windowed sinc, phase table) in pure Go.

const (
	rsTaps   = 32  // per side
	rsPhases = 512 // table resolution
)

func besselI0(x float64) float64 {
	s, t := 1.0, 1.0
	for k := 1; k < 30; k++ {
		t *= (x / (2 * float64(k))) * (x / (2 * float64(k)))
		s += t
	}
	return s
}

// Resample converts ch interleaved channels from srcRate to dstRate
func Resample(in []float32, ch, srcRate, dstRate int) []float32 {
	if srcRate == dstRate {
		out := make([]float32, len(in))
		copy(out, in)
		return out
	}
	n := len(in) / ch
	ratio := float64(srcRate) / float64(dstRate)
	outN := int(float64(n) / ratio)
	cut := math.Min(1, 1/ratio) * 0.94 // relative cutoff frequency (anti-aliasing)
	beta := 8.6
	// Table h[phase][k] for k in [-taps+1, taps]
	table := make([]float32, (rsPhases+1)*2*rsTaps)
	for ph := 0; ph <= rsPhases; ph++ {
		frac := float64(ph) / rsPhases
		var sum float64
		row := table[ph*2*rsTaps : (ph+1)*2*rsTaps]
		for k := -rsTaps + 1; k <= rsTaps; k++ {
			x := float64(k) - frac
			var s float64
			if x == 0 {
				s = cut
			} else {
				s = math.Sin(math.Pi*cut*x) / (math.Pi * x)
			}
			r := x / rsTaps
			w := 0.0
			if r > -1 && r < 1 {
				w = besselI0(beta*math.Sqrt(1-r*r)) / besselI0(beta)
			}
			v := s * w
			row[k+rsTaps-1] = float32(v)
			sum += v
		}
		for i := range row {
			row[i] = float32(float64(row[i]) / sum)
		}
	}
	out := make([]float32, outN*ch)
	var wg sync.WaitGroup
	workers := runtime.NumCPU()
	for wk := 0; wk < workers; wk++ {
		wg.Add(1)
		go func(wk int) {
			defer wg.Done()
			for i := wk * outN / workers; i < (wk+1)*outN/workers; i++ {
				pos := float64(i) * ratio
				base := int(pos)
				frac := pos - float64(base)
				ph := int(frac*rsPhases + 0.5)
				row := table[ph*2*rsTaps : (ph+1)*2*rsTaps]
				for c := 0; c < ch; c++ {
					var acc float32
					for k := -rsTaps + 1; k <= rsTaps; k++ {
						j := base + k
						if j < 0 || j >= n {
							continue
						}
						acc += in[j*ch+c] * row[k+rsTaps-1]
					}
					out[i*ch+c] = acc
				}
			}
		}(wk)
	}
	wg.Wait()
	return out
}

// ToMono downmixes interleaved stereo to mono
func ToMono(stereo []float32) []float32 {
	out := make([]float32, len(stereo)/2)
	for i := range out {
		out[i] = (stereo[2*i] + stereo[2*i+1]) * 0.5
	}
	return out
}
