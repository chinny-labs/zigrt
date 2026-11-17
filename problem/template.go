// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/big"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/chinny-labs/zigrt/problem/expr"
)

// Limits on templates.
const (
	// MaxTries is how many draws Generate makes before giving up on a seed.
	MaxTries = 100
	// MaxChoices is the most options a multiple-choice problem shows.
	MaxChoices = 4
	// DefaultVariety is how many distinct problems the generate gate
	// requires across GateSeeds seeds.
	DefaultVariety = 30
	maxRange       = 1_000_000
)

// Layout is how a problem is drawn.
type Layout string

const (
	LayoutText         Layout = "text" // the prompt alone, as in a word problem
	LayoutAcross       Layout = "across"
	LayoutStacked      Layout = "stacked"
	LayoutMissing      Layout = "missing"
	LayoutLongDivision Layout = "long-division"
	LayoutFigure       Layout = "figure"
)

var layouts = []Layout{LayoutText, LayoutAcross, LayoutStacked, LayoutMissing, LayoutLongDivision, LayoutFigure}

// AnswerType is the kind of answer a problem takes (plan section 3.9).
type AnswerType string

const (
	Integer           AnswerType = "integer"
	Decimal           AnswerType = "decimal"
	Fraction          AnswerType = "fraction"
	QuotientRemainder AnswerType = "quotient_remainder"
	Comparison        AnswerType = "comparison"
	Choice            AnswerType = "choice"
)

var answerTypes = []AnswerType{Integer, Decimal, Fraction, QuotientRemainder, Comparison, Choice}

var ops = map[string]string{"+": "+", "-": "−", "*": "×", "/": "÷"}

var idRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Template is a parsed, validated problem template.
type Template struct {
	ID         string
	Layout     Layout
	MinVariety int

	line     int
	vars     []variable
	where    []*expr.Expr
	whereAt  []int // where[i] is checked once vars[whereAt[i]] is drawn (-1: first)
	prompt   texts
	op       string
	parts    []*expr.Expr
	blank    int
	figure   string
	figArgs  map[string]*expr.Expr
	answer   answerSpec
	solution []step
}

// step is one line of a worked solution, shown only when its condition holds.
type step struct {
	when *expr.Expr
	text texts
}

// Step is a solution line in YAML: text, or {when: condition, text: text}.
type Step struct {
	When string
	Text Text
}

// UnmarshalYAML accepts plain text or a conditional step.
func (s *Step) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == "when" {
				if err := checkKeys(n, "when", "text"); err != nil {
					return err
				}
				var raw struct {
					When string `yaml:"when"`
					Text Text   `yaml:"text"`
				}
				if err := n.Decode(&raw); err != nil {
					return err
				}
				s.When, s.Text = raw.When, raw.Text
				return nil
			}
		}
	}
	return n.Decode(&s.Text)
}

type varKind int

const (
	varLet varKind = iota
	varInt
	varChoose
	varWords
)

type variable struct {
	name   string
	kind   varKind
	lo, hi *expr.Expr
	step   *expr.Expr
	list   []*expr.Expr
	words  []string
	let    *expr.Expr
}

type answerSpec struct {
	typ         AnswerType
	value       *expr.Expr
	rem         *expr.Expr
	compare     [2]*expr.Expr
	right       texts
	distractors []texts
	simplest    bool
}

// rawTemplate is the YAML shape of a template.
type rawTemplate struct {
	ID         string            `yaml:"id"`
	Vars       yaml.Node         `yaml:"vars"`
	Where      []string          `yaml:"where"`
	Prompt     Text              `yaml:"prompt"`
	Layout     Layout            `yaml:"layout"`
	Op         string            `yaml:"op"`
	Parts      []string          `yaml:"parts"`
	Blank      *int              `yaml:"blank"`
	Figure     string            `yaml:"figure"`
	FigureArgs map[string]string `yaml:"figure_args"`
	Answer     struct {
		Type        AnswerType `yaml:"type"`
		Value       string     `yaml:"value"`
		Remainder   string     `yaml:"remainder"`
		Compare     []string   `yaml:"compare"`
		Right       Text       `yaml:"right"`
		Distractors []Text     `yaml:"distractors"`
		Simplest    *bool      `yaml:"simplest"`
	} `yaml:"answer"`
	Solution   []Step `yaml:"solution"`
	MinVariety int    `yaml:"min_variety"`
}

