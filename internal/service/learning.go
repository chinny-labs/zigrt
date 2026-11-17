package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/problem"
)

// Lessons, reviews and mastery (plan section 3). The rules are in learning;
// this file loads state, calls it, and writes the logs and their
// projections in one transaction (ADR 3).

// Locale is the pack locale lessons are shown in. Version 1 is English only.
const Locale = "en"

var (
	// ErrNotOffered means the task asked for is not one the engine offers
	// the student now: the server chooses lessons and reviews.
	ErrNotOffered = errors.New("that isn't one of your tasks right now")
	// ErrTaskGone means a task cannot go on because the content pack no
	// longer has its lesson or template. The task is abandoned.
	ErrTaskGone = errors.New("that lesson has changed, so it starts again from your home page")
)

// Phase is what a task shows.
type Phase string

const (
	PhaseIntro    Phase = "intro"    // the placement test's welcome (S4)
	PhaseExplain  Phase = "explain"  // a knowledge point's explanation and worked example
	PhaseProblem  Phase = "problem"  // a problem to answer
	PhaseFeedback Phase = "feedback" // right, or the worked solution to the problem just missed
	PhaseSlow     Phase = "slow"     // "slow down" after rushed answers
	PhasePaused   Phase = "paused"   // three misses: the lesson comes back tomorrow
	PhaseDone     Phase = "done"     // the task is finished
)

// taskState is the JSON kept in tasks.state.
type taskState struct {
	Phase    Phase            `json:"phase"`
	Then     Phase            `json:"then,omitempty"` // where feedback and slow-down lead
	SlowNext bool             `json:"slow_next,omitempty"`
	Lesson   *learning.Lesson `json:"lesson,omitempty"`
	Review   *learning.Review `json:"review,omitempty"`
	Pace     learning.Pace    `json:"pace"`
	Issued   int              `json:"issued"` // problems issued so far
	Problem  *issued          `json:"problem,omitempty"`
	Last     *lastAnswer      `json:"last,omitempty"`
	PausedOn string           `json:"paused_on,omitempty"` // the day three misses paused it

	Placement *learning.Placement `json:"placement,omitempty"`
	Set       []string            `json:"set,omitempty"`    // placement: the working set it places within
	Placed    []string            `json:"placed,omitempty"` // placement: what it found known
	Quiz      *learning.Quiz      `json:"quiz,omitempty"`
	Results   []quizResult        `json:"results,omitempty"` // quiz: every answer, for the results page
}

// quizResult is one quiz answer, kept to show at the end (S11).
type quizResult struct {
	Template string           `json:"template"`
	Seed     uint64           `json:"seed"`
	Given    problem.Response `json:"given"`
	Right    bool             `json:"right"`
}

// issued is the problem on screen. The answer is never stored or sent: the
// problem is regenerated from template and seed to check it (plan 7.4).
type issued struct {
	AttemptID string    `json:"attempt"`
	Topic     string    `json:"topic,omitempty"` // the problem's topic (placement and quizzes span topics)
	KP        string    `json:"kp"`
	Template  string    `json:"template"`
	Seed      uint64    `json:"seed"`
	At        time.Time `json:"at"`
}

type lastAnswer struct {
	issued
	Given problem.Response `json:"given"`
	Right bool             `json:"right"`
	Note  problem.Note     `json:"note,omitempty"`
}

// TaskView is everything a task page shows.
type TaskView struct {
	Task  store.Task
	Topic *content.Topic
	Phase Phase

	KP      *content.KP // lessons: the knowledge point
	KPIndex int
	KPCount int

	Example   *problem.Problem // explain: the worked example, solution and all
	Problem   *problem.Problem // problem: what to answer. Its Answer must never reach the page
	AttemptID string

	Feedback *Feedback

	Asked, Total int // reviews, quizzes and placement: problems answered, and in all (placement: at most)

	ProblemTopic *content.Topic // the topic of the problem on screen (placement and quizzes span topics)
	Placed       []*content.Topic
	QuizResults  []Feedback // a finished quiz: every problem, answer and worked solution

	Result *TaskResult // paused or done
}

// Feedback is the result of the last answer.
type Feedback struct {
	Problem *problem.Problem // the problem answered, with its worked solution
	Given   problem.Response
	Right   bool
	Note    problem.Note
	Answer  string // the right answer, as written
}

// TaskResult is the end of a task: what it earned and what comes next.
type TaskResult struct {
	Passed    bool
	XP        int
	Rushed    bool
	Today     DaySummary
	Next      []NextTask
	Promote   string     // the next recommended topic's title, if any
	Milestone *Milestone // a tier or face this lesson finished (S9b)
}

// DaySummary is today's XP against the goal, and the streak.
type DaySummary struct {
	XP, Goal int
	GoalMet  bool
	Streak   int
}

// NextTask is a task on the student home.
type NextTask struct {
	Kind   store.TaskKind
	Topic  *content.Topic
	Resume bool   // a lesson already started
	Early  bool   // a review before it is due
	TaskID string // the task in progress, when continuing it
}

