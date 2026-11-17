package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
)

// What a parent sees (P2, P3): each student's week, and one student's days,
// activity and where they are stuck.

// StudentCard is one student on the dashboard (P2).
type StudentCard struct {
	Student    StudentView
	Course     string // the course's title
	Today      DaySummary
	WeekXP     int
	WeekGoal   int // the daily goal on each day of the week that is not a rest day
	NewBlocks  int // first placed this week
	LastActive *time.Time
	Stuck      *content.Topic // a lesson that stopped twice and is not passed yet
	Week       []DayBar       // Monday to Sunday
	Placed     int
}

// DayBar is one day of a student's week.
type DayBar struct {
	Date    string // YYYY-MM-DD
	Weekday time.Weekday
	XP      int
	Goal    int
	Met     bool
	Rest    bool
	Future  bool
}

// Dashboard is every student's week, for a parent (P2).
func (s *Service) Dashboard(ctx context.Context, parent store.User) ([]StudentCard, error) {
	if err := requireParent(parent); err != nil {
		return nil, err
	}
	students, err := s.Students(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StudentCard, 0, len(students))
	for _, sv := range students {
		c, err := s.studentCard(ctx, sv)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// weekStart is the Monday on or before a day.
func weekStart(d learning.Date) learning.Date {
	off := (int(d.Weekday()) + 6) % 7 // Monday 0 … Sunday 6
	return d.Add(-off)
}

func (s *Service) studentCard(ctx context.Context, sv StudentView) (StudentCard, error) {
	c := StudentCard{Student: sv}
	today, err := s.today(ctx, s.Store)
	if err != nil {
		return c, err
	}
	if c.Today, err = s.daySummary(ctx, s.Store, sv.ID, today); err != nil {
		return c, err
	}
	monday := weekStart(today.date)
	rows, err := s.Store.ListDailyTotals(ctx, sv.ID, monday.String(), monday.Add(6).String())
	if err != nil {
		return c, err
	}
	byDate := map[string]store.DailyTotal{}
	for _, r := range rows {
		byDate[r.LocalDate] = r
	}
	rest := learning.RestDays(sv.Settings.RestDays)
	goal := max(sv.Settings.DailyXPGoal, 1)
	for i := range 7 {
		d := monday.Add(i)
		r := byDate[d.String()]
		bar := DayBar{Date: d.String(), Weekday: d.Weekday(), XP: r.XP, Goal: goal, Met: r.GoalMet,
			Rest: slices.Contains(rest, d.Weekday()), Future: d > today.date}
		if r.Goal > 0 {
			bar.Goal = r.Goal
		}
		c.Week = append(c.Week, bar)
		c.WeekXP += r.XP
		if !bar.Rest {
			c.WeekGoal += bar.Goal
		}
	}

	l := s.lib.Load()
	if l != nil {
		cid := sv.Settings.CourseID
		if cid == "" && len(l.pack.Manifest.Courses) > 0 {
			cid = l.pack.Manifest.Courses[0].ID
		}
		for _, co := range l.pack.Manifest.Courses {
			if co.ID == cid {
				c.Course = co.Title.In(Locale)
			}
		}
	}
	events, err := s.Store.ListMasteryEvents(ctx, sv.ID)
	if err != nil {
		return c, err
	}
	first, _ := placementHistory(events)
	weekFrom := monday.Start(today.loc)
	for _, at := range first {
		if !at.Before(weekFrom) {
			c.NewBlocks++
		}
	}
	states, err := loadStates(ctx, s.Store, sv.ID)
	if err != nil {
		return c, err
	}
	for id, st := range states {
		if st.State == learning.Mastered {
			c.Placed++
		}
		if l != nil && st.State == learning.Learning && st.Pauses >= s.Learn.PausesBeforeReview {
			if t := l.pack.Topic(id); t != nil && (c.Stuck == nil || t.Order < c.Stuck.Order) {
				c.Stuck = t
			}
		}
	}
	c.LastActive, err = s.lastActive(ctx, sv.ID)
	return c, err
}

// lastActive is when a student last answered anything.
func (s *Service) lastActive(ctx context.Context, studentID string) (*time.Time, error) {
	var last *time.Time
	later := func(t *time.Time) {
		if t != nil && (last == nil || t.After(*last)) {
			v := *t
			last = &v
		}
	}
	done, err := s.Store.ListFinishedTasks(ctx, studentID, 1)
	if err != nil {
		return nil, err
	}
	for _, t := range done {
		later(t.FinishedAt)
	}
	open, err := s.Store.ListOpenTasks(ctx, studentID)
	if err != nil {
		return nil, err
	}
	for _, t := range open {
		later(&t.UpdatedAt)
	}
	drills, err := s.Store.ListDrills(ctx, studentID, 1)
	if err != nil {
		return nil, err
	}
	for _, d := range drills {
		later(&d.CreatedAt)
	}
	return last, nil
}

// Activity is one thing a student did (P3).
type Activity struct {
	At     time.Time
	Kind   string // lesson, review, quiz, placement, paused, drill, worksheet
	Title  string
	Detail string // the score or outcome, in the parent's words
	Passed bool
}

// StudentReport is one student's page for a parent (P3).
type StudentReport struct {
	StudentCard
	Activity []Activity
	// When stuck: the step it stopped at, and the topics under it being
	// mended first.
	StuckStep    int
	StuckMending []*content.Topic
}

// StudentReport builds a student's page.
func (s *Service) StudentReport(ctx context.Context, parent store.User, studentID string) (StudentReport, error) {
	var r StudentReport
	if err := requireParent(parent); err != nil {
		return r, err
	}
	sv, err := s.Student(ctx, studentID)
	if err != nil {
		return r, err
	}
	if r.StudentCard, err = s.studentCard(ctx, sv); err != nil {
		return r, err
	}
	l := s.lib.Load()
	title := func(id string) string {
		if l != nil {
			if t := l.pack.Topic(id); t != nil {
				return t.Title.In(Locale)
			}
		}
		return id
	}

	tasks, err := s.Store.ListFinishedTasks(ctx, studentID, 30)
	if err != nil {
		return r, err
	}
	for _, t := range tasks {
		if t.Status == store.TaskAbandoned {
			continue
		}
		st, _ := decodeState(t)
		a := Activity{At: *t.FinishedAt, Kind: string(t.Kind), Title: title(t.TopicID), Passed: t.Status == store.TaskPassed}
		switch {
		case st.Review != nil:
			a.Detail = fmtScore(st.Review.Right, st.Review.Asked)
		case st.Quiz != nil:
			a.Title, a.Detail = "Quiz", fmtScore(st.Quiz.Right, st.Quiz.Asked)
		case st.Placement != nil:
			a.Kind, a.Title, a.Detail = "placement", "Placement check", plural(len(st.Placed), "topic", "topics")+" placed"
		default:
			a.Detail = "Mastered"
		}
		r.Activity = append(r.Activity, a)
	}
	events, err := s.Store.ListMasteryEvents(ctx, studentID)
	if err != nil {
		return r, err
	}
	for _, e := range events {
		if e.Kind == string(learning.LessonPaused) {
			r.Activity = append(r.Activity, Activity{At: e.CreatedAt, Kind: "paused", Title: title(e.TopicID), Detail: "Paused"})
		}
	}
	drills, err := s.Store.ListDrills(ctx, studentID, 20)
	if err != nil {
		return r, err
	}
	for _, d := range drills {
		a := Activity{At: d.CreatedAt, Kind: string(d.Kind), Title: d.Title}
		if d.Kind == store.KindDrill {
			res, err := s.Store.GetDrillResult(ctx, d.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue // never finished
			}
			if err != nil {
				return r, err
			}
			a.At, a.Detail, a.Passed = res.FinishedAt, fmtScore(res.Correct, res.Total), true
		} else {
			a.Detail = "Printed"
		}
		r.Activity = append(r.Activity, a)
	}
	slices.SortFunc(r.Activity, func(a, b Activity) int { return b.At.Compare(a.At) })
	r.Activity = r.Activity[:min(len(r.Activity), 25)]

	if r.Stuck != nil && l != nil {
		open, err := s.Store.ListOpenTasks(ctx, studentID)
		if err != nil {
			return r, err
		}
		for _, t := range open {
			if t.TopicID == r.Stuck.ID {
				if st, err := decodeState(t); err == nil && st.Lesson != nil {
					r.StuckStep = st.Lesson.KP + 1
				}
			}
		}
		for _, id := range r.Stuck.Requires {
			if t := l.pack.Topic(id); t != nil {
				r.StuckMending = append(r.StuckMending, t)
			}
		}
	}
	return r, nil
}

// StudentTower is a student's tower for a parent to look at (read-only).
func (s *Service) StudentTower(ctx context.Context, parent store.User, studentID string) (TowerView, error) {
	if err := requireParent(parent); err != nil {
		return TowerView{}, err
	}
	u, err := getStudent(ctx, s.Store, studentID)
	if err != nil {
		return TowerView{}, err
	}
	v, err := s.Tower(ctx, u)
	v.Next = nil // a parent cannot start a student's task
	return v, err
}

func fmtScore(right, of int) string { return strconv.Itoa(right) + " of " + strconv.Itoa(of) }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// Struggle is a topic a student keeps finding hard: lessons stopped and
// reviews failed (plan §11, "topics with repeated failures").
type Struggle struct {
	Topic         *content.Topic
	Stops         int // lessons stopped for the day
	FailedReviews int
	Last          time.Time
}

// Struggles lists a student's topics with two or more stops or failed
// reviews in the last days days, the most recent first.
func (s *Service) Struggles(ctx context.Context, parent store.User, studentID string, days int) ([]Struggle, error) {
	if err := requireParent(parent); err != nil {
		return nil, err
	}
	return s.struggles(ctx, studentID, days)
}

func (s *Service) struggles(ctx context.Context, studentID string, days int) ([]Struggle, error) {
	if _, err := getStudent(ctx, s.Store, studentID); err != nil {
		return nil, err
	}
	l, err := s.library()
	if err != nil {
		return nil, err
	}
	events, err := s.Store.ListMasteryEvents(ctx, studentID)
	if err != nil {
		return nil, err
	}
	since := s.now().AddDate(0, 0, -days)
	byTopic := map[string]*Struggle{}
	for _, e := range events {
		if e.CreatedAt.Before(since) || (e.Kind != string(learning.LessonPaused) && e.Kind != string(learning.ReviewFailed)) {
			continue
		}
		t := l.pack.Topic(e.TopicID)
		if t == nil {
			continue
		}
		st := byTopic[t.ID]
		if st == nil {
			st = &Struggle{Topic: t}
			byTopic[t.ID] = st
		}
		if e.Kind == string(learning.LessonPaused) {
			st.Stops++
		} else {
			st.FailedReviews++
		}
		st.Last = e.CreatedAt
	}
	var out []Struggle
	for _, st := range byTopic {
		if st.Stops+st.FailedReviews >= 2 {
			out = append(out, *st)
		}
	}
	slices.SortFunc(out, func(a, b Struggle) int { return b.Last.Compare(a.Last) })
	return out, nil
}

// Counts are household totals for metrics: students, and those who
// practised in the last seven days.
type Counts struct {
	Students, Active int
}

// Counts counts students and active students.
func (s *Service) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	users, err := s.Store.ListUsers(ctx, store.RoleStudent)
	if err != nil {
		return c, err
	}
	c.Students = len(users)
	week := s.now().Add(-7 * 24 * time.Hour)
	for _, u := range users {
		last, err := s.lastActive(ctx, u.ID)
		if err != nil {
			return c, err
		}
		if last != nil && last.After(week) {
			c.Active++
		}
	}
	return c, nil
}
