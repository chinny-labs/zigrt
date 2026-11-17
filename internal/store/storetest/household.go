package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/sim"
	"github.com/chinny-labs/zigrt/internal/store"
)

// testHouseholdStore covers notifications (deduplicated per parent),
// assigned drills, job bookkeeping and uploaded packs, and that deleting a
// student clears what is about them.
func testHouseholdStore(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	st, now := f.Store, f.Clock.Now()
	tx := func(fn func(store.Tx) error) {
		t.Helper()
		must(t, st.Tx(ctx, "", fn))
	}

	note := func(user, key string, at time.Time) bool {
		var ok bool
		tx(func(x store.Tx) (err error) {
			ok, err = x.InsertNotification(ctx, store.Notification{ID: store.NewID(), UserID: user, Kind: "goal_met",
				SubjectID: f.Mika.ID, Payload: json.RawMessage(`{"xp":15}`), DedupeKey: key, CreatedAt: at})
			return
		})
		return ok
	}
	if !note(f.Sam.ID, "goal:mika:2026-03-07", now) || note(f.Sam.ID, "goal:mika:2026-03-07", now) {
		t.Fatal("a notification was not deduplicated")
	}
	if !note(f.Alex.ID, "goal:mika:2026-03-07", now) || !note(f.Sam.ID, "goal:mika:2026-03-08", now.Add(time.Hour)) {
		t.Fatal("dedupe is per parent and per key")
	}
	ns, err := st.ListNotifications(ctx, f.Sam.ID, 10)
	must(t, err)
	if len(ns) != 2 || ns[0].DedupeKey != "goal:mika:2026-03-08" || string(ns[1].Payload) != `{"xp":15}` || ns[0].ReadAt != nil {
		t.Fatalf("notifications %+v", ns)
	}
	if n, _ := st.CountUnread(ctx, f.Sam.ID); n != 2 {
		t.Fatalf("unread %d", n)
	}
	tx(func(x store.Tx) error { return x.MarkNotificationsRead(ctx, f.Sam.ID, ns[0].ID, now) })
	if n, _ := st.CountUnread(ctx, f.Sam.ID); n != 1 {
		t.Fatalf("unread after one %d", n)
	}
	tx(func(x store.Tx) error { return x.MarkNotificationsRead(ctx, f.Sam.ID, "", now) })
	if n, _ := st.CountUnread(ctx, f.Sam.ID); n != 0 {
		t.Fatalf("unread after all %d", n)
	}
	if n, _ := st.CountUnread(ctx, f.Alex.ID); n != 1 {
		t.Fatal("marking one parent's read touched another's")
	}

	a := store.DrillAssignment{ID: store.NewID(), StudentID: f.Mika.ID, ParentID: f.Sam.ID, Kind: store.KindDrill,
		Title: "Friday times tables", Settings: json.RawMessage(`{"count":20}`), CreatedAt: now}
	tx(func(x store.Tx) error { return x.CreateAssignment(ctx, a) })
	got, err := st.GetAssignment(ctx, a.ID)
	must(t, err)
	if got.Title != a.Title || got.DrillID != "" || got.DoneAt != nil || string(got.Settings) != `{"count":20}` {
		t.Fatalf("assignment %+v", got)
	}
	got.DrillID, got.DoneAt = "d1", &now
	tx(func(x store.Tx) error { return x.UpdateAssignment(ctx, got) })
	if open1, _ := st.ListAssignments(ctx, f.Mika.ID, true); len(open1) != 0 {
		t.Fatal("a done assignment is still open")
	}
	if all, _ := st.ListAssignments(ctx, f.Mika.ID, false); len(all) != 1 || all[0].DrillID != "d1" {
		t.Fatalf("all assignments %+v", all)
	}

	if _, err := st.GetJobRun(ctx, "rollover"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no run yet: %v", err)
	}
	tx(func(x store.Tx) error { return x.PutJobRun(ctx, "rollover", "2026-03-06") })
	tx(func(x store.Tx) error { return x.PutJobRun(ctx, "rollover", "2026-03-07") })
	if last, err := st.GetJobRun(ctx, "rollover"); err != nil || last != "2026-03-07" {
		t.Fatalf("job run %q %v", last, err)
	}

	tx(func(x store.Tx) error {
		return x.PutContentPack(ctx, store.ContentPackRecord{Version: "2026.12.1", SHA256: "ab", File: "p.zip", InstalledBy: f.Sam.ID, InstalledAt: now})
	})
	if ps, err := st.ListContentPacks(ctx); err != nil || len(ps) != 1 || ps[0].InstalledBy != f.Sam.ID {
		t.Fatalf("packs %+v %v", ps, err)
	}

	// Deleting Mika removes her assignments and every notification about her.
	tx(func(x store.Tx) error { return x.DeleteUser(ctx, f.Mika.ID) })
	if all, _ := st.ListAssignments(ctx, f.Mika.ID, false); len(all) != 0 {
		t.Error("assignments survived")
	}
	if ns, _ := st.ListNotifications(ctx, f.Sam.ID, 0); len(ns) != 0 {
		t.Error("notifications about a deleted student survived")
	}
}

