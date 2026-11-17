// SPDX-License-Identifier: Apache-2.0

package expr

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

type fn struct {
	min, max int // max -1: any number
	doc      string
	call     func(ev *evaluator, n callNode, args []Value) (Value, error)
}

func (f fn) arity() string {
	switch {
	case f.min == f.max && f.min == 1:
		return "1 value"
	case f.min == f.max:
		return fmt.Sprintf("%d values", f.min)
	case f.max < 0:
		return fmt.Sprintf("%d or more values", f.min)
	}
	return fmt.Sprintf("%d to %d values", f.min, f.max)
}

var funcs map[string]fn

func init() {
	funcs = map[string]fn{
		"gcd":   {2, -1, "greatest common factor of whole numbers", foldInt(func(a, b *big.Int) *big.Int { return new(big.Int).GCD(nil, nil, a, b) })},
		"lcm":   {2, -1, "least common multiple of whole numbers", foldInt(lcm)},
		"min":   {2, -1, "the smallest", fold(func(a, b *big.Rat) bool { return a.Cmp(b) <= 0 })},
		"max":   {2, -1, "the largest", fold(func(a, b *big.Rat) bool { return a.Cmp(b) >= 0 })},
		"abs":   {1, 1, "distance from zero", num1(func(r *big.Rat) *big.Rat { return new(big.Rat).Abs(r) })},
		"floor": {1, 1, "round down to a whole number", num1(floor)},
		"ceil": {1, 1, "round up to a whole number", num1(func(r *big.Rat) *big.Rat {
			return new(big.Rat).Neg(floor(new(big.Rat).Neg(r)))
		})},
		"round": {1, 2, "round(x) to a whole number, round(x, 10) to the nearest ten, round(x, 0.1) to the nearest tenth (halves round up)", roundTo},
		"num":   {1, 1, "numerator in lowest terms", num1(func(r *big.Rat) *big.Rat { return new(big.Rat).SetInt(r.Num()) })},
		"den":   {1, 1, "denominator in lowest terms", num1(func(r *big.Rat) *big.Rat { return new(big.Rat).SetInt(r.Denom()) })},
		"digit": {2, 2, "digit(4706, 100) is the hundreds digit, 7; digit(3.25, 0.1) is 2", digit},
		"isint": {1, 1, "true for a whole number", func(_ *evaluator, _ callNode, a []Value) (Value, error) {
			return Value{Kind: Bool, B: a[0].IsInt()}, nil
		}},
		"in":     {2, -1, "in(x, a, b, c) is true when x is one of a, b, c", in},
		"pick":   {2, -1, "pick(i, a, b, c) is a when i is 0, b when 1, and so on", pick},
		"dec":    {1, 2, "dec(x) writes a decimal; dec(x, 2) keeps at least 2 places, as in money", dec},
		"frac":   {1, 1, "frac(x) writes 7/4", text1(Fraction)},
		"mixed":  {1, 1, "mixed(x) writes 1 3/4", text1(Mixed)},
		"pad":    {2, 2, "pad(5, 2) writes 05, as in 3:05", pad},
		"group":  {1, 1, "group(12345) writes 12,345", group},
		"plural": {3, 3, "plural(n, 'apple', 'apples')", plural},
		"text":   {1, 1, "text(x) turns a number into text, for joining", func(_ *evaluator, _ callNode, a []Value) (Value, error) { return Str(a[0].String()), nil }},
		"join":   {2, -1, "join(a, b) puts texts together", join},
	}
}

// FuncDoc lists the functions with a line about each, for authoring docs.
func FuncDoc() []string {
	names := make([]string, 0, len(funcs))
	for k := range funcs {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + " (" + funcs[n].arity() + "): " + funcs[n].doc
	}
	return out
}

func (ev *evaluator) call(n callNode, args []Value) (Value, error) {
	return funcs[n.fn].call(ev, n, args)
}

func nums(ev *evaluator, n callNode, args []Value) ([]*big.Rat, error) {
	out := make([]*big.Rat, len(args))
	for i, a := range args {
		if a.Kind != Number {
			return nil, ev.fail(n, "%s needs numbers", n.fn)
		}
		out[i] = a.Num
	}
	return out, nil
}

func ints(ev *evaluator, n callNode, args []Value) ([]*big.Int, error) {
	rs, err := nums(ev, n, args)
	if err != nil {
		return nil, err
	}
	out := make([]*big.Int, len(rs))
	for i, r := range rs {
		if !r.IsInt() {
			return nil, ev.fail(n, "%s needs whole numbers", n.fn)
		}
		out[i] = new(big.Int).Abs(r.Num())
	}
	return out, nil
}

func foldInt(f func(a, b *big.Int) *big.Int) func(*evaluator, callNode, []Value) (Value, error) {
	return func(ev *evaluator, n callNode, args []Value) (Value, error) {
		xs, err := ints(ev, n, args)
		if err != nil {
			return Value{}, err
		}
		acc := xs[0]
		for _, x := range xs[1:] {
			acc = f(acc, x)
		}
		return ev.num(n, new(big.Rat).SetInt(acc))
	}
}

func lcm(a, b *big.Int) *big.Int {
	if a.Sign() == 0 || b.Sign() == 0 {
		return new(big.Int)
	}
	g := new(big.Int).GCD(nil, nil, a, b)
	return new(big.Int).Mul(new(big.Int).Quo(a, g), b)
}

