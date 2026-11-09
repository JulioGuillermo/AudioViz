package engine

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"audioviz/internal/config"
)

// gasSession: a small project with one emitter clip of the given preset (an audio file and a
// background made here)
func gasSession(t testing.TB, preset, extra string) *Session {
	t.Helper()
	dir := t.TempDir()
	// a second of silence (16-bit mono WAV) and a plain background
	n := 44100 * 3
	wav := make([]byte, 44+2*n)
	copy(wav, "RIFF")
	put32 := func(o int, v uint32) {
		wav[o], wav[o+1], wav[o+2], wav[o+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	put16 := func(o int, v uint16) { wav[o], wav[o+1] = byte(v), byte(v>>8) }
	put32(4, uint32(36+2*n))
	copy(wav[8:], "WAVEfmt ")
	put32(16, 16)
	put16(20, 1)
	put16(22, 1)
	put32(24, 44100)
	put32(28, 88200)
	put16(32, 2)
	put16(34, 16)
	copy(wav[36:], "data")
	put32(40, uint32(2*n))
	if err := os.WriteFile(filepath.Join(dir, "a.wav"), wav, 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Create(filepath.Join(dir, "bg.png"))
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 64, 36)))
	f.Close()
	y := `
audio: { path: "a.wav" }
background: { path: "bg.png" }
output: { width: ` + gasTestW + `, height: ` + gasTestH + `, fps: 30 }
layers:
  - name: G
    kind: emitter
    clips:
      - { start: 0, end: 3, x: 0.5, y: 0.9, emitter: { preset: ` + preset + extra + ` } }
`
	cfg, err := config.ParseConfig([]byte(y), dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func gasCells(s *Session, idx int) []uint32 {
	c0 := &s.Cfg.Layers[0].Clips[0]
	ef := s.emitFrame(0, 0, c0, c0, idx, nil)
	if ef == nil || ef.P.Gas == nil {
		return nil
	}
	return slices.Clone(ef.P.Gas.Cells)
}

// the gas at a frame is the same however it is reached: step by step, straight there, or back
// from later (the checkpoints)
func TestGasSeek(t *testing.T) {
	a := gasSession(t, "fire_gas", "")
	var seq [][]uint32
	for k := 0; k <= 75; k++ {
		seq = append(seq, gasCells(a, k))
	}
	if seq[75] == nil {
		t.Fatal("no gas")
	}
	b := gasSession(t, "fire_gas", "")
	if !slices.Equal(gasCells(b, 75), seq[75]) {
		t.Fatal("straight to frame 75 differs from step by step")
	}
	for _, k := range []int{40, 29, 61, 10} {
		if !slices.Equal(gasCells(b, k), seq[k]) {
			t.Fatalf("back to frame %d differs", k)
		}
	}
	// a change of how it looks keeps the simulation; a change of how it moves does not
	run := b.gasRuns["0:0"]
	b.Cfg.Layers[0].Clips[0].Emitter.Gas.LightAngle = 40
	gasCells(b, 75)
	if len(run.fp) < 76 || b.gasRuns["0:0"] != run {
		t.Fatal("the simulation was dropped for a change of light")
	}
	b.Cfg.Layers[0].Clips[0].Emitter.Gas.Rise = 100
	if slices.Equal(gasCells(b, 75), seq[75]) {
		t.Fatal("a change of rise did not change the gas")
	}
	sum := 0.0
	for _, v := range seq[75] {
		sum += float64(v & 0xffff)
	}
	if math.IsNaN(sum) {
		t.Fatal("NaN")
	}
}

var gasTestW, gasTestH = "320", "180"

// the gas of one emitter at 1080p, 86 frames from the start
func BenchmarkGasEngine(b *testing.B) {
	gasTestW, gasTestH = "1920", "1080"
	defer func() { gasTestW, gasTestH = "320", "180" }()
	for i := 0; i < b.N; i++ {
		a := gasSession(b, "fire_gas", "")
		if gasCells(a, 85) == nil {
			b.Fatal("no gas")
		}
	}
}
