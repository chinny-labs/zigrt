package learning

import (
	"math"
	"slices"
	"time"
)

// Lesson is a student's place in a lesson (plan section 3.4). The service
// keeps it in the task's state.
type Lesson struct {
	KP      int   `json:"kp"`      // the knowledge point being practised
	Streak  int   `json:"streak"`  // right in a row in this knowledge point
	Misses  int   `json:"misses"`  // wrong in this knowledge point since the lesson last resumed
	Retries []int `json:"retries"` // wrong answers in each knowledge point, over the whole lesson
}

// Step is what an answer did to a lesson.
type Step int

const (
	StepContinue Step = iota // another problem in the same knowledge point
	StepKPPassed             // on to the next knowledge point
	StepPassed               // every knowledge point passed: the topic is mastered
	StepPaused               // too many misses: the lesson stops for today
)

// LessonAnswer records one answer in a lesson of kps knowledge points.
func (c Config) LessonAnswer(l Lesson, right bool, kps int) (Lesson, Step) {
	l.Retries = slices.Clone(l.Retries)
	for len(l.Retries) < kps {
		l.Retries = append(l.Retries, 0)
	}
	if !right {
		l.Streak = 0
		l.Misses++
		l.Retries[l.KP]++
		if l.Misses >= c.KPMaxMisses {
			l.Misses = 0
			return l, StepPaused
		}
		return l, StepContinue
	}
	l.Streak++
	if l.Streak < c.KPPassStreak {
		return l, StepContinue
	}
	l.Streak, l.Misses = 0, 0
	if l.KP+1 >= kps {
		return l, StepPassed
	}
	l.KP++
	return l, StepKPPassed
}

// LessonXP is the XP for a passed lesson: the topic's, plus a bonus when no
// knowledge point needed more than one retry.
func (c Config) LessonXP(t *Topic, l Lesson) int {
	xp := c.DefaultLessonXP
	if t != nil && t.XP > 0 {
		xp = t.XP
	}
	for _, r := range l.Retries {
		if r > 1 {
			return xp
		}
	}
	return int(math.Round(float64(xp) * float64(100+c.LessonBonusPercent) / 100))
}

// Review is a student's place in a review (plan section 3.5).
type Review struct {
	Asked int     `json:"asked"`
	Right int     `json:"right"`
	MS    []int64 `json:"ms"` // answer times, for fluency topics
}

// ReviewAnswer records one answer and reports whether the review is over.
func (c Config) ReviewAnswer(r Review, right bool, ms int64) (Review, bool) {
	r.Asked++
	if right {
		r.Right++
	}
	r.MS = append(slices.Clone(r.MS), ms)
	return r, r.Asked >= c.ReviewProblems
}

// ReviewPassed reports whether a finished review passed: enough right, and
// for a fluency topic (when fluency is on for the student) a median answer
// time under the target. The child never sees the timer.
func (c Config) ReviewPassed(r Review, t *Topic, fluencyOn bool) bool {
	if r.Asked == 0 || r.Right*100 < r.Asked*c.ReviewPassPercent {
		return false
	}
	if fluencyOn && t != nil && t.FluencySeconds > 0 {
		return median(r.MS) < int64(t.FluencySeconds)*1000
	}
	return true
}

// ReviewXP is the XP for a finished review.
func (c Config) ReviewXP(r Review, passed bool) int {
	if !passed {
		return 0
	}
	if r.Right == r.Asked {
		return c.ReviewBaseXP + c.PerfectReviewBonus
	}
	return c.ReviewBaseXP
}

func median(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(ms))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// Pace watches for rushing (plan section 3.7): answers quicker than a floor
// several times running pause the task with a "slow down" screen, and the
// task earns no XP.
type Pace struct {
	Fast   int  `json:"fast"`
	Rushed bool `json:"rushed"`
}

