package config

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// Línea de tiempo: cada pista es una lista de tramos. El tramo 0 es el bloque de nivel
// superior del YAML (background, visualizer, ...); los siguientes vienen de `timeline:` y
// solo necesitan los campos que cambian (se aplican sobre el tramo anterior).
//
//	timeline:
//	  visualizer:
//	    - at: 86.4
//	      label: "Estribillo"
//	      transition: { mode: fade, duration: 1.5 }
//	      type: circular
//	      radius: 220

// Transition indica cómo se entra en un tramo
type Transition struct {
	Mode     string  `yaml:"mode"`     // "cut" (de golpe) o "fade" (interpola/funde)
	Duration float64 `yaml:"duration"` // segundos (solo fade)
}

// Seg es un tramo de una pista
type Seg[T any] struct {
	At         float64
	Label      string
	Transition Transition
	Cfg        T
}

// Marker es una marca con nombre en la línea de tiempo (secciones de la canción)
type Marker struct {
	At    float64 `yaml:"at"`
	Label string  `yaml:"label"`
}

type Timeline struct {
	Background []Seg[BackgroundConfig]
	BassZoom   []Seg[BassZoomConfig]
	Shake      []Seg[ShakeConfig]
	Chromatic  []Seg[ChromaticAberrationConfig]
	Ripple     []Seg[RippleConfig]
	Glitch     []Seg[GlitchConfig]
	ColorFlash []Seg[ColorFlashConfig]
	Visualizer []Seg[VisualizerConfig]
	Particles  []Seg[ParticlesConfig]
	Lyrics     []Seg[LyricsConfig]
}

// TrackNames son las claves de las pistas en `timeline:` (y en la API del estudio)
var TrackNames = []string{"background", "bass_zoom", "shake", "chromatic_aberration", "ripple",
	"glitch", "color_flash", "visualizer", "particles", "lyrics"}

// Active devuelve el índice del tramo activo en t (el último con At <= t)
func Active[T any](segs []Seg[T], t float64) int {
	i := sort.Search(len(segs), func(i int) bool { return segs[i].At > t }) - 1
	return max(i, 0)
}

// Blend devuelve (anterior, actual, k): k = 1 fuera de transición; en un fade, k va de 0 a 1
func Blend[T any](segs []Seg[T], t float64) (prev, cur *T, k float64) {
	i := Active(segs, t)
	cur = &segs[i].Cfg
	prev, k = cur, 1
	if i > 0 {
		s := segs[i]
		if s.Transition.Mode == "fade" && s.Transition.Duration > 0 && t < s.At+s.Transition.Duration {
			prev = &segs[i-1].Cfg
			k = (t - s.At) / s.Transition.Duration
		}
	}
	return
}

// deepCopy clona un bloque de config (incluidos slices) vía YAML
func deepCopy[T any](v T) T {
	b, _ := yaml.Marshal(v)
	var out T
	_ = yaml.Unmarshal(b, &out)
	return out
}

