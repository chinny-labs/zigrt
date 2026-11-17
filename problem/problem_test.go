// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Template {
	t.Helper()
	tpl, err := ParseTemplate([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return tpl
}

func gate(t *testing.T, tpl *Template) {
	t.Helper()
	for _, err := range tpl.Gate(GateSeeds) {
		t.Error(err)
	}
}

// The example from the plan (section 4.2), with // for whole-number division.
const planExample = `
id: add-2d-2d-regroup.kp2.t1
vars:
  a: {int: [10, 99]}
  b: {int: [10, 99]}
where:
  - (a % 10) + (b % 10) >= 10      # forces regrouping in the ones
  - a + b <= 99
prompt: "{a} + {b}"
layout: stacked
op: "+"
parts: [a, b]
answer: {type: integer, value: a + b}
solution:
  - "Add the ones: {a % 10} + {b % 10} = {(a % 10) + (b % 10)}. Write {(a + b) % 10}, carry 1."
  - "Add the tens: {a // 10} + {b // 10} + 1 = {(a + b) // 10}."
  - "{a} + {b} = {a + b}"
`

func TestPlanExample(t *testing.T) {
	tpl := mustParse(t, planExample)
	gate(t, tpl)
	p, err := tpl.Generate(7, "en")
	if err != nil {
		t.Fatal(err)
	}
	if p.Op != "+" || len(p.Parts) != 2 || p.Layout != LayoutStacked {
		t.Errorf("problem = %+v", p)
	}
	var a, b int
	if _, err := fmtSscan(p.Prompt, &a, &b); err != nil {
		t.Fatal(err)
	}
	if a%10+b%10 < 10 || a+b > 99 {
		t.Errorf("%d + %d breaks the where clauses", a, b)
	}
	if !p.Check(Response{Main: itoa(a + b)}).Right || p.Check(Response{Main: itoa(a + b - 10)}).Right {
		t.Error("Check is wrong")
	}
}

func TestDeterministic(t *testing.T) {
	tpl := mustParse(t, planExample)
	for seed := uint64(1); seed < 50; seed++ {
		p1, _ := tpl.Generate(seed, "en")
		p2, _ := tpl.Generate(seed, "en")
		if p1.Prompt != p2.Prompt || strings.Join(p1.Solution, "\n") != strings.Join(p2.Solution, "\n") {
			t.Fatalf("seed %d gave %q then %q", seed, p1.Prompt, p2.Prompt)
		}
	}
	// The template ID is mixed into the seed.
	other := mustParse(t, strings.Replace(planExample, "kp2.t1", "kp2.t2", 1))
	same := 0
	for seed := uint64(1); seed <= 100; seed++ {
		p1, _ := tpl.Generate(seed, "en")
		p2, _ := other.Generate(seed, "en")
		if p1.Prompt == p2.Prompt {
			same++
		}
	}
	if same > 10 {
		t.Errorf("%d of 100 seeds gave the same problem in two templates", same)
	}
}

func TestAnswerTypes(t *testing.T) {
	cases := []struct {
		name, src string
		right     []Response
		wrong     []Response
		note      map[string]Note
	}{
		{
			name: "fraction",
			src: `
id: add-unlike
vars:
  a: 1/2
  b: 1/4
prompt: "{a} + {b}"
answer: {type: fraction, value: a + b}
min_variety: 1
solution: ["{a} + {b} = {frac(a + b)}"]`,
			right: []Response{{Main: "3/4"}, {Main: " 3 / 4 "}},
			wrong: []Response{{Main: "6/8"}, {Main: "2/6"}, {Main: "0.75"}, {Main: "1 3/4"}},
			note:  map[string]Note{"6/8": NoteSimplify, "0.75": NoteUnreadable, "3/0": NoteUnreadable},
		},
		{
			name: "mixed",
			src: `
id: mixed
vars:
  a: 7/4
prompt: "Write {a} as a mixed number"
answer: {type: fraction, value: a}
min_variety: 1
solution: ["{a} = {mixed(a)}"]`,
			right: []Response{{Main: "1 3/4"}, {Main: "7/4"}},
			wrong: []Response{{Main: "1 6/8"}, {Main: "1 7/4"}, {Main: "3/4"}},
		},
		{
			name: "not simplest",
			src: `
id: unsimplified
vars:
  a: 2/4
prompt: "Shade {a}"
answer: {type: fraction, value: a, simplest: false}
min_variety: 1
solution: ["{a}"]`,
			right: []Response{{Main: "2/4"}, {Main: "1/2"}, {Main: "4/8"}},
		},
		{
			name: "decimal",
			src: `
id: dec
vars:
  a: 1.25
  b: 2.5
prompt: "{dec(a)} + {dec(b)}"
answer: {type: decimal, value: a + b}
min_variety: 1
solution: ["{dec(a)} + {dec(b)} = {dec(a + b)}"]`,
			right: []Response{{Main: "3.75"}, {Main: "3.750"}, {Main: "03.75"}},
			wrong: []Response{{Main: "3.7"}, {Main: "15/4"}, {Main: "3.75.0"}},
		},
		{
			name: "integer with separators",
			src: `
id: big
vars:
  a: 12000
prompt: "{group(a)} + 345"
answer: {type: integer, value: a + 345}
min_variety: 1
solution: ["{group(a)} + 345 = {group(a + 345)}"]`,
			right: []Response{{Main: "12345"}, {Main: "12,345"}},
			wrong: []Response{{Main: "12346"}, {Main: "12345.0"}},
		},
		{
			name: "quotient and remainder",
			src: `
id: qr
vars:
  a: 27
  b: 4
prompt: "{a} ÷ {b}"
layout: long-division
parts: [a, b]
answer: {type: quotient_remainder, value: a // b, remainder: a % b}
min_variety: 1
solution: ["{a} ÷ {b} = {a // b} r {a % b}"]`,
			right: []Response{{Main: "6", Rem: "3"}},
			wrong: []Response{{Main: "6", Rem: "2"}, {Main: "7", Rem: "3"}, {Main: "6"}},
		},
		{
			name: "no remainder may be left empty",
			src: `
id: qr0
vars:
  a: 28
prompt: "{a} ÷ 4"
answer: {type: quotient_remainder, value: a // 4, remainder: a % 4}
min_variety: 1
solution: ["{a} ÷ 4 = 7 r 0"]`,
			right: []Response{{Main: "7", Rem: "0"}, {Main: "7"}},
		},
		{
			name: "comparison",
			src: `
id: cmp
vars:
  a: 3/4
  b: 2/3
prompt: "{a} ○ {b}"
answer: {type: comparison, compare: [a, b]}
min_variety: 1
solution: ["{a} = 9/12 and {b} = 8/12, so {a} > {b}"]`,
			right: []Response{{Main: ">"}},
			wrong: []Response{{Main: "<"}, {Main: "="}},
			note:  map[string]Note{"≥": NoteUnreadable},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl := mustParse(t, c.src)
			gate(t, tpl)
			p, err := tpl.Generate(1, "en")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range c.right {
				if v := p.Check(r); !v.Right {
					t.Errorf("%+v marked %+v, want right", r, v)
				}
			}
			for _, r := range c.wrong {
				if p.Check(r).Right {
					t.Errorf("%+v marked right", r)
				}
			}
			for in, want := range c.note {
				if v := p.Check(Response{Main: in}); v.Note != want {
					t.Errorf("%q noted %q, want %q", in, v.Note, want)
				}
			}
		})
	}
}

