package expr

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The bytecode: a stack of values (a number or nil); each op is its code then its operands. The
// GPU runs the same (exprRun in render.comp), so the ops and their numbers must not change.
const (
	OpConst  = iota // c: push Consts[c]
	OpNil           // push nil
	OpVar           // v: push a variable (Vars)
	OpBand          // b: push an audio band now (Bands)
	OpBandAt        // b: pop dt, push the band dt s from now
	OpLag           // b: pop s, push the band smoothed over s s
	OpPeak          // b: pop s, push the band's loudest of the last s s
	OpKey           // kind k: push a value of the path (0) or route (1) by Keys[k] (nil if none)
	OpLoad          // slot
	OpStore         // slot: pop into it
	OpPop           //
	OpAdd           // the binary ops pop b, a and push a op b (nil counts as 0)
	OpSub
	OpMul
	OpDiv
	OpMod
	OpPow
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpNeg
	OpNot
	OpTruthy   // 1 if it is more than 0 (and not nil), else 0
	OpJmp      // addr
	OpJmpF     // addr: pop; jump if it is not true
	OpCoalesce // addr: if the top is not nil jump (keeping it), else pop it
	OpRetTo    // base addr: keep the top, drop the stack down to base, jump
	OpCall     // fn: a built-in function (Funcs) on its arguments
	OpEnd      // the result: the top
)

// opArgs: how many operands each op has
var opArgs = [...]int{OpConst: 1, OpNil: 0, OpVar: 1, OpBand: 1, OpBandAt: 1, OpLag: 1, OpPeak: 1, OpKey: 2,
	OpLoad: 1, OpStore: 1, OpPop: 0, OpAdd: 0, OpSub: 0, OpMul: 0, OpDiv: 0, OpMod: 0, OpPow: 0, OpEq: 0,
	OpNe: 0, OpLt: 0, OpLe: 0, OpGt: 0, OpGe: 0, OpNeg: 0, OpNot: 0, OpTruthy: 0, OpJmp: 1, OpJmpF: 1,
	OpCoalesce: 1, OpRetTo: 2, OpCall: 1, OpEnd: 0}

// Vars: the variables of ours, in the order OpVar numbers them: the slider's value; the
// project's time (s) and progress (0…1 of the song); the element's (clip_…: from its start, of
// its length); the frame, its rate; per item: its index and how many
var Vars = []string{"value", "project_time", "clip_time", "clip_progress", "frame", "fps", "index", "count", "project_progress"}

// varAlias: short names, both of the whole project
var varAlias = map[string]string{"time": "project_time", "progress": "project_progress"}

// Var indexes
const (
	VarValue = iota
	VarProjectTime
	VarClipTime
	VarClipProgress
	VarFrame
	VarFPS
	VarIndex
	VarCount
	VarProjectProgress
)

// Bands: the audio bands (as react_to), in the order OpBand numbers them
var Bands = []string{"bass", "mid", "treble", "volume", "transient", "beat", "drums", "kick", "snare", "hats",
	"bassline", "vocals", "other"}

var bandAlias = map[string]string{"overall": "volume", "instruments": "other", "mids": "mid", "hihat": "hats"}

// Funcs: the built-in functions (angles in degrees; rad_… in radians), in the order OpCall
// numbers them, with how many arguments they take
var Funcs = []struct {
	Name string
	Args int
}{
	{"sin", 1}, {"cos", 1}, {"tan", 1}, {"asin", 1}, {"acos", 1}, {"atan", 1}, {"atan2", 2},
	{"rad_sin", 1}, {"rad_cos", 1}, {"rad_tan", 1}, {"rad_asin", 1}, {"rad_acos", 1}, {"rad_atan", 1}, {"rad_atan2", 2},
	{"deg", 1}, {"rad", 1}, {"abs", 1}, {"sign", 1}, {"floor", 1}, {"ceil", 1}, {"round", 1}, {"fract", 1},
	{"sqrt", 1}, {"exp", 1}, {"log", 1}, {"pow", 2}, {"mod", 2}, {"min", 2}, {"max", 2}, {"clamp", 3},
	{"mix", 3}, {"smoothstep", 3}, {"step", 2}, {"map", 5}, {"noise", 1}, {"noise2", 2}, {"rand", 0},
	{"random", 0}, {"is_nil", 1},
}