// HomeView is the student home (S3).
type HomeView struct {
	NoContent bool
	Today     DaySummary
	Active    *NextTask // the task on screen, to continue
	Tasks     []NextTask
	Complete  bool   // nothing left to learn and nothing due
	Placed    int    // topics mastered in the course
	Tiers     []Tier // the course's grades, youngest first
}

// Tier is one grade band of the student's tower.
type Tier struct {
	Grade         string
	Placed, Total int
}

// --- reading state ----------------------------------------------------------

type day struct {
	date learning.Date
	loc  *time.Location
}

func (s *Service) today(ctx context.Context, r store.Reader) (day, error) {
	h, err := r.GetHousehold(ctx)
	if err != nil {
		return day{}, err
	}
	loc := h.Location()
	return day{date: learning.DateOf(s.now(), loc), loc: loc}, nil
}

func loadStates(ctx context.Context, r store.Reader, studentID string) (learning.States, error) {
	rows, err := r.ListTopicStates(ctx, studentID)
	if err != nil {
		return nil, err
	}
	out := learning.States{}
	for _, row := range rows {
		st, err := fromStoreState(row)
		if err != nil {
			return nil, err
		}
		out[st.Topic] = st
	}
	return out, nil
}

func fromStoreState(row store.TopicState) (learning.TopicState, error) {
	st := learning.TopicState{Topic: row.TopicID, State: learning.State(row.State), Stage: row.Stage,
		LastResult: row.LastResult, FailStreak: row.FailStreak, Pauses: row.Pauses}
	if row.DueOn != "" {
		d, err := learning.ParseDate(row.DueOn)
		if err != nil {
			return st, err
		}
		st.Due = d
	}
	return st, nil
}

func toStoreState(studentID string, st learning.TopicState, at time.Time) store.TopicState {
	row := store.TopicState{StudentID: studentID, TopicID: st.Topic, State: string(st.State), Stage: st.Stage,
		LastResult: st.LastResult, FailStreak: st.FailStreak, Pauses: st.Pauses, UpdatedAt: at}
	if st.State == learning.Mastered {
		row.DueOn = st.Due.String()
	}
	return row
}

func decodeState(t store.Task) (taskState, error) {
	var st taskState
	if err := json.Unmarshal(t.State, &st); err != nil {
		return st, fmt.Errorf("task %s: %w", t.ID, err)
	}
	return st, nil
}

func encodeState(t *store.Task, st taskState) error {
	b, err := json.Marshal(st)
	t.State = b
	return err
}

// situation is what the engine needs to choose a student's tasks.
func (s *Service) situation(ctx context.Context, r store.Reader, l *library, studentID string, today day) (learning.Situation, error) {
	set, err := r.GetStudentSettings(ctx, studentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return learning.Situation{}, err
	}
	states, err := loadStates(ctx, r, studentID)
	if err != nil {
		return learning.Situation{}, err
	}
	sit := learning.Situation{Today: today.date, Course: l.courseTopics(set.CourseID), States: states,
		PausedToday: map[string]bool{}}
	open, err := r.ListOpenTasks(ctx, studentID)
	if err != nil {
		return sit, err
	}
	for _, t := range open {
		if t.Kind != store.TaskLesson {
			continue
		}
		sit.InProgress = append(sit.InProgress, t.TopicID)
		if st, err := decodeState(t); err == nil && st.PausedOn == today.date.String() {
			sit.PausedToday[t.TopicID] = true
		}
	}
	if err := s.placementAndQuizzes(ctx, r, studentID, open, &sit); err != nil {
		return sit, err
	}
	done, err := r.ListFinishedTasks(ctx, studentID, 20)
	if err != nil {
		return sit, err
	}
	for _, t := range done {
		if t.Kind == store.TaskReview {
			sit.ReviewsInRow++
			continue
		}
		if t.Kind == store.TaskLesson {
			if gt := l.graph.Topic(t.TopicID); gt != nil {
				sit.LastStrand = gt.Strand
			}
			break
		}
	}
	return sit, nil
}

func (s *Service) daySummary(ctx context.Context, r store.Reader, studentID string, today day) (DaySummary, error) {
	set, err := r.GetStudentSettings(ctx, studentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return DaySummary{}, err
	}
	goal := set.DailyXPGoal
	if goal == 0 {
		goal = store.DefaultDailyXPGoal
	}
	rows, err := r.ListDailyTotals(ctx, studentID, today.date.Add(-400).String(), today.date.String())
	if err != nil {
		return DaySummary{}, err
	}
	days := map[learning.Date]learning.Day{}
	out := DaySummary{Goal: goal}
	for _, row := range rows {
		d, err := learning.ParseDate(row.LocalDate)
		if err != nil {
			return out, err
		}
		days[d] = learning.Day{Date: d, XP: row.XP, Goal: row.Goal, GoalMet: row.GoalMet}
		if d == today.date {
			out.XP, out.GoalMet = row.XP, row.GoalMet
		}
	}
	out.Streak = learning.Streak(days, today.date, learning.RestDays(set.RestDays))
	return out, nil
}

