// SPDX-License-Identifier: Apache-2.0

package expr

import (
	"fmt"
	"math/big"
	"strings"
)

// Budget counts the steps left for a run of evaluations. Share one budget
// across everything one problem evaluates.
type Budget struct{ Left int }

// NewBudget returns a full budget.
func NewBudget() *Budget { return &Budget{Left: MaxSteps} }

type evaluator struct {
	src    string
	env    Env
	budget *Budget
}

// Eval evaluates the expression against env, spending from budget (a nil
// budget gets a fresh one).
func (e *Expr) Eval(env Env, budget *Budget) (Value, error) {
	if budget == nil {
		budget = NewBudget()
	}
	ev := &evaluator{src: e.src, env: env, budget: budget}
	return ev.eval(e.root)
}

// EvalNumber evaluates an expression that must give a number.
func (e *Expr) EvalNumber(env Env, budget *Budget) (*big.Rat, error) {
	v, err := e.Eval(env, budget)
	if err != nil {
		return nil, err
	}
	if v.Kind != Number {
		return nil, &Error{e.src, 0, fmt.Sprintf("this gives %s, not a number", v.Kind)}
	}
	return v.Num, nil
}

// EvalBool evaluates an expression that must give true or false.
func (e *Expr) EvalBool(env Env, budget *Budget) (bool, error) {
	v, err := e.Eval(env, budget)
	if err != nil {
		return false, err
	}
	if v.Kind != Bool {
		return false, &Error{e.src, 0, fmt.Sprintf("this gives %s, not true or false", v.Kind)}
	}
	return v.B, nil
}

func (ev *evaluator) fail(n node, format string, args ...any) error {
	return &Error{ev.src, n.pos(), fmt.Sprintf(format, args...)}
}

func (ev *evaluator) step() error {
	ev.budget.Left--
	if ev.budget.Left < 0 {
		return ErrBudget
	}
	return nil
}

// num checks a result stays within MaxBits.
func (ev *evaluator) num(n node, r *big.Rat) (Value, error) {
	if r.Num().BitLen() > MaxBits || r.Denom().BitLen() > MaxBits {
		return Value{}, ev.fail(n, "a number got too big")
	}
	return Value{Kind: Number, Num: r}, nil
}

func (ev *evaluator) eval(n node) (Value, error) {
	if err := ev.step(); err != nil {
		return Value{}, err
	}
	switch x := n.(type) {
	case numNode:
		return ev.num(n, x.v)
	case strNode:
		return Str(x.v), nil
	case nameNode:
		v, ok := ev.env[x.name]
		if !ok {
			return Value{}, ev.fail(n, "%q has no value here", x.name)
		}
		return v, nil
	case unNode:
		v, err := ev.eval(x.x)
		if err != nil {
			return Value{}, err
		}
		if x.op == "!" {
			if v.Kind != Bool {
				return Value{}, ev.fail(n, "! needs true or false")
			}
			return Value{Kind: Bool, B: !v.B}, nil
		}
		if v.Kind != Number {
			return Value{}, ev.fail(n, "- needs a number")
		}
		return ev.num(n, new(big.Rat).Neg(v.Num))
	case condNode:
		c, err := ev.eval(x.c)
		if err != nil {
			return Value{}, err
		}
		if c.Kind != Bool {
			return Value{}, ev.fail(n, "the part before ? must be true or false")
		}
		if c.B {
			return ev.eval(x.a)
		}
		return ev.eval(x.b)
	case binNode:
		return ev.binary(x)
	case callNode:
		switch x.fn {
		case "true":
			return Value{Kind: Bool, B: true}, nil
		case "false":
			return Value{Kind: Bool}, nil
		}
		args := make([]Value, len(x.args))
		for i, a := range x.args {
			v, err := ev.eval(a)
			if err != nil {
				return Value{}, err
			}
			args[i] = v
		}
		return ev.call(x, args)
	}
	return Value{}, ev.fail(n, "cannot evaluate this")
}

