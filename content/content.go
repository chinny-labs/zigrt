// SPDX-License-Identifier: Apache-2.0

// Package content loads and validates a content pack (plan section 4): the
// topic graph, the lessons and their problem templates. A pack is read from
// any fs.FS, so the same code reads a folder, an embedded pack and a pack
// file (a zip archive). A loaded pack is immutable.
package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/chinny-labs/zigrt/figure"
	"github.com/chinny-labs/zigrt/problem"
)

// Text is localised pack text.
type Text = problem.Text

// Grades, youngest first.
var Grades = []string{"K", "1", "2", "3", "4", "5"}

// Manifest is manifest.yaml.
type Manifest struct {
	ID            string   `yaml:"id"`
	Title         Text     `yaml:"title"`
	Version       string   `yaml:"version"`         // calendar version, like 2027.02.1
	MinAppVersion string   `yaml:"min_app_version"` // like 0.3.0
	Licence       string   `yaml:"licence"`
	Locales       []string `yaml:"locales"`
	Courses       []Course `yaml:"courses"`
}

// Course is a named target set within the graph, such as "Grade 2".
type Course struct {
	ID     string   `yaml:"id"`
	Title  Text     `yaml:"title"`
	Grades []string `yaml:"grades"`
	Topics []string `yaml:"topics"` // extra topics outside those grades
}

// Strand is a face of the tower, such as fractions.
type Strand struct {
	ID    string `yaml:"id"`
	Title Text   `yaml:"title"`
}

// Edge is an encompasses edge: doing this topic exercises Topic at Weight.
type Edge struct {
	Topic  string  `yaml:"topic"`
	Weight float64 `yaml:"weight"`
}

// Topic is one node of the graph with its lesson.
type Topic struct {
	ID             string   `yaml:"id"`
	Title          Text     `yaml:"title"`
	Strand         string   `yaml:"strand"`
	Grade          string   `yaml:"grade"`
	Requires       []string `yaml:"requires"`
	Encompasses    []Edge   `yaml:"encompasses"`
	XP             int      `yaml:"xp"`
	Standards      []string `yaml:"standards"` // shown to parents only
	FluencySeconds int      `yaml:"fluency_seconds"`

	Order int   `yaml:"-"` // teaching order, from the position in topics.yaml
	KPs   []*KP `yaml:"-"`
}

// KP is a knowledge point: an explanation, a worked example and practice.
type KP struct {
	ID        string              `yaml:"id"`
	Explain   Text                `yaml:"explain"`
	Example   string              `yaml:"example"` // a template ID; the first template by default
	Templates []*problem.Template `yaml:"templates"`
}

// Transition records a split or merged topic (plan section 4.5).
type Transition struct {
	From string   `yaml:"from"`
	To   []string `yaml:"to"`
}

type topicsFile struct {
	Strands     []Strand     `yaml:"strands"`
	Topics      []*Topic     `yaml:"topics"`
	Transitions []Transition `yaml:"transitions"`
}

type lessonFile struct {
	ID  string `yaml:"id"`
	KPs []*KP  `yaml:"kps"`
}

// Pack is a loaded, structurally valid content pack.
type Pack struct {
	Manifest    Manifest
	Strands     []Strand
	Topics      []*Topic // in teaching order
	Transitions []Transition
	Released    []string // released-ids.json
	Figures     []string // static figures in figures/

	fsys      fs.FS
	static    map[string]staticFigure
	topics    map[string]*Topic
	kps       map[string]*KP
	templates map[string]*problem.Template
	owner     map[string]*Topic // template ID to its topic
	ids       map[string]string // every ID to what it names
}

type staticFigure struct {
	svg string
	err error
}

// StaticFigure returns a static figure from figures/, cleaned of anything
// but drawing (figure.Clean), as an SVG element.
func (p *Pack) StaticFigure(name string) (string, error) {
	sf, ok := p.static[name]
	if !ok {
		return "", fmt.Errorf("no figure %q in the pack", name)
	}
	return sf.svg, sf.err
}

