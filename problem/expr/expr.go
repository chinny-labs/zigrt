// SPDX-License-Identifier: Apache-2.0

// Package expr is the small expression language problem templates are
// written in (ADR 9). Numbers are exact rationals, so 1/3 + 1/6 is exactly
// 1/2 and 0.1 + 0.2 is exactly 0.3. There are no loops, no I/O and no way to
// define functions; every evaluation has a step budget and a size limit, so a
// content pack can never make the server do unbounded work.
//
// Grammar, loosest binding first:
//
//	expr   = or ["?" expr ":" expr]
//	or     = and {"||" and}
//	and    = cmp {"&&" cmp}
//	cmp    = sum [("==" | "!=" | "<" | "<=" | ">" | ">=") sum]
//	sum    = prod {("+" | "-") prod}
//	prod   = unary {("*" | "/" | "//" | "%") unary}
//	unary  = ("-" | "!") unary | pow
//	pow    = atom ["^" unary]
//	atom   = number | string | name | name "(" [expr {"," expr}] ")" | "(" expr ")"
//
// "/" is exact division, "//" is whole-number division rounding down, and
// "%" is the remainder that goes with it.
package expr

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Limits on what one evaluation may do.
const (
	// MaxSteps bounds the operations in one evaluation.
	MaxSteps = 10000
	// MaxBits bounds the size of any numerator or denominator (about 15
	// decimal digits), far beyond any K-5 problem.
	MaxBits    = 50
	maxExprLen = 2000
)

// Kind is the type of a value.
type Kind int

const (
	Number Kind = iota
	Bool
	String
)

func (k Kind) String() string {
	return [...]string{"number", "true/false", "text"}[k]
}

// Value is a number, a truth value or a piece of text.
type Value struct {
	Kind Kind
	Num  *big.Rat
	B    bool
	S    string
}

// N makes a number value from an integer.
func N(n int64) Value { return Value{Kind: Number, Num: big.NewRat(n, 1)} }

// R makes a number value from a fraction.
func R(num, den int64) Value { return Value{Kind: Number, Num: big.NewRat(num, den)} }

// Str makes a text value.
func Str(s string) Value { return Value{Kind: String, S: s} }

// IsInt reports whether a value is a whole number.
func (v Value) IsInt() bool { return v.Kind == Number && v.Num.IsInt() }

// Int returns a whole number as an int64, and whether it was one that fits.
func (v Value) Int() (int64, bool) {
	if !v.IsInt() || !v.Num.Num().IsInt64() {
		return 0, false
	}
	return v.Num.Num().Int64(), true
}

// String formats a value the way a template shows it by default: whole
// numbers plainly, other numbers as a fraction like 3/4, text as itself.
func (v Value) String() string {
	switch v.Kind {
	case Bool:
		if v.B {
			return "true"
		}
		return "false"
	case String:
		return v.S
	}
	if v.Num.IsInt() {
		return v.Num.Num().String()
	}
	return v.Num.String()
}

// Env holds the named values an expression can read.
type Env map[string]Value

// Error is a parse or evaluation error, with where it happened.
type Error struct {
	Src string
	Pos int
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (at %d in %q)", e.Msg, e.Pos+1, e.Src)
}

// ErrBudget means an evaluation ran out of steps.
var ErrBudget = errors.New("the expression did too much work")

// --- lexer ------------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tStr
	tName
	tOp
)

type token struct {
	kind tokKind
	text string
	pos  int
}

func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j, dot := i, false
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.' && !dot) {
				dot = dot || src[j] == '.'
				j++
			}
			out = append(out, token{tNum, src[i:j], i})
			i = j
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			out = append(out, token{tName, src[i:j], i})
			i = j
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				j++
			}
			if j == len(src) {
				return nil, &Error{src, i, "text is not closed"}
			}
			out = append(out, token{tStr, src[i+1 : j], i})
			i = j + 1
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||", "//":
				out = append(out, token{tOp, two, i})
				i += 2
				continue
			}
			if strings.IndexByte("+-*/%^()<>!?:,", c) < 0 {
				return nil, &Error{src, i, fmt.Sprintf("unexpected %q", string(c))}
			}
			out = append(out, token{tOp, string(c), i})
			i++
		}
	}
	return append(out, token{tEOF, "", len(src)}), nil
}

