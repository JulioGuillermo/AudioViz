package lyricsgen

import (
	"strings"
	"testing"
)

// The writer marks the end of a word followed by a pause, and only then
func TestLRCWritesPauses(t *testing.T) {
	out := LRC([]LLine{{Words: []LWord{
		{Text: "hello", Start: 10, End: 10.5},
		{Text: "big", Start: 11.2, End: 11.6},
		{Text: "world", Start: 11.6, End: 12.3},
	}}})
	want := "[00:10.00]<00:10.00>hello <00:10.50> <00:11.20>big <00:11.60>world <00:12.30>\n"
	if !strings.Contains(out, want) {
		t.Errorf("got\n%s\nwant line\n%s", out, want)
	}
}
