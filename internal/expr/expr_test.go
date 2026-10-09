package expr

import (
	"math"
	"strings"
	"testing"
)

type testEnv struct{ value, time float64 }

func (e testEnv) Var(v int) float64 {
	switch v {
	case VarValue:
		return e.value
	case VarProjectTime:
		return e.time
	}
	return 0
}
func (e testEnv) Band(b int, dt float64) float64 { return 0.5 + float64(b)*0.01 + dt*0.1 }
func (e testEnv) Lag(b int, s float64) float64   { return 0.25 }
func (e testEnv) Peak(b int, s float64) float64  { return 0.9 }
func (e testEnv) Key(kind int, name string) (float64, bool) {
	if name == "glow" {
		return 2, true
	}
	return 0, false
}
func (e testEnv) Rand(perFrame bool) float64 { return 0.3 }

func run(t *testing.T, src string, lib *Library, env testEnv) Result {
	t.Helper()
	p, err := Compile(src, lib)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return p.Run(env)
}

func TestExpressions(t *testing.T) {
	env := testEnv{value: 10, time: 2}
	lib := &Library{Consts: map[string]float64{"k": 3}, Funcs: `
$double(x) = x * 2
$pulse(x, s) =
  if(s <= 0 -> ret(x))
  let k = lag(kick, 0.2) * s
  x * (1 + k)
`}
	cases := []struct {
		src  string
		want float64
		nil_ bool
		warn bool
	}{
		{"value", 10, false, false},
		{"1 + 2 * 3", 7, false, false},
		{"(1 + 2) * 3", 9, false, false},
		{"2 ^ 3 ^ 2", 512, false, false},
		{"-2 ^ 2", -4, false, false},
		{"7 % 3", 1, false, false},
		{"sin(90)", 1, false, false},
		{"rad_sin(pi / 2)", 1, false, false},
		{"bass", 0.5, false, false},
		{"kick@-1", 0.57 - 0.1, false, false},
		{"lag(bass, 0.3) + peak(kick, 1)", 1.15, false, false},
		{"path.glow * value", 20, false, false},
		{"path.nope", 0, true, false},
		{"path.nope ?? value", 10, false, false},
		{"path.nope + 1", 1, false, false},
		{"if(value > 5 -> 1, value > 2 -> 2, 3)", 1, false, false},
		{"if(value > 50 -> 1, value > 2 -> 2, 3)", 2, false, false},
		{"if(value > 50 -> 1)", 0, true, false},
		{"if(value > 50 -> 1) ?? value", 10, false, false},
		{"value > 5 && time > 1", 1, false, false},
		{"value > 50 || time > 3", 0, false, false},
		{"!(value > 5)", 0, false, false},
		{"1 / 0", 0, false, true},
		{"sqrt(-1)", 0, false, true},
		{"$k * value", 30, false, false},
		{"$double(value) + 1", 21, false, false},
		{"$pulse(value, 1)", 12.5, false, false},
		{"$pulse(value, 0)", 10, false, false},
		{"let a = value * 2\nlet b = a + 1\nb * 2", 42, false, false},
		{"$tri(x) = x * 3\n$tri(value)", 30, false, false},
		{"min(3, 1, 2) + max(1, 5, 2)", 6, false, false},
		{"clamp(value, 0, 4) + mix(0, 10, 0.5)", 9, false, false},
		{"map(5, 0, 10, 100, 200)", 150, false, false},
		{"rand() + random()", 0.6, false, false},
		{"is_nil(path.nope) + is_nil(1)", 1, false, false},
		{"lerp(0, 2, 0.25)", 0.5, false, false},
		{"# a comment\nvalue # the slider\n", 10, false, false},
		{"if(value > 5 -> ret(1))\n2", 1, false, false},
		{"1 + (if(value > 5 -> ret(7)) ?? 0)", 7, false, false},
	}
	for _, c := range cases {
		r := run(t, c.src, lib, env)
		if math.Abs(r.V-c.want) > 1e-9 || r.Nil != c.nil_ || r.Warn != c.warn {
			t.Errorf("%q = %v (nil %v warn %v), want %v (nil %v warn %v)", c.src, r.V, r.Nil, r.Warn, c.want, c.nil_, c.warn)
		}
	}
}

func TestErrors(t *testing.T) {
	lib := &Library{Funcs: "$loop(x) = $loop(x)"}
	for _, c := range []struct{ src, has string }{
		{"value +", "ends too soon"},
		{"foo", "unknown name"},
		{"$nope", "no such constant"},
		{"$nope(1)", "no such function"},
		{"$loop(1)", "call itself"},
		{"sin(1, 2)", "takes 1"},
		{"1 < 2 < 3", "don't chain"},
		{"if(1, 2 -> 3)", "only the last"},
		{"lag(3, 1)", "lag(band"},
		{"value@1", "@ goes after"},
		{"", "empty"},
	} {
		_, err := Compile(c.src, lib)
		if err == nil || !strings.Contains(err.Error(), c.has) {
			t.Errorf("%q: error %v, want one with %q", c.src, err, c.has)
		}
	}
}
