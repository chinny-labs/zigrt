package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/problem"
)

// The placement test (plan 3.3) and quizzes (plan 3.6). Both ask about many
// topics, one problem each, with no feedback until the end.

// answerAcross records an answer in a placement test or quiz and puts the
// next problem on screen, or finishes.
func (s *Service) answerAcross(ctx context.Context, tx store.Tx, l *library, t *store.Task, st *taskState,
	topicID string, p *problem.Problem, resp problem.Response, right bool, elapsed time.Duration, today day, now time.Time) error {
	st.Problem, st.Last = nil, nil
	if st.Placement != nil {
		pl := s.Learn.PlacementAnswer(l.graph, st.Set, *st.Placement, topicID, right)
		st.Placement = &pl
		return s.nextAcross(ctx, tx, l, t, st, today, now)
	}
	fluency := false
	if gt := l.graph.Topic(topicID); gt != nil {
		fluency = gt.FluencySeconds > 0
	}
	st.Pace, _ = s.Learn.Pace(st.Pace, elapsed, fluency)
	st.Results = append(st.Results, quizResult{Template: p.Template, Seed: p.Seed, Given: resp, Right: right})
	q, done := s.Learn.QuizAnswer(*st.Quiz, right)
	st.Quiz = &q
	if done {
		return s.finishQuiz(ctx, tx, l, t, st, today, now)
	}
	return s.nextAcross(ctx, tx, l, t, st, today, now)
}

