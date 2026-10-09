// Package expr is the language of the properties' advanced settings: an expression gives a
// property's value from its own (value), the audio, the time, the values on the path it goes
// along… and the project's own constants and functions ($name). It is compiled to a small
// bytecode run by the same interpreter on the CPU (vm.go) and the GPU (render.comp), so that
// both give the same result. No loops: an expression always ends, and quickly.
//
//	value * (1 + lag(bass, 0.2))                       the slider's value, pushed by the bass
//	if(kick > 0.7 -> value * 2, bass > 0.4 -> 1.5)     nil when nothing matches (0 in math)
//	if(kick > 0.7 -> value * 2) ?? value               ?? gives a value for nil
//	sin(project_time * 90) * 20 + $wobble(path.glow)           degrees; $… are the project's
//
// Functions (the project's, or before the expression's last line), a body of lines indented
// under them: let steps, ret(x) to give a result early, the last line the result.
//
//	$pulse(x, strength) =
//	  if(strength <= 0 -> ret(x))
//	  let k = lag(kick, 0.2) * strength
//	  x * (1 + k)
package expr

import (
	"fmt"
	"strings"
	"unicode"
)

// token kinds
type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tIdent  // a name of ours: value, bass, sin, if, let…
	tUser   // $name: the project's or the expression's own
	tOp     // + - * / % ^ == != < <= > >= && || ! ?? -> = @ .
	tPunct  // ( ) ,
	tNL     // end of a line (statement)
	tIndent // a line that starts with spaces (inside a function's body)
)

type token struct {
	k     tokKind
	s     string
	num   float64
	pos   int // byte offset
	line  int
	isInt bool
}

// Error is a mistake in an expression, with where it is (for the editor to underline)
type Error struct {
	Pos  int    `json:"pos"`  // byte offset
	Line int    `json:"line"` // 0-based (−1: not in this text, e.g. the project's functions)
	Msg  string `json:"msg"`
	// Msg's format and its values (%s, %d, %q in order), for the studio to translate it
	Fmt  string   `json:"fmt,omitempty"`
	Args []string `json:"args,omitempty"`
}

// newErr: an Error at pos / line from a format and its values
func newErr(pos, line int, f string, a ...any) *Error {
	args := make([]string, len(a))
	for i, x := range a {
		args[i] = fmt.Sprint(x)
	}
	return &Error{Pos: pos, Line: line, Msg: fmt.Sprintf(f, a...), Fmt: f, Args: args}
}

func (e *Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line+1, e.Msg) }

func errAt(t token, f string, a ...any) *Error { return newErr(t.pos, t.line, f, a...) }

// lex splits src into tokens; newlines inside brackets don't end a statement; a line starting with
// spaces or tabs gives a tIndent before its first token; # starts a comment to the line's end
func lex(src string) ([]token, *Error) {
	var out []token
	depth, line := 0, 0
	atLineStart := true
	i := 0
	for i < len(src) {
		c := src[i]
		if atLineStart {
			atLineStart = false
			j := i
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j > i && j < len(src) && src[j] != '\n' && src[j] != '#' && depth == 0 {
				out = append(out, token{k: tIndent, pos: i, line: line})
			}
			i = j
			continue
		}
		switch {
		case c == '\n':
			if depth == 0 && len(out) > 0 && out[len(out)-1].k != tNL {
				out = append(out, token{k: tNL, pos: i, line: line})
			}
			line++
			atLineStart = true
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == ';':
			if depth == 0 {
				out = append(out, token{k: tNL, pos: i, line: line})
			}
			i++
		case c >= '0' && c <= '9' || (c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9'):
			j := i
			isInt := true
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				if src[j] == '.' {
					isInt = false
				}
				j++
			}
			var v float64
			if _, err := fmt.Sscanf(src[i:j], "%g", &v); err != nil {
				return nil, newErr(i, line, "a number is wrong: %s", src[i:j])
			}
			out = append(out, token{k: tNum, s: src[i:j], num: v, pos: i, line: line, isInt: isInt})
			i = j
		case c == '$' || c == '_' || unicode.IsLetter(rune(c)):
			j := i + 1
			for j < len(src) && (src[j] == '_' || src[j] >= '0' && src[j] <= '9' || unicode.IsLetter(rune(src[j]))) {
				j++
			}
			k := tIdent
			if c == '$' {
				k = tUser
				if j == i+1 {
					return nil, newErr(i, line, "a name is missing after $")
				}
			}
			out = append(out, token{k: k, s: src[i:j], pos: i, line: line})
			i = j
		case c == '(' || c == ')' || c == ',':
			if c == '(' {
				depth++
			} else if c == ')' {
				depth = max(0, depth-1)
			}
			out = append(out, token{k: tPunct, s: string(c), pos: i, line: line})
			i++
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||", "??", "->":
				out = append(out, token{k: tOp, s: two, pos: i, line: line})
				i += 2
				continue
			}
			if strings.ContainsRune("+-*/%^<>!=@.", rune(c)) {
				out = append(out, token{k: tOp, s: string(c), pos: i, line: line})
				i++
				continue
			}
			return nil, newErr(i, line, "unexpected %q", string(c))
		}
	}
	out = append(out, token{k: tEOF, pos: len(src), line: line})
	return out, nil
}
