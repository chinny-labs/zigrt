package learning

import (
	"maps"
	"reflect"
	"testing"
	"time"
)

// A small graph: counting, then adding, which requires and fully
// encompasses counting; taking away requires adding and half-encompasses it;
// shapes is on its own strand.
func testGraph() *Graph {
	return BuildGraph([]Topic{
		{ID: "count", Strand: "number"},
		{ID: "add", Strand: "number", Requires: []string{"count"}, Encompasses: []Edge{{"count", 1.0}}, XP: 10},
		{ID: "shapes", Strand: "geometry"},
		{ID: "sub", Strand: "number", Requires: []string{"add"}, Encompasses: []Edge{{"add", 0.5}}, XP: 12, FluencySeconds: 6},
		{ID: "clock", Strand: "measure", Requires: []string{"count"}},
	}, map[string][]string{"old-add": {"add"}})
}

const day0 = Date(20000)

func TestDates(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 03:00 UTC on the 8th is still the 7th in New York.
	at := time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC)
	if got := DateOf(at, ny).String(); got != "2026-03-07" {
		t.Errorf("DateOf = %s", got)
	}
	d, err := ParseDate("2026-03-07")
	if err != nil || d != DateOf(at, ny) || d.Add(1).String() != "2026-03-08" || d.Weekday() != time.Saturday {
		t.Errorf("ParseDate = %v %v", d, err)
	}
	if _, err := ParseDate("7 March"); err == nil {
		t.Error("bad date parsed")
	}
	if got := RestDays("06x6"); !reflect.DeepEqual(got, []time.Weekday{time.Sunday, time.Saturday}) {
		t.Errorf("RestDays = %v", got)
	}
}

func TestReviewSchedule(t *testing.T) {
	c, g := Default(), testGraph()
	s := States{}
	c.Apply(g, s, Event{Kind: LessonPassed, Topic: "count", On: day0})
	if st := s["count"]; st.State != Mastered || st.Stage != 0 || st.Due != day0.Add(1) {
		t.Fatalf("after the lesson: %+v", st)
	}
	// Passing every review on its due day walks the intervals, and the last
	// one repeats.
	on := s["count"].Due
	for i, want := range []int{3, 7, 14, 30, 60, 120, 240, 240} {
		c.Apply(g, s, Event{Kind: ReviewPassed, Topic: "count", On: on})
		st := s["count"]
		if st.Stage != i+1 || st.Due.Sub(on) != want {
			t.Fatalf("review %d: stage %d, due in %d days; want stage %d in %d", i+1, st.Stage, st.Due.Sub(on), i+1, want)
		}
		on = st.Due
	}
}

func TestFailedReviews(t *testing.T) {
	c, g := Default(), testGraph()
	s := States{"count": {Topic: "count", State: Mastered, Stage: 4, Due: day0}}
	ch := c.Apply(g, s, Event{Kind: ReviewFailed, Topic: "count", On: day0})
	if st := s["count"]; st.Stage != 2 || st.Due != day0.Add(1) || st.FailStreak != 1 || st.State != Mastered {
		t.Fatalf("one fail: %+v", st)
	}
	if len(ch.Events) != 1 || ch.Events[0].Stage != 2 || ch.Events[0].Due != day0.Add(1) {
		t.Errorf("events %+v", ch.Events)
	}
	// A pass in between resets the run; two fails in a row reopen the lesson.
	c.Apply(g, s, Event{Kind: ReviewPassed, Topic: "count", On: day0.Add(1)})
	c.Apply(g, s, Event{Kind: ReviewFailed, Topic: "count", On: day0.Add(2)})
	if s["count"].State != Mastered {
		t.Fatal("a fail after a pass reopened the lesson")
	}
	ch = c.Apply(g, s, Event{Kind: ReviewFailed, Topic: "count", On: day0.Add(3)})
	if st := s["count"]; st.State != Learning || st.Stage != 0 || st.FailStreak != 0 {
		t.Fatalf("two fails: %+v", st)
	}
	if kinds(ch) != "review_failed reopened" {
		t.Errorf("events: %s", kinds(ch))
	}
	if got := s.Status(g, "add"); got != Locked {
		t.Errorf("add is %s once count reopened, want locked", got)
	}
	// Stage never goes below 0.
	s["count"] = TopicState{Topic: "count", State: Mastered, Stage: 1, Due: day0}
	c.Apply(g, s, Event{Kind: ReviewFailed, Topic: "count", On: day0})
	if s["count"].Stage != 0 {
		t.Errorf("stage %d", s["count"].Stage)
	}
}