func (s *Service) nextAcross(ctx context.Context, tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error {
	err := s.issue(l, t, st, now)
	if errors.Is(err, errPlacementDone) {
		return s.finishPlacement(ctx, tx, l, t, st, today, now)
	}
	return err
}

// finishPlacement places every topic found known that is not mastered yet:
// mastered at stage 2, so each is checked once within a week (plan 3.3).
func (s *Service) finishPlacement(ctx context.Context, tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error {
	states, err := loadStates(ctx, tx, t.StudentID)
	if err != nil {
		return err
	}
	st.Placed = nil
	for _, id := range s.Learn.PlacementKnown(st.Set, *st.Placement) {
		if states[id].State == learning.Mastered {
			continue
		}
		if err := s.applyEvent(ctx, tx, l, t.StudentID, states, learning.Event{
			Kind: learning.Placed, Topic: id, On: today.date, TaskID: t.ID}, now); err != nil {
			return err
		}
		st.Placed = append(st.Placed, id)
	}
	xp := s.Learn.PlacementXP(st.Placement.Asked)
	st.Phase, st.Problem = PhaseDone, nil
	t.Status, t.FinishedAt, t.XPAwarded = store.TaskPassed, &now, xp
	return s.awardXP(ctx, tx, t.StudentID, store.XPFromTask, t.ID, xp, today, now)
}

// finishQuiz pays the quiz's XP and brings each missed topic's review due
// now (plan 3.6).
func (s *Service) finishQuiz(ctx context.Context, tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error {
	xp := s.Learn.QuizXP(*st.Quiz)
	if st.Pace.Rushed {
		xp = 0
	}
	states, err := loadStates(ctx, tx, t.StudentID)
	if err != nil {
		return err
	}
	for _, id := range st.Quiz.Missed {
		if err := s.applyEvent(ctx, tx, l, t.StudentID, states, learning.Event{
			Kind: learning.QuizMissed, Topic: id, On: today.date, TaskID: t.ID}, now); err != nil {
			return err
		}
	}
	st.Phase, st.Problem = PhaseDone, nil
	t.Status, t.FinishedAt, t.XPAwarded = store.TaskPassed, &now, xp
	return s.awardXP(ctx, tx, t.StudentID, store.XPFromTask, t.ID, xp, today, now)
}

// NotLearned answers a placement question with "I haven't learned this",
// which settles the topic and everything that requires it as not known.
func (s *Service) NotLearned(ctx context.Context, student store.User, id, attemptID string) (TaskView, error) {
	return s.update(ctx, student, id, func(tx store.Tx, l *library, t *store.Task, st *taskState, today day, now time.Time) error {
		if t.Status != store.TaskActive || st.Placement == nil || st.Phase != PhaseProblem || st.Problem == nil || st.Problem.AttemptID != attemptID {
			return errUnchanged
		}
		given, err := json.Marshal(map[string]bool{"not_learned": true})
		if err != nil {
			return err
		}
		ok, err := tx.InsertAttempt(ctx, store.Attempt{ID: attemptID, TaskID: t.ID, StudentID: t.StudentID,
			TopicID: st.Problem.Topic, KPID: st.Problem.KP, TemplateID: st.Problem.Template, Seed: st.Problem.Seed,
			Answer: given, Note: "not_learned", ResponseMS: min(max(now.Sub(st.Problem.At), 0), time.Hour).Milliseconds(), CreatedAt: now})
		if err != nil || !ok {
			return errors.Join(err, errUnchanged)
		}
		topicID := st.Problem.Topic
		st.Problem = nil
		pl := s.Learn.PlacementAnswer(l.graph, st.Set, *st.Placement, topicID, false)
		st.Placement = &pl
		return s.nextAcross(ctx, tx, l, t, st, today, now)
	})
}

// SkipPlacement lets a new student start from the bottom instead: the
// placement test is recorded as skipped and not offered again (a parent
// can start one later).
func (s *Service) SkipPlacement(ctx context.Context, student store.User) error {
	if !student.IsStudent() {
		return ErrForbidden
	}
	l, err := s.library()
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, student.ID, func(tx store.Tx) error {
		now := s.now()
		today, err := s.today(ctx, tx)
		if err != nil {
			return err
		}
		sit, err := s.situation(ctx, tx, l, student.ID, today)
		if err != nil {
			return err
		}
		if !sit.NeedsPlacement && !sit.PlacementOpen {
			return ErrNotOffered
		}
		open, err := tx.ListOpenTasks(ctx, student.ID)
		if err != nil {
			return err
		}
		for _, t := range open {
			if t.Kind == store.TaskDiagnostic {
				t.Status, t.FinishedAt, t.UpdatedAt = store.TaskAbandoned, &now, now
				return tx.UpdateTask(ctx, t)
			}
		}
		t := store.Task{ID: store.NewID(), StudentID: student.ID, Kind: store.TaskDiagnostic,
			PackVersion: l.pack.Manifest.Version, Status: store.TaskAbandoned, State: json.RawMessage(`{"phase":"done"}`),
			StartedAt: now, UpdatedAt: now, FinishedAt: &now}
		return tx.CreateTask(ctx, t)
	})
}

// placementAndQuizzes fills in the parts of a situation that decide whether
// to offer a placement test or a quiz.
func (s *Service) placementAndQuizzes(ctx context.Context, r store.Reader, studentID string, open []store.Task, sit *learning.Situation) error {
	if s.Learn.OfferPlacement {
		sit.PlacementOpen = slices.ContainsFunc(open, func(t store.Task) bool { return t.Kind == store.TaskDiagnostic })
		if !sit.PlacementOpen && len(sit.States) == 0 {
			taken, err := r.ListTasksOfKind(ctx, studentID, store.TaskDiagnostic, 1)
			if err != nil {
				return err
			}
			sit.NeedsPlacement = len(taken) == 0
		}
	}
	if s.Learn.OfferQuizzes && !sit.NeedsPlacement && !sit.PlacementOpen {
		topics, err := s.quizTopics(ctx, r, studentID)
		if err != nil {
			return err
		}
		sit.QuizDue = len(topics) > 0
	}
	return nil
}

// quizTopics lists the topics a quiz would draw on now: those whose lessons
// were passed since the last quiz, oldest first, once QuizEveryXP XP have
// been earned since it. It is empty when no quiz is due.
func (s *Service) quizTopics(ctx context.Context, r store.Reader, studentID string) ([]string, error) {
	var since time.Time
	last, err := r.ListTasksOfKind(ctx, studentID, store.TaskQuiz, 5)
	if err != nil {
		return nil, err
	}
	for _, t := range last {
		if t.FinishedAt != nil && t.Status == store.TaskPassed {
			since = *t.FinishedAt
			break
		}
	}
	xp, err := r.SumXPSince(ctx, studentID, since)
	if err != nil || xp < s.Learn.QuizEveryXP {
		return nil, err
	}
	events, err := r.ListMasteryEvents(ctx, studentID)
	if err != nil {
		return nil, err
	}
	states, err := loadStates(ctx, r, studentID)
	if err != nil {
		return nil, err
	}
	var topics []string
	for _, e := range events {
		if e.Kind == string(learning.LessonPassed) && e.CreatedAt.After(since) &&
			states[e.TopicID].State == learning.Mastered && !slices.Contains(topics, e.TopicID) {
			topics = append(topics, e.TopicID)
		}
	}
	return topics, nil
}