func TestChoice(t *testing.T) {
	tpl := mustParse(t, `
id: carry-mistake
vars:
  a: {int: [15, 49]}
  b: {int: [15, 49]}
where: ["a % 10 + b % 10 >= 10"]
prompt: "{a} + {b} = ?"
answer:
  type: choice
  right: "{a + b}"
  distractors:
    - "{a + b - 10}"          # forgot to carry
    - "{a + b + 10}"
    - "{(a + b) % 10 + 10 * (a // 10 + b // 10) + 100}"
solution: ["{a} + {b} = {a + b}"]`)
	gate(t, tpl)
	positions := map[int]bool{}
	for seed := uint64(1); seed <= 40; seed++ {
		p, err := tpl.Generate(seed, "en")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Choices) != 4 {
			t.Fatalf("choices = %v", p.Choices)
		}
		positions[p.Answer.Choice] = true
	}
	if len(positions) != 4 {
		t.Errorf("the right option only ever sat at %v", positions)
	}
}

func TestDuplicateChoicesRedraw(t *testing.T) {
	// When a == 0 both options read "0"; Generate must draw again.
	tpl := mustParse(t, `
id: dup
vars:
  a: {int: [0, 40]}
prompt: "{a}?"
answer: {type: choice, right: "{a}", distractors: ["{a * 2}"]}
solution: ["{a}"]`)
	gate(t, tpl)
}

