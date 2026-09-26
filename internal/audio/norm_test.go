package audio_test

import (
	"math"
	"path/filepath"
	"testing"

	"audioviz/internal/audio"
)

// Compara saturación de barras y rango dinámico del bajo con y sin normalización
func TestNormalizationStats(t *testing.T) {
	d, err := audio.DecodeAudio(filepath.Join("..", "..", "I didnt ask to be the devil.flac"), 1.0)
	if err != nil {
		t.Skip(err)
	}
	n := int(d.DurationSec * 60)
	for _, norm := range []bool{false, true} {
		a := audio.NewAudioAnalyzer(d, 60, 56, 1, 0.75, 0.12)
		a.Normalize = norm
		f := a.AnalyzeAllFrames(n)
		sat, tot := 0, 0
		var bs, bs2, bmin, bmax float64
		bmin = 1
		for _, fr := range f {
			for _, v := range fr.LeftBars {
				if v > 0.95 {
					sat++
				}
				tot++
			}
			b := fr.BassEnergy
			bs += b
			bs2 += b * b
			bmin, bmax = math.Min(bmin, b), math.Max(bmax, b)
		}
		m := bs / float64(n)
		t.Logf("normalize=%v: barras saturadas(>0.95)=%.1f%%  bajo media=%.2f desv=%.2f rango=[%.2f,%.2f]",
			norm, 100*float64(sat)/float64(tot), m, math.Sqrt(bs2/float64(n)-m*m), bmin, bmax)
	}
}
