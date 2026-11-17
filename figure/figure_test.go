// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestDraw(t *testing.T) {
	good := map[string]Args{
		"array":        {"rows": "3", "cols": "4"},
		"clock":        {"hour": "4", "minute": "35"},
		"coordinate":   {"size": "10", "x": "3", "y": "7"},
		"fraction-bar": {"parts": "8", "shaded": "3"},
		"number-line":  {"from": "0", "to": "2", "step": "1/4", "mark": "5/4"},
		"rectangle":    {"width": "9", "height": "3", "unit": "cm"},
		"prism":        {"length": "5", "width": "2", "height": "3", "unit": "cubes"},
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
