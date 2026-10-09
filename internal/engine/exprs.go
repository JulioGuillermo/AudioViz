package engine

import (
	"hash/fnv"
	"math"
	"strconv"

	"audioviz/internal/config"
	"audioviz/internal/effects"
	"audioviz/internal/expr"
)

// The properties' advanced settings (expressions): the session is their config.Evaluator. Each
// one is compiled once (with the project's library) and run with the audio of the frame it is
// worked out at.

type exprEntry struct {
	p   *expr.Program
	err error
}

// exprProgram: src compiled (nil if it has a mistake), cached
func (s *Session) exprProgram(src string) (*expr.Program, error) {
	if s.exprCache == nil {
		s.exprCache = map[string]exprEntry{}
	}
	if e, ok := s.exprCache[src]; ok {
		return e.p, e.err
	}
	lib := &expr.Library{Consts: s.Cfg.Expressions.Constants, Funcs: s.Cfg.Expressions.Functions}
	p, err := expr.Compile(src, lib)
	s.exprCache[src] = exprEntry{p, err}
	return p, err
}

// EvalExpr (config.Evaluator): the value of a property from its expression; a mistake keeps it
// as it is, nil gives 0
func (s *Session) EvalExpr(src string, value float64, ctx *config.ExprCtx) (float64, bool) {
	p, err := s.exprProgram(src)
	if err != nil || p == nil {
		return 0, false
	}
	r := p.Run(&exprEnv{s: s, ctx: ctx, value: value})
	if r.Nil {
		return 0, true
	}
	return r.V, true
}

// exprEnv: what an expression reads at one place (expr.Env)
type exprEnv struct {
	s     *Session
	ctx   *config.ExprCtx
	value float64
}

func (e *exprEnv) Var(v int) float64 {
	c := e.ctx
	switch v {
	case expr.VarValue:
		return e.value
	case expr.VarProjectTime:
		return c.T
	case expr.VarProjectProgress:
		if d := e.s.Duration(); d > 0 {
			return math.Max(0, math.Min(1, c.T/d))
		}
	case expr.VarClipTime:
		return c.U
	case expr.VarClipProgress:
		if c.Dur > 0 {
			return math.Max(0, math.Min(1, c.U/c.Dur))
		}
	case expr.VarFrame:
		return math.Round(c.T * float64(e.s.FPS))
	case expr.VarFPS:
		return float64(e.s.FPS)
	case expr.VarCount:
		return 1
	}
	return 0
}

// band: band b at frame f (the analysis of the frame being made; 0 without it)
func (e *exprEnv) band(b int, f int) float64 {
	fs := e.s.exprFeats
	if len(fs) == 0 {
		return 0
	}
	f = max(0, min(len(fs)-1, f))
	return fs[f].Band(expr.Bands[b], expr.Bands[b])
}

func (e *exprEnv) frame() int { return int(math.Round(e.ctx.T * float64(e.s.FPS))) }

// Band: b dt seconds from now
func (e *exprEnv) Band(b int, dt float64) float64 {
	return e.band(b, e.frame()+int(math.Round(dt*float64(e.s.FPS))))
}

// Lag: b smoothed over the last s seconds (the more recent, the more it counts)
func (e *exprEnv) Lag(b int, s float64) float64 {
	fps := float64(e.s.FPS)
	if s*fps < 1 {
		return e.Band(b, 0)
	}
	f0, n := e.frame(), min(int(math.Ceil(3*s*fps)), 900)
	var sum, ws float64
	for k := 0; k <= n; k++ {
		w := math.Exp(-float64(k) / (s * fps))
		sum += w * e.band(b, f0-k)
		ws += w
	}
	return sum / ws
}

// Peak: the loudest of b in the last s seconds
func (e *exprEnv) Peak(b int, s float64) float64 {
	f0, n := e.frame(), min(int(math.Ceil(s*float64(e.s.FPS))), 1800)
	m := e.band(b, f0)
	for k := 1; k <= n; k++ {
		m = math.Max(m, e.band(b, f0-k))
	}
	return m
}

