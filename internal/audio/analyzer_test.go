package audio_test

import (
	"path/filepath"
	"testing"

	"audioviz/internal/audio"
)

func TestDecodeAndAnalyze(t *testing.T) {
	testAudio := filepath.Join("..", "..", "I didnt ask to be the devil.flac")
	decoded, err := audio.DecodeAudio(testAudio, 1.0)
	if err != nil {
		t.Fatalf("Error decodificando audio: %v", err)
	}

	if decoded.SampleRate != 48000 {
		t.Errorf("Se esperaba 48000Hz, se obtuvo %d", decoded.SampleRate)
	}
	if decoded.DurationSec < 10.0 {
		t.Errorf("Duración inesperada: %f", decoded.DurationSec)
	}

	t.Logf("Audio decodificado correctamente: Duración = %.2f s, Muestras = %d", decoded.DurationSec, len(decoded.Samples))

	analyzer := audio.NewAudioAnalyzer(decoded, 60, 48, 1.0, 0.75, 0.12)
	// Analizar los primeros 60 cuadros (1 segundo)
	frames := analyzer.AnalyzeAllFrames(60)
	if len(frames) != 60 {
		t.Fatalf("Esperados 60 cuadros, obtenidos %d", len(frames))
	}

	f0 := frames[0]
	if len(f0.LeftBars) != 48 || len(f0.RightBars) != 48 {
		t.Errorf("Tamaño de barras incorrecto: L=%d, R=%d", len(f0.LeftBars), len(f0.RightBars))
	}

	t.Logf("Frame 30 Bass=%.3f, Mid=%.3f, Treble=%.3f, L[0]=%.3f, R[0]=%.3f",
		frames[30].BassEnergy, frames[30].MidEnergy, frames[30].TrebleEnergy,
		frames[30].LeftBars[0], frames[30].RightBars[0])
}
