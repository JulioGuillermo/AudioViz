package lyricsgen

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------- lyrics

var (
	reBracketLine = regexp.MustCompile(`^\[[^\]]*\]$`)
	reParenLine   = regexp.MustCompile(`^\([^)]*\)$`)
	reBracket     = regexp.MustCompile(`\[[^\]]*\]`)
)

// CleanLyrics removes Suno/Udio-style tags ([Verse], [Soft piano...], (whispered)) and empty lines
func CleanLyrics(text string) []string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || reBracketLine.MatchString(s) || reParenLine.MatchString(s) {
			continue
		}
		s = strings.TrimSpace(reBracket.ReplaceAllString(s, ""))
		if s != "" {
			out = append(out, strings.Join(strings.Fields(s), " "))
		}
	}
	return out
}

func norm(w string) string {
	w = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(w, "’", "'"), "‘", "'"))
	var b strings.Builder
	for _, r := range w {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "'")
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// similarity 0..1 between two normalized words
func similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	return 1 - float64(levenshtein(ra, rb))/float64(max(len(ra), len(rb)))
}

// ---------------------------------------------------------------- voice (energy)

// voice describes at which instants there is voice (20 ms frames)
type voice struct {
	on    []bool
	rms   []float64
	thr   float64
	frame float64
}

func newVoice(mono16 []float32) *voice {
	const fr = 320 // 20 ms at 16 kHz
	n := len(mono16) / fr
	rms := make([]float64, n)
	for i := 0; i < n; i++ {
		var s float64
		for _, x := range mono16[i*fr : (i+1)*fr] {
			s += float64(x) * float64(x)
		}
		rms[i] = math.Sqrt(s / fr)
	}
	sorted := append([]float64(nil), rms...)
	sort.Float64s(sorted)
	p95 := 0.0
	if len(sorted) > 0 {
		p95 = sorted[int(float64(len(sorted)-1)*0.95)]
	}
	thr := math.Max(0.06*p95, 1e-5)
	v := &voice{on: make([]bool, n), rms: rms, thr: thr, frame: 0.02}
	for i, r := range rms {
		v.on[i] = r > thr
	}
	return v
}

func (v *voice) idx(t float64) int { return max(0, min(len(v.on)-1, int(t/v.frame))) }

// voicedSpans returns the voiced spans within [a, b]
func (v *voice) voicedSpans(a, b float64) [][2]float64 {
	var out [][2]float64
	i0, i1 := v.idx(a), v.idx(b)
	start := -1
	for i := i0; i <= i1; i++ {
		if v.on[i] && start < 0 {
			start = i
		}
		if (!v.on[i] || i == i1) && start >= 0 {
			end := i
			if v.on[i] {
				end = i + 1
			}
			out = append(out, [2]float64{math.Max(a, float64(start)*v.frame), math.Min(b, float64(end)*v.frame)})
			start = -1
		}
	}
	return out
}

// decayEnd: where a sung word ends. The level is compared with the word's own peak (in its
// first `peakWin` seconds): the word ends when it stays below a quarter of it (-12 dB) for
// 0.1 s. Unlike a silence search over the global (very low) voice threshold, it is not fooled
// by reverb tails or backing vocals, which stay above that threshold long after the word is over.
func (v *voice) decayEnd(t, peakWin, limit float64) float64 {
	i0, i1 := v.idx(t), v.idx(limit)
	pk, peak := i0, 0.0
	for i := i0; i <= min(i1, v.idx(t+peakWin)); i++ {
		if v.rms[i] > peak {
			pk, peak = i, v.rms[i]
		}
	}
	thr := math.Max(0.25*peak, v.thr)
	run := 0
	for i := pk; i < i1; i++ {
		if v.rms[i] < thr {
			run++
			if run >= 5 {
				return float64(i-run+1) * v.frame
			}
		} else {
			run = 0
		}
	}
	return limit
}