func TestWordsAndLocales(t *testing.T) {
	tpl := mustParse(t, `
id: apples
vars:
  who: {words: [Mika, Theo, June]}
  n: {int: [2, 9]}
  m: {int: [1, 9]}
  total: n + m
prompt:
  en: "{who} has {n} apples and picks {m} more. How many now?"
  fr: "{who} a {n} pommes et en cueille {m} de plus. Combien maintenant ?"
answer: {type: integer, value: total}
min_variety: 20
solution:
  - en: "{n} + {m} = {total}. {who} has {total} {plural(total, 'apple', 'apples')}."
    fr: "{n} + {m} = {total}."`)
	gate(t, tpl)
	en, _ := tpl.Generate(3, "en")
	fr, _ := tpl.Generate(3, "fr")
	de, _ := tpl.Generate(3, "de")
	if !strings.Contains(fr.Prompt, "pommes") || de.Prompt != en.Prompt || en.Answer.Value.Cmp(fr.Answer.Value) != 0 {
		t.Errorf("en %q, fr %q, de %q", en.Prompt, fr.Prompt, de.Prompt)
	}
}

func TestMissingAndFigure(t *testing.T) {
	tpl := mustParse(t, `
id: missing-addend
vars:
  a: {int: [1, 20]}
  b: {int: [1, 20]}
prompt: "{a} + ? = {a + b}"
layout: missing
op: "+"
parts: [a, b, a + b]
blank: 1
answer: {type: integer, value: b}
solution: ["{a + b} − {a} = {b}"]`)
	gate(t, tpl)
	p, _ := tpl.Generate(1, "en")
	if len(p.Parts) != 3 || p.Blank != 1 {
		t.Errorf("missing = %+v", p)
	}
	fig := mustParse(t, `
id: shade
vars:
  d: {choose: [2, 3, 4, 5, 6, 8, 10, 12]}
  n: {int: [1, d - 1]}
prompt: "What fraction is shaded?"
layout: figure
figure: fraction-bar
figure_args: {parts: d, shaded: n}
answer: {type: fraction, value: n / d, simplest: false}
min_variety: 20
solution: ["{n} of {d} parts are shaded: {n}/{d}"]`)
	gate(t, fig)
	p, _ = fig.Generate(1, "en")
	if p.Figure != "fraction-bar" || p.FigArgs["parts"] == "" {
		t.Errorf("figure = %+v", p)
	}
}