var funcAlias = map[string]string{"lerp": "mix"}

// consts of ours
var builtinConsts = map[string]float64{"pi": math.Pi, "tau": 2 * math.Pi, "e": math.E}

// Program is a compiled expression
type Program struct {
	Code   []int32
	Consts []float64
	Keys   []string
	Slots  int // locals (let, parameters)
	Stack  int // the deepest the stack goes
	// what it uses: the slider's value; something that changes per particle, letter or pixel
	// (index, count, rand, a route's values); the time; the audio
	UsesValue, PerItem, UsesTime, UsesAudio bool
}

// Library: the project's own: constants and functions (source text of $f(…) = … definitions)
type Library struct {
	Consts map[string]float64
	Funcs  string
}

type compiler struct {
	prog   *Program
	funcs  map[string]*funcDef
	consts map[string]float64
	depth  int
	calls  []string // functions being inlined (no recursion)
	// the scope: names → slots; ret's target and the stack depth it goes back to
	scope   map[string]int
	retBase int
	retFix  *[]int // jumps to patch to the end of the current function (or program)
}

// Compile turns an expression (with its own functions before it) into a program, with the
// project's library
func Compile(src string, lib *Library) (*Program, error) {
	s, err := parse(src)
	if err != nil {
		return nil, err
	}
	c := &compiler{prog: &Program{}, funcs: map[string]*funcDef{}, consts: map[string]float64{}, scope: map[string]int{}}
	if lib != nil {
		for k, v := range lib.Consts {
			c.consts["$"+strings.TrimPrefix(k, "$")] = v
		}
		if strings.TrimSpace(lib.Funcs) != "" {
			ls, err := parse(lib.Funcs)
			if err != nil {
				return nil, fmt.Errorf("the project's functions: %w", err)
			}
			if len(ls.body) > 0 {
				return nil, fmt.Errorf("the project's functions: only $name(…) = … definitions")
			}
			for _, f := range ls.funcs {
				c.funcs[f.name] = f
			}
		}
	}
	for _, f := range s.funcs {
		c.funcs[f.name] = f
	}
	if len(s.body) == 0 {
		return nil, &Error{Msg: "it is empty: write an expression (value is the slider's)"}
	}
	var fix []int
	c.retFix = &fix
	if err := c.body(s.body); err != nil {
		return nil, err
	}
	for _, at := range fix {
		c.prog.Code[at] = int32(len(c.prog.Code))
	}
	c.emit(OpEnd)
	return c.prog, nil
}

func (c *compiler) emit(op int, args ...int) int {
	c.prog.Code = append(c.prog.Code, int32(op))
	for _, a := range args {
		c.prog.Code = append(c.prog.Code, int32(a))
	}
	switch op {
	case OpConst, OpNil, OpVar, OpBand, OpKey, OpLoad:
		c.push(1)
	case OpStore, OpPop, OpJmpF, OpAdd, OpSub, OpMul, OpDiv, OpMod, OpPow, OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		c.push(-1)
	}
	return len(c.prog.Code) - 1 // the last operand (to patch jumps)
}

func (c *compiler) push(n int) {
	c.depth += n
	if c.depth > c.prog.Stack {
		c.prog.Stack = c.depth
	}
}

func (c *compiler) konst(v float64) int {
	for i, x := range c.prog.Consts {
		if x == v {
			return i
		}
	}
	c.prog.Consts = append(c.prog.Consts, v)
	return len(c.prog.Consts) - 1
}

