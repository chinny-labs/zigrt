// Package sim plays scripted students through the real service with a
// fake clock (plan section 12): the scenario tests, `zigrt demo` and, later,
// `zigrt simulate` use it. A simulated student does what a child does
// through the web pages: opens home, starts the recommended task, reads,
// answers, and stops when the day's goal is met.
package sim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/problem"
)

// Clock is a settable clock the service reads through Service.Now.
type Clock interface {
	Now() time.Time
	Advance(time.Duration)
}

// Student is one scripted student.
type Student struct {
	Svc   *service.Service
	User  store.User
	Clock Clock
	// Right decides whether the student answers a problem right.
	Right func(p *problem.Problem) bool
	// Think is how long each answer takes.
	Think func(p *problem.Problem) time.Duration
	// MaxTasks bounds the tasks in one day.
	MaxTasks int
}

// Day is what a student did in one day.
type Day struct {
	Lessons, Reviews int // tasks finished
	Passed, Failed   int
	Paused           int // lessons paused by misses
	XP               int
	GoalMet          bool
	Complete         bool // the course had nothing left
}

// Day works through tasks until the day's goal is met, nothing is offered,
// or MaxTasks tasks are done.
func (s *Student) Day(ctx context.Context) (Day, error) {
	var d Day
	limit := s.MaxTasks
	if limit == 0 {
		limit = 12
	}
	for range limit {
		home, err := s.Svc.Home(ctx, s.User)
		if err != nil {
			return d, err
		}
		d.XP, d.GoalMet, d.Complete = home.Today.XP, home.Today.GoalMet, home.Complete
		if home.NoContent || home.Complete || d.GoalMet {
			return d, nil
		}
		var next service.NextTask
		switch {
		case home.Active != nil:
			next = *home.Active
		case len(home.Tasks) > 0:
			next = home.Tasks[0]
		default:
			return d, nil
		}
		v, err := s.Do(ctx, next.Kind, next.Topic.ID)
		if err != nil {
			return d, fmt.Errorf("%s %s: %w", next.Kind, next.Topic.ID, err)
		}
		switch {
		case v.Phase == service.PhasePaused:
			d.Paused++
		case v.Task.Kind == store.TaskLesson:
			d.Lessons++
		default:
			d.Reviews++
		}
		switch v.Task.Status {
		case store.TaskPassed:
			d.Passed++
		case store.TaskFailed:
			d.Failed++
		}
	}
	home, err := s.Svc.Home(ctx, s.User)
	d.XP, d.GoalMet = home.Today.XP, home.Today.GoalMet
	return d, err
}

// Do starts a task and works it to the end: done, or paused.
func (s *Student) Do(ctx context.Context, kind store.TaskKind, topicID string) (service.TaskView, error) {
	t, err := s.Svc.StartTask(ctx, s.User, kind, topicID)
	if err != nil {
		return service.TaskView{}, err
	}
	v, err := s.Svc.Task(ctx, s.User, t.ID)
	for step := 0; err == nil; step++ {
		if step > 500 {
			return v, errors.New("the task never ended")
		}
		switch v.Phase {
		case service.PhaseDone, service.PhasePaused:
			return v, nil
		case service.PhaseProblem:
			s.Clock.Advance(s.think(v.Problem))
			resp := v.Problem.Wrong()
			if s.Right == nil || s.Right(v.Problem) {
				resp = v.Problem.Right()
			}
			v, err = s.Svc.SubmitAnswer(ctx, s.User, t.ID, v.AttemptID, resp)
		default:
			s.Clock.Advance(5 * time.Second)
			v, err = s.Svc.Continue(ctx, s.User, t.ID)
		}
	}
	return v, err
}

func (s *Student) think(p *problem.Problem) time.Duration {
	if s.Think != nil {
		return s.Think(p)
	}
	return 8 * time.Second
}