func (ev *evaluator) binary(x binNode) (Value, error) {
	// && and || only evaluate their right side when needed.
	if x.op == "&&" || x.op == "||" {
		l, err := ev.eval(x.l)
		if err != nil {
			return Value{}, err
		}
		if l.Kind != Bool {
			return Value{}, ev.fail(x, "%s needs true or false on both sides", x.op)
		}
		if (x.op == "&&" && !l.B) || (x.op == "||" && l.B) {
			return l, nil
		}
		r, err := ev.eval(x.r)
		if err != nil {
			return Value{}, err
		}
		if r.Kind != Bool {
			return Value{}, ev.fail(x, "%s needs true or false on both sides", x.op)
		}
		return r, nil
	}
	l, err := ev.eval(x.l)
	if err != nil {
		return Value{}, err
	}
	r, err := ev.eval(x.r)
	if err != nil {
		return Value{}, err
	}
	switch x.op {
	case "==", "!=":
		if l.Kind != r.Kind {
			return Value{}, ev.fail(x, "cannot compare %s with %s", l.Kind, r.Kind)
		}
		eq := false
		switch l.Kind {
		case Number:
			eq = l.Num.Cmp(r.Num) == 0
		case Bool:
			eq = l.B == r.B
		case String:
			eq = l.S == r.S
		}
		return Value{Kind: Bool, B: eq == (x.op == "==")}, nil
	}
	if l.Kind != Number || r.Kind != Number {
		return Value{}, ev.fail(x, "%s needs numbers on both sides", x.op)
	}
	a, b := l.Num, r.Num
	switch x.op {
	case "<":
		return Value{Kind: Bool, B: a.Cmp(b) < 0}, nil
	case "<=":
		return Value{Kind: Bool, B: a.Cmp(b) <= 0}, nil
	case ">":
		return Value{Kind: Bool, B: a.Cmp(b) > 0}, nil
	case ">=":
		return Value{Kind: Bool, B: a.Cmp(b) >= 0}, nil
	case "+":
		return ev.num(x, new(big.Rat).Add(a, b))
	case "-":
		return ev.num(x, new(big.Rat).Sub(a, b))
	case "*":
		return ev.num(x, new(big.Rat).Mul(a, b))
	case "/":
		if b.Sign() == 0 {
			return Value{}, ev.fail(x, "division by zero")
		}
		return ev.num(x, new(big.Rat).Quo(a, b))
	case "//", "%":
		if !a.IsInt() || !b.IsInt() {
			return Value{}, ev.fail(x, "%s needs whole numbers", x.op)
		}
		if b.Sign() == 0 {
			return Value{}, ev.fail(x, "division by zero")
		}
		q, m := new(big.Int), new(big.Int)
		q.DivMod(a.Num(), b.Num(), m) // Euclidean: the remainder is never negative
		if x.op == "//" {
			return ev.num(x, new(big.Rat).SetInt(q))
		}
		return ev.num(x, new(big.Rat).SetInt(m))
	case "^":
		if !b.IsInt() || b.Num().Sign() < 0 || b.Num().Cmp(big.NewInt(12)) > 0 {
			return Value{}, ev.fail(x, "^ needs a whole power from 0 to 12")
		}
		out := big.NewRat(1, 1)
		for range b.Num().Int64() {
			out.Mul(out, a)
			if out.Num().BitLen() > MaxBits || out.Denom().BitLen() > MaxBits {
				return Value{}, ev.fail(x, "a number got too big")
			}
		}
		return Value{Kind: Number, Num: out}, nil
	}
	return Value{}, ev.fail(x, "unknown operator %s", x.op)
}

// --- formatting helpers shared with the functions ---------------------------

// Decimal writes a number as a decimal, with at least places digits after the
// point. It fails for numbers like 1/3 whose decimal never ends.
func Decimal(r *big.Rat, places int) (string, bool) {
	d := new(big.Int).Set(r.Denom())
	for _, p := range []int64{2, 5} {
		bp := big.NewInt(p)
		for new(big.Int).Mod(d, bp).Sign() == 0 {
			d.Quo(d, bp)
		}
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return "", false
	}
	exact := 0
	for x := new(big.Rat).Set(r); !x.IsInt(); exact++ {
		x.Mul(x, big.NewRat(10, 1))
	}
	return r.FloatString(max(exact, places)), true
}

// Fraction writes a number as a fraction in lowest terms, like 7/4 or 3.
func Fraction(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	return r.Num().String() + "/" + r.Denom().String()
}

// Mixed writes a number as a mixed number, like 1 3/4.
func Mixed(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	sign := ""
	a := new(big.Rat).Set(r)
	if a.Sign() < 0 {
		sign = "-"
		a.Neg(a)
	}
	whole, rem := new(big.Int).QuoRem(a.Num(), a.Denom(), new(big.Int))
	if whole.Sign() == 0 {
		return sign + rem.String() + "/" + a.Denom().String()
	}
	return sign + whole.String() + " " + rem.String() + "/" + a.Denom().String()
}

// Group writes a whole number with thousands separators, like 12,345.
func Group(n *big.Int) string {
	s := new(big.Int).Abs(n).String()
	var b strings.Builder
	if n.Sign() < 0 {
		b.WriteByte('-')
	}
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