// --- parser -----------------------------------------------------------------

type node interface{ pos() int }

type (
	numNode struct {
		p int
		v *big.Rat
	}
	strNode struct {
		p int
		v string
	}
	nameNode struct {
		p    int
		name string
	}
	unNode struct {
		p  int
		op string
		x  node
	}
	binNode struct {
		p    int
		op   string
		l, r node
	}
	condNode struct {
		p       int
		c, a, b node
	}
	callNode struct {
		p    int
		fn   string
		args []node
	}
)

func (n numNode) pos() int  { return n.p }
func (n strNode) pos() int  { return n.p }
func (n nameNode) pos() int { return n.p }
func (n unNode) pos() int   { return n.p }
func (n binNode) pos() int  { return n.p }
func (n condNode) pos() int { return n.p }
func (n callNode) pos() int { return n.p }

// Expr is a parsed expression, ready to evaluate many times.
type Expr struct {
	src  string
	root node
}

// Source is the text the expression was parsed from.
func (e *Expr) Source() string { return e.src }

type parser struct {
	src  string
	toks []token
	i    int
}

// Parse parses one expression.
func Parse(src string) (*Expr, error) {
	if len(src) > maxExprLen {
		return nil, &Error{src[:40] + "…", 0, "the expression is too long"}
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, toks: toks}
	root, err := p.expr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, p.err(t, fmt.Sprintf("unexpected %q", t.text))
	}
	if err := checkCalls(src, root); err != nil {
		return nil, err
	}
	return &Expr{src: src, root: root}, nil
}

// MustParse is Parse for expressions known to be valid, such as in tests.
func MustParse(src string) *Expr {
	e, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return e
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }
func (p *parser) err(t token, msg string) error {
	return &Error{p.src, t.pos, msg}
}

func (p *parser) isOp(ops ...string) (token, bool) {
	t := p.peek()
	if t.kind != tOp {
		return t, false
	}
	for _, o := range ops {
		if t.text == o {
			return t, true
		}
	}
	return t, false
}

func (p *parser) expr() (node, error) {
	c, err := p.or()
	if err != nil {
		return nil, err
	}
	if t, ok := p.isOp("?"); ok {
		p.next()
		a, err := p.expr()
		if err != nil {
			return nil, err
		}
		if _, ok := p.isOp(":"); !ok {
			return nil, p.err(p.peek(), `expected ":"`)
		}
		p.next()
		b, err := p.expr()
		if err != nil {
			return nil, err
		}
		return condNode{t.pos, c, a, b}, nil
	}
	return c, nil
}

func (p *parser) binary(next func() (node, error), ops ...string) (node, error) {
	l, err := next()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.isOp(ops...)
		if !ok {
			return l, nil
		}
		p.next()
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = binNode{t.pos, t.text, l, r}
	}
}

func (p *parser) or() (node, error)  { return p.binary(p.and, "||") }
func (p *parser) and() (node, error) { return p.binary(p.cmp, "&&") }
func (p *parser) sum() (node, error) { return p.binary(p.prod, "+", "-") }
func (p *parser) prod() (node, error) {
	return p.binary(p.unary, "*", "/", "//", "%")
}

func (p *parser) cmp() (node, error) {
	l, err := p.sum()
	if err != nil {
		return nil, err
	}
	if t, ok := p.isOp("==", "!=", "<", "<=", ">", ">="); ok {
		p.next()
		r, err := p.sum()
		if err != nil {
			return nil, err
		}
		if _, again := p.isOp("==", "!=", "<", "<=", ">", ">="); again {
			return nil, p.err(p.peek(), "write a < b && b < c, not a < b < c")
		}
		return binNode{t.pos, t.text, l, r}, nil
	}
	return l, nil
}