func kinds(ch Change) string {
	out := ""
	for i, e := range ch.Events {
		if i > 0 {
			out += " "
		}
		out += string(e.Kind)
		if e.Source != "" && e.Source != e.Topic {
			out += ":" + e.Topic
		}
	}
	return out
}

func TestImplicitCredit(t *testing.T) {
	c, g := Default(), testGraph()

	// Weight 1.0 on a due topic counts as its review: count advances a stage.
	s := States{"count": {Topic: "count", State: Mastered, Stage: 2, Due: day0}}
	ch := c.Apply(g, s, Event{Kind: LessonPassed, Topic: "add", On: day0})
	if st := s["count"]; st.Stage != 3 || st.Due != day0.Add(14) {
		t.Errorf("due count: %+v", st)
	}
	if kinds(ch) != "lesson_passed implicit_credit:count" {
		t.Errorf("events: %s", kinds(ch))
	}

	// Weight 0.5 on a topic not yet due moves it out by half its interval
	// (stage 2: 7 days, so 4 after rounding), capped at what a passed review
	// would give (stage 3 from today: 14 days).
	s = States{"add": {Topic: "add", State: Mastered, Stage: 2, Due: day0.Add(5)}}
	c.Apply(g, s, Event{Kind: LessonPassed, Topic: "sub", On: day0})
	if st := s["add"]; st.Stage != 2 || st.Due != day0.Add(9) {
		t.Errorf("not-due add: %+v", st)
	}
	s = States{"add": {Topic: "add", State: Mastered, Stage: 2, Due: day0.Add(12)}}
	c.Apply(g, s, Event{Kind: LessonPassed, Topic: "sub", On: day0})
	if st := s["add"]; st.Due != day0.Add(14) {
		t.Errorf("capped add: %+v", st)
	}
	// Already further out than the cap: nothing changes and nothing is logged.
	s = States{"add": {Topic: "add", State: Mastered, Stage: 2, Due: day0.Add(30)}}
	ch = c.Apply(g, s, Event{Kind: LessonPassed, Topic: "sub", On: day0})
	if s["add"].Due != day0.Add(30) || kinds(ch) != "lesson_passed" {
		t.Errorf("far add: %+v %s", s["add"], kinds(ch))
	}

	// Only mastered topics get credit, and credit is not transitive.
	s = States{"count": {Topic: "count", State: Mastered, Stage: 0, Due: day0}}
	c.Apply(g, s, Event{Kind: LessonPassed, Topic: "sub", On: day0})
	if _, ok := s["add"]; ok || s["count"].Stage != 0 {
		t.Errorf("credit leaked: %+v", s)
	}
}

func TestFailureFlowsUp(t *testing.T) {
	c, g := Default(), testGraph()
	s := States{
		"count": {Topic: "count", State: Mastered, Stage: 3, Due: day0},
		"add":   {Topic: "add", State: Mastered, Stage: 4, Due: day0.Add(20)},
		"clock": {Topic: "clock", State: Mastered, Stage: 4, Due: day0.Add(20)},
	}
	ch := c.Apply(g, s, Event{Kind: ReviewFailed, Topic: "count", On: day0})
	if s["add"].Due != day0.Add(10) {
		t.Errorf("add due in %d days, want 10", s["add"].Due.Sub(day0))
	}
	if s["clock"].Due != day0.Add(20) {
		t.Error("clock does not encompass count, but moved")
	}
	if kinds(ch) != "review_failed pulled_forward:add" {
		t.Errorf("events: %s", kinds(ch))
	}
}

