package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/store"
)

// The parent side (plan section 6.3): settings for each student, overrides,
// assigned drills, notifications and the day's jobs.

// Notification kinds.
const (
	NoteLocked        = "locked"         // a student hard-locked on wrong PINs
	NoteLessonStopped = "lesson_stopped" // a lesson stopped twice
	NoteGoalMet       = "goal_met"
	NoteStreakLost    = "streak_lost"
	NoteMilestone     = "milestone"    // a tier or face finished
	NotePackUpdated   = "pack_updated" // the lessons changed to a newer pack
)

// notify tells every parent, once per dedupe key.
func (s *Service) notify(ctx context.Context, tx store.Tx, kind, subjectID, key string, payload map[string]any, at time.Time) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	parents, err := tx.ListUsers(ctx, store.RoleParent)
	if err != nil {
		return err
	}
	for _, p := range parents {
		if _, err := tx.InsertNotification(ctx, store.Notification{ID: store.NewID(), UserID: p.ID, Kind: kind,
			SubjectID: subjectID, Payload: b, DedupeKey: kind + ":" + key, CreatedAt: at}); err != nil {
			return err
		}
	}
	return nil
}

// NotificationView is a notification with the student it is about.
type NotificationView struct {
	store.Notification
	Student *store.User
	Data    map[string]any
}

// Notifications lists a parent's notifications, newest first, and how many
// are unread.
func (s *Service) Notifications(ctx context.Context, parent store.User, limit int) ([]NotificationView, int, error) {
	if err := requireParent(parent); err != nil {
		return nil, 0, err
	}
	ns, err := s.Store.ListNotifications(ctx, parent.ID, limit)
	if err != nil {
		return nil, 0, err
	}
	unread, err := s.Store.CountUnread(ctx, parent.ID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]NotificationView, 0, len(ns))
	for _, n := range ns {
		v := NotificationView{Notification: n, Data: map[string]any{}}
		if len(n.Payload) > 0 {
			_ = json.Unmarshal(n.Payload, &v.Data)
		}
		if n.SubjectID != "" {
			if u, err := s.Store.GetUser(ctx, n.SubjectID); err == nil {
				v.Student = &u
			}
		}
		out = append(out, v)
	}
	return out, unread, nil
}

// UnreadNotifications counts a parent's unread notifications, for the bell.
func (s *Service) UnreadNotifications(ctx context.Context, parent store.User) (int, error) {
	if err := requireParent(parent); err != nil {
		return 0, err
	}
	return s.Store.CountUnread(ctx, parent.ID)
}

// MarkRead marks one notification read, or all of a parent's when id is "".
func (s *Service) MarkRead(ctx context.Context, parent store.User, id string) error {
	if err := requireParent(parent); err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.MarkNotificationsRead(ctx, parent.ID, id, s.now()) })
}

// --- a student's settings (P4) ----------------------------------------------

// StudentUpdate is the settings form.
type StudentUpdate struct {
	DisplayName string
	Avatar      string
	CourseID    string // "" for the pack's first course
	DailyXPGoal int
	RestDays    string // weekday digits, 0 = Sunday
	Fluency     bool
}

// UpdateStudent saves a student's profile and learning settings.
func (s *Service) UpdateStudent(ctx context.Context, actor store.User, studentID string, in StudentUpdate) error {
	if err := requireParent(actor); err != nil {
		return err
	}
	name, err := cleanName(in.DisplayName)
	if err != nil {
		return err
	}
	if in.DailyXPGoal < 1 || in.DailyXPGoal > 120 {
		return invalid("daily_xp_goal", "Set a daily goal from 1 to 120 XP.")
	}
	if in.CourseID != "" {
		if l := s.lib.Load(); l == nil || !hasCourse(l.pack.Manifest.Courses, in.CourseID) {
			return invalid("course", "Choose one of the courses.")
		}
	}
	var rest []string
	for _, d := range learning.RestDays(in.RestDays) {
		rest = append(rest, fmt.Sprint(int(d)))
	}
	slices.Sort(rest)
	if len(rest) == 7 {
		return invalid("rest_days", "Leave at least one day that is not a rest day.")
	}
	return s.Store.Tx(ctx, studentID, func(tx store.Tx) error {
		u, err := getStudent(ctx, tx, studentID)
		if err != nil {
			return err
		}
		u.DisplayName = name
		if a := cleanAvatar(in.Avatar); a != "" {
			u.Avatar = a
		}
		if err := tx.UpdateUser(ctx, u); err != nil {
			return err
		}
		if err := tx.PutStudentSettings(ctx, store.StudentSettings{UserID: u.ID, CourseID: in.CourseID,
			DailyXPGoal: in.DailyXPGoal, RestDays: strings.Join(rest, ""), FluencyEnabled: in.Fluency}); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "student.settings", u.ID, map[string]any{
			"course": in.CourseID, "goal": in.DailyXPGoal, "rest": strings.Join(rest, ""), "fluency": in.Fluency})
	})
}

