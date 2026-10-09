package expr

import "math"

// Env gives an expression what it reads: the variables (Vars), the audio bands (Bands: now, dt
// seconds from now, smoothed, the loudest), the values of the path / route it goes along (nil
// when none), and its random numbers (stable per element and item; per frame)
type Env interface {
	Var(v int) float64
	Band(b int, dt float64) float64
	Lag(b int, s float64) float64
	Peak(b int, s float64) float64
	Key(kind int, name string) (float64, bool)
	Rand(perFrame bool) float64
}

// Result of a run: its value, nil, and whether something impossible was worked out (a division
// by 0, the root of a negative…: 0 instead)
type Result struct {
	V    float64
	Nil  bool
	Warn bool
}

// Run runs the program
func (p *Program) Run(env Env) Result {
	st := make([]float64, 0, p.Stack+2)
	nl := make([]bool, 0, p.Stack+2)
	slots := make([]float64, p.Slots)
	slotNil := make([]bool, p.Slots)
	warn := false
	push := func(v float64, isNil bool) {
		if !isNil && (math.IsNaN(v) || math.IsInf(v, 0)) {
			v, warn = 0, true
		}
		st = append(st, v)
		nl = append(nl, isNil)
	}
	pop := func() (float64, bool) {
		n := len(st) - 1
		v, isNil := st[n], nl[n]
		st, nl = st[:n], nl[:n]
		return v, isNil
	}
	num := func() float64 { // nil counts as 0
		v, isNil := pop()
		if isNil {
			return 0
		}
		return v
	}
	b2f := func(b bool) float64 {
		if b {
			return 1
		}
		return 0
	}
	code := p.Code
	for pc := 0; pc < len(code); {
		op := int(code[pc])
		arg := func(i int) int { return int(code[pc+1+i]) }
		next := pc + 1 + opArgs[op]
		switch op {
		case OpConst:
			push(p.Consts[arg(0)], false)
		case OpNil:
			push(0, true)
		case OpVar:
			push(env.Var(arg(0)), false)
		case OpBand:
			push(env.Band(arg(0), 0), false)
		case OpBandAt:
			push(env.Band(arg(0), num()), false)
		case OpLag:
			push(env.Lag(arg(0), math.Max(num(), 0)), false)
		case OpPeak:
			push(env.Peak(arg(0), math.Max(num(), 0)), false)
		case OpKey:
			v, ok := env.Key(arg(0), p.Keys[arg(1)])
			push(v, !ok)
		case OpLoad:
			push(slots[arg(0)], slotNil[arg(0)])
		case OpStore:
			slots[arg(0)], slotNil[arg(0)] = pop()
		case OpPop:
			pop()
		case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpPow, OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
			b, a := num(), num()
			var r float64
			switch op {
			case OpAdd:
				r = a + b
			case OpSub:
				r = a - b
			case OpMul:
				r = a * b
			case OpDiv:
				if b == 0 {
					warn = true
				} else {
					r = a / b
				}
			case OpMod:
				if b == 0 {
					warn = true
				} else {
					r = a - b*math.Floor(a/b)
				}
			case OpPow:
				r = math.Pow(a, b)
			case OpEq:
				r = b2f(a == b)
			case OpNe:
				r = b2f(a != b)
			case OpLt:
				r = b2f(a < b)
			case OpLe:
				r = b2f(a <= b)
			case OpGt:
				r = b2f(a > b)
			case OpGe:
				r = b2f(a >= b)
			}
			push(r, false)
		case OpNeg:
			push(-num(), false)
		case OpNot:
			push(b2f(!(num() > 0)), false)
		case OpTruthy:
			push(b2f(num() > 0), false)
		case OpJmp:
			next = arg(0)
		case OpJmpF:
			if !(num() > 0) {
				next = arg(0)
			}
		case OpCoalesce:
			if !nl[len(nl)-1] {
				next = arg(0)
			} else {
				pop()
			}
		case OpRetTo:
			v, isNil := pop()
			st, nl = st[:arg(0)], nl[:arg(0)]
			push(v, isNil)
			next = arg(1)
		case OpCall:
			fi := arg(0)
			n := Funcs[fi].Args
			var a [5]float64
			isNil := false
			for i := n - 1; i >= 0; i-- {
				v, vn := pop()
				if vn {
					isNil = true
				}
				a[i] = v
				if vn {
					a[i] = 0
				}
			}
			r, w := callFunc(fi, a[:n], isNil, env)
			warn = warn || w
			push(r, false)
		case OpNop:
		case OpPopPush:
			pop()
			push(p.Consts[arg(0)], false)
		case OpEnd:
			if len(st) == 0 {
				return Result{Nil: true, Warn: warn}
			}
			v, isNil := pop()
			return Result{V: v, Nil: isNil, Warn: warn}
		}
		pc = next
	}
	return Result{Nil: true, Warn: warn}
}

