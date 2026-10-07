package config

import (
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Keyframes: any property of a segment (or of a fixed text) can change over time. Each animated
// property has a list of keys; between two keys the value goes from one to the other with the
// easing of the first key. Before the first key and after the last one the value stays.
//
//	timeline:
//	  visualizer:
//	    - at: 0
//	      keys:
//	        radius: [{t: 0, v: 200}, {t: 4.5, v: 320, ease: ease_in_out}]
//	        glow.color: [{t: 0, v: "#ff0040"}, {t: 8, v: "#00e0ff"}]
//
// The path is made of the YAML keys of the block, separated by dots, with list indices as
// numbers ("layers.1.speed"). Numbers and colors (#rrggbb, #rrggbbaa) are interpolated; anything
// else (switches, options, texts) changes at the key.

// Key is one keyframe of a property
type Key struct {
	T    float64 `yaml:"t" json:"t"`                           // seconds from the start of the segment (text: from its start)
	V    any     `yaml:"v" json:"v"`                           // the value at that instant
	Ease string  `yaml:"ease,omitempty" json:"ease,omitempty"` // towards the next key (see Eases); default linear
}

// Keys: the keyframes of a segment, per property path
type Keys map[string][]Key

// Eases are the easings a key can have towards the next one
var Eases = []string{"linear", "step", "ease_in", "ease_out", "ease_in_out", "back", "spring"}

// Ease applies an easing to k (0..1)
func Ease(name string, k float64) float64 {
	k = math.Max(0, math.Min(1, k))
	switch name {
	case "step":
		return 0
	case "ease_in":
		return k * k * k
	case "ease_out":
		return 1 - math.Pow(1-k, 3)
	case "ease_in_out":
		if k < 0.5 {
			return 4 * k * k * k
		}
		return 1 - math.Pow(-2*k+2, 3)/2
	case "back":
		// Anticipation and overshoot: it first moves a little the other way, goes past the
		// target and comes back to it
		const c = 1.70158 * 1.525
		if k < 0.5 {
			return (2 * k) * (2 * k) * ((c+1)*2*k - c) / 2
		}
		return ((2*k-2)*(2*k-2)*((c+1)*(2*k-2)+c) + 2) / 2
	case "spring":
		// A spring: goes past the target (~26 %) and settles on it with damped oscillations
		// (exactly 1 at the end: cos(4.5π) = 0)
		if k >= 1 {
			return 1
		}
		return 1 - math.Exp(-6*k)*math.Cos(4.5*math.Pi*k)
	}
	return k
}

// Sort orders every key list by time (the YAML or the studio may give them in any order)
func (ks Keys) Sort() {
	for p, l := range ks {
		if len(l) == 0 {
			delete(ks, p)
			continue
		}
		sort.SliceStable(l, func(i, j int) bool { return l[i].T < l[j].T })
	}
}

// ValueAt evaluates a sorted key list at u seconds
func ValueAt(l []Key, u float64) any {
	if len(l) == 0 {
		return nil
	}
	if u <= l[0].T {
		return l[0].V
	}
	i := sort.Search(len(l), func(i int) bool { return l[i].T > u }) - 1
	if i >= len(l)-1 {
		return l[len(l)-1].V
	}
	a, b := l[i], l[i+1]
	k := 0.0
	if b.T > a.T {
		k = Ease(a.Ease, (u-a.T)/(b.T-a.T))
	}
	if v, ok := lerpAny(a.V, b.V, k); ok {
		return v
	}
	if sa, ok := a.V.(string); ok {
		if sb, ok := b.V.(string); ok {
			ca, okA := parseHex(sa)
			cb, okB := parseHex(sb)
			if okA && okB {
				var c [4]float64
				for j := range c {
					c[j] = ca[j] + (cb[j]-ca[j])*k
				}
				return formatHex(c, len(strings.TrimPrefix(sa, "#")) == 8 || len(strings.TrimPrefix(sb, "#")) == 8)
			}
		}
	}
	if k >= 1 {
		return b.V
	}
	return a.V
}

// lerpAny interpolates numbers, and lists / maps of the same shape element by element (e.g.
// the points of a mask's shapes); false when the two values cannot be mixed
func lerpAny(a, b any, k float64) (any, bool) {
	if fa, ok := toFloat(a); ok {
		if fb, ok := toFloat(b); ok {
			return fa + (fb-fa)*k, true
		}
		return nil, false
	}
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok {
			return nil, false
		}
		if v, ok := lerpNode(x, y, k); ok {
			return v, true
		}
		if len(x) != len(y) {
			return nil, false
		}
		out := make([]any, len(x))
		for i := range x {
			v, ok := lerpAny(x[i], y[i], k)
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return nil, false
		}
		out := make(map[string]any, len(x))
		for key, xv := range x {
			yv, ok := y[key]
			if !ok {
				return nil, false
			}
			v, ok := lerpAny(xv, yv, k)
			if !ok {
				return nil, false
			}
			out[key] = v
		}
		return out, true
	}
	return nil, false
}

