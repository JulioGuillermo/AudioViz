package expr

// The syntax tree of an expression

type node struct {
	kind string // num nil var user key at un bin call user-call if ret
	tok  token
	op   string
	num  float64
	name string // var / band / function / key name
	sub  string // key: path / route
	kids []*node
	// if: arms (cond, then) in kids pairs, the else last when odd
}

type stmt struct {
	let string // let name = e (empty: an expression)
	e   *node
	tok token
}

// funcDef: a function of the project or of the expression: $name(params) = body
type funcDef struct {
	name   string
	params []string
	body   []stmt
	tok    token
}

// source: what a text holds: functions, then the statements (the last one is the result)
type source struct {
	funcs []*funcDef
	body  []stmt
}

type parser struct {
	t []token
	i int
}

func (p *parser) peek() token { return p.t[p.i] }
func (p *parser) next() token {
	t := p.t[p.i]
	if t.k != tEOF {
		p.i++
	}
	return t
}
func (p *parser) is(k tokKind, s string) bool {
	t := p.peek()
	return t.k == k && (s == "" || t.s == s)
}
func (p *parser) expect(k tokKind, s string) (token, *Error) {
	t := p.peek()
	if t.k != k || (s != "" && t.s != s) {
		want := s
		if want == "" {
			want = map[tokKind]string{tNum: "a number", tIdent: "a name", tUser: "a $name"}[k]
		}
		return t, errAt(t, "expected %s here", want)
	}
	return p.next(), nil
}

// parse reads a whole text: function definitions ($f(a) = …, their body indented under them)
// and statements (let x = …, expressions), one per line
func parse(src string) (*source, *Error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{t: toks}
	out := &source{}
	for {
		for p.is(tNL, "") || p.is(tIndent, "") {
			p.next()
		}
		if p.is(tEOF, "") {
			return out, nil
		}
		if p.isFuncDef() {
			f, err := p.funcDef()
			if err != nil {
				return nil, err
			}
			out.funcs = append(out.funcs, f)
			continue
		}
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		out.body = append(out.body, s)
		if !p.is(tNL, "") && !p.is(tEOF, "") {
			return nil, errAt(p.peek(), "unexpected %q: one expression per line", p.peek().s)
		}
	}
}

// isFuncDef: $name( … ) = at the start of a line
func (p *parser) isFuncDef() bool {
	if !p.is(tUser, "") || p.t[p.i+1].k != tPunct || p.t[p.i+1].s != "(" {
		return false
	}
	depth := 0
	for j := p.i + 1; j < len(p.t); j++ {
		t := p.t[j]
		if t.k == tPunct && t.s == "(" {
			depth++
		} else if t.k == tPunct && t.s == ")" {
			depth--
			if depth == 0 {
				n := p.t[j+1]
				return n.k == tOp && n.s == "="
			}
		} else if t.k == tNL || t.k == tEOF {
			return false
		}
	}
	return false
}

func (p *parser) funcDef() (*funcDef, *Error) {
	nt := p.next()
	f := &funcDef{name: nt.s, tok: nt}
	p.next() // (
	for !p.is(tPunct, ")") {
		a, err := p.expect(tIdent, "")
		if err != nil {
			return nil, errAt(p.peek(), "a parameter's name (without $) expected")
		}
		f.params = append(f.params, a.s)
		if p.is(tPunct, ",") {
			p.next()
		} else if !p.is(tPunct, ")") {
			return nil, errAt(p.peek(), "expected , or )")
		}
	}
	p.next() // )
	p.next() // =
	if !p.is(tNL, "") && !p.is(tEOF, "") {
		// one line: $f(x) = x * 2
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		f.body = append(f.body, s)
	}
	// the lines indented under it
	for p.is(tNL, "") && p.t[p.i+1].k == tIndent {
		p.next()
		p.next()
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		f.body = append(f.body, s)
	}
	if len(f.body) == 0 {
		return nil, errAt(nt, "%s has no body", nt.s)
	}
	return f, nil
}

func (p *parser) stmt() (stmt, *Error) {
	t := p.peek()
	if t.k == tIdent && t.s == "let" {
		p.next()
		n, err := p.expect(tIdent, "")
		if err != nil {
			return stmt{}, errAt(p.peek(), "a name expected after let")
		}
		if _, err := p.expect(tOp, "="); err != nil {
			return stmt{}, err
		}
		e, err2 := p.expr()
		if err2 != nil {
			return stmt{}, err2
		}
		return stmt{let: n.s, e: e, tok: n}, nil
	}
	e, err := p.expr()
	return stmt{e: e, tok: t}, err
}

func (p *parser) expr() (*node, *Error) { return p.binary(0) }

