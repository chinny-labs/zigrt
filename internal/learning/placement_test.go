package learning

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// runPlacement plays a placement test to the end, answering from know.
func runPlacement(c Config, g *Graph, set []string, know func(string) bool) (Placement, []string) {
	var p Placement
	var asked []string
	for {
		id, done := c.PlacementNext(g, set, p)
		if done {
			return p, asked
		}
		asked = append(asked, id)
		p = c.PlacementAnswer(g, set, p, id, know(id))
	}
}

func TestPlacementFindsTheEdge(t *testing.T) {
	c := Default()
	// A chain of eight topics, each requiring the one before.
	var topics []Topic
	for i := range 8 {
		tp := Topic{ID: fmt.Sprintf("t%d", i), Strand: "s"}
		if i > 0 {
			tp.Requires = []string{fmt.Sprintf("t%d", i-1)}
		}
		topics = append(topics, tp)
	}
	g := BuildGraph(topics, nil)
	set := g.WorkingSet([]string{"t7"})
	for edge := 0; edge <= 8; edge++ { // knows t0 .. t(edge-1)
		know := func(id string) bool { return id < fmt.Sprintf("t%d", edge) }
		p, asked := runPlacement(c, g, set, know)
		known := c.PlacementKnown(set, p)
		if len(known) != edge {
			t.Errorf("knows %d: placed %v after %v", edge, known, asked)
		}
		// Asking near the middle first finds the edge in few questions.
		if len(asked) > 8 {
			t.Errorf("knows %d: %d questions (%v)", edge, len(asked), asked)
		}
		if p.Asked != len(asked) || c.PlacementXP(p.Asked) != len(asked)/2 {
			t.Errorf("asked %d", p.Asked)
		}
	}
}

// One lucky guess does not place a topic (plan 3.3: a topic counts as known
// after two right answers on it or on topics that require it).
func TestPlacementLuckyGuess(t *testing.T) {
	c := Default()
	var topics []Topic
	for i := range 8 {
		tp := Topic{ID: fmt.Sprintf("t%d", i), Strand: "s"}
		if i > 0 {
			tp.Requires = []string{fmt.Sprintf("t%d", i-1)}
		}
		topics = append(topics, tp)
	}
	g := BuildGraph(topics, nil)
	set := g.WorkingSet([]string{"t7"})
	first := true
	p, asked := runPlacement(c, g, set, func(id string) bool {
		if first { // guesses the first question right, whatever it is
			first = false
			return true
		}
		return id <= "t2" // really knows t0 to t2
	})
	if known := c.PlacementKnown(set, p); !slices.Equal(known, []string{"t0", "t1", "t2"}) {
		t.Errorf("placed %v after %v", known, asked)
	}
}

// Whatever the answers, nothing is placed whose prerequisites are not, the
// test ends within the limit, and every topic ends settled or the limit was
// reached (plan section 12's property).
func TestPlacementProperties(t *testing.T) {
	c := Default()
	rng := rand.New(rand.NewPCG(3, 9)) //nolint:gosec // a repeatable test
	for trial := range 300 {
		n := 5 + rng.IntN(45)
		var topics []Topic
		for i := range n {
			tp := Topic{ID: fmt.Sprintf("t%02d", i), Strand: fmt.Sprint(i % 3)}
			for j := range i {
				if rng.IntN(8) == 0 {
					tp.Requires = append(tp.Requires, fmt.Sprintf("t%02d", j))
				}
			}
			topics = append(topics, tp)
		}
		g := BuildGraph(topics, nil)
		var set []string
		for _, tp := range topics {
			set = append(set, tp.ID)
		}
		answers := map[string]bool{}
		var p Placement
		var asked []string
		for {
			id, done := c.PlacementNext(g, set, p)
			if done {
				break
			}
			if c.settled(p, id) {
				t.Fatalf("trial %d: asked %s, which is already settled", trial, id)
			}
			if _, ok := answers[id]; !ok {
				answers[id] = rng.IntN(2) == 0 // random, even inconsistent, answers
			}
			asked = append(asked, id)
			p = c.PlacementAnswer(g, set, p, id, answers[id])
		}
		if len(asked) > c.PlacementMaxQuestions {
			t.Fatalf("trial %d: %d questions", trial, len(asked))
		}
		known := c.PlacementKnown(set, p)
		for _, id := range known {
			for _, pre := range g.Prerequisites(id) {
				if !slices.Contains(known, pre) {
					t.Fatalf("trial %d: %s placed without its prerequisite %s", trial, id, pre)
				}
			}
		}
		if len(asked) < c.PlacementMaxQuestions {
			for _, id := range set {
				if !c.settled(p, id) {
					t.Fatalf("trial %d: ended with %s unsettled", trial, id)
				}
			}
		}
	}
}

