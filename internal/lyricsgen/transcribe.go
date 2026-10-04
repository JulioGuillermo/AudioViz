package lyricsgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Transcription mode (no lyrics text): the words Whisper recognized become the lyrics, grouped
// into lines. The user then corrects the text in the studio, which can align the corrected text
// again (Generate with the text, reusing the cached recognition).

// Line breaking (seconds / words)
const (
	pauseBreak  = 0.6 // a pause this long in the voice always starts a new line
	phrasePause = 0.25
	minWords    = 3  // whisper phrase breaks are followed from this many words on
	commaWords  = 4  // a comma ends the line from this many words on (lyric lines often end in one)
	softMax     = 10 // from here on any phrase break or short pause ends the line
	hardMax     = 14 // a line never gets longer
)

// Non-lyric tokens Whisper writes for music: ♪, [Music], (instrumental), *applause*…
var nonLyric = regexp.MustCompile(`^[\[(*].*[\])*]$`)

// cleanWord trims symbols that are not part of the sung text; "" = drop the word
func cleanWord(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '♪', '♫', '♬', '♩', '🎵', '🎶':
			return -1
		}
		return r
	}, s))
	if s == "" || nonLyric.MatchString(s) {
		return ""
	}
	if !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return ""
	}
	return s
}

func endsPhrase(s string) bool {
	return strings.ContainsAny(s[len(s)-1:], ".!?…;")
}

// linesFromWords groups recognized words into lyric lines: a new line starts at a clear pause in
// the voice, at a Whisper phrase break once the line has a few words, after sentence punctuation,
// or when the line gets long. Start times come from the recognition (anchored); finish() then
// sets the ends from the voice.
func linesFromWords(rec []Word, v *voice) []LWord {
	var out []LWord
	line, count := 0, 0
	prevSeg := -1
	for _, w := range rec {
		txt := cleanWord(w.Text)
		if txt == "" {
			continue
		}
		if len(out) > 0 {
			p := out[len(out)-1]
			// Silence between the previous word (where its voice fades) and this one
			gap := w.Start - v.decayEnd(p.Start, 0.5, w.Start)
			brk := gap >= pauseBreak ||
				(w.Seg != prevSeg && count >= minWords) ||
				(endsPhrase(p.Text) && count >= minWords) ||
				(strings.HasSuffix(p.Text, ",") && count >= commaWords) ||
				(count >= softMax && (w.Seg != prevSeg || gap >= phrasePause || strings.HasSuffix(p.Text, ","))) ||
				count >= hardMax
			if brk {
				line++
				count = 0
			}
		}
		out = append(out, LWord{Text: txt, Line: line, Start: w.Start, End: w.End, anchored: true, n: norm(txt)})
		count++
		prevSeg = w.Seg
	}
	return out
}

// ---------------------------------------------------------------- recognition cache

// recCachePath: the full-song recognition of some vocals with a language (and with or without
// separation), next to the separated-vocals cache
func recCachePath(vocalsPath, lang string, separate bool) string {
	if vocalsPath == "" {
		return ""
	}
	mode := "mix"
	if separate {
		mode = "vocals"
	}
	return strings.TrimSuffix(vocalsPath, filepath.Ext(vocalsPath)) + "." + lang + "." + mode + ".rec.json"
}

func loadRec(path string) []Word {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rec []Word
	if json.Unmarshal(b, &rec) != nil || len(rec) == 0 {
		return nil
	}
	return rec
}

func saveRec(path string, rec []Word) {
	if path == "" || len(rec) == 0 {
		return
	}
	if b, err := json.Marshal(rec); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, b, 0o644)
	}
}