// DeleteStudent removes a student and all their history (plan 8.2: the
// one exception to append-only, for privacy). The parent types the
// student's name to confirm.
func (s *Service) DeleteStudent(ctx context.Context, actor store.User, studentID, typedName string) error {
	if err := requireParent(actor); err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		u, err := getStudent(ctx, tx, studentID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(typedName), u.DisplayName) {
			return invalid("confirm", "Type %s to confirm.", u.DisplayName)
		}
		if err := tx.DeleteUser(ctx, u.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "student.delete", u.ID, map[string]any{"name": u.DisplayName})
	})
}

// OverrideTopic marks a block placed or not for a student (plan 3.3: a
// parent override, recorded as an event like everything else).
func (s *Service) OverrideTopic(ctx context.Context, actor store.User, studentID, topicID string, mastered bool) error {
	if err := requireParent(actor); err != nil {
		return err
	}
	l, err := s.library()
	if err != nil {
		return err
	}
	if l.pack.Topic(topicID) == nil {
		return ErrNotFound
	}
	return s.Store.Tx(ctx, studentID, func(tx store.Tx) error {
		if _, err := getStudent(ctx, tx, studentID); err != nil {
			return err
		}
		today, err := s.today(ctx, tx)
		if err != nil {
			return err
		}
		states, err := loadStates(ctx, tx, studentID)
		if err != nil {
			return err
		}
		now := s.now()
		if err := s.applyEvent(ctx, tx, l, studentID, states, learning.Event{Kind: learning.ParentOverride,
			Topic: topicID, On: today.date, Mastered: mastered}, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "student.override", studentID, map[string]any{"topic": topicID, "mastered": mastered})
	})
}

// RerunPlacement offers a student the placement test again: it is waiting
// at the top of their list next time they sign in.
func (s *Service) RerunPlacement(ctx context.Context, actor store.User, studentID string) error {
	if err := requireParent(actor); err != nil {
		return err
	}
	l, err := s.library()
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, studentID, func(tx store.Tx) error {
		if _, err := getStudent(ctx, tx, studentID); err != nil {
			return err
		}
		open, err := tx.ListOpenTasks(ctx, studentID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(open, func(t store.Task) bool { return t.Kind == store.TaskDiagnostic }) {
			return nil // one is already waiting
		}
		set, err := tx.GetStudentSettings(ctx, studentID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		now := s.now()
		t := store.Task{ID: store.NewID(), StudentID: studentID, Kind: store.TaskDiagnostic, PackVersion: l.pack.Manifest.Version,
			Status: store.TaskPaused, StartedAt: now, UpdatedAt: now}
		if err := encodeState(&t, taskState{Phase: PhaseIntro, Placement: &learning.Placement{}, Set: l.courseTopics(set.CourseID)}); err != nil {
			return err
		}
		if err := tx.CreateTask(ctx, t); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "student.placement", studentID, nil)
	})
}

// --- assigned drills (plan 5.1) ---------------------------------------------

// AssignDrill sets a drill or worksheet for one student. It shows on their
// home until done.
func (s *Service) AssignDrill(ctx context.Context, actor store.User, studentID string, kind store.DrillKind, in drill.Settings, title string) (store.DrillAssignment, error) {
	var a store.DrillAssignment
	if err := requireParent(actor); err != nil {
		return a, err
	}
	set, err := drill.Normalize(in)
	if err != nil {
		return a, invalid("ops", "%s.", capitalize(err.Error()))
	}
	if title, err = cleanTitle(title); err != nil {
		return a, err
	}
	if kind != store.KindDrill && kind != store.KindWorksheet {
		return a, invalid("kind", "Choose a drill or a worksheet.")
	}
	b, err := json.Marshal(set)
	if err != nil {
		return a, err
	}
	err = s.Store.Tx(ctx, studentID, func(tx store.Tx) error {
		if _, err := getStudent(ctx, tx, studentID); err != nil {
			return err
		}
		a = store.DrillAssignment{ID: store.NewID(), StudentID: studentID, ParentID: actor.ID, Kind: kind, Title: title,
			Settings: b, CreatedAt: s.now()}
		if err := tx.CreateAssignment(ctx, a); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor.ID, "student.assign", studentID, map[string]any{"title": title, "kind": kind})
	})
	return a, err
}

// AssignmentView is an assigned drill with who set it.
type AssignmentView struct {
	store.DrillAssignment
	From string // the parent's name
}

// Assignments lists a student's assigned drills that are not done.
func (s *Service) Assignments(ctx context.Context, studentID string) ([]AssignmentView, error) {
	as, err := s.Store.ListAssignments(ctx, studentID, true)
	if err != nil {
		return nil, err
	}
	out := make([]AssignmentView, 0, len(as))
	for _, a := range as {
		v := AssignmentView{DrillAssignment: a}
		if p, err := s.Store.GetUser(ctx, a.ParentID); err == nil {
			v.From = p.DisplayName
		}
		out = append(out, v)
	}
	return out, nil
}

