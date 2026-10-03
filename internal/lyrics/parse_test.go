package lyrics

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.006 }

// A word followed by a pause keeps its own end (empty tag); the line ends at the final tag
func TestLRCWordEndAtPause(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lrc")
	body := "[00:10.00]<00:10.00>hello <00:10.50> <00:11.20>big <00:11.60>world <00:12.30>\n" +
		"[00:20.00]<00:20.00>next <00:20.80>\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := Load(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	w := lines[0].Words
	if len(w) != 3 {
		t.Fatalf("words = %d (%+v), want 3", len(w), w)
	}
	want := [][2]float64{{10, 10.5}, {11.2, 11.6}, {11.6, 12.3}}
	for i, x := range want {
		if !near(w[i].Start, x[0]) || !near(w[i].End, x[1]) {
			t.Errorf("word %d %q = %.2f-%.2f, want %.2f-%.2f", i, w[i].Text, w[i].Start, w[i].End, x[0], x[1])
		}
	}
	if !near(lines[0].End, 12.3) {
		t.Errorf("line end = %.2f, want 12.30", lines[0].End)
	}
}