// voicedFrac: fraction of [a, b] that is voiced
func (v *voice) voicedFrac(a, b float64) float64 {
	i0, i1 := v.idx(a), v.idx(b)
	n, on := 0, 0
	for i := i0; i <= i1; i++ {
		n++
		if v.on[i] {
			on++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(on) / float64(n)
}

// dropSilent removes recognized words where the voice is silent: Whisper tends to
// "hallucinate" lyrics during instrumentals
func dropSilent(rec []Word, v *voice) []Word {
	out := rec[:0:0]
	for _, w := range rec {
		if v.voicedFrac(w.Start-0.1, w.Start+0.6) >= 0.15 {
			out = append(out, w)
		}
	}
	return out
}

// trim trims [a, b] to the first and last voiced instants
func (v *voice) trim(a, b float64) (float64, float64) {
	sp := v.voicedSpans(a, b)
	if len(sp) == 0 {
		return a, b
	}
	return math.Max(a, sp[0][0]-0.25), math.Min(b, sp[len(sp)-1][1]+0.25)
}

// ---------------------------------------------------------------- alignment

// LWord is a lyrics word with its timing
type LWord struct {
	Text       string
	Line       int
	Start, End float64
	anchored   bool
	n          string
}

// LLine is an already synchronized lyrics line
type LLine struct {
	Words []LWord
}

func lyricWords(lines []string) []LWord {
	var out []LWord
	for li, l := range lines {
		for _, w := range strings.Fields(l) {
			out = append(out, LWord{Text: w, Line: li, n: norm(w)})
		}
	}
	return out
}

// alignNW aligns (Needleman-Wunsch) the lyrics words lw with the recognized ones rec and
// sets the start of those that match. Returns how many were anchored.
func alignNW(lw []LWord, rec []Word) int {
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
	// dp[i][j]: best score aligning lw[:i] with rec[:j]
	dp := make([][]float64, n+1)
	bt := make([][]uint8, n+1) // 0 diag, 1 up (skip lyrics word), 2 left (skip recognized word)
	for i := range dp {
		dp[i] = make([]float64, m+1)
		bt[i] = make([]uint8, m+1)
	}
	for i := 1; i <= n; i++ {
		dp[i][0], bt[i][0] = float64(i)*gap, 1
	}
	for j := 1; j <= m; j++ {
		dp[0][j], bt[0][j] = float64(j)*gap, 2
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			d := dp[i-1][j-1] + score(i-1, j-1)
			u := dp[i-1][j] + gap
			l := dp[i][j-1] + gap
			// On a tie, prefer skipping the recognized word: this way a repeated line is
			// matched to its earliest occurrence (not to an echo or hallucination at the end)
			switch {
			case l >= d && l >= u:
				dp[i][j], bt[i][j] = l, 2
			case d >= u:
				dp[i][j], bt[i][j] = d, 0
			default:
				dp[i][j], bt[i][j] = u, 1
			}
		}
	}
	anchored := 0
	for i, j := n, m; i > 0 && j > 0; {
		switch bt[i][j] {
		case 0:
			if similarity(lw[i-1].n, rn[j-1]) >= 0.5 {
				lw[i-1].Start = rec[j-1].Start
				lw[i-1].End = rec[j-1].End
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

// fillUnanchored distributes unanchored words between their neighbors, only over voiced spans
func fillUnanchored(lw []LWord, v *voice, total float64) {
	i := 0
	for i < len(lw) {
		if lw[i].anchored {
			i++
			continue
		}
		j := i
		for j < len(lw) && !lw[j].anchored {
			j++
		}
		cnt := j - i
		var a, b float64
		switch {
		case i == 0 && j == len(lw):
			a, b = 0, total
		case i == 0:
			b = lw[j].Start
			a = math.Max(0, b-0.6*float64(cnt))
		case j == len(lw):
			a = math.Min(lw[i-1].Start+math.Min(lw[i-1].End-lw[i-1].Start, 1.0), total)
			b = math.Min(total, a+0.6*float64(cnt))
		default:
			a = lw[i-1].Start + math.Min(lw[i-1].End-lw[i-1].Start, 1.0)
			b = lw[j].Start
		}
		if b < a+0.05*float64(cnt) {
			b = a + 0.05*float64(cnt)
		}
		// Weight by length; distributed over voiced time (not over instrumentals)
		spans := v.voicedSpans(a, b)
		voiced := 0.0
		for _, s := range spans {
			voiced += s[1] - s[0]
		}
		if voiced < 0.15*float64(cnt) {
			spans, voiced = [][2]float64{{a, b}}, b-a
		}
		weights := make([]float64, cnt)
		wsum := 0.0
		for k := 0; k < cnt; k++ {
			weights[k] = float64(len([]rune(lw[i+k].Text)) + 1)
			wsum += weights[k]
		}
		at := func(x float64) float64 { // x in [0, voiced] -> real instant
			for _, s := range spans {
				if x <= s[1]-s[0] {
					return s[0] + x
				}
				x -= s[1] - s[0]
			}
			return spans[len(spans)-1][1]
		}
		acc := 0.0
		for k := 0; k < cnt; k++ {
			lw[i+k].Start = at(acc / wsum * voiced)
			acc += weights[k]
			lw[i+k].End = at(acc / wsum * voiced)
		}
		i = j
	}
}

// lineCoverage: fraction of anchored words per line
func lineCoverage(lw []LWord, nlines int) []float64 {
	tot := make([]int, nlines)
	anc := make([]int, nlines)
	for _, w := range lw {
		tot[w.Line]++
		if w.anchored {
			anc[w.Line]++
		}
	}
	cov := make([]float64, nlines)
	for i := range cov {
		if tot[i] > 0 {
			cov[i] = float64(anc[i]) / float64(tot[i])
		}
	}
	return cov
}

// finish sets the end times (the last word of each line from its level, see decayEnd) and
// guarantees increasing times
func finish(lw []LWord, v *voice, total float64) []LLine {
	for i := range lw {
		if i > 0 && lw[i].Start < lw[i-1].Start+0.02 {
			lw[i].Start = lw[i-1].Start + 0.02
		}
	}
	for i := range lw {
		next := total
		if i+1 < len(lw) {
			next = lw[i+1].Start
		}
		if i+1 < len(lw) && lw[i+1].Line == lw[i].Line {
			end := math.Min(next, lw[i].Start+2.0)
			// A real pause before the next word: end where the voice fades, not at the next
			// word (so the karaoke fill doesn't run through the silence)
			if e := v.decayEnd(lw[i].Start, math.Min(0.5, end-lw[i].Start), end); end-e >= 0.25 {
				end = math.Max(e, lw[i].Start+0.12)
			}
			lw[i].End = end
		} else {
			// Last word of the line: until its level fades (held notes included), and never
			// past the next word Whisper heard (that is what a recognized word's End holds)
			limit := math.Min(next, lw[i].Start+3.0)
			if lw[i].anchored && lw[i].End > lw[i].Start+0.1 {
				limit = math.Min(limit, lw[i].End)
			}
			e := v.decayEnd(lw[i].Start, 0.5, limit)
			lw[i].End = math.Max(e, math.Min(lw[i].Start+0.25, next))
		}
	}
	var lines []LLine
	for _, w := range lw {
		if len(lines) == 0 || lines[len(lines)-1].Words[0].Line != w.Line {
			lines = append(lines, LLine{})
		}
		lines[len(lines)-1].Words = append(lines[len(lines)-1].Words, w)
	}
	return lines
}

// LRC generates the .lrc with per-word timing
func LRC(lines []LLine) string {
	ts := func(t float64) string {
		t = math.Max(t, 0)
		m := int(t / 60)
		return fmt.Sprintf("%02d:%05.2f", m, t-float64(m)*60)
	}
	var b strings.Builder
	b.WriteString("[re:audioviz]\n")
	for _, l := range lines {
		if len(l.Words) == 0 {
			continue
		}
		fmt.Fprintf(&b, "[%s]", ts(l.Words[0].Start))
		for i, w := range l.Words {
			if i > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "<%s>%s", ts(w.Start), w.Text)
			// A pause before the next word: an empty tag marks where this one ends
			if i+1 < len(l.Words) && l.Words[i+1].Start-w.End >= 0.05 {
				fmt.Fprintf(&b, " <%s>", ts(w.End))
			}
		}
		fmt.Fprintf(&b, " <%s>\n", ts(l.Words[len(l.Words)-1].End))
	}
	return b.String()
}