// StartAssignment makes the student's set for an assigned drill, or returns
// the one already made. A worksheet is done once it is made; a drill when it
// is finished.
func (s *Service) StartAssignment(ctx context.Context, student store.User, id string) (DrillView, error) {
	a, err := s.Store.GetAssignment(ctx, id)
	if err != nil {
		return DrillView{}, err
	}
	if a.StudentID != student.ID {
		return DrillView{}, ErrNotFound
	}
	if a.DrillID != "" {
		return s.Drill(ctx, student, a.DrillID)
	}
	var set drill.Settings
	if err := json.Unmarshal(a.Settings, &set); err != nil {
		return DrillView{}, err
	}
	v, err := s.CreateDrill(ctx, student, a.Kind, set, a.Title)
	if err != nil {
		return v, err
	}
	a.DrillID = v.ID
	if a.Kind == store.KindWorksheet {
		now := s.now()
		a.DoneAt = &now
	}
	return v, s.Store.Tx(ctx, student.ID, func(tx store.Tx) error { return tx.UpdateAssignment(ctx, a) })
}

// assignmentDone marks the assignment a finished drill was made for.
func assignmentDone(ctx context.Context, tx store.Tx, studentID, drillID string, at time.Time) error {
	as, err := tx.ListAssignments(ctx, studentID, true)
	if err != nil {
		return err
	}
	for _, a := range as {
		if a.DrillID == drillID {
			a.DoneAt = &at
			return tx.UpdateAssignment(ctx, a)
		}
	}
	return nil
}

// --- the day's jobs (plan 7.5) ----------------------------------------------

// Rollover runs once for each household-local day that has ended since it
// last ran: it tells parents about streaks lost. It catches up after
// downtime (at most a month) and never runs twice for one day.
func (s *Service) Rollover(ctx context.Context) error {
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		today, err := s.today(ctx, tx)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil // no household yet
			}
			return err
		}
		yesterday := today.date.Add(-1)
		last, err := tx.GetJobRun(ctx, "rollover")
		if errors.Is(err, store.ErrNotFound) {
			return tx.PutJobRun(ctx, "rollover", yesterday.String()) // start from now, no backlog
		}
		if err != nil {
			return err
		}
		from, err := learning.ParseDate(last)
		if err != nil {
			return err
		}
		from = max(from+1, yesterday.Add(-30))
		if from > yesterday {
			return nil
		}
		students, err := tx.ListUsers(ctx, store.RoleStudent)
		if err != nil {
			return err
		}
		now := s.now()
		for _, u := range students {
			set, err := tx.GetStudentSettings(ctx, u.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			rest := learning.RestDays(set.RestDays)
			rows, err := tx.ListDailyTotals(ctx, u.ID, from.Add(-400).String(), yesterday.String())
			if err != nil {
				return err
			}
			days := map[learning.Date]learning.Day{}
			for _, r := range rows {
				if d, err := learning.ParseDate(r.LocalDate); err == nil {
					days[d] = learning.Day{Date: d, XP: r.XP, Goal: r.Goal, GoalMet: r.GoalMet}
				}
			}
			for d := from; d <= yesterday; d++ {
				if days[d].GoalMet || slices.Contains(rest, d.Weekday()) {
					continue
				}
				// The streak that day's miss ended (a streak of one is not news).
				if n := learning.Streak(days, d-1, rest); n >= 2 && days[d-1].GoalMet {
					if err := s.notify(ctx, tx, NoteStreakLost, u.ID, u.ID+":"+d.String(), map[string]any{"days": n, "on": d.String()}, now); err != nil {
						return err
					}
				}
			}
		}
		return tx.PutJobRun(ctx, "rollover", yesterday.String())
	})
}

// IdleTaskAge is how long a task can sit untouched on screen before
// housekeeping sets it aside.
const IdleTaskAge = 7 * 24 * time.Hour

// SetAsideIdleTasks sets aside tasks left on screen for a week: a lesson or
// placement test keeps its place, anything else is abandoned. It reports
// how many.
func (s *Service) SetAsideIdleTasks(ctx context.Context) (int, error) {
	students, err := s.Store.ListUsers(ctx, store.RoleStudent)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range students {
		err := s.Store.Tx(ctx, u.ID, func(tx store.Tx) error {
			t, err := tx.GetActiveTask(ctx, u.ID)
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			if err != nil || s.now().Sub(t.UpdatedAt) < IdleTaskAge {
				return err
			}
			n++
			return s.setAside(ctx, tx, t, s.now())
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// NotePack records the pack now running, and tells parents when it changed
// to a newer one since the last start.
func (s *Service) NotePack(ctx context.Context) error {
	l := s.lib.Load()
	if l == nil {
		return nil
	}
	version := l.pack.Manifest.Version
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		prev, err := tx.GetSetting(ctx, "active_pack")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if prev == version {
			return nil
		}
		if err := tx.PutSetting(ctx, "active_pack", version, false); err != nil {
			return err
		}
		if prev == "" {
			return nil // the first pack this install ran: no news
		}
		return s.notify(ctx, tx, NotePackUpdated, "", version, map[string]any{"version": version, "from": prev}, s.now())
	})
}