// ParseTemplate parses one template from YAML.
func ParseTemplate(src []byte) (*Template, error) {
	var t Template
	if err := yaml.Unmarshal(src, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// UnmarshalYAML parses and validates a template, so a template can sit
// inside a larger YAML document such as a topic file.
func (t *Template) UnmarshalYAML(n *yaml.Node) error {
	var raw rawTemplate
	if err := n.Decode(&raw); err != nil {
		return err
	}
	err := checkKeys(n, "id", "vars", "where", "prompt", "layout", "op", "parts", "blank", "figure", "figure_args", "answer", "solution", "min_variety")
	if err == nil {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "answer" {
				err = checkKeys(n.Content[i+1], "type", "value", "remainder", "compare", "right", "distractors", "simplest")
			}
		}
	}
	var tpl *Template
	if err == nil {
		tpl, err = compile(&raw, n.Line)
	}
	if err != nil {
		where := fmt.Sprintf("line %d", n.Line)
		if raw.ID != "" {
			where = fmt.Sprintf("template %s (line %d)", raw.ID, n.Line)
		}
		return fmt.Errorf("%s: %w", where, err)
	}
	*t = *tpl
	return nil
}

func compile(raw *rawTemplate, line int) (*Template, error) {
	t := &Template{ID: raw.ID, line: line, Layout: raw.Layout, MinVariety: raw.MinVariety}
	if !idRE.MatchString(t.ID) {
		return nil, fmt.Errorf("id %q must be lower case letters, digits, dots and dashes", t.ID)
	}
	if t.Layout == "" {
		t.Layout = LayoutText
	}
	if !slices.Contains(layouts, t.Layout) {
		return nil, fmt.Errorf("layout %q is not one of %v", t.Layout, layouts)
	}
	if t.MinVariety == 0 {
		t.MinVariety = DefaultVariety
	}
	if t.MinVariety < 1 {
		return nil, errors.New("min_variety must be at least 1")
	}

	defined := map[string]bool{}
	use := func(what string, names []string) error {
		for _, n := range names {
			if !defined[n] {
				return fmt.Errorf("%s uses %q, which is not defined before it", what, n)
			}
		}
		return nil
	}
	parse := func(what, src string) (*expr.Expr, error) {
		if strings.TrimSpace(src) == "" {
			return nil, fmt.Errorf("%s is empty", what)
		}
		e, err := expr.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		return e, use(what, e.Names())
	}
	text := func(what string, tx Text) (texts, error) {
		if tx == nil {
			return nil, fmt.Errorf("%s is missing", what)
		}
		c, err := compileText(tx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		return c, use(what, c.names())
	}

	// Variables, in the order written.
	if raw.Vars.Kind != 0 && raw.Vars.Kind != yaml.MappingNode {
		return nil, errors.New("vars must be a map of name to value")
	}
	for i := 0; i+1 < len(raw.Vars.Content); i += 2 {
		name, val := raw.Vars.Content[i].Value, raw.Vars.Content[i+1]
		if !nameRE.MatchString(name) {
			return nil, fmt.Errorf("line %d: %q is not a usable variable name", raw.Vars.Content[i].Line, name)
		}
		if defined[name] {
			return nil, fmt.Errorf("line %d: %q is defined twice", raw.Vars.Content[i].Line, name)
		}
		v, err := compileVar(name, val, parse)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", val.Line, err)
		}
		t.vars = append(t.vars, v)
		defined[name] = true
	}

	for i, w := range raw.Where {
		e, err := parse(fmt.Sprintf("where %d", i+1), w)
		if err != nil {
			return nil, err
		}
		t.where = append(t.where, e)
		at := -1
		for _, n := range e.Names() {
			at = max(at, slices.IndexFunc(t.vars, func(v variable) bool { return v.name == n }))
		}
		t.whereAt = append(t.whereAt, at)
	}

	var err error
	if t.prompt, err = text("prompt", raw.Prompt); err != nil {
		return nil, err
	}

	// Layout operands.
	switch t.Layout {
	case LayoutAcross, LayoutStacked, LayoutMissing, LayoutLongDivision:
		if t.Layout == LayoutLongDivision {
			raw.Op = "/"
		}
		if _, ok := ops[raw.Op]; !ok {
			return nil, fmt.Errorf("layout %s needs op: one of + - * /", t.Layout)
		}
		t.op = raw.Op
		want := 2
		if t.Layout == LayoutMissing {
			want = 3
		}
		if len(raw.Parts) != want {
			return nil, fmt.Errorf("layout %s needs %d parts", t.Layout, want)
		}
		for i, p := range raw.Parts {
			e, err := parse(fmt.Sprintf("part %d", i+1), p)
			if err != nil {
				return nil, err
			}
			t.parts = append(t.parts, e)
		}
		if t.Layout == LayoutMissing {
			if raw.Blank == nil || *raw.Blank < 0 || *raw.Blank > 2 {
				return nil, errors.New("layout missing needs blank: 0, 1 or 2, the part to hide")
			}
			t.blank = *raw.Blank
		}
	case LayoutFigure:
		if !idRE.MatchString(raw.Figure) {
			return nil, errors.New("layout figure needs figure: the name of a figure in the pack")
		}
		t.figure = raw.Figure
		t.figArgs = map[string]*expr.Expr{}
		for k, src := range raw.FigureArgs {
			e, err := parse("figure_args."+k, src)
			if err != nil {
				return nil, err
			}
			t.figArgs[k] = e
		}
	}
	if t.Layout != LayoutFigure && (raw.Figure != "" || raw.FigureArgs != nil) {
		return nil, errors.New("figure is only for layout figure")
	}

	// The answer.
	a := &t.answer
	a.typ = raw.Answer.Type
	if !slices.Contains(answerTypes, a.typ) {
		return nil, fmt.Errorf("answer type %q is not one of %v", a.typ, answerTypes)
	}
	a.simplest = raw.Answer.Simplest == nil || *raw.Answer.Simplest
	if raw.Answer.Simplest != nil && a.typ != Fraction {
		return nil, errors.New("simplest is only for fraction answers")
	}
	switch a.typ {
	case Integer, Decimal, Fraction, QuotientRemainder:
		if a.value, err = parse("answer value", raw.Answer.Value); err != nil {
			return nil, err
		}
		if a.typ == QuotientRemainder {
			if a.rem, err = parse("answer remainder", raw.Answer.Remainder); err != nil {
				return nil, err
			}
		}
	case Comparison:
		if len(raw.Answer.Compare) != 2 {
			return nil, errors.New("a comparison answer needs compare: [left, right]")
		}
		for i, s := range raw.Answer.Compare {
			if a.compare[i], err = parse("compare", s); err != nil {
				return nil, err
			}
		}
	case Choice:
		if a.right, err = text("answer right", raw.Answer.Right); err != nil {
			return nil, err
		}
		if n := len(raw.Answer.Distractors); n < 1 || n > MaxChoices-1 {
			return nil, fmt.Errorf("a choice answer needs 1 to %d distractors", MaxChoices-1)
		}
		for i, d := range raw.Answer.Distractors {
			c, err := text(fmt.Sprintf("distractor %d", i+1), d)
			if err != nil {
				return nil, err
			}
			a.distractors = append(a.distractors, c)
		}
	}
	if a.typ != QuotientRemainder && raw.Answer.Remainder != "" {
		return nil, errors.New("remainder is only for quotient_remainder answers")
	}

	if len(raw.Solution) == 0 {
		return nil, errors.New("a template needs a worked solution")
	}
	for i, s := range raw.Solution {
		what := fmt.Sprintf("solution line %d", i+1)
		st := step{}
		if st.text, err = text(what, s.Text); err != nil {
			return nil, err
		}
		if s.When != "" {
			if st.when, err = parse(what+" when", s.When); err != nil {
				return nil, err
			}
		}
		t.solution = append(t.solution, st)
	}
	if t.solution[len(t.solution)-1].when != nil {
		return nil, errors.New("the last solution line states the answer, so it cannot have a when")
	}
	return t, nil
}

func compileVar(name string, n *yaml.Node, parse func(string, string) (*expr.Expr, error)) (variable, error) {
	v := variable{name: name}
	what := "var " + name
	if n.Kind == yaml.ScalarNode {
		e, err := parse(what, n.Value)
		v.kind, v.let = varLet, e
		return v, err
	}
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return v, fmt.Errorf("%s: write an expression, {int: [lo, hi]}, {choose: [...]} or {words: [...]}", what)
	}
	if err := checkKeys(n, "int", "step", "choose", "words", "let"); err != nil {
		return v, fmt.Errorf("%s: %w", what, err)
	}
	var raw struct {
		Int    []string `yaml:"int"`
		Step   string   `yaml:"step"`
		Choose []string `yaml:"choose"`
		Words  []string `yaml:"words"`
		Let    string   `yaml:"let"`
	}
	if err := n.Decode(&raw); err != nil {
		return v, fmt.Errorf("%s: %w", what, err)
	}
	set := 0
	for _, b := range []bool{raw.Int != nil, raw.Choose != nil, raw.Words != nil, raw.Let != ""} {
		if b {
			set++
		}
	}
	if set != 1 {
		return v, fmt.Errorf("%s: use exactly one of int, choose, words or let", what)
	}
	if raw.Step != "" && raw.Int == nil {
		return v, fmt.Errorf("%s: step is only for int", what)
	}
	var err error
	switch {
	case raw.Int != nil:
		if len(raw.Int) != 2 {
			return v, fmt.Errorf("%s: int needs [lo, hi]", what)
		}
		v.kind = varInt
		if v.lo, err = parse(what+" lo", raw.Int[0]); err != nil {
			return v, err
		}
		if v.hi, err = parse(what+" hi", raw.Int[1]); err != nil {
			return v, err
		}
		if raw.Step != "" {
			if v.step, err = parse(what+" step", raw.Step); err != nil {
				return v, err
			}
		}
	case raw.Choose != nil:
		if len(raw.Choose) == 0 {
			return v, fmt.Errorf("%s: choose needs at least one value", what)
		}
		v.kind = varChoose
		for _, s := range raw.Choose {
			e, err := parse(what, s)
			if err != nil {
				return v, err
			}
			v.list = append(v.list, e)
		}
	case raw.Words != nil:
		if len(raw.Words) == 0 {
			return v, fmt.Errorf("%s: words needs at least one word", what)
		}
		v.kind, v.words = varWords, raw.Words
	default:
		v.kind = varLet
		v.let, err = parse(what, raw.Let)
	}
	return v, err
}

