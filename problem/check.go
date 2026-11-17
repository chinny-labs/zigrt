// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/chinny-labs/zigrt/problem/expr"
)

// Response is what a student entered. Main holds the answer, the whole
// fraction or mixed number ("2 3/4", "7/4"), the comparison symbol, or the
// index of the chosen option; Rem holds a remainder.
type Response struct {
	Main string
	Rem  string
}

// Note says why a response that was not right was close.
type Note string

const (
	// NoteSimplify: the fraction has the right value but is not in lowest
	// terms, and the template asks for lowest terms.
	NoteSimplify Note = "simplify"
	// NoteUnreadable: the response is not a number of the expected kind.
	NoteUnreadable Note = "unreadable"
)

// Verdict is the result of checking a response.
type Verdict struct {
	Right bool
	Note  Note
}

var (
	intRE   = regexp.MustCompile(`^-?\d+$`)
	decRE   = regexp.MustCompile(`^-?(\d+\.?\d*|\.\d+)$`)
	fracRE  = regexp.MustCompile(`^(-)?(?:(\d+)\s+)?(\d+)\s*/\s*(\d+)$`)
	spaceRE = regexp.MustCompile(`\s+`)
)

// clean trims a typed number and drops thousands separators.
func clean(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	s = strings.ReplaceAll(s, "−", "-")
	return spaceRE.ReplaceAllString(s, " ")
}

// parseNumber reads a response as the given type. lowest reports whether a
// fraction was written in lowest terms.
func parseNumber(typ AnswerType, s string) (r *big.Rat, lowest bool, ok bool) {
	s = clean(s)
	switch typ {
	case Integer, QuotientRemainder:
		if !intRE.MatchString(s) {
			return nil, false, false
		}
	case Decimal:
		if !decRE.MatchString(s) {
			return nil, false, false
		}
	case Fraction:
		if m := fracRE.FindStringSubmatch(s); m != nil {
			num, _ := new(big.Int).SetString(m[3], 10)
			den, _ := new(big.Int).SetString(m[4], 10)
			if den.Sign() == 0 {
				return nil, false, false
			}
			whole := new(big.Int)
			if m[2] != "" {
				whole.SetString(m[2], 10)
				if num.Cmp(den) >= 0 { // 1 5/4 is not a mixed number
					return nil, false, false
				}
			}
			lowest = new(big.Int).GCD(nil, nil, num, den).Cmp(big.NewInt(1)) == 0
			r = new(big.Rat).SetFrac(num, den)
			r.Add(r, new(big.Rat).SetInt(whole))
			if m[1] != "" {
				r.Neg(r)
			}
			return r, lowest, true
		}
		if !intRE.MatchString(s) {
			return nil, false, false
		}
	default:
		return nil, false, false
	}
	r, ok = new(big.Rat).SetString(s)
	return r, true, ok
}

// Check marks a response. It is the only place answers are marked, so lessons,
// drills and the API agree.
func (p *Problem) Check(resp Response) Verdict {
	a := p.Answer
	switch a.Type {
	case Comparison:
		s := strings.TrimSpace(resp.Main)
		if s != "<" && s != "=" && s != ">" {
			return Verdict{Note: NoteUnreadable}
		}
		return Verdict{Right: s == a.Symbol}
	case Choice:
		i, err := strconv.Atoi(strings.TrimSpace(resp.Main))
		if err != nil || i < 0 || i >= len(p.Choices) {
			return Verdict{Note: NoteUnreadable}
		}
		return Verdict{Right: i == a.Choice}
	}
	v, lowest, ok := parseNumber(a.Type, resp.Main)
	if !ok {
		return Verdict{Note: NoteUnreadable}
	}
	if v.Cmp(a.Value) != 0 {
		return Verdict{}
	}
	if a.Type == QuotientRemainder {
		r, _, ok := parseNumber(QuotientRemainder, resp.Rem)
		if !ok {
			if strings.TrimSpace(resp.Rem) == "" && a.Rem.Sign() == 0 {
				return Verdict{Right: true} // no remainder, box left empty
			}
			return Verdict{Note: NoteUnreadable}
		}
		return Verdict{Right: r.Cmp(a.Rem) == 0}
	}
	if a.Type == Fraction && a.Simplest && !lowest {
		return Verdict{Note: NoteSimplify}
	}
	return Verdict{Right: true}
}

// Text is the right answer as it would be written: "347", "3.75", "2 3/4",
// "12 r 3", ">" or the right option.
func (a Answer) Text(p *Problem) string {
	return a.forms(p)[0]
}

// forms lists the ways the right answer may be written, the usual one first.
func (a Answer) forms(p *Problem) []string {
	switch a.Type {
	case Comparison:
		return []string{a.Symbol}
	case Choice:
		return []string{p.Choices[a.Choice]}
	case QuotientRemainder:
		q, r := a.Value.Num().String(), a.Rem.Num().String()
		return []string{q + " r " + r, q + " R " + r, q + " remainder " + r}
	case Decimal:
		s, ok := expr.Decimal(a.Value, 0)
		if !ok {
			return []string{a.Value.RatString()}
		}
		return []string{s}
	case Fraction:
		return []string{expr.Mixed(a.Value), expr.Fraction(a.Value)}
	}
	n := a.Value.Num()
	return []string{n.String(), expr.Group(n)}
}

// Right returns a response that Check marks right.
func (p *Problem) Right() Response {
	a := p.Answer
	switch a.Type {
	case Comparison:
		return Response{Main: a.Symbol}
	case Choice:
		return Response{Main: strconv.Itoa(a.Choice)}
	case QuotientRemainder:
		return Response{Main: a.Value.Num().String(), Rem: a.Rem.Num().String()}
	}
	return Response{Main: a.forms(p)[0]}
}

// Wrong returns a response that Check must mark wrong.
func (p *Problem) Wrong() Response {
	a := p.Answer
	switch a.Type {
	case Comparison:
		if a.Symbol == "<" {
			return Response{Main: ">"}
		}
		return Response{Main: "<"}
	case Choice:
		return Response{Main: strconv.Itoa((a.Choice + 1) % len(p.Choices))}
	case QuotientRemainder:
		return Response{Main: a.Value.Num().String(), Rem: new(big.Int).Add(a.Rem.Num(), big.NewInt(1)).String()}
	}
	off := new(big.Rat).Add(a.Value, big.NewRat(1, 1))
	p2 := *p
	p2.Answer.Value = off
	return Response{Main: p2.Answer.forms(&p2)[0]}
}
