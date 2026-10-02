// Package lyricsgen genera la letra sincronizada (tiempo por palabra) a partir del audio y la
// letra en texto, todo dentro del binario: Demucs (separación de voz) + Whisper (reconocimiento)
// + alineado con la letra conocida. Los modelos se descargan una vez a ~/.cache/audioviz.
package lyricsgen

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Step es un paso del proceso, con su estado y progreso
type Step struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	State    string  `json:"state"` // pending, running, done, skipped, error
	Progress float64 `json:"progress"`
	Detail   string  `json:"detail"`
	Elapsed  float64 `json:"elapsed"` // s
	ETA      float64 `json:"eta"`     // s restantes estimados (-1 = desconocido)
	started  time.Time
}

// Status es el estado completo que se envía a la interfaz
type Status struct {
	Steps    []Step  `json:"steps"`
	Current  int     `json:"current"`
	Progress float64 `json:"progress"` // global 0..1
	Device   string  `json:"device"`   // GPU/CPU del reconocimiento
	Elapsed  float64 `json:"elapsed"`
}

type Options struct {
	Stereo     []float32 // audio estéreo entrelazado
	SampleRate int
	AudioKey   string // identifica el audio para la caché de la voz separada (p. ej. ruta)
	Lyrics     string // letra en texto (vale el formato de Suno)
	Lang       string
	Separate   bool // separar la voz antes de reconocer (más preciso, más lento)
}

type Result struct {
	Lines  []LLine
	LRC    string
	Device string
}

// Pesos de cada paso en el progreso global (aprox. proporcional a su duración)
var stepWeights = map[string]float64{"models": 0.05, "prepare": 0.03, "separate": 0.62, "recognize": 0.22, "align": 0.08}

type tracker struct {
	mu     sync.Mutex
	st     Status
	t0     time.Time
	report func(Status)
	last   time.Time
}