const degRad = math.Pi / 180

// callFunc runs built-in function fi (Funcs) on a; w: something impossible, 0 instead. As
// exprCall in render.comp.
func callFunc(fi int, a []float64, anyNil bool, env Env) (r float64, w bool) {
	safe := func(v float64) (float64, bool) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, true
		}
		return v, false
	}
	switch Funcs[fi].Name {
	case "sin":
		return math.Sin(a[0] * degRad), false
	case "cos":
		return math.Cos(a[0] * degRad), false
	case "tan":
		return safe(math.Tan(a[0] * degRad))
	case "asin":
		return safe(math.Asin(a[0]) / degRad)
	case "acos":
		return safe(math.Acos(a[0]) / degRad)
	case "atan":
		return math.Atan(a[0]) / degRad, false
	case "atan2":
		return math.Atan2(a[0], a[1]) / degRad, false
	case "rad_sin":
		return math.Sin(a[0]), false
	case "rad_cos":
		return math.Cos(a[0]), false
	case "rad_tan":
		return safe(math.Tan(a[0]))
	case "rad_asin":
		return safe(math.Asin(a[0]))
	case "rad_acos":
		return safe(math.Acos(a[0]))
	case "rad_atan":
		return math.Atan(a[0]), false
	case "rad_atan2":
		return math.Atan2(a[0], a[1]), false
	case "deg":
		return a[0] / degRad, false
	case "rad":
		return a[0] * degRad, false
	case "abs":
		return math.Abs(a[0]), false
	case "sign":
		switch {
		case a[0] > 0:
			return 1, false
		case a[0] < 0:
			return -1, false
		}
		return 0, false
	case "floor":
		return math.Floor(a[0]), false
	case "ceil":
		return math.Ceil(a[0]), false
	case "round":
		return math.Floor(a[0] + 0.5), false
	case "fract":
		return a[0] - math.Floor(a[0]), false
	case "sqrt":
		if a[0] < 0 {
			return 0, true
		}
		return math.Sqrt(a[0]), false
	case "exp":
		return safe(math.Exp(a[0]))
	case "log":
		if a[0] <= 0 {
			return 0, true
		}
		return math.Log(a[0]), false
	case "pow":
		return safe(math.Pow(a[0], a[1]))
	case "mod":
		if a[1] == 0 {
			return 0, true
		}
		return a[0] - a[1]*math.Floor(a[0]/a[1]), false
	case "min":
		return math.Min(a[0], a[1]), false
	case "max":
		return math.Max(a[0], a[1]), false
	case "clamp":
		return math.Max(a[1], math.Min(a[2], a[0])), false
	case "mix":
		return a[0] + (a[1]-a[0])*a[2], false
	case "smoothstep":
		if a[1] == a[0] {
			if a[2] < a[0] {
				return 0, false
			}
			return 1, false
		}
		t := math.Max(0, math.Min(1, (a[2]-a[0])/(a[1]-a[0])))
		return t * t * (3 - 2*t), false
	case "step":
		if a[1] < a[0] {
			return 0, false
		}
		return 1, false
	case "map":
		// map(x, from a, from b, to c, to d)
		if a[2] == a[1] {
			return a[3], true
		}
		return a[3] + (a[0]-a[1])/(a[2]-a[1])*(a[4]-a[3]), false
	case "noise":
		return Noise1(a[0]), false
	case "noise2":
		return Noise2(a[0], a[1]), false
	case "rand":
		return env.Rand(false), false
	case "random":
		return env.Rand(true), false
	case "is_nil":
		if anyNil {
			return 1, false
		}
		return 0, false
	}
	return 0, true
}

