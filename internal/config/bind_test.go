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

// the segment of a tied effect that works on each image of a background transition (Picks)
func TestPicks(t *testing.T) {
	const y = `
audio: { path: "a.flac" }
background: { path: "a.png" }
timeline:
  background:
    - { at: 0, id: a }
    - { at: 10, id: b, path: "b.png", transition: { mode: fade, duration: 4 } }
  parallax:
    - { at: 0, enabled: true, bind: [a] }
    - { at: 8, enabled: true, bind: [a] }
    - { at: 12, enabled: true, bind: [b] }
  glitch:
    - { at: 0, enabled: true, bind: [a], hold: false }
    - { at: 11, enabled: true, bind: [b] }
  ripple:
    - { at: 0, enabled: true }
    - { at: 9, enabled: true, bind: [a] }
    - { at: 16, enabled: true, bind: [b] }
  shake:
    - { at: 0, enabled: true, bind: [a] }
    - { at: 10, enabled: true, bind: [b] }
`
	c, err := ParseConfig([]byte(y), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	tl := &c.Timeline
	if !tl.Glitch[0].NoHold || tl.Glitch[1].NoHold {
		t.Fatal("hold not read")
	}
	act := func(i int) Pick { return Pick{Mode: PickActive, Seg: i} }
	held := func(i int) Pick { return Pick{Mode: PickHeld, Seg: i} }
	off := Pick{}
	check := func(name string, at float64, got [3]Pick, want [3]Pick) {
		t.Helper()
		if got != want {
			t.Errorf("%s at %v: %v, want %v", name, at, got, want)
		}
	}
	p3 := func(a, b, c Pick) [3]Pick { return [3]Pick{a, b, c} }
	for _, k := range []struct {
		at        float64
		par, glit [3]Pick
		rip, shk  [3]Pick
	}{
		{5, p3(act(0), act(0), act(0)), p3(act(0), act(0), act(0)), p3(act(0), act(0), act(0)), p3(act(0), act(0), act(0))},
		// the transition (10–14): A keeps the one tied to it active at 10, B takes the one
		// active at 14 already; glitch on A is not held; ripple: B has none tied at 14, the
		// active one is tied to A
		// (shake: the next one starts with the transition, A keeps the one before)
		{11, p3(off, act(1), held(2)), p3(off, off, act(1)), p3(off, act(1), off), p3(off, held(0), act(1))},
		{13, p3(off, held(1), act(2)), p3(off, off, act(1)), p3(off, act(1), off), p3(off, held(0), act(1))},
		{15, p3(act(2), act(2), act(2)), p3(act(1), act(1), act(1)), p3(off, off, off), p3(act(1), act(1), act(1))},
	} {
		b := tl.BgBindAt(k.at)
		check("parallax", k.at, p3(Picks(tl.Parallax, k.at, &b)), k.par)
		check("glitch", k.at, p3(Picks(tl.Glitch, k.at, &b)), k.glit)
		check("ripple", k.at, p3(Picks(tl.Ripple, k.at, &b)), k.rip)
		check("shake", k.at, p3(Picks(tl.Shake, k.at, &b)), k.shk)
	}
	// hold survives saving
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := ParseConfig(out, "/proj")
	if err != nil || !c2.Timeline.Glitch[0].NoHold || c2.Timeline.Glitch[1].NoHold {
		t.Fatalf("hold not kept on save: %v\n%s", err, out)
	}
}
