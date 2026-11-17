// SPDX-License-Identifier: Apache-2.0

package problem

import (
	"fmt"
	"slices"
	"strings"
)

// Figures is the catalogue of figures the app draws from arguments, with the
// arguments each one takes (ADR 9). A figure that needs no arguments, such as
// a picture of a pentagon, is a static SVG file in the pack's figures/
// folder instead.
var Figures = map[string][]string{
	"array":        {"rows", "cols"},                      // dots in rows and columns
	"clock":        {"hour", "minute"},                    // an analogue clock face
	"coordinate":   {"size", "x", "y"},                    // a first-quadrant grid with one point
	"fraction-bar": {"parts", "shaded"},                   // a bar cut into equal parts
	"number-line":  {"from", "to", "step", "mark"},        // ticks every step, a dot at mark
	"rectangle":    {"width", "height", "unit"},           // a labelled rectangle
	"prism":        {"length", "width", "height", "unit"}, // a labelled box
}

// FigureName is the figure a template draws, or "" when it draws none.
func (t *Template) FigureName() string { return t.figure }

// CheckFigure reports whether the template's figure exists: in the
// catalogue with exactly its arguments, or as one of the static figures
// given.
func (t *Template) CheckFigure(static []string) error {
	if t.figure == "" {
		return nil
	}
	want, ok := Figures[t.figure]
	if !ok {
		if slices.Contains(static, t.figure) {
			if len(t.figArgs) > 0 {
				return fmt.Errorf("template %s: figure %s is a static picture and takes no arguments", t.ID, t.figure)
			}
			return nil
		}
		return fmt.Errorf("template %s: there is no figure %q", t.ID, t.figure)
	}
	got := make([]string, 0, len(t.figArgs))
	for k := range t.figArgs {
		got = append(got, k)
	}
	slices.Sort(got)
	w := slices.Sorted(slices.Values(want))
	if !slices.Equal(got, w) {
		return fmt.Errorf("template %s: figure %s takes %s", t.ID, t.figure, strings.Join(w, ", "))
	}
	return nil
}
