package lyricsgen

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
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

// Resync recognizes again only the audio around the target lines (without a prompt, see
// below) and re-aligns their words. The rest of the lyric is left untouched. Uses the cached
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
		var sub, orig []LWord
		for li := a; li <= b; li++ {
			for _, wd := range lines[li].Words {
				sub = append(sub, LWord{Text: wd.Text, Line: li, n: norm(wd.Text)})
				orig = append(orig, wd)
			}
		}
		// No prompt: Whisper takes it as what was said *before* the window and skips it
		// (the target text makes it skip the line; the previous line, whose audio may be
		// inside the window, gets its time lumped into the first word). Everything in the
		// window is transcribed and alignFit picks the right run.
		seg := vocals16[int(lo*16000):min(len(vocals16), int(hi*16000))]
		rec, err := w.Transcribe(ctx, seg, lo, opt.Lang, "", nil)
		if err != nil {
			return nil, "", err
		}
		rec = dropSilent(rec, v)
		// The window also holds words of the neighbouring lines: the target words must
		// match one compact run of recognized words (the one closest to where they are now)
		n := alignFit(sub, rec, (start+end)/2)
		if dbg := os.Getenv("AUDIOVIZ_DEBUG_REC"); dbg != "" {
			if f, err := os.OpenFile(dbg, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintf(f, "lines %d-%d window %.2f-%.2f anchored %d/%d\n", a, b, lo, hi, n, len(sub))
				for _, x := range rec {
					fmt.Fprintf(f, "  %.2f\t%.2f\t%s\n", x.Start, x.End, x.Text)
				}
				f.Close()
			}
		}
		if n < min(len(sub), max(2, int(math.Ceil(0.4*float64(len(sub)))))) {
			continue // too little recognized: better to leave the line as it was
		}
		placeEdges(sub, orig, lo, hi)
		fillUnanchored(sub, v, hi)
		// The last word may last until the voice fades, but not into the next line
		limit := hi
		if b+1 < len(lines) && len(lines[b+1].Words) > 0 {
			if ns := lines[b+1].Words[0].Start; ns > sub[len(sub)-1].Start {
				limit = math.Min(limit, ns)
			}
		}
		for _, l := range finish(sub, v, limit) {
			lines[l.Words[0].Line] = l
		}
	}
	return lines, source, nil
}

// alignFit aligns all the lyric words lw with a compact run of the recognized words rec
// (semi-global alignment: recognized words before and after the run are free, skipping
// one inside it costs like any gap). This keeps a line from stretching over common
// words ("I", "the", "my") of the neighbouring lines. Among equally good runs, the one
// closest to the time `near` wins. Returns how many words were anchored.
func alignFit(lw []LWord, rec []Word, near float64) int {
	n, m := len(lw), len(rec)
	if n == 0 || m == 0 {
		return 0
	}
	rn := make([]string, m)
	for j := range rec {
		rn[j] = norm(rec[j].Text)
	}
	const gap = -1.0
	score := func(i, j int) float64 {
		s := similarity(lw[i].n, rn[j])
		switch {
		case s >= 0.8:
			return 3 * s
		case s >= 0.5:
			return 1
		}
		return -2
	}
	dp := make([][]float64, n+1)
	bt := make([][]uint8, n+1) // 0 diag, 1 up (skip lyric word), 2 left (skip recognized word)
	for i := range dp {
		dp[i] = make([]float64, m+1)
		bt[i] = make([]uint8, m+1)
	}
	for i := 1; i <= n; i++ {
		dp[i][0], bt[i][0] = float64(i)*gap, 1
	}
	// dp[0][j] = 0: recognized words before the run are free
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			d := dp[i-1][j-1] + score(i-1, j-1)
			u := dp[i-1][j] + gap
			l := dp[i][j-1] + gap
			switch {
			case d >= u && d >= l:
				dp[i][j], bt[i][j] = d, 0
			case u >= l:
				dp[i][j], bt[i][j] = u, 1
			default:
				dp[i][j], bt[i][j] = l, 2
			}
		}
	}
	// Recognized words after the run are free: best end, closest to `near` on a tie
	bj := 0
	for j := 1; j <= m; j++ {
		if bj == 0 || dp[n][j] > dp[n][bj]+1e-9 ||
			(math.Abs(dp[n][j]-dp[n][bj]) <= 1e-9 && math.Abs(rec[j-1].Start-near) < math.Abs(rec[bj-1].Start-near)) {
			bj = j
		}
	}
	anchored := 0
	for i, j := n, bj; i > 0 && j > 0; {
		switch bt[i][j] {
		case 0:
			if similarity(lw[i-1].n, rn[j-1]) >= 0.5 {
				lw[i-1].Start, lw[i-1].End = rec[j-1].Start, rec[j-1].End
				lw[i-1].anchored = true
				anchored++
			}
			i, j = i-1, j-1
		case 1:
			i--
		default:
			j--
		}
	}
	return anchored
}

// placeEdges places the unanchored words before the first anchored word and after the last
// one following the rhythm the line already had (gaps between word starts, capped), instead
// of spreading them up to the edges of the window. They are marked as anchored so that
// fillUnanchored only fills the gaps between recognized words.
func placeEdges(lw, orig []LWord, lo, hi float64) {
	fa, la := -1, -1
	for i := range lw {
		if lw[i].anchored {
			if fa < 0 {
				fa = i
			}
			la = i
		}
	}
	if fa < 0 {
		return
	}
	step := func(q int) float64 { // original gap between the starts of words q and q+1
		return math.Max(0.12, math.Min(0.8, orig[q+1].Start-orig[q].Start))
	}
	for k := fa - 1; k >= 0; k-- {
		lw[k].Start = math.Max(lo, lw[k+1].Start-step(k))
		lw[k].End = lw[k+1].Start
		lw[k].anchored = true
	}
	for k := la + 1; k < len(lw); k++ {
		lw[k].Start = math.Min(hi-0.05, lw[k-1].Start+step(k-1))
		lw[k].anchored = true
	}
}