// --- the student home -------------------------------------------------------

// Home is the student's home page: today's climb and their tasks.
func (s *Service) Home(ctx context.Context, student store.User) (HomeView, error) {
	if !student.IsStudent() {
		return HomeView{}, ErrForbidden
	}
	var v HomeView
	today, err := s.today(ctx, s.Store)
	if err != nil {
		return v, err
	}
	if v.Today, err = s.daySummary(ctx, s.Store, student.ID, today); err != nil {
		return v, err
	}
	l := s.lib.Load()
	if l == nil {
		v.NoContent = true
		return v, nil
	}
	if t, err := s.Store.GetActiveTask(ctx, student.ID); err == nil {
		// Placement tests and quizzes span topics, so they have none.
		if topic := l.pack.Topic(t.TopicID); topic != nil || t.TopicID == "" {
			v.Active = &NextTask{Kind: t.Kind, Topic: topic, Resume: true, TaskID: t.ID}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return v, err
	}
	sit, err := s.situation(ctx, s.Store, l, student.ID, today)
	if err != nil {
		return v, err
	}
	v.Tasks, v.Complete = s.toNext(l, sit)
	byGrade := map[string]*Tier{}
	for _, id := range sit.Course {
		t := l.pack.Topic(id)
		tier := byGrade[t.Grade]
		if tier == nil {
			tier = &Tier{Grade: t.Grade}
			byGrade[t.Grade] = tier
		}
		tier.Total++
		if sit.States[id].State == learning.Mastered {
			tier.Placed++
			v.Placed++
		}
	}
	for _, g := range content.Grades {
		if t := byGrade[g]; t != nil {
			v.Tiers = append(v.Tiers, *t)
		}
	}
	return v, nil
}

func (s *Service) nextTasks(ctx context.Context, r store.Reader, l *library, studentID string, today day) ([]NextTask, bool, error) {
	sit, err := s.situation(ctx, r, l, studentID, today)
	if err != nil {
		return nil, false, err
	}
	out, complete := s.toNext(l, sit)
	return out, complete, nil
}

func (s *Service) toNext(l *library, sit learning.Situation) ([]NextTask, bool) {
	next, complete := s.Learn.NextTasks(l.graph, sit)
	out := make([]NextTask, 0, len(next))
	for _, n := range next {
		out = append(out, NextTask{Kind: store.TaskKind(n.Kind), Topic: l.pack.Topic(n.Topic), Resume: n.Resume, Early: n.Early})
	}
	return out, complete
}

// --- starting and moving through a task -------------------------------------

// StartTask starts (or resumes) a lesson or review the engine offers the
// student. A task already on screen is set aside: a lesson keeps its place,
// a review is abandoned.
func (s *Service) StartTask(ctx context.Context, student store.User, kind store.TaskKind, topicID string) (store.Task, error) {
	if !student.IsStudent() {
		return store.Task{}, ErrForbidden
	}
	l, err := s.library()
	if err != nil {
		return store.Task{}, err
	}
	var out store.Task
	err = s.Store.Tx(ctx, student.ID, func(tx store.Tx) error {
		now := s.now()
		today, err := s.today(ctx, tx)
		if err != nil {
			return err
		}
		if active, err := tx.GetActiveTask(ctx, student.ID); err == nil {
			if active.Kind == kind && active.TopicID == topicID {
				out = active
				return nil
			}
			if err := s.setAside(ctx, tx, active, now); err != nil {
				return err
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		next, _, err := s.nextTasks(ctx, tx, l, student.ID, today)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(next, func(n NextTask) bool {
			return n.Kind == kind && (n.Topic == nil && topicID == "" || n.Topic != nil && n.Topic.ID == topicID)
		})
		if i < 0 {
			return ErrNotOffered
		}

		if kind == store.TaskLesson || kind == store.TaskDiagnostic {
			open, err := tx.ListOpenTasks(ctx, student.ID)
			if err != nil {
				return err
			}
			for _, t := range open {
				if t.Kind == kind && t.TopicID == topicID {
					out, err = s.resumeTask(ctx, tx, t, now)
					return err
				}
			}
		}

		t := store.Task{ID: store.NewID(), StudentID: student.ID, Kind: kind, TopicID: topicID,
			PackVersion: l.pack.Manifest.Version, Status: store.TaskActive, StartedAt: now, UpdatedAt: now}
		var st taskState
		switch kind {
		case store.TaskLesson:
			st = taskState{Phase: PhaseExplain, Lesson: &learning.Lesson{}}
		case store.TaskReview:
			st = taskState{Review: &learning.Review{}}
			if err := s.issue(l, &t, &st, now); err != nil {
				return err
			}
		case store.TaskDiagnostic:
			set, err := tx.GetStudentSettings(ctx, student.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			st = taskState{Phase: PhaseIntro, Placement: &learning.Placement{}, Set: l.courseTopics(set.CourseID)}
		case store.TaskQuiz:
			topics, err := s.quizTopics(ctx, tx, student.ID)
			if err != nil {
				return err
			}
			st = taskState{Quiz: &learning.Quiz{Topics: s.Learn.QuizTopics(topics)}}
			if len(st.Quiz.Topics) == 0 {
				return ErrNotOffered
			}
			if err := s.issue(l, &t, &st, now); err != nil {
				return err
			}
		default:
			return ErrNotOffered
		}
		if err := encodeState(&t, st); err != nil {
			return err
		}
		if err := tx.CreateTask(ctx, t); err != nil {
			return err
		}
		if kind == store.TaskLesson {
			states, err := loadStates(ctx, tx, student.ID)
			if err != nil {
				return err
			}
			if err := s.applyEvent(ctx, tx, l, student.ID, states, learning.Event{
				Kind: learning.LessonStarted, Topic: topicID, On: today.date, TaskID: t.ID}, now); err != nil {
				return err
			}
		}
		out = t
		return nil
	})
	return out, err
}

// setAside takes a task off screen so another can start.
func (s *Service) setAside(ctx context.Context, tx store.Tx, t store.Task, now time.Time) error {
	t.UpdatedAt = now
	if t.Kind == store.TaskLesson || t.Kind == store.TaskDiagnostic {
		st, err := decodeState(t)
		if err != nil {
			return err
		}
		if st.Phase == PhaseProblem || st.Phase == PhaseFeedback || st.Phase == PhaseSlow {
			st.Phase, st.Problem, st.SlowNext = PhaseExplain, nil, false
			if st.Placement != nil {
				st.Phase = PhaseIntro
			}
		}
		if err := encodeState(&t, st); err != nil {
			return err
		}
		t.Status = store.TaskPaused
	} else {
		t.Status, t.FinishedAt = store.TaskAbandoned, &now
	}
	return tx.UpdateTask(ctx, t)
}

// resumeTask picks a paused lesson up where it stopped, with the misses
// that paused it forgiven, or a placement test where it was left.
func (s *Service) resumeTask(ctx context.Context, tx store.Tx, t store.Task, now time.Time) (store.Task, error) {
	st, err := decodeState(t)
	if err != nil {
		return t, err
	}
	st.Phase, st.Then, st.SlowNext, st.Problem, st.PausedOn = PhaseExplain, "", false, nil, ""
	if st.Placement != nil {
		st.Phase = PhaseIntro
	} else {
		if st.Lesson == nil {
			st.Lesson = &learning.Lesson{}
		}
		st.Lesson.Misses, st.Lesson.Streak = 0, 0
	}
	t.Status, t.UpdatedAt = store.TaskActive, now
	if err := encodeState(&t, st); err != nil {
		return t, err
	}
	return t, tx.UpdateTask(ctx, t)
}

// errPlacementDone means the placement test has nothing left to ask.
var errPlacementDone = errors.New("placement done")

// issue puts the next problem on screen. A lesson cycles through its
// knowledge point's templates; a review cycles through the topic's
// knowledge points; the placement test and quizzes ask the hardest
// knowledge point of each topic they choose.
func (s *Service) issue(l *library, t *store.Task, st *taskState, now time.Time) error {
	topicID := t.TopicID
	switch {
	case st.Placement != nil:
		id, done := s.Learn.PlacementNext(l.graph, st.Set, *st.Placement)
		if done {
			return errPlacementDone
		}
		topicID = id
	case st.Quiz != nil:
		topicID = st.Quiz.Topics[min(st.Quiz.Asked, len(st.Quiz.Topics)-1)]
	}
	topic := l.pack.Topic(topicID)
	if topic == nil || len(topic.KPs) == 0 {
		return ErrTaskGone
	}
	var kp *content.KP
	var tpl *problem.Template
	switch {
	case st.Lesson != nil:
		kp = topic.KPs[min(st.Lesson.KP, len(topic.KPs)-1)]
		tpl = kp.Templates[st.Issued%len(kp.Templates)]
	case st.Review != nil:
		kp = topic.KPs[st.Issued%len(topic.KPs)]
		tpl = kp.Templates[(st.Issued/len(topic.KPs))%len(kp.Templates)]
	default:
		kp = topic.KPs[len(topic.KPs)-1]
		tpl = kp.Templates[st.Issued%len(kp.Templates)]
	}
	var seed uint64
	for try := 0; ; try++ {
		seed = s.seed()
		if _, err := tpl.Generate(seed, Locale); err == nil {
			break
		} else if try == 4 {
			return fmt.Errorf("%w: %w", ErrTaskGone, err)
		}
	}
	st.Problem = &issued{AttemptID: store.NewID(), Topic: topic.ID, KP: kp.ID, Template: tpl.ID, Seed: seed, At: now}
	st.Issued++
	st.Phase = PhaseProblem
	return nil
}

// Task shows a task's current screen.
func (s *Service) Task(ctx context.Context, student store.User, id string) (TaskView, error) {
	l, err := s.library()
	if err != nil {
		return TaskView{}, err
	}
	t, err := s.Store.GetTask(ctx, id)
	if err != nil {
		return TaskView{}, err
	}
	if t.StudentID != student.ID {
		return TaskView{}, ErrNotFound
	}
	return s.view(ctx, s.Store, l, t)
}

// Continue moves a task on from the explanation, feedback or slow-down
// screen.
func (s *Service) Continue(ctx context.Context, student store.User, id string) (TaskView, error) {
	return s.update(ctx, student, id, func(tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error {
		var next Phase
		switch st.Phase {
		case PhaseExplain, PhaseIntro:
			next = PhaseProblem
		case PhaseFeedback:
			if st.SlowNext {
				st.SlowNext = false
				st.Phase = PhaseSlow
				return nil
			}
			next = st.Then
		case PhaseSlow:
			next = st.Then
		default:
			return nil
		}
		if next == PhaseProblem && t.Status == store.TaskActive {
			err := s.issue(l, t, st, now)
			if errors.Is(err, errPlacementDone) {
				return s.finishPlacement(ctx, tx, l, t, st, today, now)
			}
			return err
		}
		st.Phase = next
		return nil
	})
}

// SubmitAnswer checks an answer to the problem on screen (plan 7.4). An
// attempt answered before returns the task as it is, so a double tap or a
// resent form is harmless.
func (s *Service) SubmitAnswer(ctx context.Context, student store.User, id, attemptID string, resp problem.Response) (TaskView, error) {
	resp.Main, resp.Rem = clip(resp.Main), clip(resp.Rem)
	return s.update(ctx, student, id, func(tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error {
		if t.Status != store.TaskActive || st.Phase != PhaseProblem || st.Problem == nil || st.Problem.AttemptID != attemptID {
			return errUnchanged
		}
		topicID := st.Problem.Topic
		if topicID == "" {
			topicID = t.TopicID
		}
		topic := l.pack.Topic(topicID)
		tpl := l.pack.Template(st.Problem.Template)
		if topic == nil || tpl == nil {
			return ErrTaskGone
		}
		p, err := tpl.Generate(st.Problem.Seed, Locale)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrTaskGone, err)
		}
		verdict := p.Check(resp)
		elapsed := min(max(now.Sub(st.Problem.At), 0), time.Hour)
		given, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		inserted, err := tx.InsertAttempt(ctx, store.Attempt{ID: attemptID, TaskID: t.ID, StudentID: t.StudentID,
			TopicID: topicID, KPID: st.Problem.KP, TemplateID: tpl.ID, Seed: st.Problem.Seed, Answer: given,
			Correct: verdict.Right, Note: string(verdict.Note), ResponseMS: elapsed.Milliseconds(), CreatedAt: now})
		if err != nil {
			return err
		}
		if !inserted {
			return errUnchanged
		}

		set, err := tx.GetStudentSettings(ctx, t.StudentID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		lt := l.graph.Topic(topicID)
		if st.Placement != nil || st.Quiz != nil {
			// No feedback between questions: the next one comes straight away.
			return s.answerAcross(ctx, tx, l, t, st, topicID, p, resp, verdict.Right, elapsed, today, now)
		}
		fluency := lt != nil && lt.FluencySeconds > 0 && set.FluencyEnabled
		var slow bool
		st.Pace, slow = s.Learn.Pace(st.Pace, elapsed, fluency)
		st.Last = &lastAnswer{issued: *st.Problem, Given: resp, Right: verdict.Right, Note: verdict.Note}
		st.Problem = nil
		st.Phase, st.Then = PhaseFeedback, PhaseProblem

		switch {
		case st.Lesson != nil:
			lesson, step := s.Learn.LessonAnswer(*st.Lesson, verdict.Right, len(topic.KPs))
			st.Lesson = &lesson
			switch step {
			case learning.StepKPPassed:
				st.Then = PhaseExplain
			case learning.StepPassed:
				st.Then = PhaseDone
				return s.finish(ctx, tx, l, t, st, true, s.Learn.LessonXP(lt, lesson), learning.LessonPassed, today, now)
			case learning.StepPaused:
				st.Then, st.PausedOn = PhasePaused, today.date.String()
				t.Status = store.TaskPaused
				states, err := loadStates(ctx, tx, t.StudentID)
				if err != nil {
					return err
				}
				return s.applyEvent(ctx, tx, l, t.StudentID, states, learning.Event{
					Kind: learning.LessonPaused, Topic: t.TopicID, On: today.date, TaskID: t.ID}, now)
			}
		case st.Review != nil:
			review, done := s.Learn.ReviewAnswer(*st.Review, verdict.Right, elapsed.Milliseconds())
			st.Review = &review
			if done {
				passed := s.Learn.ReviewPassed(review, lt, set.FluencyEnabled)
				kind := learning.ReviewFailed
				if passed {
					kind = learning.ReviewPassed
				}
				st.Then = PhaseDone
				return s.finish(ctx, tx, l, t, st, passed, s.Learn.ReviewXP(review, passed), kind, today, now)
			}
		}
		st.SlowNext = slow
		return nil
	})
}

// clip bounds what a student can type into a stored answer.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 24 {
		s = string([]rune(s)[:24])
	}
	return s
}

// finish closes a task, records its mastery event and pays its XP. A task
// rushed through earns none.
func (s *Service) finish(ctx context.Context, tx store.Tx, l *library, t *store.Task, st *taskState, passed bool,
	xp int, kind learning.EventKind, today day, now time.Time) error {
	if st.Pace.Rushed {
		xp = 0
	}
	t.Status, t.FinishedAt, t.XPAwarded = store.TaskFailed, &now, xp
	if passed {
		t.Status = store.TaskPassed
	}
	states, err := loadStates(ctx, tx, t.StudentID)
	if err != nil {
		return err
	}
	if err := s.applyEvent(ctx, tx, l, t.StudentID, states, learning.Event{Kind: kind, Topic: t.TopicID, On: today.date, TaskID: t.ID}, now); err != nil {
		return err
	}
	return s.awardXP(ctx, tx, t.StudentID, store.XPFromTask, t.ID, xp, today, now)
}

// errUnchanged ends an update without writing: the task is shown as it is.
var errUnchanged = errors.New("unchanged")

type taskUpdate func(tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error

// update runs fn on a student's task in a transaction, saves the task and
// returns its new view.
func (s *Service) update(ctx context.Context, student store.User, id string, fn taskUpdate) (TaskView, error) {
	l, err := s.library()
	if err != nil {
		return TaskView{}, err
	}
	var out TaskView
	gone := false
	err = s.Store.Tx(ctx, student.ID, func(tx store.Tx) error {
		t, err := tx.GetTask(ctx, id)
		if err != nil {
			return err
		}
		if t.StudentID != student.ID {
			return ErrNotFound
		}
		st, err := decodeState(t)
		if err != nil {
			return err
		}
		now := s.now()
		today, err := s.today(ctx, tx)
		if err != nil {
			return err
		}
		switch err := fn(tx, l, &t, &st, today, now); {
		case errors.Is(err, errUnchanged):
		case errors.Is(err, ErrTaskGone):
			// The pack changed under this task: abandon it, keep its history.
			gone = true
			t.Status, t.FinishedAt, t.UpdatedAt = store.TaskAbandoned, &now, now
			return tx.UpdateTask(ctx, t)
		case err != nil:
			return err
		default:
			t.UpdatedAt = now
			if err := encodeState(&t, st); err != nil {
				return err
			}
			if err := tx.UpdateTask(ctx, t); err != nil {
				return err
			}
		}
		out, err = s.view(ctx, tx, l, t)
		return err
	})
	if err == nil && gone {
		err = ErrTaskGone
	}
	return out, err
}

// view builds a task's screen from its state.
func (s *Service) view(ctx context.Context, r store.Reader, l *library, t store.Task) (TaskView, error) {
	st, err := decodeState(t)
	if err != nil {
		return TaskView{}, err
	}
	v := TaskView{Task: t, Topic: l.pack.Topic(t.TopicID), Phase: st.Phase}
	if v.Topic == nil && (t.Kind == store.TaskLesson || t.Kind == store.TaskReview) {
		return v, ErrTaskGone
	}
	if t.Status == store.TaskAbandoned {
		v.Phase = PhaseDone
	}
	if st.Lesson != nil {
		v.KPCount = len(v.Topic.KPs)
		v.KPIndex = min(st.Lesson.KP, v.KPCount-1)
		v.KP = v.Topic.KPs[v.KPIndex]
	}
	if st.Review != nil {
		v.Asked, v.Total = st.Review.Asked, s.Learn.ReviewProblems
	}
	if st.Placement != nil {
		v.Asked, v.Total = st.Placement.Asked, s.Learn.PlacementMaxQuestions
		for _, id := range st.Placed {
			if pt := l.pack.Topic(id); pt != nil {
				v.Placed = append(v.Placed, pt)
			}
		}
	}
	if st.Quiz != nil {
		v.Asked, v.Total = st.Quiz.Asked, len(st.Quiz.Topics)
		if t.FinishedAt != nil {
			for _, r := range st.Results {
				if tpl := l.pack.Template(r.Template); tpl != nil {
					if p, err := tpl.Generate(r.Seed, Locale); err == nil {
						v.QuizResults = append(v.QuizResults, Feedback{Problem: p, Given: r.Given, Right: r.Right, Answer: p.Answer.Text(p)})
					}
				}
			}
		}
	}
	switch v.Phase {
	case PhaseExplain:
		if v.KP != nil {
			v.Example, err = v.KP.ExampleTemplate().Generate(exampleSeed(t.ID, v.KP.ID), Locale)
			if err != nil {
				return v, fmt.Errorf("%w: %w", ErrTaskGone, err)
			}
		}
	case PhaseProblem:
		if st.Problem == nil {
			break
		}
		tpl := l.pack.Template(st.Problem.Template)
		if tpl == nil {
			return v, ErrTaskGone
		}
		if v.Problem, err = tpl.Generate(st.Problem.Seed, Locale); err != nil {
			return v, fmt.Errorf("%w: %w", ErrTaskGone, err)
		}
		v.AttemptID = st.Problem.AttemptID
		v.ProblemTopic = l.pack.Topic(st.Problem.Topic)
		if v.ProblemTopic == nil {
			v.ProblemTopic = v.Topic
		}
		if st.Lesson != nil {
			v.KP = l.pack.KP(st.Problem.KP)
		}
	}
	if st.Last != nil && st.Placement == nil && st.Quiz == nil && (v.Phase == PhaseFeedback || v.Phase == PhasePaused || v.Phase == PhaseDone) {
		if tpl := l.pack.Template(st.Last.Template); tpl != nil {
			if p, err := tpl.Generate(st.Last.Seed, Locale); err == nil {
				v.Feedback = &Feedback{Problem: p, Given: st.Last.Given, Right: st.Last.Right, Note: st.Last.Note, Answer: p.Answer.Text(p)}
			}
		}
	}
	if v.Phase == PhasePaused || v.Phase == PhaseDone {
		today, err := s.today(ctx, r)
		if err != nil {
			return v, err
		}
		res := &TaskResult{Passed: t.Status == store.TaskPassed, XP: t.XPAwarded, Rushed: st.Pace.Rushed}
		if res.Today, err = s.daySummary(ctx, r, t.StudentID, today); err != nil {
			return v, err
		}
		if res.Next, _, err = s.nextTasks(ctx, r, l, t.StudentID, today); err != nil {
			return v, err
		}
		if t.Kind == store.TaskLesson && t.Status == store.TaskPassed {
			if res.Milestone, err = s.milestone(ctx, r, l, t.StudentID, t.TopicID, t.ID); err != nil {
				return v, err
			}
		}
		v.Result = res
	}
	return v, nil
}

// exampleSeed fixes a knowledge point's worked example for one task, so it
// stays the same while the student reads it.
func exampleSeed(taskID, kpID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(taskID + "|" + kpID))
	return h.Sum64()
}

// --- writing events and XP --------------------------------------------------

// eventDetail is the JSON in mastery_events.detail.
type eventDetail struct {
	On       string  `json:"on"`
	Mastered bool    `json:"mastered,omitempty"`
	Source   string  `json:"source,omitempty"`
	Weight   float64 `json:"weight,omitempty"`
	Stage    int     `json:"stage"`
	Due      string  `json:"due,omitempty"`
}

// applyEvent applies a primary mastery event and writes it, the events it
// caused and the states it changed.
func (s *Service) applyEvent(ctx context.Context, tx store.Tx, l *library, studentID string, states learning.States,
	e learning.Event, now time.Time) error {
	ch := s.Learn.Apply(l.graph, states, e)
	for _, ev := range ch.Events {
		d := eventDetail{On: ev.On.String(), Mastered: ev.Mastered, Source: ev.Source, Weight: ev.Weight, Stage: ev.Stage}
		if ev.Due != 0 {
			d.Due = ev.Due.String()
		}
		b, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if err := tx.InsertMasteryEvent(ctx, store.MasteryEvent{ID: store.NewID(), StudentID: studentID, TopicID: ev.Topic,
			Kind: string(ev.Kind), TaskID: ev.TaskID, Detail: b, CreatedAt: now}); err != nil {
			return err
		}
	}
	for _, st := range ch.States {
		if err := tx.PutTopicState(ctx, toStoreState(studentID, st, now)); err != nil {
			return err
		}
	}
	for _, id := range ch.Removed {
		if err := tx.DeleteTopicState(ctx, studentID, id); err != nil {
			return err
		}
	}
	return nil
}

// awardXP records XP and updates the day's total against the goal. The goal
// is taken from the student's settings on the day's first XP.
func (s *Service) awardXP(ctx context.Context, tx store.Tx, studentID, kind, sourceID string, amount int, today day, now time.Time) error {
	if amount <= 0 {
		return nil
	}
	date := today.date.String()
	ok, err := tx.InsertXPEvent(ctx, store.XPEvent{ID: store.NewID(), StudentID: studentID, SourceKind: kind,
		SourceID: sourceID, Amount: amount, LocalDate: date, CreatedAt: now})
	if err != nil || !ok {
		return err
	}
	d, err := tx.GetDailyTotal(ctx, studentID, date)
	if errors.Is(err, store.ErrNotFound) {
		set, err := tx.GetStudentSettings(ctx, studentID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		d = store.DailyTotal{StudentID: studentID, LocalDate: date, Goal: set.DailyXPGoal}
		if d.Goal == 0 {
			d.Goal = store.DefaultDailyXPGoal
		}
	} else if err != nil {
		return err
	}
	d.XP += amount
	d.GoalMet = d.XP >= d.Goal
	return tx.PutDailyTotal(ctx, d)
}

// --- rebuilding the projections ---------------------------------------------

// projections works out a student's topic_state and daily_totals from the
// logs, as `zigrt rebuild` writes them (ADR 3). A day keeps the goal it was
// recorded with; a day missing from daily_totals gets the current goal.
func (s *Service) projections(ctx context.Context, r store.Reader, l *library, studentID string, at time.Time) ([]store.TopicState, []store.DailyTotal, error) {
	rows, err := r.ListMasteryEvents(ctx, studentID)
	if err != nil {
		return nil, nil, err
	}
	events := make([]learning.Event, 0, len(rows))
	for _, row := range rows {
		var d eventDetail
		if len(row.Detail) > 0 {
			if err := json.Unmarshal(row.Detail, &d); err != nil {
				return nil, nil, fmt.Errorf("mastery event %s: %w", row.ID, err)
			}
		}
		on, err := learning.ParseDate(d.On)
		if err != nil {
			return nil, nil, fmt.Errorf("mastery event %s: %w", row.ID, err)
		}
		events = append(events, learning.Event{Kind: learning.EventKind(row.Kind), Topic: row.TopicID, On: on,
			TaskID: row.TaskID, Mastered: d.Mastered})
	}
	states := s.Learn.Fold(l.graph, events)
	var topics []store.TopicState
	for _, st := range states {
		topics = append(topics, toStoreState(studentID, st, at))
	}
	slices.SortFunc(topics, func(a, b store.TopicState) int { return strings.Compare(a.TopicID, b.TopicID) })

	xp, err := r.ListXPEvents(ctx, studentID)
	if err != nil {
		return nil, nil, err
	}
	old, err := r.ListDailyTotals(ctx, studentID, "0000-01-01", "9999-12-31")
	if err != nil {
		return nil, nil, err
	}
	goals := map[string]int{}
	for _, d := range old {
		goals[d.LocalDate] = d.Goal
	}
	set, err := r.GetStudentSettings(ctx, studentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	sums := map[string]int{}
	for _, e := range xp {
		sums[e.LocalDate] += e.Amount
	}
	var days []store.DailyTotal
	for date, n := range sums {
		goal, ok := goals[date]
		if !ok {
			goal = max(set.DailyXPGoal, 1)
		}
		days = append(days, store.DailyTotal{StudentID: studentID, LocalDate: date, XP: n, Goal: goal, GoalMet: n >= goal})
	}
	slices.SortFunc(days, func(a, b store.DailyTotal) int { return strings.Compare(a.LocalDate, b.LocalDate) })
	return topics, days, nil
}

// Rebuild recomputes every student's projections from the logs and reports
// how many students it rebuilt.
func (s *Service) Rebuild(ctx context.Context) (int, error) {
	l, err := s.library()
	if err != nil {
		return 0, err
	}
	students, err := s.Store.ListUsers(ctx, store.RoleStudent)
	if err != nil {
		return 0, err
	}
	for _, u := range students {
		err := s.Store.Tx(ctx, u.ID, func(tx store.Tx) error {
			topics, days, err := s.projections(ctx, tx, l, u.ID, s.now())
			if err != nil {
				return err
			}
			if err := tx.ReplaceTopicStates(ctx, u.ID, topics); err != nil {
				return err
			}
			return tx.ReplaceDailyTotals(ctx, u.ID, days)
		})
		if err != nil {
			return 0, fmt.Errorf("rebuild %s: %w", u.DisplayName, err)
		}
	}
	return len(students), nil
}

// CheckProjections reports how a student's stored projections differ from
// a rebuild from the logs; nil means they agree. Tests run it after every
// scenario.
func (s *Service) CheckProjections(ctx context.Context, studentID string) error {
	l, err := s.library()
	if err != nil {
		return err
	}
	topics, days, err := s.projections(ctx, s.Store, l, studentID, time.Time{})
	if err != nil {
		return err
	}
	stored, err := s.Store.ListTopicStates(ctx, studentID)
	if err != nil {
		return err
	}
	for i := range stored {
		stored[i].UpdatedAt = time.Time{}
	}
	var diffs []string
	if !slices.Equal(stored, topics) {
		diffs = append(diffs, fmt.Sprintf("topic_state:\n  stored  %+v\n  rebuilt %+v", stored, topics))
	}
	storedDays, err := s.Store.ListDailyTotals(ctx, studentID, "0000-01-01", "9999-12-31")
	if err != nil {
		return err
	}
	if !slices.Equal(storedDays, days) {
		diffs = append(diffs, fmt.Sprintf("daily_totals:\n  stored  %+v\n  rebuilt %+v", storedDays, days))
	}
	if len(diffs) > 0 {
		return errors.New(strings.Join(diffs, "\n"))
	}
	return nil
}