// Topic returns a topic by ID, or nil.
func (p *Pack) Topic(id string) *Topic { return p.topics[id] }

// Template returns a template by ID, or nil.
func (p *Pack) Template(id string) *problem.Template { return p.templates[id] }

// TopicOf returns the topic a template belongs to, or nil.
func (p *Pack) TopicOf(templateID string) *Topic { return p.owner[templateID] }

// KP returns a knowledge point by ID, or nil.
func (p *Pack) KP(id string) *KP { return p.kps[id] }

// ExampleTemplate is the template a knowledge point's worked example uses.
func (k *KP) ExampleTemplate() *problem.Template {
	for _, t := range k.Templates {
		if t.ID == k.Example {
			return t
		}
	}
	return k.Templates[0]
}

// CourseTopics lists the topics a course targets, in teaching order.
func (p *Pack) CourseTopics(courseID string) []*Topic {
	i := slices.IndexFunc(p.Manifest.Courses, func(c Course) bool { return c.ID == courseID })
	if i < 0 {
		return nil
	}
	c := p.Manifest.Courses[i]
	var out []*Topic
	for _, t := range p.Topics {
		if slices.Contains(c.Grades, t.Grade) || slices.Contains(c.Topics, t.ID) {
			out = append(out, t)
		}
	}
	return out
}

var (
	idRE      = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	versionRE = regexp.MustCompile(`^\d{4}\.\d{2}\.\d+$`)
	semverRE  = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
)

// maxErrors bounds how many problems Load reports at once.
const maxErrors = 50

type errList struct{ errs []error }

func (l *errList) add(format string, args ...any) {
	if len(l.errs) < maxErrors {
		l.errs = append(l.errs, fmt.Errorf(format, args...))
	}
}

func (l *errList) err() error { return errors.Join(l.errs...) }