// body: statements; the last one's value is left on the stack
func (c *compiler) body(b []stmt) error {
	for i, s := range b {
		last := i == len(b)-1
		if s.let != "" {
			if err := c.expr(s.e); err != nil {
				return err
			}
			slot := c.prog.Slots
			c.prog.Slots++
			c.emit(OpStore, slot)
			c.scope[s.let] = slot
			if last {
				c.emit(OpLoad, slot)
			}
			continue
		}
		if err := c.expr(s.e); err != nil {
			return err
		}
		if !last {
			c.emit(OpPop)
		}
	}
	return nil
}

func (c *compiler) expr(n *node) error {
	switch n.kind {
	case "num":
		c.emit(OpConst, c.konst(n.num))
	case "nil":
		c.emit(OpNil)
	case "var":
		return c.name(n)
	case "user":
		if slot, ok := c.scope[n.name]; ok {
			c.emit(OpLoad, slot)
			return nil
		}
		v, ok := c.consts[n.name]
		if !ok {
			return errAt(n.tok, "%s: no such constant (Project → Functions and constants)", n.name)
		}
		c.emit(OpConst, c.konst(v))
	case "key":
		kind := map[string]int{"path": 0, "route": 1}
		k, ok := kind[n.sub]
		if !ok {
			return errAt(n.tok, "%s.%s: only path.name and route.name have values", n.sub, n.name)
		}
		if k == 1 {
			c.prog.PerItem = true
		}
		ki := -1
		for i, x := range c.prog.Keys {
			if x == n.name {
				ki = i
			}
		}
		if ki < 0 {
			c.prog.Keys = append(c.prog.Keys, n.name)
			ki = len(c.prog.Keys) - 1
		}
		c.emit(OpKey, k, ki)
	case "at":
		b, ok := bandIndex(n.name)
		if !ok {
			return errAt(n.tok, "@ goes after an audio band (bass, kick…), not %s", n.name)
		}
		if err := c.expr(n.kids[0]); err != nil {
			return err
		}
		c.prog.UsesAudio = true
		c.emit(OpBandAt, b)
	case "un":
		if err := c.expr(n.kids[0]); err != nil {
			return err
		}
		if n.op == "-" {
			c.emit(OpNeg)
		} else {
			c.emit(OpNot)
		}
	case "bin":
		return c.binary(n)
	case "if":
		return c.ifExpr(n)
	case "ret":
		if len(n.kids) != 1 {
			return errAt(n.tok, "ret takes one value: ret(x)")
		}
		if err := c.expr(n.kids[0]); err != nil {
			return err
		}
		at := c.emit(OpRetTo, c.retBase, 0)
		*c.retFix = append(*c.retFix, at)
	case "call":
		return c.call(n)
	case "user-call":
		return c.userCall(n)
	case "arm":
		return errAt(n.tok, "-> only goes inside if( … )")
	default:
		return errAt(n.tok, "unexpected")
	}
	return nil
}

func bandIndex(name string) (int, bool) {
	if a, ok := bandAlias[name]; ok {
		name = a
	}
	for i, b := range Bands {
		if b == name {
			return i, true
		}
	}
	return 0, false
}

// name: a variable, an audio band or a constant of ours
func (c *compiler) name(n *node) error {
	if slot, ok := c.scope[n.name]; ok {
		c.emit(OpLoad, slot)
		return nil
	}
	name := n.name
	if a, ok := varAlias[name]; ok {
		name = a
	}
	for i, v := range Vars {
		if v == name {
			switch i {
			case VarValue:
				c.prog.UsesValue = true
			case VarIndex, VarCount:
				c.prog.PerItem = true
			case VarProjectTime, VarClipTime, VarClipProgress, VarFrame, VarProjectProgress:
				c.prog.UsesTime = true
			}
			c.emit(OpVar, i)
			return nil
		}
	}
	if b, ok := bandIndex(n.name); ok {
		c.prog.UsesAudio = true
		c.emit(OpBand, b)
		return nil
	}
	if v, ok := builtinConsts[n.name]; ok {
		c.emit(OpConst, c.konst(v))
		return nil
	}
	if _, ok := c.consts["$"+n.name]; ok {
		return errAt(n.tok, "%s: your constants go with $: $%s", n.name, n.name)
	}
	return errAt(n.tok, "%s: unknown name", n.name)
}