func TestPausesHoldTheLesson(t *testing.T) {
	c, g := Default(), testGraph()
	s := States{
		"count": {Topic: "count", State: Mastered, Stage: 3, Due: day0.Add(10)},
		"clock": {Topic: "clock", State: Mastered, Stage: 3, Due: day0.Add(10)},
	}
	c.Apply(g, s, Event{Kind: LessonStarted, Topic: "add", On: day0})
	if ch := c.Apply(g, s, Event{Kind: LessonPaused, Topic: "add", On: day0}); kinds(ch) != "lesson_paused" {
		t.Errorf("first pause: %s", kinds(ch))
	}
	ch := c.Apply(g, s, Event{Kind: LessonPaused, Topic: "add", On: day0.Add(1)})
	if kinds(ch) != "lesson_paused pulled_forward:count" || s["count"].Due != day0.Add(1) {
		t.Fatalf("second pause: %s, count due %v", kinds(ch), s["count"].Due)
	}
	sit := Situation{Today: day0.Add(2), Course: []string{"count", "add", "clock"}, States: s, InProgress: []string{"add"}}
	tasks, _ := c.NextTasks(g, sit)
	if got := topics(tasks); got != "review:count" {
		t.Errorf("with count due: %s", got)
	}
	c.Apply(g, s, Event{Kind: ReviewPassed, Topic: "count", On: day0.Add(2)})
	tasks, _ = c.NextTasks(g, sit)
	if got := topics(tasks); got != "resume:add" {
		t.Errorf("after the review: %s", got)
	}
	// Passing the lesson clears the pauses.
	c.Apply(g, s, Event{Kind: LessonPassed, Topic: "add", On: day0.Add(2)})
	if s["add"].Pauses != 0 {
		t.Errorf("pauses %d", s["add"].Pauses)
	}
}

// TestHeldLessonsJumpTheQueue: a lesson stopped twice waits for its
// prerequisites' reviews, so those go ahead of a backlog of older reviews,
// and are offered even when another due review would cover them. Without
// that, a struggling student with a long backlog never got back to the
// lesson (simulated: months stuck on one topic).
func TestHeldLessonsJumpTheQueue(t *testing.T) {
	c, g := Default(), testGraph()
	s := States{
		"shapes": {Topic: "shapes", State: Mastered, Stage: 1, Due: day0.Add(-20)},
		"add":    {Topic: "add", State: Mastered, Stage: 1, Due: day0.Add(-10)},
		"count":  {Topic: "count", State: Mastered, Stage: 2, Due: day0.Add(-1)},
		"clock":  {Topic: "clock", State: Learning, Pauses: 2},
	}
	sit := Situation{Today: day0, Course: []string{"count", "add", "shapes", "sub", "clock"}, States: s, InProgress: []string{"clock"}}
	tasks, _ := c.NextTasks(g, sit)
	// count is the least overdue and add (due) fully covers it, yet it
	// comes first: clock is waiting on it.
	if got := topics(tasks); got != "review:count review:shapes review:add lesson:sub" {
		t.Fatalf("with clock held: %s", got)
	}
	// Once count is reviewed, clock is back at the front.
	c.Apply(g, s, Event{Kind: ReviewPassed, Topic: "count", On: day0})
	tasks, _ = c.NextTasks(g, sit)
	if got := topics(tasks); got != "resume:clock review:shapes review:add lesson:sub" {
		t.Fatalf("after count's review: %s", got)
	}
	// A held lesson with no task (set aside, or reopened) still pulls its
	// prerequisites forward.
	s["count"] = TopicState{Topic: "count", State: Mastered, Stage: 2, Due: day0.Add(-1)}
	sit.InProgress = nil
	tasks, _ = c.NextTasks(g, sit)
	if got := topics(tasks); got != "review:count review:shapes review:add lesson:sub" {
		t.Fatalf("held without a task: %s", got)
	}
	// Not held while paused today: the backlog keeps its usual order.
	sit.PausedToday = map[string]bool{"clock": true}
	tasks, _ = c.NextTasks(g, sit)
	if got := topics(tasks); got != "review:shapes review:add lesson:sub" {
		t.Fatalf("paused today: %s", got)
	}
}

