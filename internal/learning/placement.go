package learning

import (
	"math"
	"slices"
)

// Placement is a student's place in the placement test (plan section 3.3).
// The service keeps it in the task's state.
type Placement struct {
	Evidence map[string]int  `json:"ev,omitempty"`  // right answers on a topic or on topics that require it
	Not      map[string]bool `json:"not,omitempty"` // topics settled as not known
	Asked    int             `json:"asked"`
}

func (p *Placement) init() {
	if p.Evidence == nil {
		p.Evidence = map[string]int{}
	}
	if p.Not == nil {
		p.Not = map[string]bool{}
	}
}

func (c Config) settled(p Placement, id string) bool {
	return p.Not[id] || p.Evidence[id] >= c.PlacementEvidence
}

// PlacementNext chooses the next topic to ask about, from the working set:
// the one that would settle the most topics whichever way it is answered
// (plan 3.3). A right answer is evidence for the topic and everything it
// requires; a wrong one settles the topic and everything that requires it
// as not known. It reports done when nothing is left unsettled or the
// question limit is reached.
func (c Config) PlacementNext(g *Graph, set []string, p Placement) (string, bool) {
	if p.Asked >= c.PlacementMaxQuestions {
		return "", true
	}
	in := map[string]bool{}
	for _, id := range set {
		in[id] = true
	}
	best, bestMin, bestSum := "", -1.0, -1.0
	for _, id := range set { // teaching order breaks ties
		if c.settled(p, id) {
			continue
		}
		right := 0.0
		for _, a := range append(g.Prerequisites(id), id) {
			if in[a] && !c.settled(p, a) {
				// A right answer is one step of the evidence a topic needs.
				right += 1 / float64(c.PlacementEvidence-p.Evidence[a])
			}
		}
		wrong := 0.0
		for _, d := range append(g.Dependents(id), id) {
			if in[d] && !c.settled(p, d) {
				wrong++
			}
		}
		lo, sum := math.Min(right, wrong), right+wrong
		if lo > bestMin || lo == bestMin && sum > bestSum {
			best, bestMin, bestSum = id, lo, sum
		}
	}
	return best, best == ""
}

// PlacementAnswer records an answer about a topic. "I haven't learned this"
// counts as wrong.
func (c Config) PlacementAnswer(g *Graph, set []string, p Placement, id string, right bool) Placement {
	p.init()
	p.Evidence, p.Not = clone(p.Evidence), clone(p.Not)
	p.Asked++
	in := map[string]bool{}
	for _, t := range set {
		in[t] = true
	}
	if right {
		for _, a := range append(g.Prerequisites(id), id) {
			if in[a] && !p.Not[a] {
				p.Evidence[a]++
			}
		}
		return p
	}
	for _, d := range append(g.Dependents(id), id) {
		if in[d] {
			p.Not[d] = true
		}
	}
	return p
}

// PlacementKnown lists the topics the placement test found known, in the
// working set's order. Anything not settled as known is treated as not known.
func (c Config) PlacementKnown(set []string, p Placement) []string {
	var out []string
	for _, id := range set {
		if !p.Not[id] && p.Evidence[id] >= c.PlacementEvidence {
			out = append(out, id)
		}
	}
	return out
}

// PlacementXP is the XP for a placement test: 1 per 2 questions.
func (c Config) PlacementXP(asked int) int { return asked / 2 }

func clone[V any](m map[string]V) map[string]V {
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Quiz is a student's place in a quiz (plan 3.6): a fixed list of topics,
// one problem each, and the ones missed.
type Quiz struct {
	Topics []string `json:"topics"`
	Asked  int      `json:"asked"`
	Right  int      `json:"right"`
	Missed []string `json:"missed,omitempty"`
}

// QuizTopics lays out a quiz from the topics mastered since the last one,
// newest last: between QuizMin and QuizMax problems, the newest topics first
// when there are more than fit, cycling when there are fewer.
func (c Config) QuizTopics(recent []string) []string {
	if len(recent) == 0 {
		return nil
	}
	pool := slices.Clone(recent)
	if len(pool) > c.QuizMax {
		pool = pool[len(pool)-c.QuizMax:]
	}
	n := min(c.QuizMax, max(c.QuizMin, len(pool)))
	out := make([]string, n)
	for i := range out {
		out[i] = pool[i%len(pool)]
	}
	return out
}

// QuizAnswer records one answer and reports whether the quiz is over.
func (c Config) QuizAnswer(q Quiz, right bool) (Quiz, bool) {
	topic := q.Topics[q.Asked]
	q.Asked++
	if right {
		q.Right++
	} else if !slices.Contains(q.Missed, topic) {
		q.Missed = append(slices.Clone(q.Missed), topic)
	}
	return q, q.Asked >= len(q.Topics)
}

// QuizXP scales the quiz's XP by its score, and gives none below half.
func (c Config) QuizXP(q Quiz) int {
	if q.Asked == 0 || q.Right*2 < q.Asked {
		return 0
	}
	return int(math.Round(float64(c.QuizBaseXP) * float64(q.Right) / float64(q.Asked)))
}
