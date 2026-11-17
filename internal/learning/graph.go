package learning

import (
	"slices"

	"github.com/chinny-labs/zigrt/content"
)

// Topic is what the rules need to know about one topic.
type Topic struct {
	ID             string
	Strand         string
	Grade          string
	Order          int // teaching order
	Requires       []string
	Encompasses    []Edge // topics this one exercises
	EncompassedBy  []Edge // topics that exercise this one
	XP             int
	FluencySeconds int
}

// Edge is an encompasses edge seen from one end.
type Edge struct {
	Topic  string
	Weight float64
}

// Graph is the topic graph of a pack, read-only once built.
type Graph struct {
	topics      map[string]*Topic
	order       []*Topic
	transitions map[string][]string
}

// NewGraph builds the graph from a loaded pack.
func NewGraph(p *content.Pack) *Graph {
	topics := make([]Topic, 0, len(p.Topics))
	for _, ct := range p.Topics {
		t := Topic{
			ID: ct.ID, Strand: ct.Strand, Grade: ct.Grade, Order: ct.Order,
			Requires: slices.Clone(ct.Requires), XP: ct.XP, FluencySeconds: ct.FluencySeconds,
		}
		for _, e := range ct.Encompasses {
			t.Encompasses = append(t.Encompasses, Edge{Topic: e.Topic, Weight: e.Weight})
		}
		topics = append(topics, t)
	}
	transitions := map[string][]string{}
	for _, tr := range p.Transitions {
		transitions[tr.From] = slices.Clone(tr.To)
	}
	return BuildGraph(topics, transitions)
}

// BuildGraph builds a graph from topics in teaching order (their Order is
// set from the position) and transitions from an old ID to the new ones.
// EncompassedBy is worked out from Encompasses.
func BuildGraph(topics []Topic, transitions map[string][]string) *Graph {
	g := &Graph{topics: map[string]*Topic{}, transitions: map[string][]string{}}
	for i, t := range topics {
		t.Order = i
		t.EncompassedBy = nil
		g.topics[t.ID] = &t
		g.order = append(g.order, &t)
	}
	for _, t := range g.order {
		for _, e := range t.Encompasses {
			if a := g.topics[e.Topic]; a != nil {
				a.EncompassedBy = append(a.EncompassedBy, Edge{Topic: t.ID, Weight: e.Weight})
			}
		}
	}
	for from, to := range transitions {
		g.transitions[from] = slices.Clone(to)
	}
	return g
}

// Topic returns a topic by ID, or nil.
func (g *Graph) Topic(id string) *Topic { return g.topics[id] }

// Topics lists every topic in teaching order.
func (g *Graph) Topics() []*Topic { return g.order }

// Resolve maps a topic ID from the history to the topics it is now: itself
// when it still exists, else what its transitions point to (plan section
// 4.5). A topic that is gone without a transition resolves to itself, so its
// history is kept even though no lesson offers it.
func (g *Graph) Resolve(id string) []string {
	if _, ok := g.topics[id]; ok {
		return []string{id}
	}
	seen := map[string]bool{id: true}
	var out []string
	var walk func(string)
	walk = func(from string) {
		for _, to := range g.transitions[from] {
			if seen[to] {
				continue
			}
			seen[to] = true
			if _, ok := g.topics[to]; ok {
				out = append(out, to)
			} else {
				walk(to)
			}
		}
	}
	walk(id)
	if len(out) == 0 {
		return []string{id}
	}
	return out
}

// WorkingSet is what a course asks a student to master: its topics and,
// transitively, everything they require, in teaching order (the diagnostic
// later places a student inside it).
func (g *Graph) WorkingSet(course []string) []string {
	in := map[string]bool{}
	var add func(string)
	add = func(id string) {
		t := g.topics[id]
		if t == nil || in[id] {
			return
		}
		in[id] = true
		for _, r := range t.Requires {
			add(r)
		}
	}
	for _, id := range course {
		add(id)
	}
	var out []string
	for _, t := range g.order {
		if in[t.ID] {
			out = append(out, t.ID)
		}
	}
	return out
}