func (p *parser) unary() (node, error) {
	if t, ok := p.isOp("-", "!"); ok {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return unNode{t.pos, t.text, x}, nil
	}
	return p.pow()
}

func (p *parser) pow() (node, error) {
	b, err := p.atom()
	if err != nil {
		return nil, err
	}
	if t, ok := p.isOp("^"); ok {
		p.next()
		e, err := p.unary()
		if err != nil {
			return nil, err
		}
		return binNode{t.pos, "^", b, e}, nil
	}
	return b, nil
}

func (p *parser) atom() (node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		r, ok := new(big.Rat).SetString(t.text)
		if !ok {
			return nil, p.err(t, fmt.Sprintf("%q is not a number", t.text))
		}
		return numNode{t.pos, r}, nil
	case tStr:
		return strNode{t.pos, t.text}, nil
	case tName:
		switch t.text {
		case "true", "false":
			return callNode{t.pos, t.text, nil}, nil
		}
		if _, ok := p.isOp("("); !ok {
			return nameNode{t.pos, t.text}, nil
		}
		p.next()
		var args []node
		if _, ok := p.isOp(")"); !ok {
			for {
				a, err := p.expr()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if _, ok := p.isOp(","); !ok {
					break
				}
				p.next()
			}
		}
		if _, ok := p.isOp(")"); !ok {
			return nil, p.err(p.peek(), `expected ")"`)
		}
		p.next()
		return callNode{t.pos, t.text, args}, nil
	case tOp:
		if t.text == "(" {
			x, err := p.expr()
			if err != nil {
				return nil, err
			}
			if _, ok := p.isOp(")"); !ok {
				return nil, p.err(p.peek(), `expected ")"`)
			}
			p.next()
			return x, nil
		}
	case tEOF:
		return nil, p.err(t, "the expression ends too soon")
	}
	return nil, p.err(t, fmt.Sprintf("unexpected %q", t.text))
}

// checkCalls rejects unknown functions and wrong argument counts at parse
// time, so a pack fails to load rather than failing later in a lesson.
func checkCalls(src string, n node) error {
	switch x := n.(type) {
	case unNode:
		return checkCalls(src, x.x)
	case binNode:
		if err := checkCalls(src, x.l); err != nil {
			return err
		}
		return checkCalls(src, x.r)
	case condNode:
		for _, c := range []node{x.c, x.a, x.b} {
			if err := checkCalls(src, c); err != nil {
				return err
			}
		}
	case callNode:
		if x.fn == "true" || x.fn == "false" {
			return nil
		}
		f, ok := funcs[x.fn]
		if !ok {
			return &Error{src, x.p, fmt.Sprintf("there is no function %q", x.fn)}
		}
		if len(x.args) < f.min || (f.max >= 0 && len(x.args) > f.max) {
			return &Error{src, x.p, fmt.Sprintf("%s takes %s", x.fn, f.arity())}
		}
		for _, a := range x.args {
			if err := checkCalls(src, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// Names lists the variables an expression reads, in order of first use.
func (e *Expr) Names() []string {
	var out []string
	seen := map[string]bool{}
	var walk func(node)
	walk = func(n node) {
		switch x := n.(type) {
		case nameNode:
			if !seen[x.name] {
				seen[x.name] = true
				out = append(out, x.name)
			}
		case unNode:
			walk(x.x)
		case binNode:
			walk(x.l)
			walk(x.r)
		case condNode:
			walk(x.c)
			walk(x.a)
			walk(x.b)
		case callNode:
			for _, a := range x.args {
				walk(a)
			}
		}
	}
	walk(e.root)
	return out
}
