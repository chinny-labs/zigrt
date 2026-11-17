package learning

import "math"

// State is where a student is with one topic (plan section 3.2).
type State string

const (
	Locked   State = "locked"   // a prerequisite is not mastered
	Ready    State = "ready"    // the lesson can be offered
	Learning State = "learning" // the lesson is started or reopened
	Mastered State = "mastered" // passed or placed; on a review schedule
)

// TopicState is the stored state of one topic. Only Learning and Mastered
// are stored; Locked and Ready follow from the graph.
type TopicState struct {
	Topic      string
	State      State
	Stage      int
	Due        Date // mastered only
	LastResult string
	FailStreak int // failed reviews in a row
	Pauses     int // times the lesson paused since it was last passed
}

// States is one student's stored topic states by topic ID.
type States map[string]TopicState

// Status is a topic's state, working out Locked and Ready from the graph.
func (s States) Status(g *Graph, id string) State {
	if st, ok := s[id]; ok {
		return st.State
	}
	if s.prereqsMastered(g, id) {
		return Ready
	}
	return Locked
}

func (s States) prereqsMastered(g *Graph, id string) bool {
	t := g.Topic(id)
	if t == nil {
		return false
	}
	for _, r := range t.Requires {
		if s[r].State != Mastered {
			return false
		}
	}
	return true
}

// IsDue reports whether a mastered topic's review is due on a day.
func (s States) IsDue(id string, on Date) bool {
	st, ok := s[id]
	return ok && st.State == Mastered && st.Due <= on
}

// EventKind names a mastery event (the mastery_events.kind column).
type EventKind string

const (
	LessonStarted  EventKind = "lesson_started"
	LessonPaused   EventKind = "lesson_paused"
	LessonPassed   EventKind = "lesson_passed"
	ReviewPassed   EventKind = "review_passed"
	ReviewFailed   EventKind = "review_failed"
	Placed         EventKind = "placed"
	ParentOverride EventKind = "parent_override"
	QuizMissed     EventKind = "quiz_missed" // a quiz problem on the topic was missed

	// Derived events are recorded with the event that caused them, to say
	// why a date moved. A rebuild works them out again rather than replaying
	// them, so a change to the rules applies to old history too.
	ImplicitCredit EventKind = "implicit_credit"
	PulledForward  EventKind = "pulled_forward"
	Reopened       EventKind = "reopened"
)

// Primary reports whether an event is a fact about what the student (or a
// parent) did, as opposed to a consequence the rules derived from one.
func (k EventKind) Primary() bool {
	switch k {
	case ImplicitCredit, PulledForward, Reopened:
		return false
	}
	return true
}

// Event is one mastery event.
type Event struct {
	Kind   EventKind
	Topic  string
	On     Date   // the household-local day it happened
	TaskID string // the lesson or review that caused it, if any

	Mastered bool // parent_override: marked mastered, or not mastered

	// Recorded for the log, not read back by a rebuild.
	Source string  // derived events: the topic whose event caused this one
	Weight float64 // implicit_credit: the edge weight
	Stage  int     // the topic's stage afterwards
	Due    Date    // the topic's due date afterwards (0 when none)
}

// Change is what applying an event did: the topics whose state changed, the
// topics whose state was removed, and every event to record, the applied
// one first.
type Change struct {
	States  []TopicState
	Removed []string
	Events  []Event
}