func topics(ns []Next) string {
	out := ""
	for i, n := range ns {
		if i > 0 {
			out += " "
		}
		k := string(n.Kind)
		if n.Resume {
			k = "resume"
		}
		if n.Early {
			k = "early"
		}
		out += k + ":" + n.Topic
	}
	return out
}

func TestNextTasks(t *testing.T) {
	c, g := Default(), testGraph()
	course := g.WorkingSet([]string{"sub", "shapes", "clock"})
	if len(course) != 5 {
		t.Fatalf("working set %v", course)
	}
	sit := func(s States) Situation { return Situation{Today: day0, Course: course, States: s} }

	// A new student starts at the roots, alternating strands.
	tasks, done := c.NextTasks(g, sit(States{}))
	if got := topics(tasks); got != "lesson:count lesson:shapes" || done {
		t.Errorf("new student: %s", got)
	}

	// Reviews come first, most overdue first, then lessons; at most four.
	s := States{
		"count":  {Topic: "count", State: Mastered, Due: day0.Add(-1)},
		"shapes": {Topic: "shapes", State: Mastered, Due: day0.Add(-3)},
		"add":    {Topic: "add", State: Mastered, Due: day0.Add(5)},
	}
	tasks, _ = c.NextTasks(g, sit(s))
	if got := topics(tasks); got != "review:shapes review:count lesson:sub lesson:clock" {
		t.Errorf("due reviews: %s", got)
	}
	st := sit(s)
	st.LastStrand = "number"
	tasks, _ = c.NextTasks(g, st)
	if got := topics(tasks); got != "review:shapes review:count lesson:clock lesson:sub" {
		t.Errorf("alternating after a number lesson: %s", got)
	}

	// After three reviews in a row a lesson goes first.
	st = sit(s)
	st.ReviewsInRow = 3
	tasks, _ = c.NextTasks(g, st)
	if got := topics(tasks); got != "lesson:sub review:shapes review:count lesson:clock" {
		t.Errorf("after 3 reviews: %s", got)
	}

	// A lesson in progress comes first, unless it paused today.
	s = States{"count": {Topic: "count", State: Mastered, Due: day0}, "add": {Topic: "add", State: Learning}}
	st = sit(s)
	st.InProgress = []string{"add"}
	tasks, _ = c.NextTasks(g, st)
	if got := topics(tasks); got != "resume:add review:count lesson:shapes lesson:clock" {
		t.Errorf("in progress: %s", got)
	}
	st.PausedToday = map[string]bool{"add": true}
	tasks, _ = c.NextTasks(g, st)
	if got := topics(tasks); got != "review:count lesson:shapes lesson:clock" {
		t.Errorf("paused today: %s", got)
	}

	// A reopened lesson with no task yet comes before new ones.
	s = States{"count": {Topic: "count", State: Learning}}
	tasks, _ = c.NextTasks(g, sit(s))
	if got := topics(tasks); got != "lesson:count lesson:shapes" {
		t.Errorf("reopened: %s", got)
	}

	// Everything mastered and nothing due: the course is complete and early
	// reviews are offered, soonest first.
	s = States{}
	for i, id := range course {
		s[id] = TopicState{Topic: id, State: Mastered, Due: day0.Add(10 - i)}
	}
	tasks, done = c.NextTasks(g, sit(s))
	if got := topics(tasks); !done || got != "early:clock early:sub early:shapes early:add" {
		t.Errorf("complete: %v %s", done, got)
	}
}

