// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"encoding/xml"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

func TestDraw(t *testing.T) {
	good := map[string]Args{
		"array":           {"rows": "3", "cols": "4"},
		"clock":           {"hour": "4", "minute": "35"},
		"coordinate":      {"size": "10", "x": "3", "y": "7"},
		"fraction-bar":    {"parts": "8", "shaded": "3"},
		"number-line":     {"from": "0", "to": "2", "step": "1/4", "mark": "5/4"},
		"rectangle":       {"width": "9", "height": "3", "unit": "cm"},
		"prism":           {"length": "5", "width": "2", "height": "3", "unit": "cubes"},
		"dots":            {"count": "7", "arrange": "scatter"},
		"dot-groups":      {"a": "4", "b": "3", "style": "apart"},
		"ten-frame":       {"frames": "2", "filled": "13"},
		"shape":           {"name": "hexagon", "turn": "30"},
		"shape-mix":       {"circles": "3", "triangles": "2", "squares": "4"},
		"solid":           {"name": "cylinder"},
		"compare":         {"a": "4", "b": "7", "c": "0", "kind": "height"},
		"balance":         {"tip": "left"},
		"position":        {"place": "above"},
		"base-ten":        {"hundreds": "1", "tens": "3", "ones": "7"},
		"ruler":           {"start": "0", "length": "6", "max": "10", "unit": "cm"},
		"table":           {"rows": "Cats:4|Dogs:7|Fish:2", "style": "tally"},
		"cut-shape":       {"name": "circle", "parts": "4", "equal": "1", "shaded": "1"},
		"coins":           {"pennies": "3", "nickels": "1", "dimes": "2", "quarters": "1"},
		"picture-graph":   {"rows": "Cats:4|Dogs:6", "key": "2"},
		"bar-graph":       {"rows": "Red:4|Blue:7|Green:2", "scale": "1"},
		"line-plot":       {"from": "0", "to": "2", "step": "1/4", "marks": "1/2:2|3/4:1|5/4:3"},
		"grid":            {"rows": "3", "cols": "4", "shaded": "5"},
		"polygon":         {"shape": "L", "sides": "4|2|3|3|7|5", "hide": "-1", "unit": "m"},
		"dial":            {"value": "350", "max": "1000", "step": "100", "unit": "g"},
		"jug":             {"value": "600", "max": "1000", "step": "100", "unit": "ml"},
		"split-rectangle": {"cols": "20|3", "rows": "10|4", "areas": "1"},
	}
	for _, name := range Names() {
		svg, err := Draw(name, good[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// Well-formed XML, an accessible label, and nothing the CSP blocks.
		if err := xml.Unmarshal([]byte(svg), new(struct{})); err != nil {
			t.Errorf("%s: not XML: %v\n%s", name, err, svg)
		}
		if !strings.Contains(svg, `role="img" aria-label="`) || strings.Contains(svg, "style") || strings.Contains(svg, "<script") {
			t.Errorf("%s: %s", name, svg)
		}
	}
	// Spot-check what each draws.
	svg, _ := Draw("fraction-bar", good["fraction-bar"])
	if strings.Count(svg, `class="fig-fill"`) != 3 || strings.Count(svg, `class="fig-empty"`) != 5 {
		t.Errorf("fraction bar: %s", svg)
	}
	svg, _ = Draw("array", good["array"])
	if strings.Count(svg, "<circle") != 12 {
		t.Errorf("array: %s", svg)
	}
	svg, _ = Draw("number-line", Args{"from": "8", "to": "18", "step": "1", "mark": "15"})
	for _, want := range []string{">8<", ">13<", ">18<", `class="fig-mark"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("number line lacks %s: %s", want, svg)
		}
	}
	svg, _ = Draw("rectangle", Args{"width": "2.5", "height": "1/3", "unit": "<b>m</b>"})
	if !strings.Contains(svg, ">2.5 &lt;b&gt;m&lt;/b&gt;<") || !strings.Contains(svg, ">1/3 &lt;b") {
		t.Errorf("rectangle labels: %s", svg)
	}
}

func TestDrawRejects(t *testing.T) {
	for name, a := range map[string]Args{
		"fraction-bar": {"parts": "30", "shaded": "1"},
		"array":        {"rows": "2.5", "cols": "3"},
		"clock":        {"hour": "13", "minute": "0"},
		"coordinate":   {"size": "10", "x": "11", "y": "1"},
		"number-line":  {"from": "0", "to": "1", "step": "1/3", "mark": "2"},
		"rectangle":    {"width": "0", "height": "2"},
		"prism":        {"length": "x", "width": "2", "height": "2"},
		"nope":         {},
	} {
		if _, err := Draw(name, a); err == nil {
			t.Errorf("%s %v drew", name, a)
		}
	}
	if _, err := Draw("number-line", Args{"from": "0", "to": "1", "step": "1/50", "mark": "0"}); err == nil {
		t.Error("50 jumps drew")
	}
}

func TestClean(t *testing.T) {
	got, err := Clean([]byte(`<?xml version="1.0"?><!-- a pentagon -->
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" class="x"><title>Pentagon</title>
<polygon points="5,0 10,4 8,10 2,10 0,4" fill="#FFC533" stroke="#1B2559"/><text x="5" y="6">5 &amp; up</text></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	want := `<svg xmlns="http://www.w3.org/2000/svg" class="fig fig-static" role="img" viewBox="0 0 10 10"><title>Pentagon</title>` + "\n" +
		`<polygon points="5,0 10,4 8,10 2,10 0,4" fill="#FFC533" stroke="#1B2559"></polygon><text x="5" y="6">5 &amp; up</text></svg>`
	if got != want {
		t.Errorf("Clean =\n%s\nwant\n%s", got, want)
	}
	for name, bad := range map[string]string{
		"script":   `<svg><script>alert(1)</script></svg>`,
		"style":    `<svg><style>*{}</style></svg>`,
		"handler":  `<svg onload="alert(1)"></svg>`,
		"style at": `<svg><rect style="fill:red"/></svg>`,
		"href":     `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="x.png"/></svg>`,
		"use":      `<svg><use href="#a"/></svg>`,
		"url":      `<svg><rect fill="url(http://x/y#z)"/></svg>`,
		"foreign":  `<svg><foreignObject/></svg>`,
		"doctype":  `<!DOCTYPE svg [<!ENTITY x "y">]><svg></svg>`,
		"root":     `<g></g>`,
		"two":      `<svg></svg><svg></svg>`,
		"broken":   `<svg><rect></svg>`,
		"html":     `<svg><a href="https://x">x</a></svg>`,
	} {
		if out, err := Clean([]byte(bad)); err == nil {
			t.Errorf("%s: cleaned to %s", name, out)
		}
	}
}

func TestEarlyFigures(t *testing.T) {
	count := func(name string, a Args, sub string) int {
		t.Helper()
		svg, err := Draw(name, a)
		if err != nil {
			t.Fatalf("%s %v: %v", name, a, err)
		}
		return strings.Count(svg, sub)
	}
	for _, arr := range []string{"line", "scatter"} {
		for n := 0; n <= 20; n++ {
			if got := count("dots", Args{"count": strconv.Itoa(n), "arrange": arr}, "<circle"); got != n {
				t.Errorf("%d dots %s: drew %d", n, arr, got)
			}
		}
	}
	for n := 1; n <= 6; n++ {
		if got := count("dots", Args{"count": strconv.Itoa(n), "arrange": "dice"}, `class="fig-dot"`); got != n {
			t.Errorf("dice %d: %d", n, got)
		}
	}
	if got := count("dot-groups", Args{"a": "8", "b": "3", "style": "crossed"}, `class="fig-cross"`); got != 6 {
		t.Errorf("3 crossed out: %d strokes", got)
	}
	if got := count("dot-groups", Args{"a": "5", "b": "7", "style": "rows"}, "<circle"); got != 12 {
		t.Errorf("rows: %d", got)
	}
	if got := count("ten-frame", Args{"frames": "2", "filled": "13"}, `class="fig-dot"`); got != 13 {
		t.Errorf("ten frames: %d counters", got)
	}
	if got := count("ten-frame", Args{"frames": "1", "filled": "7"}, `class="fig-cell"`); got != 10 {
		t.Errorf("ten frame: %d cells", got)
	}
	for _, name := range FlatShapes {
		svg, err := Draw("shape", Args{"name": name, "turn": "45"})
		if err != nil || !strings.Contains(svg, `aria-label="a flat shape"`) || strings.Contains(svg, "<text") {
			t.Errorf("shape %s: %v, or its name is in the picture: %s", name, err, svg)
		}
	}
	if got := count("shape", Args{"name": "hexagon", "turn": "0"}, ","); got != 6 {
		t.Errorf("hexagon has %d points", got)
	}
	if got := count("shape-mix", Args{"circles": "3", "triangles": "2", "squares": "4"}, "<polygon"); got != 6 {
		t.Errorf("shape mix: %d polygons", got)
	}
	for _, name := range SolidShapes {
		svg, err := Draw("solid", Args{"name": name})
		if err != nil || !strings.Contains(svg, `aria-label="a solid shape"`) || strings.Contains(svg, "<text") {
			t.Errorf("solid %s: %v %s", name, err, svg)
		}
	}
	if got := count("compare", Args{"a": "3", "b": "5", "c": "8", "kind": "length"}, "<rect"); got != 3 {
		t.Errorf("compare three: %d", got)
	}
	for _, place := range []string{"above", "below", "left", "right", "inside"} {
		if got := count("position", Args{"place": place}, `class="fig-mark"`); got != 1 {
			t.Errorf("position %s: %d balls", place, got)
		}
	}
	for name, a := range map[string]Args{
		"dots":       {"count": "7", "arrange": "dice"},
		"dot-groups": {"a": "2", "b": "5", "style": "crossed"},
		"ten-frame":  {"frames": "1", "filled": "11"},
		"shape":      {"name": "star", "turn": "0"},
		"shape-mix":  {"circles": "0", "triangles": "0", "squares": "0"},
		"solid":      {"name": "torus"},
		"compare":    {"a": "0", "b": "3", "c": "0", "kind": "length"},
		"balance":    {"tip": "up"},
		"position":   {"place": "behind"},
	} {
		if _, err := Draw(name, a); err == nil {
			t.Errorf("%s %v: drew", name, a)
		}
	}
}

func TestGrade1Figures(t *testing.T) {
	draw := func(name string, a Args) string {
		t.Helper()
		svg, err := Draw(name, a)
		if err != nil {
			t.Fatalf("%s %v: %v", name, a, err)
		}
		return svg
	}
	svg := draw("base-ten", Args{"hundreds": "2", "tens": "3", "ones": "14"})
	if strings.Count(svg, "fig-alt2") != 2 || strings.Count(svg, `class="fig-block fig-rod"`) != 3 || strings.Count(svg, `class="fig-block fig-alt"`) != 14 {
		t.Errorf("base ten: %s", svg)
	}
	svg = draw("ruler", Args{"start": "2", "length": "5/2", "max": "8", "unit": "in"})
	if strings.Count(svg, `class="fig-tick"`) != 33 || !strings.Contains(svg, ">8<") || !strings.Contains(svg, ">in<") {
		t.Errorf("ruler: %s", svg)
	}
	svg = draw("ruler", Args{"start": "0", "length": "7", "max": "10", "unit": "cubes"})
	if strings.Count(svg, `class="fig-shape fig-fill"`) != 7 || strings.Contains(svg, "fig-tick") {
		t.Errorf("cubes: %s", svg)
	}
	svg = draw("table", Args{"rows": "Sun:12|Rain:3", "style": "tally"})
	// 12: two full groups (4 + a crossing stroke each) and 2 more; 3: three.
	if got := strings.Count(svg, `class="fig-tick"`); got != 10+2+3 {
		t.Errorf("tally strokes %d: %s", got, svg)
	}
	svg = draw("table", Args{"rows": "<b>Cats</b>:4|Dogs:7", "style": "number"})
	if strings.Contains(svg, "<b>") || !strings.Contains(svg, ">7<") {
		t.Errorf("table: %s", svg)
	}
	for _, name := range []string{"circle", "square", "rectangle"} {
		svg = draw("cut-shape", Args{"name": name, "parts": "4", "equal": "0", "shaded": "1"})
		if strings.Count(svg, `class="fig-fill fig-part"`) != 1 || strings.Count(svg, `class="fig-empty"`) != 3 {
			t.Errorf("cut %s: %s", name, svg)
		}
	}
	for name, a := range map[string]Args{
		"base-ten":  {"hundreds": "0", "tens": "0", "ones": "0"},
		"ruler":     {"start": "3", "length": "8", "max": "10", "unit": "cm"},
		"table":     {"rows": "Cats", "style": "number"},
		"cut-shape": {"name": "circle", "parts": "6", "equal": "0", "shaded": "1"},
	} {
		if _, err := Draw(name, a); err == nil {
			t.Errorf("%s %v drew", name, a)
		}
	}
	if _, err := Draw("ruler", Args{"start": "1", "length": "3", "max": "10", "unit": "cubes"}); err == nil {
		t.Error("cubes not from 0")
	}
}

func TestGrade2Figures(t *testing.T) {
	draw := func(name string, a Args) string {
		t.Helper()
		svg, err := Draw(name, a)
		if err != nil {
			t.Fatalf("%s %v: %v", name, a, err)
		}
		return svg
	}
	svg := draw("coins", Args{"pennies": "3", "nickels": "1", "dimes": "2", "quarters": "1"})
	if strings.Count(svg, "<circle") != 7 || strings.Count(svg, ">1¢<") != 3 || strings.Count(svg, ">25¢<") != 1 {
		t.Errorf("coins: %s", svg)
	}
	svg = draw("picture-graph", Args{"rows": "Cats:4|Dogs:5", "key": "2"})
	// Cats: 2 symbols; dogs: 2 and a half; plus the key's symbol.
	if strings.Count(svg, `<circle class="fig-mark"`) != 5 || strings.Count(svg, `<path class="fig-mark"`) != 1 || !strings.Contains(svg, "= 2") {
		t.Errorf("picture graph: %s", svg)
	}
	svg = draw("bar-graph", Args{"rows": "Red:40|Blue:70|Green:0", "scale": "10"})
	if strings.Count(svg, `class="fig-fill fig-part"`) != 2 || !strings.Contains(svg, ">70<") {
		t.Errorf("bar graph: %s", svg)
	}
	svg = draw("line-plot", Args{"from": "0", "to": "2", "step": "1/4", "marks": "1/2:2|3/4:1|5/4:3|1/2:1"})
	if strings.Count(svg, ">×<") != 7 || !strings.Contains(svg, ">1¼<") || !strings.Contains(svg, ">¾<") {
		t.Errorf("line plot: %s", svg)
	}
	svg = draw("grid", Args{"rows": "3", "cols": "4", "shaded": "5"})
	if strings.Count(svg, `fig-square`) != 7 || strings.Count(svg, `fig-part`) != 5 {
		t.Errorf("grid: %s", svg)
	}
	for name, a := range map[string]Args{
		"grid":          {"rows": "3", "cols": "4", "shaded": "13"},
		"coins":         {"pennies": "0", "nickels": "0", "dimes": "0", "quarters": "0"},
		"picture-graph": {"rows": "Cats:5|Dogs:6", "key": "4"},
		"bar-graph":     {"rows": "Red:130|Blue:7", "scale": "10"},
		"line-plot":     {"from": "0", "to": "2", "step": "1/2", "marks": "1/4:1"},
	} {
		if _, err := Draw(name, a); err == nil {
			t.Errorf("%s %v drew", name, a)
		}
	}
}

func TestMixedLabel(t *testing.T) {
	for in, want := range map[string]string{"5/4": "1¼", "1/2": "½", "7/8": "⅞", "3": "3", "5/3": "1 2/3", "2/3": "2/3"} {
		r, _ := new(big.Rat).SetString(in)
		if got := mixedLabel(r); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}

func TestGrade3Figures(t *testing.T) {
	draw := func(name string, a Args) string {
		t.Helper()
		svg, err := Draw(name, a)
		if err != nil {
			t.Fatalf("%s %v: %v", name, a, err)
		}
		return svg
	}
	svg := draw("polygon", Args{"shape": "rectangle", "sides": "9|4", "hide": "1", "unit": "cm"})
	if strings.Count(svg, ">9 cm<") != 2 || strings.Count(svg, ">4 cm<") != 1 || strings.Count(svg, ">?<") != 1 {
		t.Errorf("rectangle: %s", svg)
	}
	svg = draw("polygon", Args{"shape": "L", "sides": "4|2|3|3|7|5", "hide": "-1", "unit": ""})
	for _, want := range []string{">4<", ">2<", ">3<", ">7<", ">5<"} {
		if !strings.Contains(svg, want) {
			t.Errorf("L lacks %s: %s", want, svg)
		}
	}
	svg = draw("polygon", Args{"shape": "pentagon", "sides": "5|5|5|5|5", "hide": "-1", "unit": "in"})
	if strings.Count(svg, ">5 in<") != 5 {
		t.Errorf("pentagon: %s", svg)
	}
	svg = draw("dial", Args{"value": "3", "max": "10", "step": "1", "unit": "kg"})
	if strings.Count(svg, `class="fig-tick"`) != 11 || !strings.Contains(svg, ">kg<") {
		t.Errorf("dial: %s", svg)
	}
	svg = draw("jug", Args{"value": "750", "max": "1000", "step": "250", "unit": "ml"})
	if strings.Count(svg, `class="fig-tick"`) != 5 || !strings.Contains(svg, "fig-water") {
		t.Errorf("jug: %s", svg)
	}
	svg = draw("split-rectangle", Args{"cols": "20|3", "rows": "10|4", "areas": "1"})
	for _, want := range []string{">200<", ">30<", ">80<", ">12<", ">20<", ">4<"} {
		if !strings.Contains(svg, want) {
			t.Errorf("area model lacks %s: %s", want, svg)
		}
	}
	for name, a := range map[string]Args{
		"polygon":         {"shape": "L", "sides": "4|2|3|3|8|5", "hide": "-1", "unit": ""},
		"dial":            {"value": "12", "max": "10", "step": "1", "unit": "kg"},
		"jug":             {"value": "1", "max": "1000", "step": "30", "unit": "ml"},
		"split-rectangle": {"cols": "7", "rows": "3", "areas": "0"},
	} {
		if _, err := Draw(name, a); err == nil {
			t.Errorf("%s %v drew", name, a)
		}
	}
	if _, err := Draw("polygon", Args{"shape": "triangle", "sides": "3|4|5", "hide": "3", "unit": ""}); err == nil {
		t.Error("hid a fourth side of a triangle")
	}
}