// buildTrack crea los tramos de una pista: base en t=0 y los nodos de timeline aplicados en cadena
func buildTrack[T any](base T, nodes []yaml.Node, fix func(*T)) ([]Seg[T], error) {
	fix(&base)
	segs := []Seg[T]{{At: 0, Cfg: base, Transition: Transition{Mode: "cut"}}}
	type head struct {
		At         *float64    `yaml:"at"`
		Label      string      `yaml:"label"`
		Transition *Transition `yaml:"transition"`
	}
	var parsed []struct {
		h    head
		node yaml.Node
	}
	for _, n := range nodes {
		var h head
		if err := n.Decode(&h); err != nil {
			return nil, err
		}
		if h.At == nil {
			return nil, fmt.Errorf("tramo de timeline sin 'at' (línea %d)", n.Line)
		}
		parsed = append(parsed, struct {
			h    head
			node yaml.Node
		}{h, n})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return *parsed[i].h.At < *parsed[j].h.At })
	for _, p := range parsed {
		prev := segs[len(segs)-1]
		cfg := deepCopy(prev.Cfg)
		// Quitar at/label/transition y aplicar el resto sobre la copia del tramo anterior
		body := p.node
		if body.Kind == yaml.MappingNode {
			var kept []*yaml.Node
			for i := 0; i+1 < len(body.Content); i += 2 {
				switch body.Content[i].Value {
				case "at", "label", "transition":
					continue
				}
				kept = append(kept, body.Content[i], body.Content[i+1])
			}
			body.Content = kept
		}
		if err := body.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("tramo en %.2fs: %w", *p.h.At, err)
		}
		fix(&cfg)
		tr := Transition{Mode: "cut"}
		if p.h.Transition != nil {
			tr = *p.h.Transition
			if tr.Mode == "" {
				tr.Mode = "fade"
			}
		}
		seg := Seg[T]{At: *p.h.At, Label: p.h.Label, Transition: tr, Cfg: cfg}
		if seg.At <= 0 {
			// Un tramo en 0 sustituye a la base
			segs[0].Cfg, segs[0].Label = cfg, p.h.Label
			continue
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

func noFix[T any](*T) {}

// BuildTimeline construye todas las pistas a partir de la config base y el bloque `timeline:`
func (c *ProjectConfig) BuildTimeline() error {
	raw := map[string][]yaml.Node{}
	if c.TimelineRaw.Kind != 0 {
		if err := c.TimelineRaw.Decode(&raw); err != nil {
			return fmt.Errorf("bloque 'timeline' inválido: %w", err)
		}
	}
	for k := range raw {
		known := k == "markers"
		for _, n := range TrackNames {
			known = known || k == n
		}
		if !known {
			return fmt.Errorf("pista de timeline desconocida '%s' (válidas: %v)", k, TrackNames)
		}
	}
	var err error
	tl := &c.Timeline
	be := &c.BackgroundEffects
	if tl.Background, err = buildTrack(c.Background, raw["background"], noFix[BackgroundConfig]); err != nil {
		return err
	}
	if tl.BassZoom, err = buildTrack(be.BassZoom, raw["bass_zoom"], noFix[BassZoomConfig]); err != nil {
		return err
	}
	if tl.Shake, err = buildTrack(be.Shake, raw["shake"], noFix[ShakeConfig]); err != nil {
		return err
	}
	if tl.Chromatic, err = buildTrack(be.ChromaticAberration, raw["chromatic_aberration"], noFix[ChromaticAberrationConfig]); err != nil {
		return err
	}
	if tl.Ripple, err = buildTrack(be.Ripple, raw["ripple"], noFix[RippleConfig]); err != nil {
		return err
	}
	if tl.Glitch, err = buildTrack(be.Glitch, raw["glitch"], noFix[GlitchConfig]); err != nil {
		return err
	}
	if tl.ColorFlash, err = buildTrack(be.ColorFlash, raw["color_flash"], noFix[ColorFlashConfig]); err != nil {
		return err
	}
	if tl.Visualizer, err = buildTrack(c.Visualizer, raw["visualizer"], FixVisualizer); err != nil {
		return err
	}
	if tl.Particles, err = buildTrack(c.Particles, raw["particles"], FixParticles); err != nil {
		return err
	}
	if tl.Lyrics, err = buildTrack(c.Lyrics, raw["lyrics"], noFix[LyricsConfig]); err != nil {
		return err
	}
	c.Markers = nil
	for _, n := range raw["markers"] {
		var m Marker
		if err := n.Decode(&m); err != nil {
			return err
		}
		c.Markers = append(c.Markers, m)
	}
	sort.Slice(c.Markers, func(i, j int) bool { return c.Markers[i].At < c.Markers[j].At })
	return nil
}

// FixVisualizer normaliza un bloque de visualizador (límites de barras, tipo en minúsculas)
func FixVisualizer(v *VisualizerConfig) {
	if v.Bars <= 0 {
		v.Bars = 48
	}
	if v.Bars > 256 {
		v.Bars = 256
	}
	v.Type = lower(v.Type)
}

// FixParticles asegura que haya al menos una capa
func FixParticles(p *ParticlesConfig) {
	if len(p.Layers) == 0 {
		p.Layers = []ParticleLayer{p.ParticleLayer}
	}
}

// ---------------------------------------------------------------- serialización

// segYAML es la forma de un tramo al guardar (cabecera + todos los campos del bloque)
func segNode[T any](s Seg[T]) (*yaml.Node, error) {
	var body yaml.Node
	if err := body.Encode(s.Cfg); err != nil {
		return nil, err
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v any) {
		var vn yaml.Node
		_ = vn.Encode(v)
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &vn)
	}
	add("at", s.At)
	if s.Label != "" {
		add("label", s.Label)
	}
	add("transition", s.Transition)
	n.Content = append(n.Content, body.Content...)
	return n, nil
}

