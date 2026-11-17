// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/chinny-labs/zigrt/problem/expr"
)

// GateSeeds is how many seeds the curriculum gates try per template.
const GateSeeds = 1000

// GateError is one failed check (plan section 4.6).
type GateError struct {
	Template string
	Gate     string // generate, answers or solutions
	Seed     uint64
	Msg      string
}

func (e *GateError) Error() string {
	if e.Seed == 0 {
		return fmt.Sprintf("%s: %s: %s", e.Template, e.Gate, e.Msg)
	}
	return fmt.Sprintf("%s: %s: seed %d: %s", e.Template, e.Gate, e.Seed, e.Msg)
}

// maxGateErrors stops one broken template from flooding the output.
const maxGateErrors = 5

// Gate runs the generate, answers and solutions gates over seeds 1 to n, in
// every locale the template has. It returns nil when the template passes.
func (t *Template) Gate(n int) []error {
	var errs []error
	fail := func(gate string, seed uint64, format string, args ...any) bool {
		errs = append(errs, &GateError{t.ID, gate, seed, fmt.Sprintf(format, args...)})
		return len(errs) >= maxGateErrors
	}
	for _, loc := range t.locales() {
		distinct := map[string]bool{}
		for s := 1; s <= n; s++ {
			seed := uint64(s)
			p, err := t.Generate(seed, loc)
			if err != nil {
				if fail("generate", seed, "%v", err) {
					return errs
				}
				continue
			}
			distinct[p.key()] = true
			for _, msg := range p.answerProblems() {
				if fail("answers", seed, "%s", msg) {
					return errs
				}
			}
			if msg := p.solutionProblem(); msg != "" {
				if fail("solutions", seed, "%s", msg) {
					return errs
				}
			}
		}
		// With few seeds repeats are expected, so scale the bar down.
		if len(distinct) < min(t.MinVariety, max(1, n/3)) {
			fail("generate", 0, "only %d distinct problems in %d seeds (%s); at least %d are needed", len(distinct), n, loc, t.MinVariety)
		}
	}
	return errs
}

// locales lists every locale any of the template's text is written in.
func (t *Template) locales() []string {
	seen := map[string]bool{DefaultLocale: true}
	add := func(ts texts) {
		for loc := range ts {
			seen[loc] = true
		}
	}
	add(t.prompt)
	for _, s := range t.solution {
		add(s.text)
	}
	add(t.answer.right)
	for _, d := range t.answer.distractors {
		add(d)
	}
	out := make([]string, 0, len(seen))
	for loc := range seen {
		out = append(out, loc)
	}
	slices.Sort(out)
	return out
}

// key identifies what a student sees, for counting distinct problems.
func (p *Problem) key() string {
	var b strings.Builder
	b.WriteString(p.Prompt)
	for _, s := range p.Parts {
		b.WriteString("|" + s)
	}
	keys := make([]string, 0, len(p.FigArgs))
	for k := range p.FigArgs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		b.WriteString("|" + k + "=" + p.FigArgs[k])
	}
	choices := slices.Clone(p.Choices)
	slices.Sort(choices) // the same options in another order are the same problem
	for _, c := range choices {
		b.WriteString("|" + c)
	}
	return b.String()
}

func (p *Problem) answerProblems() []string {
	var out []string
	a := p.Answer
	switch a.Type {
	case Integer:
		if !a.Value.IsInt() {
			out = append(out, fmt.Sprintf("the answer %s is not a whole number", a.Value.RatString()))
		}
	case Decimal:
		if _, ok := expr.Decimal(a.Value, 0); !ok {
			out = append(out, fmt.Sprintf("the answer %s has no exact decimal", a.Value.RatString()))
		}
	case QuotientRemainder:
		if !a.Value.IsInt() || !a.Rem.IsInt() {
			out = append(out, "the quotient and remainder must be whole numbers")
		}
		if a.Rem.Sign() < 0 {
			out = append(out, "the remainder is negative")
		}
	}
	if a.Value != nil && a.Value.Sign() < 0 {
		out = append(out, fmt.Sprintf("the answer %s is negative", a.Value.RatString()))
	}
	if len(out) > 0 {
		return out // the checks below assume a well-formed answer
	}
	if v := p.Check(p.Right()); !v.Right {
		out = append(out, fmt.Sprintf("the right answer %q is not marked right (%s)", p.Right().Main, v.Note))
	}
	if p.Check(p.Wrong()).Right {
		out = append(out, fmt.Sprintf("the wrong answer %q is marked right", p.Wrong().Main))
	}
	return out
}

func (p *Problem) solutionProblem() string {
	last := p.Solution[len(p.Solution)-1]
	if p.Answer.Type == Fraction && !p.Answer.Simplest {
		// Any fraction of the right value will do: "2/4" states 1/2.
		for _, m := range fracTokRE.FindAllString(last, -1) {
			if r, _, ok := parseNumber(Fraction, m); ok && r.Cmp(p.Answer.Value) == 0 {
				return ""
			}
		}
	}
	for _, f := range p.Answer.forms(p) {
		if states(last, f) {
			return ""
		}
	}
	return fmt.Sprintf("the last solution line %q does not state the answer %q", last, p.Answer.Text(p))
}

var fracTokRE = regexp.MustCompile(`(\d+ )?\d+/\d+|\d+`)

// states reports whether line contains answer as a whole: "= 15" states 15
// but not 5, and "1 3/4" is not taken to state 3/4.
func states(line, answer string) bool {
	isNum := func(c byte) bool { return c >= '0' && c <= '9' || c == '.' || c == '/' }
	for i := 0; ; {
		j := strings.Index(line[i:], answer)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(answer)
		numeric := answer[0] >= '0' && answer[0] <= '9'
		// A number just before, or "1 " before "3/4", means this is part of
		// something bigger.
		wholeBefore := numeric && start >= 2 && line[start-1] == ' ' && line[start-2] >= '0' && line[start-2] <= '9'
		before := start == 0 || !isNum(line[start-1]) && !wholeBefore
		after := end == len(line) || !isNum(line[end]) || line[end] == '.' && (end+1 == len(line) || line[end+1] == ' ')
		if before && after {
			return true
		}
		i = start + 1
	}
}