func TestParseErrors(t *testing.T) {
	base := "id: t\nprompt: x\nanswer: {type: integer, value: 1}\nsolution: [\"1\"]\n"
	cases := map[string]string{
		"bad id":           strings.Replace(base, "id: t", "id: Bad_ID", 1),
		"undefined var":    strings.Replace(base, "value: 1", "value: z", 1),
		"use before def":   base + "vars:\n  a: b\n  b: 1\n",
		"twice":            base + "vars:\n  a: 1\n  a: 2\n",
		"bad expr":         strings.Replace(base, "value: 1", "value: 1 +", 1),
		"unknown fn":       strings.Replace(base, "value: 1", "value: sqrt(4)", 1),
		"bad type":         strings.Replace(base, "integer", "float", 1),
		"no solution":      strings.Replace(base, "solution: [\"1\"]\n", "", 1),
		"bad layout":       base + "layout: spiral\n",
		"stacked no parts": base + "layout: stacked\nop: \"+\"\n",
		"bad op":           base + "layout: across\nop: \"&\"\nparts: [1, 2]\n",
		"missing blank":    base + "layout: missing\nop: \"+\"\nparts: [1, 2, 3]\n",
		"figure name":      base + "layout: figure\n",
		"figure elsewhere": base + "figure: bar\n",
		"var shape":        base + "vars:\n  a: {int: [1]}\n",
		"two kinds":        base + "vars:\n  a: {int: [1, 2], choose: [1]}\n",
		"step without int": base + "vars:\n  a: {choose: [1], step: 2}\n",
		"bad name":         base + "vars:\n  2a: 1\n",
		"simplest":         base + "answer: {type: integer, value: 1, simplest: true}\n",
		"remainder":        strings.Replace(base, "value: 1}", "value: 1, remainder: 0}", 1),
		"compare":          strings.Replace(base, "{type: integer, value: 1}", "{type: comparison, compare: [1]}", 1),
		"no distractors":   strings.Replace(base, "{type: integer, value: 1}", "{type: choice, right: x}", 1),
		"too many":         strings.Replace(base, "{type: integer, value: 1}", "{type: choice, right: x, distractors: [a, b, c, d]}", 1),
		"open brace":       strings.Replace(base, "prompt: x", "prompt: \"{1 + 1\"", 1),
		"stray brace":      strings.Replace(base, "prompt: x", "prompt: \"1}\"", 1),
		"no english":       strings.Replace(base, "prompt: x", "prompt: {fr: x}", 1),
		"variety":          base + "min_variety: -1\n",
	}
	if _, err := ParseTemplate([]byte(base)); err != nil {
		t.Fatalf("base template: %v", err)
	}
	for name, src := range cases {
		if _, err := ParseTemplate([]byte(src)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
	_, err := ParseTemplate([]byte(strings.Replace(base, "value: 1", "value: z", 1)))
	if err == nil || !strings.Contains(err.Error(), "template t (line 1)") {
		t.Errorf("error does not say where: %v", err)
	}
}

func TestGateCatches(t *testing.T) {
	cases := map[string]string{
		"never satisfied": `
id: never
vars: {a: {int: [1, 9]}}
where: ["a > 9"]
prompt: "{a}"
answer: {type: integer, value: a}
solution: ["{a}"]`,
		"too few distinct": `
id: few
vars: {a: {int: [1, 5]}}
prompt: "{a} + 1"
answer: {type: integer, value: a + 1}
solution: ["{a + 1}"]`,
		"not whole": `
id: half
vars: {a: {int: [1, 99]}}
prompt: "{a} ÷ 2"
answer: {type: integer, value: a / 2}
solution: ["{a / 2}"]`,
		"negative": `
id: neg
vars: {a: {int: [1, 99]}, b: {int: [1, 99]}}
prompt: "{a} − {b}"
answer: {type: integer, value: a - b}
solution: ["{a - b}"]`,
		"solution does not state answer": `
id: quiet
vars: {a: {int: [10, 99]}}
prompt: "{a} + 1"
answer: {type: integer, value: a + 1}
solution: ["Add one to {a}."]`,
		"answer only inside a bigger number": `
id: inside
vars: {a: {int: [10, 99]}}
prompt: "{a} + 1"
answer: {type: integer, value: a + 1}
solution: ["{a + 1}0"]`,
		"no exact decimal": `
id: third
vars: {a: {int: [1, 99]}}
prompt: "{a} ÷ 3"
answer: {type: decimal, value: a / 3}
solution: ["{a / 3}"]`,
		"runtime error": `
id: zero
vars: {a: {int: [0, 99]}}
prompt: "{10 / a}"
answer: {type: fraction, value: 10 / a}
solution: ["{10 / a}"]`,
	}
	for name, src := range cases {
		tpl := mustParse(t, src)
		if errs := tpl.Gate(200); len(errs) == 0 {
			t.Errorf("%s: passed the gates", name)
		}
	}
}

func TestWhereGuardsLaterVars(t *testing.T) {
	tpl := mustParse(t, `
id: guarded
vars:
  b: {int: [0, 9]}
  q: 36 / b
where: ["b != 0"]
prompt: "36 ÷ {b}"
answer: {type: fraction, value: q}
min_variety: 9
solution: ["36 ÷ {b} = {q}"]`)
	gate(t, tpl)
}

func TestStates(t *testing.T) {
	cases := []struct {
		line, ans string
		want      bool
	}{
		{"so 7 + 8 = 15", "15", true},
		{"so 7 + 8 = 15", "5", false},
		{"15.", "15", true},
		{"1.5", "5", false},
		{"= 1 3/4", "3/4", false},
		{"= 1 3/4", "1 3/4", true},
		{"3/4 is bigger", "3/4", true},
		{"23/4", "3/4", false},
		{"12 r 3", "12 r 3", true},
	}
	for _, c := range cases {
		if got := states(c.line, c.ans); got != c.want {
			t.Errorf("states(%q, %q) = %v", c.line, c.ans, got)
		}
	}
}