func (t *tracker) idx(id string) int {
	for i, s := range t.st.Steps {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func (t *tracker) emit(force bool) {
	if !force && time.Since(t.last) < 150*time.Millisecond {
		return
	}
	t.last = time.Now()
	var wsum, done float64
	for i := range t.st.Steps {
		s := &t.st.Steps[i]
		w := stepWeights[s.ID]
		if s.State == "skipped" {
			continue
		}
		wsum += w
		switch s.State {
		case "done":
			done += w
		case "running":
			done += w * s.Progress
			s.Elapsed = time.Since(s.started).Seconds()
			s.ETA = -1
			if s.Progress > 0.02 && s.Elapsed > 1 {
				s.ETA = s.Elapsed * (1 - s.Progress) / s.Progress
			}
		}
	}
	if wsum > 0 {
		t.st.Progress = done / wsum
	}
	t.st.Elapsed = time.Since(t.t0).Seconds()
	cp := t.st
	cp.Steps = append([]Step(nil), t.st.Steps...)
	if t.report != nil {
		t.report(cp)
	}
}

func (t *tracker) start(id, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.idx(id)
	t.st.Current = i
	t.st.Steps[i].State, t.st.Steps[i].Detail, t.st.Steps[i].started, t.st.Steps[i].Progress = "running", detail, time.Now(), 0
	t.emit(true)
}

func (t *tracker) progress(id string, p float64, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.idx(id)
	t.st.Steps[i].Progress = math.Max(0, math.Min(1, p))
	if detail != "" {
		t.st.Steps[i].Detail = detail
	}
	t.emit(false)
}

func (t *tracker) end(id, state, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.idx(id)
	s := &t.st.Steps[i]
	if !s.started.IsZero() {
		s.Elapsed = time.Since(s.started).Seconds()
	}
	s.State, s.ETA = state, 0
	if state == "done" {
		s.Progress = 1
	}
	if detail != "" {
		s.Detail = detail
	}
	t.emit(true)
}

func mb(b int64) string { return fmt.Sprintf("%.0f MB", float64(b)/1e6) }

func vocalsCachePath(key string) string {
	if key == "" {
		return ""
	}
	h := sha1.Sum([]byte(key))
	return filepath.Join(CacheDir(), "vocals", fmt.Sprintf("%x.f32", h[:10]))
}

func loadF32(path string) []float32 {
	b, err := os.ReadFile(path)
	if err != nil || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func saveF32(path string, v []float32) {
	os.MkdirAll(filepath.Dir(path), 0755)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), len(v)*4)
	_ = os.WriteFile(path, b, 0644)
}

// Generate ejecuta todo el proceso
func Generate(ctx context.Context, opt Options, report func(Status)) (*Result, error) {
	lines := CleanLyrics(opt.Lyrics)
	if len(lines) == 0 {
		return nil, fmt.Errorf("la letra está vacía (después de quitar las etiquetas [ ])")
	}
	if opt.Lang == "" {
		opt.Lang = "en"
	}
	t := &tracker{t0: time.Now(), report: report}
	t.st.Steps = []Step{
		{ID: "models", Label: "Modelos", State: "pending"},
		{ID: "prepare", Label: "Preparar audio", State: "pending"},
		{ID: "separate", Label: "Separar la voz", State: "pending"},
		{ID: "recognize", Label: "Reconocer la voz", State: "pending"},
		{ID: "align", Label: "Alinear con la letra", State: "pending"},
	}
	cachePath := vocalsCachePath(fmt.Sprintf("%s|%d|%d", opt.AudioKey, opt.SampleRate, len(opt.Stereo)))
	var vocals16 []float32
	if opt.Separate && cachePath != "" {
		vocals16 = loadF32(cachePath)
	}

	// 1. Modelos (descarga la primera vez)
	t.start("models", "Comprobando…")
	need := []Model{WhisperModel}
	if opt.Separate && vocals16 == nil {
		need = append(need, DemucsModel)
	}
	var missing int64
	for _, m := range need {
		if !m.Present() {
			missing += m.Size
		}
	}
	var got int64
	for _, m := range need {
		if m.Present() {
			continue
		}
		base := got
		_, err := m.Ensure(ctx, func(done, total int64, rate float64) {
			d := fmt.Sprintf("%s · %s de %s", m.Label, mb(done), mb(total))
			if rate > 0 {
				d += fmt.Sprintf(" · %.1f MB/s", rate/1e6)
			}
			t.progress("models", float64(base+done)/float64(missing), d)
		})
		if err != nil {
			t.end("models", "error", err.Error())
			return nil, err
		}
		got += m.Size
	}
	if missing == 0 {
		t.end("models", "done", "Ya descargados")
	} else {
		t.end("models", "done", "Descargados ("+mb(missing)+")")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 2. Preparar audio (remuestreo)
	t.start("prepare", "Remuestreando…")
	var stereo44 []float32
	mix16 := ToMono(Resample(opt.Stereo, 2, opt.SampleRate, 16000))
	if opt.Separate && vocals16 == nil {
		stereo44 = Resample(opt.Stereo, 2, opt.SampleRate, 44100)
	}
	t.end("prepare", "done", fmt.Sprintf("%.0f s de audio", float64(len(mix16))/16000))

	// 3. Separar la voz
	switch {
	case !opt.Separate:
		t.end("separate", "skipped", "Modo rápido: se usa la mezcla completa")
		vocals16 = mix16
	case vocals16 != nil:
		t.end("separate", "skipped", "Reutilizada de la caché")
	default:
		t.start("separate", "Demucs htdemucs · CPU")
		v44, err := SeparateVocals(ctx, DemucsModel.Path(), stereo44, func(p float64) { t.progress("separate", p, "") })
		if err != nil {
			t.end("separate", "error", err.Error())
			return nil, err
		}
		vocals16 = ToMono(Resample(v44, 2, 44100, 16000))
		if cachePath != "" {
			saveF32(cachePath, vocals16)
		}
		t.end("separate", "done", "")
	}
	total := float64(len(vocals16)) / 16000
	v := newVoice(vocals16)

	// 4. Reconocer
	t.start("recognize", "Cargando el modelo…")
	w, err := OpenWhisper(WhisperModel.Path(), aheadsLargeV3Turbo, true)
	if err != nil {
		t.end("recognize", "error", err.Error())
		return nil, err
	}
	defer w.Close()
	t.mu.Lock()
	t.st.Device = w.Device
	t.mu.Unlock()
	t.progress("recognize", 0, "Whisper large-v3-turbo · "+w.Device)
	rec, err := w.Transcribe(ctx, vocals16, 0, opt.Lang, "", func(p float64) { t.progress("recognize", p, "") })
	if err != nil {
		t.end("recognize", "error", err.Error())
		return nil, err
	}
	t.end("recognize", "done", fmt.Sprintf("%d palabras reconocidas · %s", len(rec), w.Device))
	if dbg := os.Getenv("AUDIOVIZ_DEBUG_REC"); dbg != "" {
		if f, err := os.Create(dbg); err == nil {
			for _, x := range rec {
				fmt.Fprintf(f, "%.2f\t%.2f\t%.2f\t%.2f\t%s\n", x.Start, x.T0, x.DTW, x.Prob, x.Text)
			}
			f.Close()
		}
	}

	// 5. Alinear con la letra (+ reparación de las líneas mal reconocidas)
	t.start("align", "Alineando…")
	lw := lyricWords(lines)
	nrec := len(rec)
	rec = dropSilent(rec, v)
	dropped := nrec - len(rec)
	alignNW(lw, rec)
	cov := lineCoverage(lw, len(lines))
	type group struct{ a, b int } // líneas [a, b)
	var groups []group
	for i := 0; i < len(lines); i++ {
		if cov[i] >= 0.5 {
			continue
		}
		j := i
		for j < len(lines) && cov[j] < 0.5 {
			j++
		}
		groups = append(groups, group{i, j})
		i = j
	}
	fillUnanchored(lw, v, total)
	repaired := 0
	for gi, g := range groups {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		t.progress("align", float64(gi)/float64(len(groups)), fmt.Sprintf("Revisando %d de %d tramos dudosos", gi+1, len(groups)))
		// Ventana: desde el final de la línea buena anterior hasta el inicio de la siguiente
		var wb []int
		for k := range lw {
			if lw[k].Line >= g.a && lw[k].Line < g.b {
				wb = append(wb, k)
			}
		}
		if len(wb) == 0 {
			continue
		}
		a, b := 0.0, total
		if first := wb[0]; first > 0 {
			p := lw[first-1]
			a = p.Start + math.Min(p.End-p.Start, 1.5)
		}
		if last := wb[len(wb)-1]; last+1 < len(lw) {
			b = lw[last+1].Start
		}
		a, b = v.trim(a, b)
		if b-a < 0.6 {
			continue
		}
		var prompt []string
		for li := g.a; li < g.b; li++ {
			prompt = append(prompt, lines[li])
		}
		seg := vocals16[int(a*16000):min(len(vocals16), int(b*16000))]
		rec2, err := w.Transcribe(ctx, seg, a, opt.Lang, strings.Join(prompt, " "), nil)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		rec2 = dropSilent(rec2, v)
		sub := make([]LWord, len(wb))
		for k, idx := range wb {
			sub[k] = lw[idx]
			sub[k].anchored = false
		}
		if alignNW(sub, rec2) > 0 {
			for k, idx := range wb {
				if sub[k].anchored {
					lw[idx] = sub[k]
				}
			}
			repaired++
		}
	}
	// Recolocar las palabras que siguen sin ancla con los anclajes definitivos
	for k := range lw {
		if !lw[k].anchored {
			lw[k].Start, lw[k].End = 0, 0
		}
	}
	fillUnanchored(lw, v, total)
	out := finish(lw, v, total)
	anchored := 0
	for _, x := range lw {
		if x.anchored {
			anchored++
		}
	}
	t.end("align", "done", fmt.Sprintf("%d líneas · %d de %d palabras reconocidas (%.0f%%) · %d tramos revisados · %d palabras descartadas en silencio",
		len(out), anchored, len(lw), 100*float64(anchored)/float64(len(lw)), repaired, dropped))
	return &Result{Lines: out, LRC: LRC(out), Device: w.Device}, nil
}
