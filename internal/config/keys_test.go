package config

import (
	"math"
	"strings"
	"testing"
)

const keysYAML = `
audio: { path: "a.flac" }
visualizer:
  type: circular
  radius: 200
  glow: { enabled: true, radius: 10, intensity: 0.5 }
  gradient:
    - { offset: 0, color: "#ff0000" }
    - { offset: 1, color: "#0000ff" }
timeline:
  visualizer:
    - at: 0
      label: "Intro"
      keys:
        radius: [{ t: 4, v: 300 }, { t: 0, v: 200, ease: ease_in }]
        glow.intensity: [{ t: 0, v: 0.5, ease: step }, { t: 2, v: 1 }]
        gradient.1.color: [{ t: 0, v: "#0000ff" }, { t: 2, v: "#00ff00" }]
        solid_color: [{ t: 0, v: "#000000" }, { t: 2, v: "#ffffff" }]
    - at: 10
      transition: { mode: fade, duration: 2 }
      radius: 100
      keys:
        radius: [{ t: 0, v: 100 }, { t: 2, v: 50 }]
`

func TestKeys(t *testing.T) {
	c, err := ParseConfig([]byte(keysYAML), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	v := c.Timeline.Visualizer
	if len(v) != 2 || v[0].Label != "Intro" || len(v[0].Keys) != 4 || v[0].Keys["radius"][0].T != 0 {
		t.Fatalf("segments / keys not parsed or not sorted: %+v", v)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	// Before the first key, at a key, between keys (ease_in = k³), after the last
	cases := []struct{ at, radius float64 }{{0, 200}, {2, 200 + 100*0.125}, {4, 300}, {9, 300}}
	for _, cs := range cases {
		if r := v[0].CfgAt(cs.at).Radius; !near(r, cs.radius) {
			t.Fatalf("radius at %v = %v, want %v", cs.at, r, cs.radius)
		}
	}
	// step holds the first value until the next key
	if g := v[0].CfgAt(1.99).Glow.Intensity; g != 0.5 {
		t.Fatalf("step: %v", g)
	}
	if g := v[0].CfgAt(2).Glow.Intensity; g != 1 {
		t.Fatalf("step at the key: %v", g)
	}
	// Colors, also inside a list
	if c := v[0].CfgAt(1).SolidColor; c != "#808080" {
		t.Fatalf("color: %v", c)
	}
	if c := v[0].CfgAt(1).Gradient[1].Color; c != "#008080" {
		t.Fatalf("gradient color: %v", c)
	}
	// The segment's own config is not touched (the list on the path is copied)
	if v[0].Cfg.Gradient[1].Color != "#0000ff" || v[0].Cfg.Radius != 200 || v[0].Cfg.Glow.Intensity != 0.5 {
		t.Fatalf("original config changed: %+v", v[0].Cfg)
	}
	// Keys of the second segment count from its start, and a fade mixes both animated values
	if r := v[1].CfgAt(11).Radius; !near(r, 75) {
		t.Fatalf("second segment: %v", r)
	}
	if prev, cur, k := Blend(v, 11); prev.Radius != 300 || !near(cur.Radius, 75) || k != 0.5 {
		t.Fatalf("blend: %v %v %v", prev.Radius, cur.Radius, k)
	}
	// Unknown paths are ignored
	ApplyKeys(&v[0].Cfg, Keys{"nope.x": {{T: 0, V: 1.0}}, "gradient.9.color": {{T: 0, V: "#fff"}}}, 0)

	// Round trip: the name and keys of the first segment survive (entry at 0)
	if err := c.SyncFromTimeline(); err != nil {
		t.Fatal(err)
	}
	b, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := ParseConfig(b, "/proj")
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	v2 := c2.Timeline.Visualizer
	if v2[0].Label != "Intro" || len(v2[0].Keys) != 4 || len(v2[1].Keys) != 1 || !strings.Contains(string(b), "ease: ease_in") {
		t.Fatalf("round trip lost keys:\n%s", b)
	}
	if r := v2[0].CfgAt(2).Radius; !near(r, 212.5) {
		t.Fatalf("round trip radius: %v", r)
	}
}

func TestEases(t *testing.T) {
	for _, e := range Eases {
		if a, b := Ease(e, 0), Ease(e, 1); math.Abs(a) > 1e-9 || (e != "step" && math.Abs(b-1) > 1e-9) {
			t.Fatalf("%s: f(0) = %v, f(1) = %v", e, a, b)
		}
	}
	// back anticipates (< 0 early) and overshoots (> 1 late); spring overshoots
	if Ease("back", 0.15) >= 0 || Ease("back", 0.85) <= 1 || Ease("spring", 0.22) <= 1.2 {
		t.Fatalf("back %v %v spring %v", Ease("back", 0.15), Ease("back", 0.85), Ease("spring", 0.22))
	}
}

func TestTextKeys(t *testing.T) {
	c, err := ParseConfig([]byte(`
audio: { path: "a.flac" }
texts:
  - text: "Hi"
    start: 2
    end: 6
    position_x: 0.5
    keys:
      position_x: [{ t: 0, v: 0.2 }, { t: 2, v: 0.8 }]
`), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	tc := &c.Texts[0]
	tc.Keys.Sort()
	if x := ApplyKeys(tc, tc.Keys, 1).PositionX; math.Abs(x-0.5) > 1e-9 {
		t.Fatalf("text position: %v", x)
	}
	b, _ := c.Marshal()
	if !strings.Contains(string(b), "position_x:") || !strings.Contains(string(b), "keys:") {
		t.Fatalf("text keys not saved:\n%s", b)
	}
}

// Mask shapes morph between keys with the easing; a node's type is not interpolated
func TestShapeKeys(t *testing.T) {
	pts := func(ps ...any) []any { return []any{map[string]any{"points": ps}} }
	a := pts([]any{0.0, 0.0}, []any{1.0, 0.0, 1}, []any{0.0, 1.0, 2, -0.1, 0.0, 0.1, 0.0})
	b := pts([]any{1.0, 1.0}, []any{0.0, 1.0}, []any{1.0, 0.0, 3, -0.3, 0.0, 0.3, 0.2})
	l := []Key{{T: 0, V: a, Ease: "ease_in_out"}, {T: 2, V: b}}
	got := ValueAt(l, 0.5).([]any)[0].(map[string]any)["points"].([]any)
	k := Ease("ease_in_out", 0.25)
	n0 := got[0].([]any)
	if math.Abs(n0[0].(float64)-k) > 1e-9 || math.Abs(n0[1].(float64)-k) > 1e-9 {
		t.Fatalf("smooth node: %v, want %v", n0, k)
	}
	if n1 := got[1].([]any); len(n1) != 3 || n1[2] != 1 { // corner until halfway
		t.Fatalf("corner node: %v", n1)
	}
	if n2 := got[2].([]any); n2[2] != 2 || math.Abs(n2[3].(float64)-(-0.1-0.2*k)) > 1e-9 {
		t.Fatalf("handles node: %v", n2)
	}
	if n1 := ValueAt(l, 1.5).([]any)[0].(map[string]any)["points"].([]any)[1].([]any); len(n1) != 2 {
		t.Fatalf("after halfway the node is smooth: %v", n1)
	}
}