// --- generation -------------------------------------------------------------

// Problem is one generated problem, ready to show and to check.
type Problem struct {
	Template string
	Seed     uint64
	Prompt   string
	Layout   Layout
	Op       string   // the symbol to show: + − × ÷
	Parts    []string // the operands, and for layout missing the result
	Blank    int      // layout missing: which part is hidden
	Figure   string
	FigArgs  map[string]string
	Choices  []string // multiple choice: the options, in the order shown
	Solution []string
	Answer   Answer
}

// Answer is the right answer to a problem.
type Answer struct {
	Type     AnswerType
	Value    *big.Rat // integer, decimal, fraction; the quotient
	Rem      *big.Rat // quotient_remainder
	Symbol   string   // comparison: "<", "=" or ">"
	Choice   int      // choice: the index of the right option
	Simplest bool     // fraction: the answer must be in lowest terms
}

// ErrNoProblem means no draw satisfied the template within MaxTries.
var ErrNoProblem = errors.New("no draw satisfied the template")

// Rand returns the PRNG Generate uses for a template and seed. It mixes the
// template ID in, so the same seed gives unrelated problems in two templates.
func Rand(id string, seed uint64) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return rand.New(rand.NewPCG(seed, h.Sum64())) //nolint:gosec // problems must be reproducible from a seed, not secret
}