// Pace records one answer's time. fluency answers do not count: a fact topic
// is meant to be quick.
func (c Config) Pace(p Pace, elapsed time.Duration, fluency bool) (Pace, bool) {
	if fluency || elapsed >= c.FastAnswer {
		p.Fast = 0
		return p, false
	}
	p.Fast++
	if p.Fast < c.FastRun {
		return p, false
	}
	p.Fast, p.Rushed = 0, true
	return p, true
}

// DrillXP is the XP for a finished drill, given the drill XP already earned
// today.
func (c Config) DrillXP(correct, earnedToday int) int {
	return max(0, min(correct/c.DrillXPPer, c.DrillXPDailyCap-earnedToday))
}

// TaskKind is a kind of task the engine offers.
type TaskKind string

const (
	TaskLesson    TaskKind = "lesson"
	TaskReview    TaskKind = "review"
	TaskQuiz      TaskKind = "quiz"
	TaskPlacement TaskKind = "diagnostic" // the placement test (plan 3.3)
)

// Next is one task on the student home.
type Next struct {
	Kind   TaskKind
	Topic  string
	Resume bool // a lesson already started
	Early  bool // a review offered before it is due, once the course is done
}

// Situation is everything NextTasks needs about one student today.
type Situation struct {
	Today        Date
	Course       []string // the working set, in teaching order (Graph.WorkingSet)
	States       States
	InProgress   []string        // topics with a started or paused lesson task
	PausedToday  map[string]bool // lessons that paused today
	ReviewsInRow int             // reviews finished since the last lesson
	LastStrand   string          // the strand of the last lesson finished

	NeedsPlacement bool // a new student who has not taken or skipped the placement test
	PlacementOpen  bool // a placement test is started and not finished
	QuizDue        bool // enough XP since the last quiz, and topics mastered to quiz
}