// testParentControls covers what a parent changes: settings, overrides,
// placement again, assigned drills, deleting a student; and the reports.
func testParentControls(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)
	svc := f.Svc

	// Settings: validated, saved, audited.
	var ue *service.UserError
	bad := service.StudentUpdate{DisplayName: "Mika", CourseID: "no-such-course", DailyXPGoal: 15}
	if err := svc.UpdateStudent(ctx, f.Sam, f.Mika.ID, bad); !errors.As(err, &ue) {
		t.Fatalf("bad course: %v", err)
	}
	if err := svc.UpdateStudent(ctx, f.Sam, f.Mika.ID, service.StudentUpdate{DisplayName: "Mika", DailyXPGoal: 15, RestDays: "0123456"}); !errors.As(err, &ue) {
		t.Fatalf("every day a rest day: %v", err)
	}
	if err := svc.UpdateStudent(ctx, f.Mika, f.Mika.ID, service.StudentUpdate{DisplayName: "Mika", DailyXPGoal: 15}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("a student changing settings: %v", err)
	}
	must(t, svc.UpdateStudent(ctx, f.Sam, f.Mika.ID, service.StudentUpdate{DisplayName: "Mika J", Avatar: "sun",
		CourseID: "grade-5", DailyXPGoal: 20, RestDays: "60", Fluency: false}))
	sv, err := svc.Student(ctx, f.Mika.ID)
	must(t, err)
	if sv.DisplayName != "Mika J" || sv.Avatar != "sun" || sv.Settings.CourseID != "grade-5" || sv.Settings.DailyXPGoal != 20 ||
		sv.Settings.RestDays != "06" || sv.Settings.FluencyEnabled {
		t.Fatalf("saved %+v", sv)
	}
	if audit, _ := f.Store.ListAudit(ctx, f.Mika.ID, 1); len(audit) != 1 || audit[0].Action != "student.settings" {
		t.Fatalf("audit %+v", audit)
	}
	// The course now shapes the home: grade 5 and what it needs.
	home, err := svc.Home(ctx, f.Mika)
	must(t, err)
	for _, n := range home.Tasks {
		if n.Topic != nil && n.Topic.Grade == "3" && n.Topic.ID != "elapsed-time" {
			t.Errorf("grade 3 topic %s offered on a grade 5 course", n.Topic.ID)
		}
	}

	// Overrides: placed, then not.
	must(t, svc.OverrideTopic(ctx, f.Sam, f.Mika.ID, "mult-2d-2d", true))
	states, _ := f.Store.ListTopicStates(ctx, f.Mika.ID)
	if len(states) != 1 || states[0].TopicID != "mult-2d-2d" || states[0].State != "mastered" || states[0].Stage != 2 {
		t.Fatalf("after placing %+v", states)
	}
	must(t, svc.OverrideTopic(ctx, f.Sam, f.Mika.ID, "mult-2d-2d", false))
	if states, _ = f.Store.ListTopicStates(ctx, f.Mika.ID); len(states) != 0 {
		t.Fatalf("after unplacing %+v", states)
	}
	must(t, svc.CheckProjections(ctx, f.Mika.ID))
	if err := svc.OverrideTopic(ctx, f.Sam, f.Mika.ID, "no-such-topic", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown topic: %v", err)
	}

	// Placement again: waiting at the top of the list.
	svc.Learn.OfferPlacement = true
	must(t, svc.RerunPlacement(ctx, f.Sam, f.Theo.ID))
	must(t, svc.RerunPlacement(ctx, f.Sam, f.Theo.ID)) // once is enough
	home, _ = svc.Home(ctx, f.Theo)
	if home.Tasks[0].Kind != store.TaskDiagnostic || !home.Tasks[0].Resume {
		t.Fatalf("placement again: %+v", home.Tasks[0])
	}
	if open, _ := f.Store.ListOpenTasks(ctx, f.Theo.ID); len(open) != 1 {
		t.Fatalf("open tasks %d", len(open))
	}
	svc.Learn.OfferPlacement = false

	// Assigned drills: on the home until done.
	set := drill.Default()
	a, err := svc.AssignDrill(ctx, f.Sam, f.Mika.ID, store.KindDrill, set, "Friday sums")
	must(t, err)
	ws, err := svc.AssignDrill(ctx, f.Alex, f.Mika.ID, store.KindWorksheet, set, "")
	must(t, err)
	home, _ = svc.Home(ctx, f.Mika)
	if len(home.Assigned) != 2 || home.Assigned[0].Title != "Friday sums" || home.Assigned[0].From != "Sam" || home.Assigned[1].From != "Alex" {
		t.Fatalf("assigned %+v", home.Assigned)
	}
	if _, err := svc.StartAssignment(ctx, f.Theo, a.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("someone else's assignment: %v", err)
	}
	d, err := svc.StartAssignment(ctx, f.Mika, a.ID)
	must(t, err)
	d2, err := svc.StartAssignment(ctx, f.Mika, a.ID)
	must(t, err)
	if d2.ID != d.ID {
		t.Fatal("starting twice made two drills")
	}
	answers := make([]service.Answer, len(d.Problems))
	for i, pr := range d.Problems {
		answers[i] = service.Answer{Main: pr.AnswerText()}
	}
	_, err = svc.FinishDrill(ctx, f.Mika, d.ID, answers, time.Minute)
	must(t, err)
	_, err = svc.StartAssignment(ctx, f.Mika, ws.ID)
	must(t, err)
	if home, _ = svc.Home(ctx, f.Mika); len(home.Assigned) != 0 {
		t.Fatalf("still assigned %+v", home.Assigned)
	}

	// The reports.
	cards, err := svc.Dashboard(ctx, f.Sam)
	must(t, err)
	if len(cards) != 2 || len(cards[0].Week) != 7 || cards[0].Week[0].Weekday != time.Monday {
		t.Fatalf("dashboard %+v", cards)
	}
	var mika service.StudentCard
	for _, c := range cards {
		if c.Student.ID == f.Mika.ID {
			mika = c
		}
	}
	if mika.Course != "Grade 5" || mika.WeekGoal != 5*20 || mika.WeekXP == 0 || mika.LastActive == nil {
		t.Fatalf("Mika's card %+v", mika)
	}
	rep, err := svc.StudentReport(ctx, f.Sam, f.Mika.ID)
	must(t, err)
	if len(rep.Activity) != 2 || rep.Activity[0].Kind != "worksheet" || rep.Activity[1].Kind != "drill" || rep.Activity[1].Detail != "20 of 20" {
		t.Fatalf("activity %+v", rep.Activity)
	}
	if _, err := svc.Dashboard(ctx, f.Mika); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("a student's dashboard: %v", err)
	}

	// Deleting: the name must be typed.
	if err := svc.DeleteStudent(ctx, f.Sam, f.Theo.ID, "Theodore"); !errors.As(err, &ue) {
		t.Fatalf("wrong name: %v", err)
	}
	must(t, svc.DeleteStudent(ctx, f.Sam, f.Theo.ID, " theo "))
	if _, err := svc.Student(ctx, f.Theo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

// testNotifications covers each piece of news a parent gets, and the jobs.
func testNotifications(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)
	svc := f.Svc
	kinds := func(u store.User) []string {
		t.Helper()
		ns, unread, err := svc.Notifications(ctx, u, 0)
		must(t, err)
		var out []string
		for _, n := range ns {
			out = append(out, n.Kind)
		}
		if unread > len(ns) {
			t.Fatalf("unread %d of %d", unread, len(ns))
		}
		return out
	}

	// Ten wrong PINs: locked out, both parents hear.
	for range 10 {
		f.Clock.Advance(time.Hour)
		_, _ = svc.LoginStudent(ctx, f.Theo.ID, "0000", "test")
	}
	if got := kinds(f.Sam); len(got) != 1 || got[0] != service.NoteLocked {
		t.Fatalf("Sam's: %v", got)
	}
	if got := kinds(f.Alex); len(got) != 1 {
		t.Fatalf("Alex's: %v", got)
	}
	ns, _, _ := svc.Notifications(ctx, f.Sam, 0)
	if ns[0].Student == nil || ns[0].Student.ID != f.Theo.ID {
		t.Fatalf("about %+v", ns[0].Student)
	}

	// A lesson passed that finishes a tier, and the goal met: once each.
	stu := f.student(f.Mika, nil)
	_, err := stu.Do(ctx, store.TaskLesson, "elapsed-time")
	must(t, err)
	if got := kinds(f.Sam); !slices.Contains(got, service.NoteMilestone) {
		t.Fatalf("after the tier: %v", got)
	}
	// Goal met is opt-in: Sam turns it on, Alex doesn't.
	must(t, svc.SetGoalNotes(ctx, f.Sam, true))
	for range 3 {
		home, err := svc.Home(ctx, f.Mika)
		must(t, err)
		if len(home.Tasks) > 0 {
			_, err = stu.Do(ctx, home.Tasks[0].Kind, home.Tasks[0].Topic.ID)
			must(t, err)
		}
	}
	goal := 0
	for _, k := range kinds(f.Sam) {
		if k == service.NoteGoalMet {
			goal++
		}
	}
	if goal != 1 {
		t.Fatalf("goal met %d times", goal)
	}
	if slices.Contains(kinds(f.Alex), service.NoteGoalMet) {
		t.Fatal("Alex heard of a goal met without turning it on")
	}

	// A lesson stopped twice.
	wrong := f.student(f.Theo, func(sim.Question) bool { return false })
	must(t, svc.Unlock(ctx, f.Sam, f.Theo.ID))
	_, err = wrong.Do(ctx, store.TaskLesson, "elapsed-time")
	must(t, err)
	f.Clock.Advance(24 * time.Hour)
	_, err = wrong.Do(ctx, store.TaskLesson, "elapsed-time")
	must(t, err)
	if got := kinds(f.Alex); !slices.Contains(got, service.NoteLessonStopped) {
		t.Fatalf("after two stops: %v", got)
	}

	// Streak lost: Mika met her goal today; tomorrow is missed; the day
	// after, the rollover tells.
	must(t, svc.Rollover(ctx)) // first run: starts from now
	for range 2 {              // two days in a row with the goal met
		f.Clock.Advance(24 * time.Hour)
		for home, _ := svc.Home(ctx, f.Mika); !home.Today.GoalMet && len(home.Tasks) > 0; home, _ = svc.Home(ctx, f.Mika) {
			_, err = stu.Do(ctx, home.Tasks[0].Kind, home.Tasks[0].Topic.ID)
			must(t, err)
		}
		if home, _ := svc.Home(ctx, f.Mika); !home.Today.GoalMet {
			t.Fatal("ran out of tasks before the goal")
		}
	}
	f.Clock.Advance(48 * time.Hour) // a day with nothing, then the next
	must(t, svc.Rollover(ctx))
	must(t, svc.Rollover(ctx)) // never twice for one day
	lost := 0
	for _, k := range kinds(f.Sam) {
		if k == service.NoteStreakLost {
			lost++
		}
	}
	if lost != 1 {
		t.Fatalf("streak lost %d times: %v", lost, kinds(f.Sam))
	}

	// Marking read.
	_, unread, _ := svc.Notifications(ctx, f.Sam, 0)
	if unread == 0 {
		t.Fatal("nothing unread")
	}
	must(t, svc.MarkRead(ctx, f.Sam, ""))
	if n, _ := svc.UnreadNotifications(ctx, f.Sam); n != 0 {
		t.Fatalf("unread after marking %d", n)
	}
	if n, _ := svc.UnreadNotifications(ctx, f.Alex); n == 0 {
		t.Fatal("Alex's were marked too")
	}

	// A new pack since the last start.
	must(t, svc.NotePack(ctx))
	must(t, svc.NotePack(ctx))
	if slices.Contains(kinds(f.Sam), service.NotePackUpdated) {
		t.Fatal("the first pack is not news")
	}
	newer := HardestPack(t)
	newer.Manifest.Version = "2099.01.1"
	svc.SetPack(newer)
	must(t, svc.NotePack(ctx))
	if !slices.Contains(kinds(f.Sam), service.NotePackUpdated) {
		t.Fatal("a newer pack was not news")
	}

	// A task left on screen for a week is set aside.
	home, err := svc.Home(ctx, f.Mika)
	must(t, err)
	if len(home.Tasks) == 0 {
		t.Fatal("nothing offered")
	}
	task, err := svc.StartTask(ctx, f.Mika, home.Tasks[0].Kind, home.Tasks[0].Topic.ID)
	must(t, err)
	f.Clock.Advance(8 * 24 * time.Hour)
	if n, err := svc.SetAsideIdleTasks(ctx); err != nil || n != 1 {
		t.Fatalf("set aside %d %v", n, err)
	}
	want := store.TaskAbandoned
	if task.Kind == store.TaskLesson {
		want = store.TaskPaused // a lesson keeps its place
	}
	if got, _ := f.Store.GetTask(ctx, task.ID); got.Status != want {
		t.Fatalf("idle %s is %s, want %s", task.Kind, got.Status, want)
	}
}