func trackNodes[T any](segs []Seg[T]) ([]*yaml.Node, error) {
	var out []*yaml.Node
	for _, s := range segs[1:] {
		n, err := segNode(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// SyncFromTimeline copia el tramo 0 de cada pista a los bloques base y regenera `timeline:`
// para que Save escriba un YAML coherente con c.Timeline
func (c *ProjectConfig) SyncFromTimeline() error {
	tl := &c.Timeline
	if len(tl.Background) > 0 {
		c.Background = tl.Background[0].Cfg
	}
	be := &c.BackgroundEffects
	if len(tl.BassZoom) > 0 {
		be.BassZoom = tl.BassZoom[0].Cfg
	}
	if len(tl.Shake) > 0 {
		be.Shake = tl.Shake[0].Cfg
	}
	if len(tl.Chromatic) > 0 {
		be.ChromaticAberration = tl.Chromatic[0].Cfg
	}
	if len(tl.Ripple) > 0 {
		be.Ripple = tl.Ripple[0].Cfg
	}
	if len(tl.Glitch) > 0 {
		be.Glitch = tl.Glitch[0].Cfg
	}
	if len(tl.ColorFlash) > 0 {
		be.ColorFlash = tl.ColorFlash[0].Cfg
	}
	if len(tl.Visualizer) > 0 {
		c.Visualizer = tl.Visualizer[0].Cfg
	}
	if len(tl.Particles) > 0 {
		c.Particles = tl.Particles[0].Cfg
	}
	if len(tl.Lyrics) > 0 {
		c.Lyrics = tl.Lyrics[0].Cfg
	}

	root := &yaml.Node{Kind: yaml.MappingNode}
	put := func(name string, nodes []*yaml.Node, err error) error {
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return nil
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Content: nodes}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name}, seq)
		return nil
	}
	steps := []func() error{
		func() error { n, e := trackNodes(tl.Background); return put("background", n, e) },
		func() error { n, e := trackNodes(tl.BassZoom); return put("bass_zoom", n, e) },
		func() error { n, e := trackNodes(tl.Shake); return put("shake", n, e) },
		func() error { n, e := trackNodes(tl.Chromatic); return put("chromatic_aberration", n, e) },
		func() error { n, e := trackNodes(tl.Ripple); return put("ripple", n, e) },
		func() error { n, e := trackNodes(tl.Glitch); return put("glitch", n, e) },
		func() error { n, e := trackNodes(tl.ColorFlash); return put("color_flash", n, e) },
		func() error { n, e := trackNodes(tl.Visualizer); return put("visualizer", n, e) },
		func() error { n, e := trackNodes(tl.Particles); return put("particles", n, e) },
		func() error { n, e := trackNodes(tl.Lyrics); return put("lyrics", n, e) },
	}
	for _, s := range steps {
		if len(tl.Background) == 0 {
			break
		}
		if err := s(); err != nil {
			return err
		}
	}
	if len(c.Markers) > 0 {
		var mn yaml.Node
		if err := mn.Encode(c.Markers); err != nil {
			return err
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "markers"}, &mn)
	}
	c.TimelineRaw = yaml.Node{}
	if len(root.Content) > 0 {
		c.TimelineRaw = *root
	}
	return nil
}