// NextTasks chooses the student's tasks, the recommended one first (plan
// section 3.8). It reports complete when there is nothing left to learn and
// nothing due; the tasks are then early reviews.
func (c Config) NextTasks(g *Graph, sit Situation) ([]Next, bool) {
	s := sit.States
	offerable := func(id string) bool {
		t := g.Topic(id)
		if t == nil || sit.PausedToday[id] || !s.prereqsMastered(g, id) {
			return false
		}
		if s[id].Pauses >= c.PausesBeforeReview {
			for _, r := range t.Requires {
				if s.IsDue(r, sit.Today) {
					return false // its prerequisites' reviews come first
				}
			}
		}
		return true
	}

	var resume []Next
	started := map[string]bool{}
	for _, id := range sit.InProgress {
		started[id] = true
		if s[id].State == Learning && offerable(id) {
			resume = append(resume, Next{Kind: TaskLesson, Topic: id, Resume: true})
		}
	}

	// A lesson stopped twice waits for its prerequisites' reviews (plan
	// 3.4). Those reviews jump the queue, so a long backlog of other reviews
	// can't hold the lesson back for good; once they are done the lesson
	// resumes. They are never folded into another topic's review either.
	mending := map[string]int{}     // prerequisite → teaching order of the held lesson
	for _, id := range sit.Course { // started, set aside or reopened alike
		t := g.Topic(id)
		st := s[id]
		if t == nil || st.State != Learning || st.Pauses < c.PausesBeforeReview || sit.PausedToday[id] || !s.prereqsMastered(g, id) {
			continue
		}
		for _, r := range t.Requires {
			if s.IsDue(r, sit.Today) {
				if o, ok := mending[r]; !ok || t.Order < o {
					mending[r] = t.Order
				}
			}
		}
	}

	var reviews []Next
	var due []TopicState
	isDue := map[string]bool{}
	for _, st := range s {
		if st.State == Mastered && st.Due <= sit.Today && g.Topic(st.Topic) != nil {
			isDue[st.Topic] = true
		}
	}
	for id := range isDue {
		// Compression (plan 3.5): a due topic that another due topic fully
		// encompasses is left out; passing that review credits it.
		covered := false
		for _, e := range g.Topic(id).EncompassedBy {
			covered = covered || (e.Weight >= 1 && isDue[e.Topic] && e.Topic != id)
		}
		if _, held := mending[id]; held || !covered {
			due = append(due, s[id])
		}
	}
	slices.SortFunc(due, func(a, b TopicState) int {
		ma, aHeld := mending[a.Topic]
		mb, bHeld := mending[b.Topic]
		switch {
		case aHeld != bHeld:
			if aHeld {
				return -1 // a held lesson's prerequisites first
			}
			return 1
		case aHeld && ma != mb:
			return ma - mb // for the earliest held lesson first
		case a.Due != b.Due:
			return int(a.Due - b.Due) // most overdue first
		}
		return g.Topic(a.Topic).Order - g.Topic(b.Topic).Order
	})
	for _, st := range due {
		reviews = append(reviews, Next{Kind: TaskReview, Topic: st.Topic})
	}

	// New lessons: reopened topics first, then ready ones in teaching order,
	// alternating strands where the graph allows.
	var lessons []Next
	var ready []*Topic
	for _, id := range sit.Course {
		if started[id] || !offerable(id) {
			continue
		}
		switch s.Status(g, id) {
		case Learning:
			lessons = append(lessons, Next{Kind: TaskLesson, Topic: id})
		case Ready:
			ready = append(ready, g.Topic(id))
		}
	}
	last := sit.LastStrand
	for len(ready) > 0 {
		i := slices.IndexFunc(ready, func(t *Topic) bool { return t.Strand != last })
		if i < 0 {
			i = 0
		}
		lessons = append(lessons, Next{Kind: TaskLesson, Topic: ready[i].ID})
		last = ready[i].Strand
		ready = slices.Delete(ready, i, i+1)
	}

	inRow := c.MaxReviewsInRow
	if c.BacklogReviews > 0 && len(reviews) > c.BacklogReviews {
		inRow = max(inRow, c.BacklogReviewsInRow)
	}
	var out []Next
	switch {
	case len(resume) > 0:
		out = slices.Concat(resume[:1], reviews, resume[1:], lessons)
	case sit.ReviewsInRow >= inRow && len(lessons) > 0:
		out = slices.Concat(lessons[:1], reviews, lessons[1:])
	default:
		out = slices.Concat(reviews, lessons)
	}
	// A quiz comes after a lesson in progress, before reviews (plan 3.8).
	if c.OfferQuizzes && sit.QuizDue {
		at := 0
		if len(resume) > 0 {
			at = 1
		}
		out = slices.Insert(out, at, Next{Kind: TaskQuiz})
	}
	// The placement test goes first for a new student, or to finish one.
	if c.OfferPlacement && (sit.PlacementOpen || sit.NeedsPlacement) {
		out = slices.Insert(out, 0, Next{Kind: TaskPlacement, Resume: sit.PlacementOpen})
	}
	if len(out) > 0 {
		return out[:min(len(out), c.MaxTasks)], false
	}

	// Nothing to learn and nothing due: offer early reviews, soonest due first.
	var mastered []TopicState
	for _, id := range sit.Course {
		if st := s[id]; st.State == Mastered {
			mastered = append(mastered, st)
		}
	}
	slices.SortStableFunc(mastered, func(a, b TopicState) int { return int(a.Due - b.Due) })
	for _, st := range mastered[:min(len(mastered), c.MaxTasks)] {
		out = append(out, Next{Kind: TaskReview, Topic: st.Topic, Early: true})
	}
	return out, true
}

// Day is one day's XP against the goal (a daily_totals row).
type Day struct {
	Date    Date
	XP      int
	Goal    int
	GoalMet bool
}

// Streak counts the days in a row, back from today, on which the goal was
// met. Rest days neither break nor extend it, and today counts once its goal
// is met (until then, the streak is still yesterday's).
func Streak(days map[Date]Day, today Date, rest []time.Weekday) int {
	first := today
	for d := range days {
		first = min(first, d)
	}
	n := 0
	for d := today; d >= first; d-- {
		switch {
		case days[d].GoalMet:
			n++
		case d == today, slices.Contains(rest, d.Weekday()):
		default:
			return n
		}
	}
	return n
}
