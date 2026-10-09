package config

import (
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Exprs: the advanced settings of an element's properties: an expression (package expr) per
// property path, as Keys ("emitter.rate": "value * (1 + bass)"). They are worked out after the
// keyframes, from the value the property has then (value).
type Exprs map[string]string

// ExprLibrary: the project's own constants ($name) and functions ($f(…) = … definitions) the
// expressions can use
type ExprLibrary struct {
	Constants map[string]float64 `yaml:"constants,omitempty" json:"constants,omitempty"`
	Functions string             `yaml:"functions,omitempty" json:"functions,omitempty"`
}

// ExprCtx: where an expression is worked out: the song's time, the element's own (U, from its
// start) and its length (Dur; 0 = open-ended), its motion path (path.name), an id for its
// stable random numbers
type ExprCtx struct {
	T, U, Dur float64
	Motion    *Motion
	ID        string
}

// Evaluator works out expressions (the engine's): the property's value then, its new value;
// false keeps it
type Evaluator interface {
	EvalExpr(src string, value float64, ctx *ExprCtx) (float64, bool)
}

// Animate: cfg at u (s from its start) with its keyframes and expressions applied (a copy when
// anything applies). ctx: T, Dur, ID (U is set; Motion is cfg's own if it has one, after the keys)
func Animate[T any](cfg *T, keys Keys, exprs Exprs, ev Evaluator, ctx ExprCtx) *T {
	c := cfg
	if len(keys) > 0 {
		c = ApplyKeys(cfg, keys, ctx.U)
	}
	if len(exprs) == 0 || ev == nil {
		return c
	}
	if c == cfg {
		cp := *cfg
		c = &cp
	}
	rv := reflect.ValueOf(c).Elem()
	if ctx.Motion == nil {
		if m, ok := fieldByYAML(rv, "motion"); ok {
			if mo, ok := m.Addr().Interface().(*Motion); ok && mo.On() {
				ctx.Motion = mo
			}
		}
	}
	for path, src := range exprs {
		if strings.TrimSpace(src) == "" {
			continue
		}
		parts := strings.Split(path, ".")
		v, ok := getPath(rv, parts)
		if !ok {
			continue
		}
		if nv, ok := ev.EvalExpr(src, v, &ctx); ok {
			setPath(rv, parts, nv)
		}
	}
	return c
}

// getPath: the number at a property path (yaml keys; booleans as 0 / 1)
func getPath(v reflect.Value, path []string) (float64, bool) {
	for _, p := range path {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return 0, false
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			f, ok := fieldByYAML(v, p)
			if !ok {
				return 0, false
			}
			v = f
		case reflect.Slice:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= v.Len() {
				return 0, false
			}
			v = v.Index(i)
		default:
			return 0, false
		}
	}
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Float64, reflect.Float32:
		return v.Float(), true
	case reflect.Int, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Bool:
		if v.Bool() {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// boolFromExpr: C-like: more than 0 is true
func boolFromExpr(f float64) bool { return f > 0 && !math.IsNaN(f) }

// NumberAt: the number at a property path of cfg (a pointer to a config block) with its keys
// applied at u, and its motion path then (if it has one)
func NumberAt(cfg any, keys Keys, u float64, path string) (float64, *Motion, bool) {
	src := reflect.ValueOf(cfg)
	if src.Kind() != reflect.Ptr || src.Elem().Kind() != reflect.Struct {
		return 0, nil, false
	}
	cp := reflect.New(src.Elem().Type())
	cp.Elem().Set(src.Elem())
	rv := cp.Elem()
	for p, l := range keys {
		if v := ValueAt(l, u); v != nil {
			setPath(rv, strings.Split(p, "."), v)
		}
	}
	var mo *Motion
	if m, ok := fieldByYAML(rv, "motion"); ok {
		if x, ok := m.Addr().Interface().(*Motion); ok && x.On() {
			mo = x
		}
	}
	v, ok := getPath(rv, strings.Split(path, "."))
	return v, mo, ok
}

// SegRef: segment i of a track (TrackNames): its config block, keys, expressions, start and the
// next segment's start (−1: it is the last)
func (tl *Timeline) SegRef(track string, i int) (cfg any, keys Keys, exprs Exprs, at, next float64, ok bool) {
	ti := -1
	for k, n := range TrackNames {
		if n == track {
			ti = k
		}
	}
	if ti < 0 {
		return nil, nil, nil, 0, 0, false
	}
	segs := reflect.ValueOf(tl).Elem().Field(ti)
	if i < 0 || i >= segs.Len() {
		return nil, nil, nil, 0, 0, false
	}
	sg := segs.Index(i)
	next = -1
	if i+1 < segs.Len() {
		next = segs.Index(i + 1).FieldByName("At").Float()
	}
	return sg.FieldByName("Cfg").Addr().Interface(), sg.FieldByName("Keys").Interface().(Keys),
		sg.FieldByName("Exprs").Interface().(Exprs), sg.FieldByName("At").Float(), next, true
}