func TestLesson(t *testing.T) {
	c := Default()
	var l Lesson
	step := func(right bool, want Step) {
		t.Helper()
		var got Step
		l, got = c.LessonAnswer(l, right, 2)
		if got != want {
			t.Fatalf("after %v: step %d, want %d (lesson %+v)", right, got, want, l)
		}
	}
	step(true, StepContinue)
	step(false, StepContinue) // breaks the streak
	step(true, StepContinue)
	step(true, StepKPPassed)
	if l.KP != 1 || l.Streak != 0 || l.Misses != 0 || l.Retries[0] != 1 {
		t.Fatalf("on kp2: %+v", l)
	}
	step(false, StepContinue)
	step(false, StepContinue)
	step(false, StepPaused)
	if l.KP != 1 || l.Misses != 0 || l.Retries[1] != 3 {
		t.Fatalf("paused: %+v", l)
	}
	step(true, StepContinue)
	step(true, StepPassed)

	topic := &Topic{XP: 10}
	if xp := c.LessonXP(topic, l); xp != 10 {
		t.Errorf("xp with a retried kp: %d", xp)
	}
	if xp := c.LessonXP(topic, Lesson{Retries: []int{1, 0}}); xp != 12 {
		t.Errorf("xp with the bonus: %d", xp)
	}
	if xp := c.LessonXP(&Topic{}, Lesson{}); xp != 12 {
		t.Errorf("default xp with the bonus: %d", xp)
	}
}

func TestReview(t *testing.T) {
	c := Default()
	run := func(rights []bool, ms int64) (Review, bool) {
		var r Review
		var done bool
		for i, right := range rights {
			r, done = c.ReviewAnswer(r, right, ms)
			if done != (i == len(rights)-1) {
				t.Fatalf("done=%v after %d of %d", done, i+1, len(rights))
			}
		}
		return r, done
	}
	plain, fluent := &Topic{}, &Topic{FluencySeconds: 6}
	r, _ := run([]bool{true, true, false, true, true}, 2000)
	if !c.ReviewPassed(r, plain, true) || c.ReviewXP(r, true) != 4 {
		t.Errorf("4 of 5: passed=%v xp=%d", c.ReviewPassed(r, plain, true), c.ReviewXP(r, true))
	}
	r, _ = run([]bool{true, true, true, true, true}, 2000)
	if c.ReviewXP(r, c.ReviewPassed(r, plain, true)) != 5 {
		t.Error("a perfect review earns the bonus")
	}
	r, _ = run([]bool{true, false, true, false, true}, 2000)
	if c.ReviewPassed(r, plain, true) || c.ReviewXP(r, false) != 0 {
		t.Error("3 of 5 passed")
	}
	// A fluency topic also needs a median answer time under its target,
	// unless the parent turned fluency off.
	slow, _ := run([]bool{true, true, true, true, true}, 7000)
	if c.ReviewPassed(slow, fluent, true) || !c.ReviewPassed(slow, fluent, false) || !c.ReviewPassed(slow, plain, true) {
		t.Error("fluency rule")
	}
	if median([]int64{9, 1, 5, 3}) != 4 || median(nil) != 0 {
		t.Error("median")
	}
}

func TestPace(t *testing.T) {
	c := Default()
	var p Pace
	var slow bool
	for i, ms := range []int{500, 600, 2000, 400, 300, 200} {
		p, slow = c.Pace(p, time.Duration(ms)*time.Millisecond, false)
		if slow != (i == 5) {
			t.Fatalf("answer %d: slow down %v", i, slow)
		}
	}
	if !p.Rushed {
		t.Error("not marked rushed")
	}
	p = Pace{}
	for range 5 {
		if p, slow = c.Pace(p, 100*time.Millisecond, true); slow {
			t.Fatal("fluency answers count as rushing")
		}
	}
	if c.DrillXP(37, 0) != 3 || c.DrillXP(50, 3) != 2 || c.DrillXP(50, 6) != 0 {
		t.Error("drill xp")
	}
}