var binOps = map[string]int{"+": OpAdd, "-": OpSub, "*": OpMul, "/": OpDiv, "%": OpMod, "^": OpPow,
	"==": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe}

func (c *compiler) binary(n *node) error {
	switch n.op {
	case "&&", "||":
		// a && b: if a then (b is true) else 0; a || b: if a then 1 else (b is true)
		if err := c.expr(n.kids[0]); err != nil {
			return err
		}
		jf := c.emit(OpJmpF, 0)
		d := c.depth
		if n.op == "&&" {
			if err := c.expr(n.kids[1]); err != nil {
				return err
			}
			c.emit(OpTruthy)
		} else {
			c.emit(OpConst, c.konst(1))
		}
		j := c.emit(OpJmp, 0)
		c.prog.Code[jf] = int32(len(c.prog.Code))
		c.depth = d
		if n.op == "&&" {
			c.emit(OpConst, c.konst(0))
		} else {
			if err := c.expr(n.kids[1]); err != nil {
				return err
			}
			c.emit(OpTruthy)
		}
		c.prog.Code[j] = int32(len(c.prog.Code))
		return nil
	case "??":
		if err := c.expr(n.kids[0]); err != nil {
			return err
		}
		j := c.emit(OpCoalesce, 0)
		c.push(-1) // popped when nil
		if err := c.expr(n.kids[1]); err != nil {
			return err
		}
		c.prog.Code[j] = int32(len(c.prog.Code))
		return nil
	}
	for _, k := range n.kids {
		if err := c.expr(k); err != nil {
			return err
		}
	}
	c.emit(binOps[n.op])
	return nil
}

// if(c1 -> a, c2 -> b, …, else): the first true arm, else the last value (nil without one)
func (c *compiler) ifExpr(n *node) error {
	if len(n.kids) == 0 {
		return errAt(n.tok, "if needs arms: if(cond -> value, …, else)")
	}
	var ends []int
	d := c.depth
	hasElse := false
	for i, a := range n.kids {
		if a.kind != "arm" {
			if i != len(n.kids)-1 {
				return errAt(a.tok, "only the last value of if can go without -> (the else)")
			}
			c.depth = d
			if err := c.expr(a); err != nil {
				return err
			}
			hasElse = true
			break
		}
		c.depth = d
		if err := c.expr(a.kids[0]); err != nil {
			return err
		}
		jf := c.emit(OpJmpF, 0)
		if err := c.expr(a.kids[1]); err != nil {
			return err
		}
		ends = append(ends, c.emit(OpJmp, 0))
		c.prog.Code[jf] = int32(len(c.prog.Code))
	}
	if !hasElse {
		c.depth = d
		c.emit(OpNil)
	}
	for _, e := range ends {
		c.prog.Code[e] = int32(len(c.prog.Code))
	}
	c.depth = d + 1
	return nil
}