func TestQuiz(t *testing.T) {
	c := Default()
	if got := c.QuizTopics(nil); got != nil {
		t.Errorf("no topics: %v", got)
	}
	if got := c.QuizTopics([]string{"a", "b", "c"}); len(got) != 8 || got[3] != "a" || got[7] != "b" {
		t.Errorf("three topics: %v", got)
	}
	many := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	if got := c.QuizTopics(many); len(got) != 10 || got[0] != "c" || got[9] != "l" {
		t.Errorf("twelve topics: %v", got)
	}
	q := Quiz{Topics: c.QuizTopics([]string{"a", "b"})}
	var done bool
	for i := range 8 {
		q, done = c.QuizAnswer(q, i != 2 && i != 4 && i != 6)
		if done != (i == 7) {
			t.Fatalf("done %v after %d", done, i+1)
		}
	}
	if q.Right != 5 || !slices.Equal(q.Missed, []string{"a"}) || c.QuizXP(q) != 9 {
		t.Errorf("quiz %+v xp %d", q, c.QuizXP(q))
	}
	if c.QuizXP(Quiz{Asked: 8, Right: 3}) != 0 || c.QuizXP(Quiz{Asked: 8, Right: 8}) != 15 {
		t.Error("quiz xp scale")
	}

	// A miss brings that topic's review due today.
	g := testGraph()
	s := States{"add": {Topic: "add", State: Mastered, Stage: 3, Due: day0.Add(9)}}
	c.Apply(g, s, Event{Kind: QuizMissed, Topic: "add", On: day0})
	if s["add"].Due != day0 || s["add"].Stage != 3 {
		t.Errorf("after a miss: %+v", s["add"])
	}
}

func TestCompressionAndOrder(t *testing.T) {
	c, g := Default(), testGraph()
	course := g.WorkingSet([]string{"sub", "shapes", "clock"})
	// add fully encompasses count; both due: only add is offered.
	s := States{
		"count": {Topic: "count", State: Mastered, Due: day0.Add(-4)},
		"add":   {Topic: "add", State: Mastered, Due: day0.Add(-1)},
		"sub":   {Topic: "sub", State: Mastered, Due: day0.Add(-2)},
	}
	tasks, _ := c.NextTasks(g, Situation{Today: day0, Course: course, States: s})
	if got := topics(tasks); got != "review:sub review:add lesson:shapes lesson:clock" {
		t.Errorf("compressed: %s", got)
	}
	// sub only half-encompasses add, so add stays: the cover must be full.

	// With quizzes and placement on: placement first, then the quiz.
	c.OfferQuizzes, c.OfferPlacement = true, true
	tasks, _ = c.NextTasks(g, Situation{Today: day0, Course: course, States: States{}, NeedsPlacement: true, QuizDue: true})
	if got := topics(tasks); got != "diagnostic: quiz: lesson:count lesson:shapes" {
		t.Errorf("placement and quiz: %s", got)
	}
	s = States{"count": {Topic: "count", State: Mastered, Due: day0}, "add": {Topic: "add", State: Learning}}
	tasks, _ = c.NextTasks(g, Situation{Today: day0, Course: course, States: s, InProgress: []string{"add"}, QuizDue: true})
	if got := topics(tasks); got != "resume:add quiz: review:count lesson:shapes" {
		t.Errorf("quiz after a lesson in progress: %s", got)
	}
	tasks, _ = c.NextTasks(g, Situation{Today: day0, Course: course, States: States{}, PlacementOpen: true})
	if len(tasks) == 0 || tasks[0].Kind != TaskPlacement || !tasks[0].Resume {
		t.Errorf("open placement: %s", topics(tasks))
	}
	// Switched off.
	c = Default()
	c.OfferPlacement, c.OfferQuizzes = false, false
	tasks, _ = c.NextTasks(g, Situation{Today: day0, Course: course, States: States{}, NeedsPlacement: true, QuizDue: true})
	if got := topics(tasks); got != "lesson:count lesson:shapes" {
		t.Errorf("switched off: %s", got)
	}
}