func TestStreak(t *testing.T) {
	sat := Date(20000)
	for sat.Weekday() != time.Saturday {
		sat++
	}
	met := func(ds ...Date) map[Date]Day {
		m := map[Date]Day{}
		for _, d := range ds {
			m[d] = Day{Date: d, XP: 20, Goal: 15, GoalMet: true}
		}
		return m
	}
	weekends := RestDays("06")
	fri, mon, tue := sat.Add(-1), sat.Add(2), sat.Add(3)
	cases := []struct {
		days  map[Date]Day
		today Date
		rest  []time.Weekday
		want  int
	}{
		{met(), tue, nil, 0},
		{met(mon), tue, nil, 1},                   // today not met yet: yesterday's streak stands
		{met(mon, tue), tue, nil, 2},              // today met
		{met(fri, mon), tue, nil, 1},              // the weekend broke it
		{met(fri, mon), tue, weekends, 2},         // rest days don't break it
		{met(fri, sat, mon), tue, weekends, 3},    // and count when the goal was met
		{met(fri.Add(-1), mon), tue, weekends, 1}, // Thursday met, Friday missed
	}
	for i, c := range cases {
		if got := Streak(c.days, c.today, c.rest); got != c.want {
			t.Errorf("case %d: streak %d, want %d", i, got, c.want)
		}
	}
	// A goal that was not met breaks the streak like a missing day.
	days := met(mon)
	days[tue.Add(-2)] = Day{XP: 3, Goal: 15}
	if got := Streak(days, tue, nil); got != 1 {
		t.Errorf("unmet day: %d", got)
	}
}

// The rebuild applies the same rules to the log, so it must reproduce what
// the live path wrote, including derived changes.
func TestFoldMatchesLive(t *testing.T) {
	c, g := Default(), testGraph()
	live := States{}
	var log []Event
	apply := func(e Event) {
		log = append(log, c.Apply(g, live, e).Events...)
	}
	apply(Event{Kind: LessonStarted, Topic: "count", On: day0})
	apply(Event{Kind: LessonPassed, Topic: "count", On: day0})
	apply(Event{Kind: LessonStarted, Topic: "add", On: day0.Add(1)})
	apply(Event{Kind: LessonPaused, Topic: "add", On: day0.Add(1)})
	apply(Event{Kind: ReviewPassed, Topic: "count", On: day0.Add(1)})
	apply(Event{Kind: LessonPaused, Topic: "add", On: day0.Add(2)})
	apply(Event{Kind: ReviewFailed, Topic: "count", On: day0.Add(2)})
	apply(Event{Kind: ReviewPassed, Topic: "count", On: day0.Add(3)})
	apply(Event{Kind: LessonPassed, Topic: "add", On: day0.Add(3)})
	apply(Event{Kind: ParentOverride, Topic: "shapes", On: day0.Add(4), Mastered: true})
	apply(Event{Kind: Placed, Topic: "clock", On: day0.Add(4)})
	apply(Event{Kind: ParentOverride, Topic: "clock", On: day0.Add(5), Mastered: false})

	rebuilt := c.Fold(g, log)
	if !maps.Equal(live, rebuilt) {
		t.Errorf("live    %+v\nrebuilt %+v", live, rebuilt)
	}
	if _, ok := live["clock"]; ok {
		t.Error("override to not mastered kept the state")
	}

	// A topic renamed by a transition keeps its history.
	old := []Event{{Kind: LessonPassed, Topic: "old-add", On: day0}}
	if st := c.Fold(g, old)["add"]; st.State != Mastered {
		t.Errorf("transition: %+v", st)
	}
	if got := g.Resolve("gone"); !reflect.DeepEqual(got, []string{"gone"}) {
		t.Errorf("Resolve(gone) = %v", got)
	}
}
