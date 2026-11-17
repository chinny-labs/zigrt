// SPDX-License-Identifier: Apache-2.0

package expr

import (
	"errors"
	"strings"
	"testing"
)

func TestEval(t *testing.T) {
	env := Env{"a": N(12), "b": N(18), "h": R(1, 2), "name": Str("Mika")}
	cases := []struct{ src, want string }{
		{"1 + 2 * 3", "7"},
		{"(1 + 2) * 3", "9"},
		{"1/3 + 1/6", "1/2"},
		{"0.1 + 0.2", "3/10"},
		{"0.1 + 0.2 == 0.3", "true"},
		{"17 // 5", "3"},
		{"17 % 5", "2"},
		{"-7 // 2", "-4"},
		{"-7 % 2", "1"},
		{"2 ^ 10", "1024"},
		{"-2 ^ 2", "-4"},
		{"2 ^ 3 ^ 2", "512"}, // ^ groups to the right
		{"gcd(a, b)", "6"},
		{"lcm(a, b)", "36"},
		{"lcm(4, 6, 10)", "60"},
		{"min(3, 1, 2)", "1"},
		{"max(a, b)", "18"},
		{"abs(-3/4)", "3/4"},
		{"floor(7/2)", "3"},
		{"floor(-7/2)", "-4"},
		{"ceil(7/2)", "4"},
		{"round(5/2)", "3"},
		{"round(1234, 100)", "1200"},
		{"round(1250, 100)", "1300"},
		{"round(3.14159, 0.01)", "157/50"},
		{"num(a/b)", "2"},
		{"den(a/b)", "3"},
		{"digit(4706, 100)", "7"},
		{"digit(4706, 1000)", "4"},
		{"digit(3.25, 0.1)", "2"},
		{"digit(3.25, 0.01)", "5"},
		{"isint(h * 2)", "true"},
		{"isint(h)", "false"},
		{"in(3, 1, 2, 3)", "true"},
		{"in(4, 1, 2, 3)", "false"},
		{"pick(1, 'red', 'blue')", "blue"},
		{"a > b ? 'more' : 'less'", "less"},
		{"a < b && b < 20", "true"},
		{"a > b || !(a == b)", "true"},
		{"dec(h)", "0.5"},
		{"dec(h, 2)", "0.50"},
		{"dec(1.25 * 4)", "5"},
		{"frac(7/4)", "7/4"},
		{"mixed(7/4)", "1 3/4"},
		{"mixed(-7/4)", "-1 3/4"},
		{"mixed(3/4)", "3/4"},
		{"group(1234567)", "1,234,567"},
		{"join(3, ':', pad(5, 2))", "3:05"},
		{"pad(123, 2)", "123"},
		{"group(-1000)", "-1,000"},
		{"plural(1, 'apple', 'apples')", "apple"},
		{"plural(a, 'apple', 'apples')", "apples"},
		{"join(name, ' has ', a)", "Mika has 12"},
		{"name == 'Mika'", "true"},
		{"false || true", "true"},
		{".5 + 1", "3/2"},
	}
	for _, c := range cases {
		e, err := Parse(c.src)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.src, err)
			continue
		}
		v, err := e.Eval(env, nil)
		if err != nil {
			t.Errorf("Eval(%q): %v", c.src, err)
			continue
		}
		if got := v.String(); got != c.want {
			t.Errorf("Eval(%q) = %s, want %s", c.src, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		"", "1 +", "(1", "1)", "1 < 2 < 3", "foo(1)", "gcd(1)", "abs(1, 2)",
		"'open", "1 $ 2", "a ? b", strings.Repeat("1+", 1001) + "1",
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", src)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	env := Env{"x": N(1)}
	for _, src := range []string{
		"y + 1", "1 / 0", "1 // 0", "1.5 // 1", "1 + 'a'", "!1", "-'a'",
		"1 ? 2 : 3", "1 && true", "1 == 'a'", "2 ^ 13", "2 ^ 0.5",
		"10 ^ 12 * 10 ^ 12", "dec(1/3)", "pick(2, 1, 2)", "round(1, 0)",
		"digit(1, 0)", "group(1/2)", "pad(-1, 2)", "pad(1, 10)", "gcd(1/2, 3)", "dec(1, 7)",
	} {
		if _, err := MustParse(src).Eval(env, nil); err == nil {
			t.Errorf("Eval(%q) succeeded, want an error", src)
		}
	}
}

func TestShortCircuit(t *testing.T) {
	// The right side would fail; it must not be evaluated.
	for _, src := range []string{"false && 1 / 0 == 1", "true || 1 / 0 == 1", "true ? 1 : 1 / 0"} {
		if _, err := MustParse(src).Eval(nil, nil); err != nil {
			t.Errorf("Eval(%q): %v", src, err)
		}
	}
}

func TestBudget(t *testing.T) {
	e := MustParse("1 + 1 + 1 + 1")
	b := &Budget{Left: 3}
	if _, err := e.Eval(nil, b); !errors.Is(err, ErrBudget) {
		t.Fatalf("got %v, want ErrBudget", err)
	}
	b = NewBudget()
	for range 100 {
		if _, err := e.Eval(nil, b); err != nil {
			t.Fatal(err)
		}
	}
	if b.Left != MaxSteps-700 {
		t.Errorf("budget left %d, want %d", b.Left, MaxSteps-700)
	}
}

func TestNames(t *testing.T) {
	got := strings.Join(MustParse("a + gcd(b, a) > c ? d : 1").Names(), ",")
	if got != "a,b,c,d" {
		t.Errorf("Names = %s", got)
	}
}

func TestTypedEval(t *testing.T) {
	if _, err := MustParse("1 < 2").EvalNumber(nil, nil); err == nil {
		t.Error("EvalNumber of a comparison succeeded")
	}
	if _, err := MustParse("1 + 2").EvalBool(nil, nil); err == nil {
		t.Error("EvalBool of a sum succeeded")
	}
	ok, err := MustParse("1 < 2").EvalBool(nil, nil)
	if err != nil || !ok {
		t.Errorf("EvalBool = %v, %v", ok, err)
	}
}

func TestFuncDoc(t *testing.T) {
	doc := FuncDoc()
	if len(doc) != len(funcs) || !strings.HasPrefix(doc[0], "abs ") {
		t.Errorf("FuncDoc = %v", doc[:2])
	}
}

func FuzzParseEval(f *testing.F) {
	for _, s := range []string{"1 + 2", "gcd(a, 6) ^ 2", "a > 1 ? 'x' : 'y'", "dec(a / 8, 2)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		e, err := Parse(src)
		if err != nil {
			return
		}
		b := NewBudget()
		_, _ = e.Eval(Env{"a": N(4)}, b)
		if b.Left < -1 {
			t.Fatalf("budget overspent: %d", b.Left)
		}
	})
}