// lerpNode interpolates two nodes of a mask shape ([x, y], [x, y, type] or [x, y, type, inX, inY,
// outX, outY], see effects.NodeHandles): the position always moves; the type is not a quantity, it
// changes halfway; the handles move when both nodes have them, else they come with the type.
// ok = false when a or b are not lists of numbers of a node's length.
func lerpNode(a, b []any, k float64) ([]any, bool) {
	node := func(l []any) bool {
		if len(l) != 2 && len(l) != 3 && len(l) != 7 {
			return false
		}
		for _, v := range l {
			if _, ok := toFloat(v); !ok {
				return false
			}
		}
		return true
	}
	if !node(a) || !node(b) || (len(a) == 2 && len(b) == 2) {
		return nil, false
	}
	near := a
	if k >= 0.5 {
		near = b
	}
	out := make([]any, len(near))
	copy(out, near)
	n := 2
	if len(a) == 7 && len(b) == 7 {
		n = 7
	}
	for i := 0; i < n; i++ {
		if i == 2 {
			continue // the type
		}
		fa, _ := toFloat(a[i])
		fb, _ := toFloat(b[i])
		out[i] = fa + (fb-fa)*k
	}
	return out, true
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

// ApplyKeys returns a copy of cfg with its animated properties at u seconds. The copy shares
// what does not change: only the lists and pointers on the path to an animated value are copied.
func ApplyKeys[T any](cfg *T, keys Keys, u float64) *T {
	c := *cfg
	rv := reflect.ValueOf(&c).Elem()
	for path, l := range keys {
		if v := ValueAt(l, u); v != nil {
			setPath(rv, strings.Split(path, "."), v)
		}
	}
	return &c
}

// CfgAt is the segment's config at time t (song seconds) with its keyframes applied
func (s *Seg[T]) CfgAt(t float64) *T {
	if len(s.Keys) == 0 {
		return &s.Cfg
	}
	return ApplyKeys(&s.Cfg, s.Keys, t-s.At)
}

// setPath writes val at the path inside v (a settable value), copying the lists and pointers
// it goes through so the original config is not touched. Unknown paths are ignored.
func setPath(v reflect.Value, path []string, val any) {
	switch v.Kind() {
	case reflect.Ptr:
		nv := reflect.New(v.Type().Elem())
		if !v.IsNil() {
			nv.Elem().Set(v.Elem())
		}
		v.Set(nv)
		setPath(nv.Elem(), path, val)
		return
	case reflect.Interface:
		return
	}
	if len(path) == 0 {
		setLeaf(v, val)
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		if f, ok := fieldByYAML(v, path[0]); ok {
			setPath(f, path[1:], val)
		}
	case reflect.Slice:
		i, err := strconv.Atoi(path[0])
		if err != nil || i < 0 || i >= v.Len() {
			return
		}
		cp := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(cp, v)
		v.Set(cp)
		setPath(cp.Index(i), path[1:], val)
	}
}

func setLeaf(v reflect.Value, val any) {
	switch v.Kind() {
	case reflect.Float64, reflect.Float32:
		if f, ok := toFloat(val); ok {
			v.SetFloat(f)
		}
	case reflect.Int, reflect.Int32, reflect.Int64:
		if f, ok := toFloat(val); ok {
			v.SetInt(int64(math.Round(f)))
		}
	case reflect.String:
		if s, ok := val.(string); ok {
			v.SetString(s)
		}
	case reflect.Bool:
		if b, ok := val.(bool); ok {
			v.SetBool(b)
		}
	case reflect.Slice, reflect.Map, reflect.Struct:
		// Lists and blocks (e.g. a mask's shapes): through YAML into the field's type
		if b, err := yaml.Marshal(val); err == nil {
			nv := reflect.New(v.Type())
			if yaml.Unmarshal(b, nv.Interface()) == nil {
				v.Set(nv.Elem())
			}
		}
	}
}

// fieldByYAML finds the field of struct v with that YAML key (looking into inlined structs)
func fieldByYAML(v reflect.Value, name string) (reflect.Value, bool) {
	idx, ok := yamlIndex(v.Type())[name]
	if !ok {
		return reflect.Value{}, false
	}
	return v.FieldByIndex(idx), true
}

var yamlIdx sync.Map // reflect.Type → map[string][]int

func yamlIndex(t reflect.Type) map[string][]int {
	if m, ok := yamlIdx.Load(t); ok {
		return m.(map[string][]int)
	}
	m := map[string][]int{}
	var walk func(t reflect.Type, prefix []int)
	walk = func(t reflect.Type, prefix []int) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("yaml")
			name, opts, _ := strings.Cut(tag, ",")
			idx := append(append([]int{}, prefix...), i)
			if strings.Contains(opts, "inline") && f.Type.Kind() == reflect.Struct {
				walk(f.Type, idx)
				continue
			}
			if name == "-" {
				continue
			}
			if name == "" {
				name = strings.ToLower(f.Name)
			}
			if _, dup := m[name]; !dup {
				m[name] = idx
			}
		}
	}
	walk(t, nil)
	yamlIdx.Store(t, m)
	return m
}