func fold(keep func(a, b *big.Rat) bool) func(*evaluator, callNode, []Value) (Value, error) {
	return func(ev *evaluator, n callNode, args []Value) (Value, error) {
		xs, err := nums(ev, n, args)
		if err != nil {
			return Value{}, err
		}
		best := xs[0]
		for _, x := range xs[1:] {
			if !keep(best, x) {
				best = x
			}
		}
		return Value{Kind: Number, Num: best}, nil
	}
}

func num1(f func(*big.Rat) *big.Rat) func(*evaluator, callNode, []Value) (Value, error) {
	return func(ev *evaluator, n callNode, args []Value) (Value, error) {
		xs, err := nums(ev, n, args)
		if err != nil {
			return Value{}, err
		}
		return ev.num(n, f(xs[0]))
	}
}

func text1(f func(*big.Rat) string) func(*evaluator, callNode, []Value) (Value, error) {
	return func(ev *evaluator, n callNode, args []Value) (Value, error) {
		xs, err := nums(ev, n, args)
		if err != nil {
			return Value{}, err
		}
		return Str(f(xs[0])), nil
	}
}

func floor(r *big.Rat) *big.Rat {
	q, m := new(big.Int), new(big.Int)
	q.DivMod(r.Num(), r.Denom(), m) // Euclidean, so this rounds down for negatives too
	return new(big.Rat).SetInt(q)
}

func roundTo(ev *evaluator, n callNode, args []Value) (Value, error) {
	xs, err := nums(ev, n, args)
	if err != nil {
		return Value{}, err
	}
	unit := big.NewRat(1, 1)
	if len(xs) == 2 {
		unit = xs[1]
		if unit.Sign() <= 0 {
			return Value{}, ev.fail(n, "round needs a unit above zero")
		}
	}
	// floor(x/unit + 1/2) * unit: halves round up, as taught in school.
	q := new(big.Rat).Quo(xs[0], unit)
	q.Add(q, big.NewRat(1, 2))
	return ev.num(n, new(big.Rat).Mul(floor(q), unit))
}

func digit(ev *evaluator, n callNode, args []Value) (Value, error) {
	xs, err := nums(ev, n, args)
	if err != nil {
		return Value{}, err
	}
	x, place := new(big.Rat).Abs(xs[0]), xs[1]
	if place.Sign() <= 0 {
		return Value{}, ev.fail(n, "digit needs a place like 1, 10 or 0.1")
	}
	// floor(x / place) % 10
	q := floor(new(big.Rat).Quo(x, place))
	m := new(big.Int).Mod(q.Num(), big.NewInt(10))
	return ev.num(n, new(big.Rat).SetInt(m))
}

func in(_ *evaluator, _ callNode, args []Value) (Value, error) {
	x := args[0]
	for _, a := range args[1:] {
		if a.Kind != x.Kind {
			continue
		}
		if (x.Kind == Number && x.Num.Cmp(a.Num) == 0) || (x.Kind == String && x.S == a.S) || (x.Kind == Bool && x.B == a.B) {
			return Value{Kind: Bool, B: true}, nil
		}
	}
	return Value{Kind: Bool}, nil
}

func pick(ev *evaluator, n callNode, args []Value) (Value, error) {
	i, ok := args[0].Int()
	if !ok || i < 0 || int(i) >= len(args)-1 {
		return Value{}, ev.fail(n, "pick's first value must be 0 to %d", len(args)-2)
	}
	return args[i+1], nil
}

func dec(ev *evaluator, n callNode, args []Value) (Value, error) {
	xs, err := nums(ev, n, args)
	if err != nil {
		return Value{}, err
	}
	places := 0
	if len(xs) == 2 {
		p, ok := (Value{Kind: Number, Num: xs[1]}).Int()
		if !ok || p < 0 || p > 6 {
			return Value{}, ev.fail(n, "dec's places must be 0 to 6")
		}
		places = int(p)
	}
	s, ok := Decimal(xs[0], places)
	if !ok {
		return Value{}, ev.fail(n, "%s has no exact decimal", xs[0].RatString())
	}
	return Str(s), nil
}

func group(ev *evaluator, n callNode, args []Value) (Value, error) {
	xs, err := nums(ev, n, args)
	if err != nil {
		return Value{}, err
	}
	if !xs[0].IsInt() {
		return Value{}, ev.fail(n, "group needs a whole number")
	}
	return Str(Group(xs[0].Num())), nil
}

func plural(ev *evaluator, n callNode, args []Value) (Value, error) {
	if args[0].Kind != Number || args[1].Kind != String || args[2].Kind != String {
		return Value{}, ev.fail(n, "plural needs a number and two texts")
	}
	if args[0].Num.Cmp(big.NewRat(1, 1)) == 0 {
		return args[1], nil
	}
	return args[2], nil
}

func join(_ *evaluator, _ callNode, args []Value) (Value, error) {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(a.String())
	}
	return Str(b.String()), nil
}

func pad(ev *evaluator, n callNode, args []Value) (Value, error) {
	x, ok := args[0].Int()
	w, okw := args[1].Int()
	if !ok || x < 0 || !okw || w < 1 || w > 9 {
		return Value{}, ev.fail(n, "pad needs a whole number of at least 0 and a width from 1 to 9")
	}
	s := strconv.FormatInt(x, 10)
	for len(s) < int(w) {
		s = "0" + s
	}
	return Str(s), nil
}
