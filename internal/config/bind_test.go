package config

import (
	"slices"
	"testing"
)

const bindYAML = `
audio: { path: "a.flac" }
background: { path: "a.png" }
timeline:
  background:
    - { at: 0, id: a }
    - { at: 10, id: b, path: "b.png", transition: { mode: fade, duration: 2 } }
    - { at: 20, path: "c.png" }
  glitch:
    - { at: 0, enabled: true, bind: [a, gone] }
layers:
  - name: Fire
    kind: emitter
    clips:
      - { start: 0, end: 30, bind: [b] }
`

func TestBind(t *testing.T) {
	c, err := ParseConfig([]byte(bindYAML), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	bg := c.Timeline.Background
	if bg[0].ID != "a" || bg[1].ID != "b" || bg[2].ID != "" {
		t.Fatalf("ids: %q %q %q", bg[0].ID, bg[1].ID, bg[2].ID)
	}
	g := c.Timeline.Glitch[0].Bind
	if !slices.Equal(g, []string{"a", "gone"}) {
		t.Fatalf("glitch bind: %v", g)
	}
	if b := c.Layers[0].Clips[0].Bind; !slices.Equal(b, []string{"b"}) {
		t.Fatalf("clip bind: %v", b)
	}
	// where it shows, over time: only a → only b (with the fade between them) → neither
	side := func(at float64, ids ...string) int {
		b := c.Timeline.BgBindAt(at)
		return b.BindSide(ids)
	}
	cases := []struct {
		at   float64
		ids  []string
		want int
	}{
		{5, nil, 0},            // untied: everywhere
		{5, []string{"a"}, 0},  // its segment on screen
		{5, []string{"b"}, -1}, // not yet
		{11, []string{"a"}, 1}, // the fade from a to b: only on a
		{11, []string{"b"}, 2}, // only on b
		{11, []string{"a", "b"}, 0},
		{13, []string{"a"}, -1}, // b has fully entered
		{13, []string{"b"}, 0},
		{25, []string{"b"}, -1},   // c has no id: nothing tied to it
		{25, []string{"gone"}, 0}, // ids no longer there: as if untied
		{25, []string{"a", "gone"}, -1},
	}
	for _, k := range cases {
		if got := side(k.at, k.ids...); got != k.want {
			t.Errorf("at %v %v: %d, want %d", k.at, k.ids, got, k.want)
		}
	}
	// they survive saving
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := ParseConfig(out, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Timeline.Background[0].ID != "a" || c2.Timeline.Background[1].ID != "b" ||
		!slices.Equal(c2.Timeline.Glitch[0].Bind, g) || !slices.Equal(c2.Layers[0].Clips[0].Bind, []string{"b"}) {
		t.Fatalf("not kept on save:\n%s", out)
	}
}
