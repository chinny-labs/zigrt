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
	"array":           {"rows", "cols"},                            // dots in rows and columns
	"clock":           {"hour", "minute"},                          // an analogue clock face
	"coordinate":      {"size", "x", "y"},                          // a first-quadrant grid with one point
	"fraction-bar":    {"parts", "shaded"},                         // a bar cut into equal parts
	"number-line":     {"from", "to", "step", "mark"},              // ticks every step, a dot at mark
	"rectangle":       {"width", "height", "unit"},                 // a labelled rectangle
	"prism":           {"length", "width", "height", "unit"},       // a labelled box
	"dots":            {"count", "arrange"},                        // 0-20 dots: line, scatter or dice
	"dot-groups":      {"a", "b", "style"},                         // two groups: apart, rows or crossed
	"ten-frame":       {"frames", "filled"},                        // one or two ten frames
	"shape":           {"name", "turn"},                            // a flat shape, turned
	"shape-mix":       {"circles", "triangles", "squares"},         // shapes to sort and count
	"solid":           {"name"},                                    // a solid shape
	"compare":         {"a", "b", "c", "kind"},                     // bars or towers to compare; c 0 for two
	"balance":         {"tip"},                                     // a pan balance: left, right or level
	"position":        {"place"},                                   // a ball above, below, beside or in a box
	"base-ten":        {"hundreds", "tens", "ones"},                // flats, rods and cubes
	"ruler":           {"start", "length", "max", "unit"},          // a pencil above a ruler; unit 'cubes' for a row of cubes
	"table":           {"rows", "style"},                           // rows "Cats:4|Dogs:7", as numbers or tally marks
	"cut-shape":       {"name", "parts", "equal", "shaded"},        // a circle, square or rectangle cut into parts
	"coins":           {"pennies", "nickels", "dimes", "quarters"}, // US coins, marked with their values
	"picture-graph":   {"rows", "key"},                             // rows "Cats:4|Dogs:6"; each symbol is worth key
	"bar-graph":       {"rows", "scale"},                           // rows as bars; the axis marked every scale
	"line-plot":       {"from", "to", "step", "marks"},             // Xs over a line; marks "2:3|5/2:1"
	"grid":            {"rows", "cols", "shaded"},                  // a rectangle of unit squares, the first shaded
	"polygon":         {"shape", "sides", "hide", "unit"},          // sides "3|4|5" labelled, one "?" (hide -1 for none)
	"dial":            {"value", "max", "step", "unit"},            // a kitchen scale
	"jug":             {"value", "max", "step", "unit"},            // a measuring jug
	"split-rectangle": {"cols", "rows", "areas"},                   // an area model, cols "20|3", rows "10|4"
	"lines":           {"kind", "turn"},                            // point, line, ray, segment, parallel, perpendicular, intersecting
	"angle":           {"a", "b", "turn", "labels"},                // one angle, or two side by side; labels "35°|?"
	"protractor":      {"degrees", "side"},                         // an angle on a protractor; side right or left
	"symmetry":        {"name", "axis"},                            // a flat shape with a dashed line through it
	"triangle":        {"kind", "turn"},                            // equilateral, isosceles, scalene, right, obtuse, acute
	"joined-prisms":   {"a", "b", "h1", "h2", "w", "unit"},         // two boxes side by side, edges labelled
	"plot":            {"size", "points", "join"},                  // points "1,3|2,5" on a grid, joined when join is 1
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