func (c *compiler) call(n *node) error {
	name := n.name
	if a, ok := funcAlias[name]; ok {
		name = a
	}
	if name == "lag" || name == "peak" {
		if len(n.kids) != 2 || n.kids[0].kind != "var" {
			return errAt(n.tok, "%s(band, seconds): e.g. %s(bass, 0.3)", name, name)
		}
		b, ok := bandIndex(n.kids[0].name)
		if !ok {
			return errAt(n.kids[0].tok, "%s is not an audio band", n.kids[0].name)
		}
		if err := c.expr(n.kids[1]); err != nil {
			return err
		}
		c.prog.UsesAudio = true
		if name == "lag" {
			c.emit(OpLag, b)
		} else {
			c.emit(OpPeak, b)
		}
		return nil
	}
	fi := -1
	for i, f := range Funcs {
		if f.Name == name {
			fi = i
		}
	}
	if fi < 0 {
		if _, ok := c.funcs["$"+name]; ok {
			return errAt(n.tok, "%s: your functions go with $: $%s(…)", name, name)
		}
		return errAt(n.tok, "%s: no such function", name)
	}
	want := Funcs[fi].Args
	if (name == "min" || name == "max") && len(n.kids) > 2 {
		// min(a, b, c…): one after the other
		if err := c.expr(n.kids[0]); err != nil {
			return err
		}
		for _, k := range n.kids[1:] {
			if err := c.expr(k); err != nil {
				return err
			}
			c.emit(OpCall, fi)
			c.push(-1)
		}
		return nil
	}
	if len(n.kids) != want {
		return errAt(n.tok, "%s takes %d values, not %d", name, want, len(n.kids))
	}
	for _, k := range n.kids {
		if err := c.expr(k); err != nil {
			return err
		}
	}
	if name == "rand" || name == "random" {
		c.prog.PerItem = c.prog.PerItem || name == "rand"
		c.prog.UsesTime = c.prog.UsesTime || name == "random"
	}
	c.emit(OpCall, fi)
	c.push(1 - want)
	return nil
}

// userCall: a function of the project's or the expression's, inlined (they can't call
// themselves): its arguments into slots, its body, its ret's to its end
func (c *compiler) userCall(n *node) error {
	f, ok := c.funcs[n.name]
	if !ok {
		if _, isConst := c.consts[n.name]; isConst {
			return errAt(n.tok, "%s is a constant, not a function", n.name)
		}
		return errAt(n.tok, "%s: no such function (Project → Functions and constants)", n.name)
	}
	for _, x := range c.calls {
		if x == f.name {
			return errAt(n.tok, "%s can't call itself (no loops: it always ends)", f.name)
		}
	}
	if len(n.kids) != len(f.params) {
		return errAt(n.tok, "%s takes %d values, not %d", f.name, len(f.params), len(n.kids))
	}
	slots := make([]int, len(f.params))
	for i, k := range n.kids {
		if err := c.expr(k); err != nil {
			return err
		}
		slots[i] = c.prog.Slots
		c.prog.Slots++
		c.emit(OpStore, slots[i])
	}
	saveScope, saveBase, saveFix := c.scope, c.retBase, c.retFix
	c.scope = map[string]int{}
	for i, p := range f.params {
		c.scope[p] = slots[i]
	}
	c.retBase = c.depth
	var fix []int
	c.retFix = &fix
	c.calls = append(c.calls, f.name)
	err := c.body(f.body)
	c.calls = c.calls[:len(c.calls)-1]
	for _, at := range fix {
		c.prog.Code[at] = int32(len(c.prog.Code))
	}
	c.scope, c.retBase, c.retFix = saveScope, saveBase, saveFix
	if err != nil {
		var e *Error
		if ee, ok := err.(*Error); ok {
			e = ee
			return &Error{Pos: n.tok.pos, Line: n.tok.line, Msg: fmt.Sprintf("in %s: %s", f.name, e.Msg)}
		}
		return err
	}
	return nil
}

// Names: everything the editor can complete: variables, bands, functions, constants of ours
func Names() (vars, bands, funcs, consts []string) {
	for _, f := range Funcs {
		funcs = append(funcs, f.Name)
	}
	funcs = append(funcs, "lag", "peak", "if", "ret", "lerp")
	for k := range builtinConsts {
		consts = append(consts, k)
	}
	sort.Strings(consts)
	return append([]string(nil), Vars...), append([]string(nil), Bands...), funcs, consts
}
