package lyricsgen

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ResyncOptions re-aligns only some lines of an already synced lyric
type ResyncOptions struct {
	Stereo     []float32
	SampleRate int
	AudioKey   string
	Lang       string
	Lines      []LLine // current lyric (all lines, in order)
	Targets    []int   // indices of the lines to re-sync
}

// Resync recognizes again only the audio around the target lines (using the lyric text as a
// hint) and re-aligns their words. The rest of the lyric is left untouched. Uses the cached
// separated vocals when available, otherwise the full mix.
func Resync(ctx context.Context, opt ResyncOptions) ([]LLine, string, error) {
	if len(opt.Targets) == 0 {
		return opt.Lines, "", nil
	}
	if opt.Lang == "" {
		opt.Lang = "en"
	}
	if _, err := WhisperModel.Ensure(ctx, nil); err != nil {
		return nil, "", err
	}
	source := "separated vocals (cache)"
	vocals16 := loadF32(vocalsCachePath(fmt.Sprintf("%s|%d|%d", opt.AudioKey, opt.SampleRate, len(opt.Stereo))))
	if vocals16 == nil {
		source = "full mix (no separated vocals cached)"
		vocals16 = ToMono(Resample(opt.Stereo, 2, opt.SampleRate, 16000))
	}
	total := float64(len(vocals16)) / 16000
	v := newVoice(vocals16)
	w, err := OpenWhisper(WhisperModel.Path(), aheadsLargeV3Turbo, true)
	if err != nil {
		return nil, "", err
	}
	defer w.Close()

	lines := make([]LLine, len(opt.Lines))
	for i, l := range opt.Lines {
		lines[i] = LLine{Words: append([]LWord(nil), l.Words...)}
	}
	targets := append([]int(nil), opt.Targets...)
	sort.Ints(targets)

	// Process each group of consecutive target lines separately
	for gi := 0; gi < len(targets); {
		gj := gi
		for gj+1 < len(targets) && targets[gj+1] == targets[gj]+1 {
			gj++
		}
		a, b := targets[gi], targets[gj]
		gi = gj + 1
		if a < 0 || b >= len(lines) {
			continue
		}
		first, last := lines[a].Words, lines[b].Words
		if len(first) == 0 || len(last) == 0 {
			continue
		}
		// Window: around the current position (±5 s, so a line that is a few seconds
		// off is still found), without crossing the neighbouring lines. A neighbour
		// that overlaps the target (e.g. a line dragged on top of it) does not limit it.
		const margin = 5.0
		start, end := first[0].Start, last[len(last)-1].End
		lo := math.Max(0, start-margin)
		hi := math.Min(total, end+margin)
		if a > 0 && len(lines[a-1].Words) > 0 {
			p := lines[a-1].Words
			if pe := p[len(p)-1].End; pe <= start {
				lo = math.Max(lo, pe)
			}
		}
		if b+1 < len(lines) && len(lines[b+1].Words) > 0 {
			if ns := lines[b+1].Words[0].Start; ns >= end {
				hi = math.Min(hi, ns)
			}
		}
		if hi-lo < 0.4 {
			continue
		}
		var prompt []string
		var sub []LWord
		for li := a; li <= b; li++ {
			var txt []string
			for _, wd := range lines[li].Words {
				txt = append(txt, wd.Text)
				sub = append(sub, LWord{Text: wd.Text, Line: li, n: norm(wd.Text)})
			}
			prompt = append(prompt, strings.Join(txt, " "))
		}
		seg := vocals16[int(lo*16000):min(len(vocals16), int(hi*16000))]
		rec, err := w.Transcribe(ctx, seg, lo, opt.Lang, strings.Join(prompt, " "), nil)
		if err != nil {
			return nil, "", err
		}
		rec = dropSilent(rec, v)
		alignNW(sub, rec)
		// Unanchored words are spread inside the window (virtual anchors at its edges)
		withEdges := append([]LWord{{Start: lo, End: lo, anchored: true, Line: -1}}, sub...)
		withEdges = append(withEdges, LWord{Start: hi, End: hi, anchored: true, Line: -2})
		fillUnanchored(withEdges, v, hi)
		sub = withEdges[1 : len(withEdges)-1]
		for _, l := range finish(sub, v, hi) {
			lines[l.Words[0].Line] = l
		}
	}
	return lines, source, nil
}