// hash: a number 0..1 from an integer (as exprHash in render.comp)
func hash(n uint32) float64 {
	n = (n << 13) ^ n
	n = n*(n*n*15731+789221) + 1376312589
	return float64(n&0x7fffffff) / float64(0x7fffffff)
}

// Noise1: smooth value noise 0..1 (as exprNoise in render.comp)
func Noise1(x float64) float64 {
	i := math.Floor(x)
	f := x - i
	u := f * f * (3 - 2*f)
	a, b := hash(uint32(int32(i))), hash(uint32(int32(i)+1))
	return a + (b-a)*u
}

// Noise2: smooth value noise 0..1 in 2D
func Noise2(x, y float64) float64 {
	ix, iy := math.Floor(x), math.Floor(y)
	fx, fy := x-ix, y-iy
	ux, uy := fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	h := func(dx, dy int32) float64 { return hash(uint32(int32(ix)+dx) ^ uint32(int32(iy)+dy)*2654435761) }
	a, b, c, d := h(0, 0), h(1, 0), h(0, 1), h(1, 1)
	return a + (b-a)*ux + (c-a)*uy + (a-b-c+d)*ux*uy
}

// Specialize: the program for one frame, for running it per item (per particle, on the GPU and
// the CPU): the audio, the variables and the values that don't change per item worked out now
// (env), so that what is left only reads index, count, rand() / random() and the values perItem
// says (kind, name) change per item. The code keeps its length (jumps stay right).
func (p *Program) Specialize(env Env, perItem func(kind int, name string) bool) *Program {
	q := &Program{Code: append([]int32(nil), p.Code...), Consts: append([]float64(nil), p.Consts...),
		Keys: p.Keys, Slots: p.Slots, Stack: p.Stack, UsesValue: p.UsesValue, PerItem: p.PerItem}
	konst := func(v float64) int32 {
		q.Consts = append(q.Consts, v)
		return int32(len(q.Consts) - 1)
	}
	prev := -1
	for pc := 0; pc < len(q.Code); {
		op := int(q.Code[pc])
		n := 1 + opArgs[op]
		switch op {
		case OpVar:
			if v := int(q.Code[pc+1]); v != VarIndex {
				q.Code[pc], q.Code[pc+1] = OpConst, konst(env.Var(v))
			}
		case OpBand:
			q.Code[pc], q.Code[pc+1] = OpConst, konst(env.Band(int(q.Code[pc+1]), 0))
		case OpBandAt, OpLag, OpPeak:
			// its argument when it is known now (else now, 0 s)
			b, a := int(q.Code[pc+1]), 0.0
			if prev >= 0 && q.Code[prev] == OpConst {
				a = q.Consts[q.Code[prev+1]]
			}
			var v float64
			switch op {
			case OpBandAt:
				v = env.Band(b, a)
			case OpLag:
				v = env.Lag(b, a)
			default:
				v = env.Peak(b, a)
			}
			q.Code[pc], q.Code[pc+1] = OpPopPush, konst(v)
		case OpKey:
			kind, name := int(q.Code[pc+1]), p.Keys[q.Code[pc+2]]
			if !perItem(kind, name) {
				if v, ok := env.Key(kind, name); ok {
					q.Code[pc], q.Code[pc+1], q.Code[pc+2] = OpConst, konst(v), OpNop
				} else {
					q.Code[pc], q.Code[pc+1], q.Code[pc+2] = OpNil, OpNop, OpNop
				}
			}
		}
		prev = pc
		pc += n
	}
	return q
}

// ItemRand: an item's random number 0..1 (as exprItemRand in emit.comp): stable for the element
// (seed) and the item, or a new one each frame
func ItemRand(seed uint32, index int, perFrame bool, frame int) float64 {
	n := seed ^ uint32(index)*2654435761
	if perFrame {
		n ^= uint32(frame)*40503 + 0x9e3779b9
	}
	return hash(n)
}
