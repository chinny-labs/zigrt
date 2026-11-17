// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/chinny-labs/zigrt/problem/expr"
)

// DefaultLocale is the locale every piece of pack text must have.
const DefaultLocale = "en"

// Text is a piece of pack text in one or more locales. In YAML it is either a
// plain string, which is English, or a map from locale to string.
type Text map[string]string

// UnmarshalYAML accepts a string or a locale map.
func (t *Text) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*t = Text{DefaultLocale: n.Value}
		return nil
	case yaml.MappingNode:
		m := map[string]string{}
		if err := n.Decode(&m); err != nil {
			return err
		}
		if _, ok := m[DefaultLocale]; !ok {
			return fmt.Errorf("line %d: text needs an %q version", n.Line, DefaultLocale)
		}
		*t = m
		return nil
	}
	return fmt.Errorf("line %d: expected text", n.Line)
}

// In returns the text for a locale, falling back to the default.
func (t Text) In(locale string) string {
	if s, ok := t[locale]; ok {
		return s
	}
	return t[DefaultLocale]
}

// interp is a string with {expression} holes. "{{" and "}}" are literal
// braces.
type interp struct {
	src   string
	lits  []string // len(lits) == len(exprs)+1
	exprs []*expr.Expr
}

func compileInterp(src string) (*interp, error) {
	in := &interp{src: src}
	var lit strings.Builder
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '{' && i+1 < len(src) && src[i+1] == '{':
			lit.WriteByte('{')
			i++
		case c == '}' && i+1 < len(src) && src[i+1] == '}':
			lit.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(src[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("%q: a { is not closed", src)
			}
			e, err := expr.Parse(src[i+1 : i+end])
			if err != nil {
				return nil, err
			}
			in.lits = append(in.lits, lit.String())
			in.exprs = append(in.exprs, e)
			lit.Reset()
			i += end
		case c == '}':
			return nil, fmt.Errorf("%q: a } has no {; write }} for a brace", src)
		default:
			lit.WriteByte(c)
		}
	}
	in.lits = append(in.lits, lit.String())
	return in, nil
}

func (in *interp) names() []string {
	var out []string
	for _, e := range in.exprs {
		out = append(out, e.Names()...)
	}
	return out
}

func (in *interp) render(env expr.Env, b *expr.Budget) (string, error) {
	if len(in.exprs) == 0 {
		return in.lits[0], nil
	}
	var s strings.Builder
	for i, e := range in.exprs {
		s.WriteString(in.lits[i])
		v, err := e.Eval(env, b)
		if err != nil {
			return "", err
		}
		s.WriteString(show(v))
	}
	s.WriteString(in.lits[len(in.lits)-1])
	return s.String(), nil
}

// show is how a value appears in text when no formatter is used: whole
// numbers plainly, terminating decimals as decimals only if written with
// dec(), everything else as a fraction in lowest terms.
func show(v expr.Value) string {
	if v.Kind == expr.Number && !v.Num.IsInt() {
		return expr.Fraction(v.Num)
	}
	return v.String()
}

// texts compiles every locale of a Text.
type texts map[string]*interp

func compileText(t Text) (texts, error) {
	out := texts{}
	for loc, s := range t {
		in, err := compileInterp(s)
		if err != nil {
			return nil, err
		}
		out[loc] = in
	}
	return out, nil
}

func (ts texts) in(locale string) *interp {
	if in, ok := ts[locale]; ok {
		return in
	}
	return ts[DefaultLocale]
}

func (ts texts) names() []string {
	var out []string
	for _, in := range ts {
		out = append(out, in.names()...)
	}
	return out
}