// Key: a value of the path it goes along at its place on it (the route's are per particle: on
// the GPU)
func (e *exprEnv) Key(kind int, name string) (float64, bool) {
	mo := e.ctx.Motion
	if kind != 0 || mo == nil || !mo.On() {
		return 0, false
	}
	W, H := float64(e.s.W), float64(e.s.H)
	t := effects.Track(mo.Path, name, W, H)
	if t == nil {
		return 0, false
	}
	_, L := effects.NodeLens(mo.Path, W, H)
	p := mo.Progress
	if mo.Path.Open {
		p = math.Max(0, math.Min(1, p))
	} else {
		p -= math.Floor(p)
	}
	return float64(t.At(float32(p * L))), true
}

// Rand: stable for the element (perFrame: a new one each frame)
func (e *exprEnv) Rand(perFrame bool) float64 {
	h := fnv.New32a()
	h.Write([]byte(e.ctx.ID))
	n := h.Sum32()
	if perFrame {
		n ^= uint32(e.frame()) * 2654435761
	}
	n = (n << 13) ^ n
	n = n*(n*n*15731+789221) + 1376312589
	return float64(n&0x7fffffff) / float64(0x7fffffff)
}

// ExprTarget: the element whose property an expression is tried on (the studio's dialog): a clip
// (layer id, clip), a fixed text (text) or a track's segment (track, seg)
type ExprTarget struct {
	Kind  string `json:"kind"` // clip, text, seg
	Layer string `json:"layer,omitempty"`
	Clip  int    `json:"clip,omitempty"`
	Text  int    `json:"text,omitempty"`
	Track string `json:"track,omitempty"`
	Seg   int    `json:"seg,omitempty"`
}

// ExprSample: an expression's value at a time (nil: none)
type ExprSample struct {
	T   float64 `json:"t"`
	V   float64 `json:"v"`
	Nil bool    `json:"nil,omitempty"`
}

// ExprSeries: an expression tried on a property: its mistake (where), what it uses, whether
// something impossible came up (0 instead), its values over time
type ExprSeries struct {
	Error     *expr.Error  `json:"error,omitempty"`
	UsesValue bool         `json:"uses_value"`
	Warn      bool         `json:"warn,omitempty"`
	Samples   []ExprSample `json:"samples"`
}

// ExprSeries tries src on the property path of target at each of times (song seconds): the
// property's value then (its keys applied) as value, the audio of each moment
func (s *Session) ExprSeries(src string, tg ExprTarget, path string, times []float64) ExprSeries {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out ExprSeries
	p, err := s.exprProgram(src)
	if err != nil {
		if e, ok := err.(*expr.Error); ok {
			out.Error = e
		} else {
			out.Error = &expr.Error{Line: -1, Msg: err.Error()}
		}
		return out
	}
	out.UsesValue = p.UsesValue
	if base, err := s.featuresLocked(s.baseKey); err == nil {
		s.exprFeats = base
	}
	var cfg any
	var keys config.Keys
	var start, dur float64
	var id string
	switch tg.Kind {
	case "clip":
		for _, l := range s.Cfg.Layers {
			if l.ID == tg.Layer && tg.Clip >= 0 && tg.Clip < len(l.Clips) {
				c := &l.Clips[tg.Clip]
				cfg, keys, start, dur, id = c, c.Keys, c.Start, c.End-c.Start, clipID(l.ID, tg.Clip)
			}
		}
	case "text":
		if tg.Text >= 0 && tg.Text < len(s.Cfg.Texts) {
			tc := &s.Cfg.Texts[tg.Text]
			cfg, keys, start, dur, id = tc, tc.Keys, tc.Start, tc.End-tc.Start, "text:"+strconv.Itoa(tg.Text)
		}
	case "seg":
		if c, k, _, at, next, ok := s.Cfg.Timeline.SegRef(tg.Track, tg.Seg); ok {
			cfg, keys, start, id = c, k, at, tg.Track+strconv.Itoa(tg.Seg)
			dur = s.Duration() - at
			if next >= 0 {
				dur = next - at
			}
		}
	}
	for _, t := range times {
		value, mo := 0.0, (*config.Motion)(nil)
		if cfg != nil {
			value, mo, _ = config.NumberAt(cfg, keys, t-start, path)
		}
		ctx := &config.ExprCtx{T: t, U: t - start, Dur: dur, Motion: mo, ID: id}
		r := p.Run(&exprEnv{s: s, ctx: ctx, value: value})
		out.Warn = out.Warn || r.Warn
		out.Samples = append(out.Samples, ExprSample{T: t, V: r.V, Nil: r.Nil})
	}
	return out
}
