// SPDX-License-Identifier: Apache-2.0

package content

import (
	"bytes"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/chinny-labs/zigrt/problem"
)

func hardest(t *testing.T) *Pack {
	t.Helper()
	p, err := Load(os.DirFS("testdata/hardest"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestHardest proves the template format can express the 20 hardest K-5
// topics (plan section 14.1) and that every template passes every gate.
func TestHardest(t *testing.T) {
	p := hardest(t)
	if len(p.Topics) != 20 {
		t.Errorf("%d topics, want 20", len(p.Topics))
	}
	seeds := problem.GateSeeds
	if testing.Short() {
		seeds = 100
	}
	for _, err := range p.Gate(seeds) {
		t.Error(err)
	}
	if got := len(p.CourseTopics("grade-5")); got != 8 {
		t.Errorf("grade 5 has %d topics", got)
	}
	ld := p.Topic("long-division-2d")
	if ld.Order != 13 || p.TopicOf("long-division-2d.kp1.t1") != ld || p.KP("long-division-2d.kp1").ExampleTemplate().ID != "long-division-2d.kp1.t1" {
		t.Errorf("lookups are wrong: %+v", ld)
	}
	if ids := p.IDs(); len(ids) < 60 || !strings.HasPrefix(ids[0], "add-decimals") {
		t.Errorf("IDs = %d, first %s", len(ids), ids[0])
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	sumA, err := Build(os.DirFS("testdata/hardest"), &a)
	if err != nil {
		t.Fatal(err)
	}
	sumB, _ := Build(os.DirFS("testdata/hardest"), &b)
	if sumA != sumB || !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("two builds differ")
	}
	p, err := LoadFile(a.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Topics) != 20 || len(p.Gate(5)) != 0 {
		t.Error("the built pack does not load the same")
	}
	if _, err := LoadFile([]byte("not a zip")); err == nil {
		t.Error("LoadFile accepted junk")
	}
}

func TestCheckApp(t *testing.T) {
	p := hardest(t) // needs 0.1.0
	for v, ok := range map[string]bool{"0.1.0": true, "v0.2.0": true, "1.0.0": true, "0.0.9": false, "dev": true} {
		if err := p.CheckApp(v); (err == nil) != ok {
			t.Errorf("CheckApp(%s) = %v", v, err)
		}
	}
}

// pack builds a small valid pack, with changes applied.
func pack(change func(m fstest.MapFS)) fstest.MapFS {
	m := fstest.MapFS{
		"manifest.yaml": {Data: []byte(`id: tiny
title: Tiny
version: 2026.11.0
min_app_version: 0.1.0
licence: CC-BY-SA-4.0
locales: [en]
courses: [{id: k, title: K, grades: [K]}]
`)},
		"topics.yaml": {Data: []byte(`strands: [{id: count, title: Counting}]
topics:
  - {id: one, title: One, strand: count, grade: K, xp: 5}
  - {id: two, title: Two, strand: count, grade: K, xp: 5, requires: [one]}
`)},
		"topics/one.yaml": {Data: []byte(lesson("one"))},
		"topics/two.yaml": {Data: []byte(lesson("two"))},
	}
	if change != nil {
		change(m)
	}
	return m
}

func lesson(id string) string {
	return `id: ` + id + `
kps:
  - id: ` + id + `.kp1
    explain: Count them.
    templates:
      - id: ` + id + `.kp1.t1
        vars: {a: {int: [1, 50]}}
        prompt: "{a} + 1"
        answer: {type: integer, value: a + 1}
        solution: ["{a} + 1 = {a + 1}"]
`
}

func set(name, data string) func(fstest.MapFS) {
	return func(m fstest.MapFS) { m[name] = &fstest.MapFile{Data: []byte(data)} }
}

func edit(name, old, new string) func(fstest.MapFS) {
	return func(m fstest.MapFS) {
		m[name] = &fstest.MapFile{Data: []byte(strings.Replace(string(m[name].Data), old, new, 1))}
	}
}

func TestLoadErrors(t *testing.T) {
	if p, err := Load(pack(nil)); err != nil || len(p.Gate(50)) != 0 {
		t.Fatalf("the base pack: %v %v", err, p.Gate(50))
	}
	cases := map[string]struct {
		change func(fstest.MapFS)
		want   string
	}{
		"cycle":          {edit("topics.yaml", "xp: 5}\n  - {id: two", "xp: 5, requires: [two]}\n  - {id: two"), "cycle"},
		"missing edge":   {edit("topics.yaml", "requires: [one]", "requires: [three]"), "not a topic"},
		"self":           {edit("topics.yaml", "requires: [one]", "requires: [two]"), "itself"},
		"weight":         {edit("topics.yaml", "requires: [one]", "encompasses: [{topic: one, weight: 1.5}]"), "weight"},
		"no lesson":      {func(m fstest.MapFS) { delete(m, "topics/two.yaml") }, "no topics/two.yaml"},
		"stray lesson":   {set("topics/three.yaml", lesson("three")), "no topic \"three\""},
		"wrong id":       {set("topics/two.yaml", lesson("one")), "not the file name"},
		"duplicate id":   {set("topics/two.yaml", strings.Replace(lesson("two"), "two.kp1.t1", "one.kp1.t1", 1)), "already used"},
		"kp prefix":      {set("topics/two.yaml", strings.ReplaceAll(lesson("two"), "two.kp1", "twoo.kp1")), "must start with two."},
		"no kps":         {set("topics/two.yaml", "id: two\nkps: []\n"), "1 to 4 knowledge points"},
		"no templates":   {set("topics/two.yaml", "id: two\nkps: [{id: two.kp1, explain: x, templates: []}]\n"), "no templates"},
		"bad template":   {edit("topics/two.yaml", "value: a + 1", "value: b + 1"), "not defined"},
		"bad example":    {edit("topics/two.yaml", "explain: Count them.", "explain: Count them.\n    example: two.kp1.t9"), "example"},
		"misspelt key":   {edit("topics.yaml", "xp: 5}", "xp: 5, prereqs: [one]}"), "prereqs"},
		"strand":         {edit("topics.yaml", "strand: count, grade: K, xp: 5, requires", "strand: shapes, grade: K, xp: 5, requires"), "strand"},
		"grade":          {edit("topics.yaml", "grade: K, xp: 5, requires", "grade: 6, xp: 5, requires"), "grade"},
		"xp":             {edit("topics.yaml", "xp: 5, requires", "xp: 0, requires"), "xp"},
		"version":        {edit("manifest.yaml", "2026.11.0", "1.0"), "calendar version"},
		"app version":    {edit("manifest.yaml", "min_app_version: 0.1.0", "min_app_version: soon"), "min_app_version"},
		"locale":         {edit("manifest.yaml", "[en]", "[fr]"), "locales"},
		"no courses":     {edit("manifest.yaml", "courses: [{id: k, title: K, grades: [K]}]", "courses: []"), "no courses"},
		"course grade":   {edit("manifest.yaml", "grades: [K]", "grades: [P]"), "course k"},
		"figure":         {edit("topics/two.yaml", "answer:", "layout: figure\n        figure: spiral\n        answer:"), "no figure"},
		"transition":     {edit("topics.yaml", "topics:", "transitions: [{from: one, to: [two]}]\ntopics:"), "still exists"},
		"transition to":  {edit("topics.yaml", "topics:", "transitions: [{from: zero, to: [nine]}]\ntopics:"), "not a topic"},
		"bad released":   {set("released-ids.json", "{"), "released-ids.json"},
		"no manifest":    {func(m fstest.MapFS) { delete(m, "manifest.yaml") }, "manifest.yaml"},
		"topic bad yaml": {set("topics/two.yaml", "id: [\n"), "topics/two.yaml"},
	}
	for name, c := range cases {
		_, err := Load(pack(c.change))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error with %q", name, err, c.want)
		}
	}
}

func TestGates(t *testing.T) {
	cases := map[string]struct {
		change func(fstest.MapFS)
		want   string
	}{
		"released id gone": {set("released-ids.json", `["one", "one.kp1", "gone.kp1.t1"]`), "gone.kp1.t1 was released"},
		"long sentence":    {edit("topics/two.yaml", "Count them.", "Count all of them one at a time from the first one to the very last one."), "readability"},
		"url":              {edit("topics/two.yaml", "Count them.", "See https://example.com"), "subresources"},
		"svg link":         {set("figures/star.svg", `<svg xmlns="http://www.w3.org/2000/svg"><image href="star.png"/></svg>`), "subresources"},
		"generate":         {edit("topics/two.yaml", "[1, 50]", "[1, 5]"), "distinct"},
	}
	for name, c := range cases {
		p, err := Load(pack(c.change))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		errs := p.Gate(100)
		var all []string
		for _, e := range errs {
			all = append(all, e.Error())
		}
		if !strings.Contains(strings.Join(all, "\n"), c.want) {
			t.Errorf("%s: gates said %q, want %q", name, all, c.want)
		}
	}
	// A transition keeps a released ID valid, and a clean SVG passes.
	p, err := Load(pack(func(m fstest.MapFS) {
		set("released-ids.json", `["zero"]`)(m)
		edit("topics.yaml", "topics:", "transitions: [{from: zero, to: [one]}]\ntopics:")(m)
		set("figures/star.svg", `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h1"/></svg>`)(m)
		// Files that never go in the pack file are not the pack's links.
		set(".git/config", "[remote \"origin\"]\n\turl = https://github.com/chinny-labs/zigrt-curriculum\n")(m)
		set(".github/workflows/ci.yml", "# https://docs.github.com/actions\n")(m)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if errs := p.Gate(50); len(errs) != 0 {
		t.Errorf("gates: %v", errs)
	}
	if len(p.Figures) != 1 || p.Figures[0] != "star" {
		t.Errorf("figures = %v", p.Figures)
	}
}

var _ fs.FS = fstest.MapFS{}
