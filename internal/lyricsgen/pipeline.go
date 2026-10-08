// Package lyricsgen generates synchronized lyrics (per-word timing) from the audio and the
// plain-text lyrics, all inside the binary: Demucs (vocal separation) + Whisper (recognition)
// + alignment with the known lyrics. Models are downloaded once to ~/.cache/audioviz.
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
	"unsafe"

	"audioviz/internal/audio"
	"audioviz/internal/progress"
)

// Step and Status: the progress reported to the UI (see the progress package)
type (
	Step   = progress.Step
	Status = progress.Status
)

type Options struct {
	Stereo     []float32 // interleaved stereo audio
	SampleRate int
	AudioKey   string // identifies the audio for the separated-vocals cache (e.g. path)
	Lyrics     string // plain-text lyrics (Suno format is accepted); empty = transcribe the audio
	Lang       string
	Separate   bool // separate the vocals before recognizing (more accurate, slower)
	// The song's separated parts (audio.StemsDir): their vocals are used if they are there;
	// otherwise the four parts are separated (the same work) and kept there, in full quality
	// (StemsFull) or as mono for their analysis
	StemsDir  string
	StemsFull bool
}

type Result struct {
	Lines  []LLine
	LRC    string
	Device string
}

// Text is the lyrics as plain text, one line per row
func (r *Result) Text() string {
	var b strings.Builder
	for _, l := range r.Lines {
		for i, w := range l.Words {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(w.Text)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Weight of each step in the global progress (roughly proportional to its duration)
var stepWeights = map[string]float64{"models": 0.05, "prepare": 0.03, "separate": 0.62, "recognize": 0.22, "align": 0.08, "lines": 0.08}

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

// Generate runs the whole process
func Generate(ctx context.Context, opt Options, report func(Status)) (*Result, error) {
	// Without lyrics text the recognized words themselves become the lyrics (transcription)
	transcribe := strings.TrimSpace(opt.Lyrics) == ""
	var lines []string
	if !transcribe {
		if lines = CleanLyrics(opt.Lyrics); len(lines) == 0 {
			return nil, fmt.Errorf("the lyrics are empty (after removing the [ ] tags)")
		}
	}
	if opt.Lang == "" {
		opt.Lang = "en"
	}
	last := Step{ID: "align", Label: "Align with the lyrics"}
	if transcribe {
		last = Step{ID: "lines", Label: "Build the lines"}
	}
	t := progress.NewTracker([]Step{
		{ID: "models", Label: "Models"},
		{ID: "prepare", Label: "Prepare audio"},
		{ID: "separate", Label: "Separate the vocals"},
		{ID: "recognize", Label: "Recognize the vocals"},
		last,
	}, stepWeights, report)
	cachePath := vocalsCachePath(fmt.Sprintf("%s|%d|%d", opt.AudioKey, opt.SampleRate, len(opt.Stereo)))
	var vocals16 []float32
	if opt.Separate && cachePath != "" {
		vocals16 = loadF32(cachePath)
	}
	fromStems := false
	if opt.Separate && vocals16 == nil && opt.StemsDir != "" && audio.StemsReady(opt.StemsDir) {
		// the song is already separated: its vocals
		if v, err := audio.LoadStem(filepath.Join(opt.StemsDir, "vocals.wav")); err == nil {
			vocals16, fromStems = Resample(v, 1, audio.StemRate, 16000), true
		}
	}

	// 1. Models (downloaded the first time)
	t.Start("models", "Checking…")
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
			d := fmt.Sprintf("%s · %s of %s", m.Label, mb(done), mb(total))
			if rate > 0 {
				d += fmt.Sprintf(" · %.1f MB/s", rate/1e6)
			}
			t.Progress("models", float64(base+done)/float64(missing), d)
		})
		if err != nil {
			t.End("models", "error", err.Error())
			return nil, err
		}
		got += m.Size
	}
	if missing == 0 {
		t.End("models", "done", "Already downloaded")
	} else {
		t.End("models", "done", "Downloaded ("+mb(missing)+")")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 2. Prepare audio (resampling)
	t.Start("prepare", "Resampling…")
	var stereo44 []float32
	mix16 := ToMono(Resample(opt.Stereo, 2, opt.SampleRate, 16000))
	if opt.Separate && vocals16 == nil {
		stereo44 = Resample(opt.Stereo, 2, opt.SampleRate, 44100)
	}
	t.End("prepare", "done", fmt.Sprintf("%.0f s of audio", float64(len(mix16))/16000))

	// 3. Separate the vocals
	switch {
	case !opt.Separate:
		t.End("separate", "skipped", "Fast mode: using the full mix")
		vocals16 = mix16
	case fromStems:
		t.End("separate", "skipped", "Reused from the separated song")
	case vocals16 != nil:
		t.End("separate", "skipped", "Reused from the cache")
	case opt.StemsDir != "":
		// the four parts (the same work as the vocals alone), kept for the effects
		t.Start("separate", "Demucs htdemucs · CPU · drums, bass, vocals and the rest")
		err := SeparateStemsTo(ctx, DemucsModel.Path(), stereo44, opt.StemsFull, func(p float64) { t.Progress("separate", p, "") },
			func(i int, x []float32) error {
				ch := 1
				if opt.StemsFull {
					ch = 2
				}
				if StemNames[i] == "vocals" {
					v := x
					if ch == 2 {
						v = ToMono(x)
					}
					vocals16 = Resample(v, 1, 44100, 16000)
				}
				return audio.SaveStem(filepath.Join(opt.StemsDir, StemNames[i]+".wav"), x, ch, 44100, opt.StemsFull)
			})
		if err != nil {
			t.End("separate", "error", err.Error())
			return nil, err
		}
		if cachePath != "" {
			saveF32(cachePath, vocals16)
		}
		t.End("separate", "done", "")
	default:
		t.Start("separate", "Demucs htdemucs · CPU")
		v44, err := SeparateVocals(ctx, DemucsModel.Path(), stereo44, func(p float64) { t.Progress("separate", p, "") })
		if err != nil {
			t.End("separate", "error", err.Error())
			return nil, err
		}
		vocals16 = ToMono(Resample(v44, 2, 44100, 16000))
		if cachePath != "" {
			saveF32(cachePath, vocals16)
		}
		t.End("separate", "done", "")
	}
	total := float64(len(vocals16)) / 16000
	v := newVoice(vocals16)

	// 4. Recognize (the result is cached with the vocals: correcting the text and aligning
	// again then takes seconds). Whisper is opened only when it is needed.
	var w *Whisper
	openW := func() (*Whisper, error) {
		if w != nil {
			return w, nil
		}
		ww, err := OpenWhisper(WhisperModel.Path(), aheadsLargeV3Turbo, true)
		if err != nil {
			return nil, err
		}
		w = ww
		t.SetDevice(w.Device)
		return w, nil
	}
	defer func() {
		if w != nil {
			w.Close()
		}
	}()
	recPath := recCachePath(cachePath, opt.Lang, opt.Separate)
	rec := loadRec(recPath)
	if rec != nil {
		t.End("recognize", "skipped", fmt.Sprintf("Reused from the cache · %d words", len(rec)))
	} else {
		t.Start("recognize", "Loading the model…")
		if _, err := openW(); err != nil {
			t.End("recognize", "error", err.Error())
			return nil, err
		}
		t.Progress("recognize", 0, "Whisper large-v3-turbo · "+w.Device)
		var err error
		rec, err = w.Transcribe(ctx, vocals16, 0, opt.Lang, "", func(p float64) { t.Progress("recognize", p, "") })
		if err != nil {
			t.End("recognize", "error", err.Error())
			return nil, err
		}
		saveRec(recPath, rec)
		t.End("recognize", "done", fmt.Sprintf("%d words recognized · %s", len(rec), w.Device))
	}
	device := "cache"
	if w != nil {
		device = w.Device
	}
	if dbg := os.Getenv("AUDIOVIZ_DEBUG_REC"); dbg != "" {
		if f, err := os.Create(dbg); err == nil {
			for _, x := range rec {
				fmt.Fprintf(f, "%.2f\t%.2f\t%.2f\t%.2f\t%s\n", x.Start, x.T0, x.DTW, x.Prob, x.Text)
			}
			f.Close()
		}
	}

	// 5a. No lyrics text: the recognized words, grouped into lines, are the lyrics
	if transcribe {
		t.Start("lines", "Grouping the words into lines…")
		nrec := len(rec)
		rec = dropSilent(rec, v)
		out := finish(linesFromWords(rec, v), v, total)
		nw := 0
		for _, l := range out {
			nw += len(l.Words)
		}
		t.End("lines", "done", fmt.Sprintf("%d lines · %d words · %d discarded in silence", len(out), nw, nrec-len(rec)))
		return &Result{Lines: out, LRC: LRC(out), Device: device}, nil
	}

	// 5. Align with the lyrics (+ repair of poorly recognized lines)
	t.Start("align", "Aligning…")
	lw := lyricWords(lines)
	nrec := len(rec)
	rec = dropSilent(rec, v)
	dropped := nrec - len(rec)
	alignNW(lw, rec)
	cov := lineCoverage(lw, len(lines))
	type group struct{ a, b int } // lines [a, b)
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
		t.Progress("align", float64(gi)/float64(len(groups)), fmt.Sprintf("Reviewing %d of %d doubtful sections", gi+1, len(groups)))
		// Window: from the end of the previous good line to the start of the next one
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
		if _, err := openW(); err != nil {
			return nil, err
		}
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
	// Reposition the still-unanchored words using the final anchors
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
	t.End("align", "done", fmt.Sprintf("%d lines · %d of %d words recognized (%.0f%%) · %d sections reviewed · %d words discarded in silence",
		len(out), anchored, len(lw), 100*float64(anchored)/float64(len(lw)), repaired, dropped))
	if w != nil {
		device = w.Device
	}
	return &Result{Lines: out, LRC: LRC(out), Device: device}, nil
}
