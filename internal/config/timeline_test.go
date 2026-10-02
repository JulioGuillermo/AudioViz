package config

import (
	"strings"
	"testing"
)

const tlYAML = `
audio: { path: "a.flac" }
background: { path: "bg1.png", darken: 0.2 }
visualizer:
  type: horizontal
  bars: 48
  gradient:
    - { offset: 0, color: "#ff0000" }
    - { offset: 1, color: "#ffff00" }
timeline:
  markers:
    - { at: 86.4, label: "Chorus" }
  visualizer:
    - at: 90
      label: "Chorus"
      transition: { mode: fade, duration: 2 }
      type: circular
    - at: 30
      bars: 64
  background:
    - at: 60
      path: "sub/bg2.png"
      transition: { mode: fade, duration: 1 }
`

func TestTimeline(t *testing.T) {
	c, err := ParseConfig([]byte(tlYAML), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	v := c.Timeline.Visualizer
	if len(v) != 3 || v[1].At != 30 || v[2].At != 90 {
		t.Fatalf("segments out of order: %+v", v)
	}
	// The 30s segment inherits the type; the 90s one inherits the 64 bars and changes the type
	if v[1].Cfg.Type != "horizontal" || v[1].Cfg.Bars != 64 {
		t.Fatalf("segment 30: %+v", v[1].Cfg)
	}
	if v[2].Cfg.Type != "circular" || v[2].Cfg.Bars != 64 || len(v[2].Cfg.Gradient) != 2 {
		t.Fatalf("segment 90: %+v", v[2].Cfg)
	}
	if v[1].Transition.Mode != "cut" || v[2].Transition.Mode != "fade" {
		t.Fatalf("transitions: %+v %+v", v[1].Transition, v[2].Transition)
	}
	bg := c.Timeline.Background
	if bg[0].Cfg.Path != "/proj/bg1.png" || bg[1].Cfg.Path != "/proj/sub/bg2.png" || bg[1].Cfg.Darken != 0.2 {
		t.Fatalf("backgrounds: %+v", bg)
	}
	if prev, cur, k := Blend(v, 91); prev.Type != "horizontal" || cur.Type != "circular" || k != 0.5 {
		t.Fatalf("blend: %v %v %v", prev.Type, cur.Type, k)
	}
	if len(c.Markers) != 1 || c.Markers[0].Label != "Chorus" {
		t.Fatalf("markers: %+v", c.Markers)
	}

	// Round trip: saving and reloading must yield the same result, with relative paths
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "sub/bg2.png") || strings.Contains(string(out), "/proj/") {
		t.Fatalf("paths not relative:\n%s", out)
	}
	c2, err := ParseConfig(out, "/proj")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(c2.Timeline.Visualizer) != 3 || c2.Timeline.Visualizer[2].Cfg.Type != "circular" ||
		c2.Timeline.Background[1].Cfg.Path != "/proj/sub/bg2.png" || len(c2.Markers) != 1 {
		t.Fatalf("round trip:\n%s", out)
	}
}