// Apply applies a primary event to the states, in place, and returns what
// changed. The live path and the rebuild both go through here, so they
// agree.
func (c Config) Apply(g *Graph, s States, e Event) Change {
	var ch Change
	touched := map[string]bool{}
	put := func(st TopicState) {
		s[st.Topic] = st
		if !touched[st.Topic] {
			touched[st.Topic] = true
			ch.States = append(ch.States, st)
			return
		}
		for i := range ch.States {
			if ch.States[i].Topic == st.Topic {
				ch.States[i] = st
			}
		}
	}
	record := func(ev Event) {
		if st, ok := s[ev.Topic]; ok {
			ev.Stage, ev.Due = st.Stage, st.Due
		}
		ch.Events = append(ch.Events, ev)
	}

	st, has := s[e.Topic]
	if !has {
		st = TopicState{Topic: e.Topic}
	}
	switch e.Kind {
	case LessonStarted:
		if st.State == "" {
			st.State = Learning
			put(st)
		}
		record(e)

	case LessonPaused:
		if st.State == "" {
			st.State = Learning
		}
		st.Pauses++
		put(st)
		record(e)
		if st.Pauses >= c.PausesBeforeReview {
			// Bring the prerequisites' reviews to today; the lesson waits
			// until they are done (see NextTasks).
			if t := g.Topic(e.Topic); t != nil {
				for _, r := range t.Requires {
					pre, ok := s[r]
					if !ok || pre.State != Mastered || pre.Due <= e.On {
						continue
					}
					pre.Due = e.On
					put(pre)
					record(Event{Kind: PulledForward, Topic: r, On: e.On, TaskID: e.TaskID, Source: e.Topic})
				}
			}
		}

	case LessonPassed:
		st = TopicState{Topic: e.Topic, State: Mastered, Stage: 0, Due: e.On.Add(c.Interval(0)), LastResult: "pass"}
		put(st)
		record(e)
		c.credit(g, s, e, put, record)

	case ReviewPassed:
		if st.State != Mastered {
			record(e) // a review of a topic no longer mastered changes nothing
			break
		}
		st.Stage++
		st.Due = e.On.Add(c.Interval(st.Stage))
		st.FailStreak, st.LastResult = 0, "pass"
		put(st)
		record(e)
		c.credit(g, s, e, put, record)

	case ReviewFailed:
		if st.State != Mastered {
			record(e)
			break
		}
		st.FailStreak++
		st.LastResult = "fail"
		if st.FailStreak >= c.FailsToReopen {
			st = TopicState{Topic: e.Topic, State: Learning, LastResult: "fail"}
			put(st)
			record(e)
			record(Event{Kind: Reopened, Topic: e.Topic, On: e.On, TaskID: e.TaskID, Source: e.Topic})
		} else {
			st.Stage = max(0, st.Stage-c.FailStageDrop)
			st.Due = e.On.Add(1)
			put(st)
			record(e)
		}
		c.flowUp(g, s, e, put, record)

	case QuizMissed:
		// A quiz found the topic shaky: its review comes due now (plan 3.6).
		if st.State == Mastered && st.Due > e.On {
			st.Due = e.On
			put(st)
		}
		record(e)

	case Placed:
		st = TopicState{Topic: e.Topic, State: Mastered, Stage: 2, Due: e.On.Add(c.Interval(2)), LastResult: "placed"}
		put(st)
		record(e)

	case ParentOverride:
		if e.Mastered {
			st = TopicState{Topic: e.Topic, State: Mastered, Stage: 2, Due: e.On.Add(c.Interval(2)), LastResult: "override"}
			put(st)
		} else if has {
			delete(s, e.Topic)
			ch.Removed = append(ch.Removed, e.Topic)
		}
		record(e)

	default:
		// Derived events change nothing when replayed.
	}
	return ch
}

// credit gives the topics a passed topic encompasses a partial repetition
// (plan section 3.5): each mastered one's due date moves out by the edge
// weight times its interval, and at full weight a due one also advances a
// stage. No credit moves a topic further out than a passed review of it
// would.
func (c Config) credit(g *Graph, s States, e Event, put func(TopicState), record func(Event)) {
	t := g.Topic(e.Topic)
	if t == nil {
		return
	}
	for _, edge := range t.Encompasses {
		a, ok := s[edge.Topic]
		if !ok || a.State != Mastered {
			continue
		}
		before := a
		if edge.Weight >= 1 && a.Due <= e.On {
			a.Stage++
			a.Due = e.On.Add(c.Interval(a.Stage))
		} else {
			shift := int(math.Round(edge.Weight * float64(c.Interval(a.Stage))))
			a.Due = min(a.Due.Add(shift), e.On.Add(c.Interval(a.Stage+1)))
		}
		if a == before || a.Due < before.Due {
			continue
		}
		put(a)
		record(Event{Kind: ImplicitCredit, Topic: a.Topic, On: e.On, TaskID: e.TaskID, Source: e.Topic, Weight: edge.Weight})
	}
}

// flowUp pulls forward the reviews of mastered topics that encompass a
// failed one, to half their remaining wait (plan section 3.5).
func (c Config) flowUp(g *Graph, s States, e Event, put func(TopicState), record func(Event)) {
	t := g.Topic(e.Topic)
	if t == nil {
		return
	}
	for _, edge := range t.EncompassedBy {
		b, ok := s[edge.Topic]
		if !ok || b.State != Mastered || b.Due <= e.On {
			continue
		}
		due := e.On.Add(b.Due.Sub(e.On) / 2)
		if due >= b.Due {
			continue
		}
		b.Due = due
		put(b)
		record(Event{Kind: PulledForward, Topic: b.Topic, On: e.On, TaskID: e.TaskID, Source: e.Topic})
	}
}

// Fold rebuilds the states from the event log, oldest first, applying only
// primary events (derived ones are worked out again). Topic IDs are mapped
// through the pack's transitions, so history survives a split or merge.
func (c Config) Fold(g *Graph, events []Event) States {
	s := States{}
	for _, e := range events {
		if !e.Kind.Primary() {
			continue
		}
		for _, id := range g.Resolve(e.Topic) {
			e.Topic = id
			c.Apply(g, s, e)
		}
	}
	return s
}
