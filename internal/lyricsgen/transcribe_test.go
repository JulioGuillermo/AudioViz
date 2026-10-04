package lyricsgen

import (
	"fmt"
	"strings"
	"testing"
)

// testVoice: voiced exactly where the words are sung (20 ms frames)
func testVoice(rec []Word, total float64) *voice {
	n := int(total / 0.02)
	v := &voice{on: make([]bool, n), rms: make([]float64, n), thr: 0.01, frame: 0.02}
	for _, w := range rec {
		for i := v.idx(w.Start); i < v.idx(w.End); i++ {
			v.on[i], v.rms[i] = true, 1
		}
	}
	return v
}

// words: "text@start" in sequence, each sung for 0.3 s, all in whisper segment seg
func words(seg int, spec string) []Word {
	var out []Word
	for _, f := range strings.Fields(spec) {
		i := strings.LastIndex(f, "@")
		var s float64
		for _, c := range f[i+1:] {
			if c == '.' {
				continue
			}
			s = s*10 + float64(c-'0')
		}
		s /= 10 // tenths of a second
		out = append(out, Word{Text: f[:i], Start: s, End: s + 0.3, Seg: seg})
	}
	return out
}

func lineTexts(lw []LWord) []string {
	var out []string
	for _, w := range lw {
		if w.Line >= len(out) {
			out = append(out, "")
		}
		out[w.Line] = strings.TrimSpace(out[w.Line] + " " + w.Text)
	}
	return out
}

func TestLinesFromWords(t *testing.T) {
	rec := append(words(0, "I@10 didn't@13 ask.@16 ♪@19 I@30 was@33 born@36 with@39 fire,@42 in@45 my@48 hands@51"),
		words(1, "[Music]@60 but@70 no@73 one@76")...)
	got := lineTexts(linesFromWords(rec, testVoice(rec, 10)))
	want := []string{"I didn't ask.", "I was born with fire,", "in my hands", "but no one"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines\n got %q\nwant %q", got, want)
	}
}

// A long phrase without pauses or punctuation is still cut
func TestLinesFromWordsMaxLength(t *testing.T) {
	var spec []string
	for i := 0; i < 30; i++ {
		spec = append(spec, fmt.Sprintf("la@%d", 10+3*i))
	}
	rec := words(0, strings.Join(spec, " "))
	for _, l := range lineTexts(linesFromWords(rec, testVoice(rec, 20))) {
		if n := len(strings.Fields(l)); n > hardMax {
			t.Errorf("line of %d words: %q", n, l)
		}
	}
}

func TestCleanWord(t *testing.T) {
	for in, want := range map[string]string{"♪": "", "[Music]": "", "(applause)": "", "♪hello": "hello", "...": "", "don't": "don't"} {
		if got := cleanWord(in); got != want {
			t.Errorf("cleanWord(%q) = %q, want %q", in, got, want)
		}
	}
}