// Generate makes the problem for a seed, with text in the given locale.
func (t *Template) Generate(seed uint64, locale string) (*Problem, error) {
	rng := Rand(t.ID, seed)
	for range MaxTries {
		p, err := t.try(rng, locale)
		if err == nil {
			p.Seed = seed
			return p, nil
		}
		if !errors.Is(err, errRetry) {
			// An evaluation error is a bug in the template, not bad luck:
			// guard it with a where clause.
			return nil, fmt.Errorf("template %s seed %d: %w", t.ID, seed, err)
		}
	}
	return nil, fmt.Errorf("template %s seed %d: %w", t.ID, seed, ErrNoProblem)
}

var errRetry = errors.New("retry")

func (t *Template) try(rng *rand.Rand, locale string) (*Problem, error) {
	b := expr.NewBudget()
	env := expr.Env{}
	// Each where clause is checked as soon as the variables it reads are
	// drawn, so a later variable can rely on it (b != 0, then a / b).
	check := func(at int) error {
		for i, w := range t.where {
			if t.whereAt[i] != at {
				continue
			}
			ok, err := w.EvalBool(env, b)
			if err != nil {
				return err
			}
			if !ok {
				return errRetry
			}
		}
		return nil
	}
	if err := check(-1); err != nil {
		return nil, err
	}
	for i, v := range t.vars {
		val, err := v.draw(rng, env, b)
		if err != nil {
			return nil, err
		}
		env[v.name] = val
		if err := check(i); err != nil {
			return nil, err
		}
	}
	p := &Problem{Template: t.ID, Layout: t.Layout, Blank: t.blank, Figure: t.figure}
	var err error
	if p.Prompt, err = t.prompt.in(locale).render(env, b); err != nil {
		return nil, err
	}
	if t.op != "" {
		p.Op = ops[t.op]
	}
	for _, e := range t.parts {
		v, err := e.Eval(env, b)
		if err != nil {
			return nil, err
		}
		p.Parts = append(p.Parts, show(v))
	}
	if t.figArgs != nil {
		p.FigArgs = map[string]string{}
		for k, e := range t.figArgs {
			v, err := e.Eval(env, b)
			if err != nil {
				return nil, err
			}
			p.FigArgs[k] = show(v)
		}
	}
	if err := t.makeAnswer(p, rng, env, b, locale); err != nil {
		return nil, err
	}
	for _, s := range t.solution {
		if s.when != nil {
			ok, err := s.when.EvalBool(env, b)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		line, err := s.text.in(locale).render(env, b)
		if err != nil {
			return nil, err
		}
		p.Solution = append(p.Solution, line)
	}
	return p, nil
}

func (v variable) draw(rng *rand.Rand, env expr.Env, b *expr.Budget) (expr.Value, error) {
	switch v.kind {
	case varInt:
		bound := func(e *expr.Expr) (int64, error) {
			r, err := e.EvalNumber(env, b)
			if err != nil {
				return 0, err
			}
			if !r.IsInt() || !r.Num().IsInt64() {
				return 0, fmt.Errorf("%s: int bounds must be whole numbers", v.name)
			}
			return r.Num().Int64(), nil
		}
		lo, err := bound(v.lo)
		if err != nil {
			return expr.Value{}, err
		}
		hi, err := bound(v.hi)
		if err != nil {
			return expr.Value{}, err
		}
		step := int64(1)
		if v.step != nil {
			if step, err = bound(v.step); err != nil {
				return expr.Value{}, err
			}
			if step < 1 {
				return expr.Value{}, fmt.Errorf("%s: step must be at least 1", v.name)
			}
		}
		if hi < lo {
			return expr.Value{}, errRetry
		}
		n := (hi-lo)/step + 1
		if n > maxRange {
			return expr.Value{}, fmt.Errorf("%s: the range has more than %d values", v.name, maxRange)
		}
		return expr.N(lo + step*rng.Int64N(n)), nil
	case varChoose:
		return v.list[rng.IntN(len(v.list))].Eval(env, b)
	case varWords:
		return expr.Str(v.words[rng.IntN(len(v.words))]), nil
	}
	return v.let.Eval(env, b)
}

func (t *Template) makeAnswer(p *Problem, rng *rand.Rand, env expr.Env, b *expr.Budget, locale string) error {
	a := &t.answer
	p.Answer = Answer{Type: a.typ, Simplest: a.typ == Fraction && a.simplest}
	switch a.typ {
	case Integer, Decimal, Fraction, QuotientRemainder:
		v, err := a.value.EvalNumber(env, b)
		if err != nil {
			return err
		}
		p.Answer.Value = v
		if a.rem != nil {
			r, err := a.rem.EvalNumber(env, b)
			if err != nil {
				return err
			}
			p.Answer.Rem = r
		}
	case Comparison:
		l, err := a.compare[0].EvalNumber(env, b)
		if err != nil {
			return err
		}
		r, err := a.compare[1].EvalNumber(env, b)
		if err != nil {
			return err
		}
		p.Answer.Symbol = [...]string{"<", "=", ">"}[l.Cmp(r)+1]
	case Choice:
		right, err := a.right.in(locale).render(env, b)
		if err != nil {
			return err
		}
		opts := []string{right}
		for _, d := range a.distractors {
			s, err := d.in(locale).render(env, b)
			if err != nil {
				return err
			}
			if slices.Contains(opts, s) {
				return errRetry // two options read the same; draw again
			}
			opts = append(opts, s)
		}
		rng.Shuffle(len(opts), func(i, j int) { opts[i], opts[j] = opts[j], opts[i] })
		p.Choices = opts
		p.Answer.Choice = slices.Index(opts, right)
	}
	return nil
}

// checkKeys rejects keys a mapping should not have, so that a misspelt key is
// an error rather than silently ignored.
func checkKeys(n *yaml.Node, allowed ...string) error {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		if !slices.Contains(allowed, k.Value) {
			return fmt.Errorf("line %d: unknown key %q", k.Line, k.Value)
		}
	}
	return nil
}