// binary operators by precedence (low to high)
var prec = []map[string]bool{
	{"??": true},
	{"||": true, "or": true},
	{"&&": true, "and": true},
	{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true},
	{"+": true, "-": true},
	{"*": true, "/": true, "%": true},
}

func (p *parser) binary(level int) (*node, *Error) {
	if level == len(prec) {
		return p.unary()
	}
	l, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if !((t.k == tOp || t.k == tIdent) && prec[level][t.s]) {
			return l, nil
		}
		p.next()
		r, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		op := t.s
		if op == "or" {
			op = "||"
		} else if op == "and" {
			op = "&&"
		}
		l = &node{kind: "bin", op: op, tok: t, kids: []*node{l, r}}
		if level == 3 {
			// comparisons don't chain
			if nt := p.peek(); nt.k == tOp && prec[3][nt.s] {
				return nil, errAt(nt, "comparisons don't chain: use && between them")
			}
		}
	}
}

func (p *parser) unary() (*node, *Error) {
	t := p.peek()
	if (t.k == tOp && (t.s == "-" || t.s == "!")) || (t.k == tIdent && t.s == "not") {
		p.next()
		e, err := p.unary()
		if err != nil {
			return nil, err
		}
		op := t.s
		if op == "not" {
			op = "!"
		}
		return &node{kind: "un", op: op, tok: t, kids: []*node{e}}, nil
	}
	return p.power()
}

func (p *parser) power() (*node, *Error) {
	b, err := p.postfix()
	if err != nil {
		return nil, err
	}
	if p.is(tOp, "^") {
		t := p.next()
		e, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &node{kind: "bin", op: "^", tok: t, kids: []*node{b, e}}, nil
	}
	return b, nil
}

// postfix: a band some seconds before or after (bass@-0.5)
func (p *parser) postfix() (*node, *Error) {
	e, err := p.primary()
	if err != nil {
		return nil, err
	}
	if p.is(tOp, "@") {
		t := p.next()
		if e.kind != "var" {
			return nil, errAt(t, "@ goes after an audio band (bass@-0.5)")
		}
		off, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &node{kind: "at", tok: t, name: e.name, kids: []*node{off}}, nil
	}
	return e, nil
}

func (p *parser) args() ([]*node, *Error) {
	p.next() // (
	var out []*node
	for !p.is(tPunct, ")") {
		if p.is(tEOF, "") {
			return nil, errAt(p.peek(), "a ) is missing")
		}
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.is(tOp, "->") {
			// an arm of if: cond -> then (kept as a pair)
			t := p.next()
			th, err := p.expr()
			if err != nil {
				return nil, err
			}
			e = &node{kind: "arm", tok: t, kids: []*node{e, th}}
		}
		out = append(out, e)
		if p.is(tPunct, ",") {
			p.next()
		} else if !p.is(tPunct, ")") {
			return nil, errAt(p.peek(), "expected , or )")
		}
	}
	p.next()
	return out, nil
}

func (p *parser) primary() (*node, *Error) {
	t := p.next()
	switch t.k {
	case tNum:
		return &node{kind: "num", num: t.num, tok: t}, nil
	case tPunct:
		if t.s == "(" {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tPunct, ")"); err != nil {
				return nil, err
			}
			return e, nil
		}
	case tUser:
		if p.is(tPunct, "(") {
			a, err := p.args()
			if err != nil {
				return nil, err
			}
			return &node{kind: "user-call", name: t.s, tok: t, kids: a}, nil
		}
		return &node{kind: "user", name: t.s, tok: t}, nil
	case tIdent:
		switch t.s {
		case "nil":
			return &node{kind: "nil", tok: t}, nil
		case "true":
			return &node{kind: "num", num: 1, tok: t}, nil
		case "false":
			return &node{kind: "num", num: 0, tok: t}, nil
		}
		if p.is(tPunct, "(") {
			a, err := p.args()
			if err != nil {
				return nil, err
			}
			k := "call"
			if t.s == "if" {
				k = "if"
			} else if t.s == "ret" {
				k = "ret"
			}
			return &node{kind: k, name: t.s, tok: t, kids: a}, nil
		}
		if p.is(tOp, ".") {
			p.next()
			k, err := p.expect(tIdent, "")
			if err != nil {
				return nil, errAt(p.peek(), "a value's name expected after %s.", t.s)
			}
			return &node{kind: "key", sub: t.s, name: k.s, tok: t}, nil
		}
		return &node{kind: "var", name: t.s, tok: t}, nil
	}
	if t.k == tEOF {
		return nil, errAt(t, "the expression ends too soon")
	}
	return nil, errAt(t, "unexpected %q", t.s)
}