// Load reads a pack and checks its structure: the files, IDs, edges and
// template syntax (the structure gate in plan section 4.6). It does not
// generate problems; Gate does.
func Load(fsys fs.FS) (*Pack, error) {
	p := &Pack{
		fsys:      fsys,
		topics:    map[string]*Topic{},
		kps:       map[string]*KP{},
		templates: map[string]*problem.Template{},
		owner:     map[string]*Topic{},
	}
	var errs errList

	if err := decode(fsys, "manifest.yaml", &p.Manifest); err != nil {
		return nil, err
	}
	var tf topicsFile
	if err := decode(fsys, "topics.yaml", &tf); err != nil {
		return nil, err
	}
	p.Strands, p.Topics, p.Transitions = tf.Strands, tf.Topics, tf.Transitions
	if b, err := fs.ReadFile(fsys, "released-ids.json"); err == nil {
		if err := json.Unmarshal(b, &p.Released); err != nil {
			return nil, fmt.Errorf("released-ids.json: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if figs, err := fs.Glob(fsys, "figures/*.svg"); err == nil {
		p.static = map[string]staticFigure{}
		for _, f := range figs {
			name := strings.TrimSuffix(path.Base(f), ".svg")
			p.Figures = append(p.Figures, name)
			// Packs are untrusted: a static figure is drawn only as Clean
			// rewrote it, and one that does not clean fails the gates.
			var sf staticFigure
			if b, err := fs.ReadFile(fsys, f); err != nil {
				sf.err = err
			} else {
				sf.svg, sf.err = figure.Clean(b)
			}
			p.static[name] = sf
		}
	}

	p.checkManifest(&errs)

	ids := map[string]string{} // every ID in the pack, to what it names
	claim := func(id, what string) {
		if !idRE.MatchString(id) {
			errs.add("%s %q: IDs are lower case letters, digits, dots and dashes", what, id)
			return
		}
		if prev, ok := ids[id]; ok {
			errs.add("%s %q: the ID is already used by a %s", what, id, prev)
			return
		}
		ids[id] = what
	}

	strands := map[string]bool{}
	for _, s := range p.Strands {
		claim(s.ID, "strand")
		strands[s.ID] = true
		if s.Title == nil {
			errs.add("strand %s: no title", s.ID)
		}
	}
	if len(p.Topics) == 0 {
		errs.add("topics.yaml: there are no topics")
	}
	for i, t := range p.Topics {
		t.Order = i + 1
		claim(t.ID, "topic")
		p.topics[t.ID] = t
		if t.Title == nil {
			errs.add("topic %s: no title", t.ID)
		}
		if !strands[t.Strand] {
			errs.add("topic %s: strand %q is not in the strands list", t.ID, t.Strand)
		}
		if !slices.Contains(Grades, t.Grade) {
			errs.add("topic %s: grade %q is not one of %v", t.ID, t.Grade, Grades)
		}
		if t.XP < 1 || t.XP > 50 {
			errs.add("topic %s: xp must be 1 to 50", t.ID)
		}
		if t.FluencySeconds < 0 || t.FluencySeconds > 120 {
			errs.add("topic %s: fluency_seconds must be 0 to 120", t.ID)
		}
	}

	// Lessons: one file per topic, and no file without a topic.
	files, _ := fs.Glob(fsys, "topics/*.yaml")
	seen := map[string]bool{}
	for _, f := range files {
		id := strings.TrimSuffix(path.Base(f), ".yaml")
		seen[id] = true
		t := p.topics[id]
		if t == nil {
			errs.add("%s: there is no topic %q in topics.yaml", f, id)
			continue
		}
		var lf lessonFile
		if err := decode(fsys, f, &lf); err != nil {
			errs.add("%v", err)
			continue
		}
		if lf.ID != id {
			errs.add("%s: id is %q, not the file name", f, lf.ID)
		}
		t.KPs = lf.KPs
		p.checkLesson(t, f, claim, &errs)
	}
	for _, t := range p.Topics {
		if !seen[t.ID] {
			errs.add("topic %s: there is no topics/%s.yaml", t.ID, t.ID)
		}
	}

	p.ids = ids
	p.checkEdges(&errs)
	p.checkTransitions(&errs)
	if err := errs.err(); err != nil {
		return nil, err
	}
	return p, nil
}

func decode(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // a misspelt key is an error, not silently ignored
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (p *Pack) checkManifest(errs *errList) {
	m := p.Manifest
	if !idRE.MatchString(m.ID) {
		errs.add("manifest: id %q is not a valid ID", m.ID)
	}
	if m.Title == nil {
		errs.add("manifest: no title")
	}
	if !versionRE.MatchString(m.Version) {
		errs.add("manifest: version %q is not a calendar version like 2027.02.1", m.Version)
	}
	if !semverRE.MatchString(m.MinAppVersion) {
		errs.add("manifest: min_app_version %q is not a version like 0.3.0", m.MinAppVersion)
	}
	if m.Licence == "" {
		errs.add("manifest: no licence")
	}
	if !slices.Contains(m.Locales, problem.DefaultLocale) {
		errs.add("manifest: locales must include %q", problem.DefaultLocale)
	}
	if len(m.Courses) == 0 {
		errs.add("manifest: there are no courses")
	}
	for _, c := range m.Courses {
		if !idRE.MatchString(c.ID) || c.Title == nil {
			errs.add("course %q: needs an ID and a title", c.ID)
		}
		for _, g := range c.Grades {
			if !slices.Contains(Grades, g) {
				errs.add("course %s: grade %q is not one of %v", c.ID, g, Grades)
			}
		}
	}
}

func (p *Pack) checkLesson(t *Topic, file string, claim func(id, what string), errs *errList) {
	if len(t.KPs) == 0 || len(t.KPs) > 4 {
		errs.add("topic %s: a lesson has 1 to 4 knowledge points, not %d", t.ID, len(t.KPs))
	}
	for _, k := range t.KPs {
		claim(k.ID, "knowledge point")
		p.kps[k.ID] = k
		if !strings.HasPrefix(k.ID, t.ID+".") {
			errs.add("%s: knowledge point %s must start with %s.", file, k.ID, t.ID)
		}
		if k.Explain == nil {
			errs.add("%s: knowledge point %s has no explanation", file, k.ID)
		}
		if len(k.Templates) == 0 {
			errs.add("%s: knowledge point %s has no templates", file, k.ID)
		}
		for _, tpl := range k.Templates {
			claim(tpl.ID, "template")
			p.templates[tpl.ID] = tpl
			p.owner[tpl.ID] = t
			if !strings.HasPrefix(tpl.ID, k.ID+".") {
				errs.add("%s: template %s must start with %s.", file, tpl.ID, k.ID)
			}
			if err := tpl.CheckFigure(p.Figures); err != nil {
				errs.add("%s: %v", file, err)
			}
		}
		if k.Example != "" && !slices.ContainsFunc(k.Templates, func(x *problem.Template) bool { return x.ID == k.Example }) {
			errs.add("%s: knowledge point %s: example %q is not one of its templates", file, k.ID, k.Example)
		}
	}
}

func (p *Pack) checkEdges(errs *errList) {
	for _, t := range p.Topics {
		for _, r := range t.Requires {
			if p.topics[r] == nil {
				errs.add("topic %s: requires %q, which is not a topic", t.ID, r)
			}
			if r == t.ID {
				errs.add("topic %s: requires itself", t.ID)
			}
		}
		for _, e := range t.Encompasses {
			if p.topics[e.Topic] == nil {
				errs.add("topic %s: encompasses %q, which is not a topic", t.ID, e.Topic)
			}
			if e.Topic == t.ID {
				errs.add("topic %s: encompasses itself", t.ID)
			}
			if e.Weight <= 0 || e.Weight > 1 {
				errs.add("topic %s: the weight on %s must be above 0 and at most 1", t.ID, e.Topic)
			}
		}
	}
	if cycle := p.findCycle(); cycle != nil {
		errs.add("the graph has a cycle: %s", strings.Join(cycle, " → "))
	}
}

// findCycle looks for a cycle through requires and encompasses edges, which
// both point from a topic to something learnt before it.
func (p *Pack) findCycle() []string {
	const (
		white = iota
		grey
		black
	)
	colour := map[string]int{}
	var stack []string
	var visit func(id string) []string
	visit = func(id string) []string {
		colour[id] = grey
		stack = append(stack, id)
		t := p.topics[id]
		next := slices.Clone(t.Requires)
		for _, e := range t.Encompasses {
			next = append(next, e.Topic)
		}
		for _, n := range next {
			if p.topics[n] == nil {
				continue
			}
			switch colour[n] {
			case grey:
				i := slices.Index(stack, n)
				return append(slices.Clone(stack[i:]), n)
			case white:
				if c := visit(n); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		colour[id] = black
		return nil
	}
	for _, t := range p.Topics {
		if colour[t.ID] == white {
			if c := visit(t.ID); c != nil {
				return c
			}
		}
	}
	return nil
}

func (p *Pack) checkTransitions(errs *errList) {
	for _, tr := range p.Transitions {
		if p.topics[tr.From] != nil {
			errs.add("transition from %s: that topic still exists", tr.From)
		}
		if len(tr.To) == 0 {
			errs.add("transition from %s: no topics to move to", tr.From)
		}
		for _, to := range tr.To {
			if p.topics[to] == nil {
				errs.add("transition from %s: %q is not a topic", tr.From, to)
			}
		}
	}
}

// CheckApp reports whether an app version can use the pack. Development
// builds, whose version is not a release number, can use any pack.
func (p *Pack) CheckApp(appVersion string) error {
	have := semverRE.FindStringSubmatch(strings.TrimPrefix(appVersion, "v"))
	if have == nil {
		return nil
	}
	need := semverRE.FindStringSubmatch(p.Manifest.MinAppVersion)
	for i := 1; i <= 3; i++ {
		h, _ := strconv.Atoi(have[i])
		n, _ := strconv.Atoi(need[i])
		if h != n {
			if h < n {
				return fmt.Errorf("content pack %s needs Zigrt %s or later; this is %s", p.Manifest.Version, p.Manifest.MinAppVersion, appVersion)
			}
			return nil
		}
	}
	return nil
}
