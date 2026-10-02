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
    - { at: 86.4, label: "Estribillo" }
  visualizer:
    - at: 90
      label: "Estribillo"
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
		t.Fatalf("tramos mal ordenados: %+v", v)
	}
	// El tramo de 30s hereda el tipo; el de 90 hereda las 64 barras y cambia el tipo
	if v[1].Cfg.Type != "horizontal" || v[1].Cfg.Bars != 64 {
		t.Fatalf("tramo 30: %+v", v[1].Cfg)
	}
	if v[2].Cfg.Type != "circular" || v[2].Cfg.Bars != 64 || len(v[2].Cfg.Gradient) != 2 {
		t.Fatalf("tramo 90: %+v", v[2].Cfg)
	}
	if v[1].Transition.Mode != "cut" || v[2].Transition.Mode != "fade" {
		t.Fatalf("transiciones: %+v %+v", v[1].Transition, v[2].Transition)
	}
	bg := c.Timeline.Background
	if bg[0].Cfg.Path != "/proj/bg1.png" || bg[1].Cfg.Path != "/proj/sub/bg2.png" || bg[1].Cfg.Darken != 0.2 {
		t.Fatalf("fondos: %+v", bg)
	}
	if prev, cur, k := Blend(v, 91); prev.Type != "horizontal" || cur.Type != "circular" || k != 0.5 {
		t.Fatalf("blend: %v %v %v", prev.Type, cur.Type, k)
	}
	if len(c.Markers) != 1 || c.Markers[0].Label != "Estribillo" {
		t.Fatalf("marcas: %+v", c.Markers)
	}

	// Ida y vuelta: guardar y volver a cargar debe dar lo mismo, con rutas relativas
	out, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "sub/bg2.png") || strings.Contains(string(out), "/proj/") {
		t.Fatalf("rutas no relativas:\n%s", out)
	}
	c2, err := ParseConfig(out, "/proj")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(c2.Timeline.Visualizer) != 3 || c2.Timeline.Visualizer[2].Cfg.Type != "circular" ||
		c2.Timeline.Background[1].Cfg.Path != "/proj/sub/bg2.png" || len(c2.Markers) != 1 {
		t.Fatalf("ida y vuelta:\n%s", out)
	}
}
