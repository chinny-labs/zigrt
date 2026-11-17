package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/store"
)

// testLearningStore covers the task, log and projection tables directly:
// one active task, first answer wins, XP pays once, projections replace
// cleanly, and deleting a student removes all of it.
func testLearningStore(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	st, now := f.Store, f.Clock.Now()
	tx := func(fn func(store.Tx) error) {
		t.Helper()
		must(t, st.Tx(ctx, f.Mika.ID, fn))
	}

	task := store.Task{ID: store.NewID(), StudentID: f.Mika.ID, Kind: store.TaskLesson, TopicID: "add",
		PackVersion: "2026.11.1", Status: store.TaskActive, State: json.RawMessage(`{"kp":0}`), StartedAt: now, UpdatedAt: now}
	tx(func(x store.Tx) error { return x.CreateTask(ctx, task) })
	second := task
	second.ID, second.TopicID = store.NewID(), "sub"
	err := st.Tx(ctx, f.Mika.ID, func(x store.Tx) error { return x.CreateTask(ctx, second) })
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a second active task: %v, want ErrConflict", err)
	}
	// Another student's active task is fine.
	other := second
	other.ID, other.StudentID = store.NewID(), f.Theo.ID
	must(t, st.Tx(ctx, f.Theo.ID, func(x store.Tx) error { return x.CreateTask(ctx, other) }))

	got, err := st.GetActiveTask(ctx, f.Mika.ID)
	must(t, err)
	if got.ID != task.ID || got.TopicID != "add" || string(got.State) != `{"kp":0}` || got.FinishedAt != nil || !got.StartedAt.Equal(now) {
		t.Fatalf("active task %+v", got)
	}
	// Pausing frees the active slot; a paused task is still open.
	task.Status, task.State, task.UpdatedAt = store.TaskPaused, json.RawMessage(`{"kp":1}`), now.Add(time.Minute)
	tx(func(x store.Tx) error { return x.UpdateTask(ctx, task) })
	if _, err := st.GetActiveTask(ctx, f.Mika.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("active after pause: %v", err)
	}
	tx(func(x store.Tx) error { return x.CreateTask(ctx, second) })
	open1, err := st.ListOpenTasks(ctx, f.Mika.ID)
	must(t, err)
	if len(open1) != 2 || open1[0].ID != task.ID || open1[0].Status != store.TaskPaused {
		t.Fatalf("open tasks %+v", open1)
	}
	done := now.Add(2 * time.Minute)
	second.Status, second.XPAwarded, second.UpdatedAt, second.FinishedAt = store.TaskPassed, 12, done, &done
	tx(func(x store.Tx) error { return x.UpdateTask(ctx, second) })
	fin, err := st.ListFinishedTasks(ctx, f.Mika.ID, 5)
	must(t, err)
	if len(fin) != 1 || fin[0].ID != second.ID || fin[0].XPAwarded != 12 || fin[0].FinishedAt == nil || !fin[0].FinishedAt.Equal(done) {
		t.Fatalf("finished tasks %+v", fin)
	}
	missing := task
	missing.ID = store.NewID()
	if err := st.Tx(ctx, f.Mika.ID, func(x store.Tx) error { return x.UpdateTask(ctx, missing) }); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("updating a missing task: %v", err)
	}

	// The first answer to an attempt ID stands.
	att := store.Attempt{ID: store.NewID(), TaskID: task.ID, StudentID: f.Mika.ID, TopicID: "add", KPID: "add.kp1",
		TemplateID: "add.kp1.t1", Seed: 1<<63 + 5, Answer: json.RawMessage(`{"main":"7"}`), Correct: true, ResponseMS: 4200, CreatedAt: now}
	var first, again bool
	tx(func(x store.Tx) (err error) { first, err = x.InsertAttempt(ctx, att); return })
	att2 := att
	att2.Answer, att2.Correct = json.RawMessage(`{"main":"8"}`), false
	tx(func(x store.Tx) (err error) { again, err = x.InsertAttempt(ctx, att2); return })
	if !first || again {
		t.Fatalf("insert attempt: first %v, again %v", first, again)
	}
	ga, err := st.GetAttempt(ctx, att.ID)
	must(t, err)
	if ga.Seed != att.Seed || !ga.Correct || string(ga.Answer) != `{"main":"7"}` || ga.KPID != "add.kp1" || ga.ResponseMS != 4200 {
		t.Fatalf("attempt %+v", ga)
	}
	if as, err := st.ListAttempts(ctx, task.ID); err != nil || len(as) != 1 {
		t.Fatalf("attempts %v %v", as, err)
	}

	// Events come back in the order written.
	for i, kind := range []string{"lesson_started", "lesson_passed", "implicit_credit"} {
		e := store.MasteryEvent{ID: store.NewID(), StudentID: f.Mika.ID, TopicID: "add", Kind: kind, TaskID: task.ID, CreatedAt: now}
		if i == 2 {
			e.TopicID, e.Detail = "count", json.RawMessage(`{"source":"add"}`)
		}
		tx(func(x store.Tx) error { return x.InsertMasteryEvent(ctx, e) })
	}
	evs, err := st.ListMasteryEvents(ctx, f.Mika.ID)
	must(t, err)
	if len(evs) != 3 || evs[0].Kind != "lesson_started" || evs[2].Kind != "implicit_credit" || string(evs[2].Detail) != `{"source":"add"}` || evs[1].Detail != nil {
		t.Fatalf("events %+v", evs)
	}

	// topic_state upserts, deletes and is replaced whole by a rebuild.
	ts := store.TopicState{StudentID: f.Mika.ID, TopicID: "add", State: "learning", UpdatedAt: now}
	tx(func(x store.Tx) error { return x.PutTopicState(ctx, ts) })
	ts.State, ts.Stage, ts.DueOn, ts.LastResult = "mastered", 1, "2026-03-10", "pass"
	tx(func(x store.Tx) error { return x.PutTopicState(ctx, ts) })
	tx(func(x store.Tx) error {
		return x.PutTopicState(ctx, store.TopicState{StudentID: f.Mika.ID, TopicID: "sub", State: "learning", Pauses: 2, UpdatedAt: now})
	})
	states, err := st.ListTopicStates(ctx, f.Mika.ID)
	must(t, err)
	if len(states) != 2 || states[0].TopicID != "add" || states[0].DueOn != "2026-03-10" || states[0].Stage != 1 || states[1].Pauses != 2 || states[1].DueOn != "" {
		t.Fatalf("states %+v", states)
	}
	tx(func(x store.Tx) error { return x.DeleteTopicState(ctx, f.Mika.ID, "sub") })
	tx(func(x store.Tx) error {
		return x.ReplaceTopicStates(ctx, f.Mika.ID, []store.TopicState{{TopicID: "count", State: "mastered", DueOn: "2026-03-08", UpdatedAt: now}})
	})
	states, err = st.ListTopicStates(ctx, f.Mika.ID)
	must(t, err)
	if len(states) != 1 || states[0].TopicID != "count" || states[0].StudentID != f.Mika.ID {
		t.Fatalf("replaced states %+v", states)
	}

	// XP pays once per source, and sums by kind and day.
	pay := func(kind, src string, amount int, day string) bool {
		var ok bool
		tx(func(x store.Tx) (err error) {
			ok, err = x.InsertXPEvent(ctx, store.XPEvent{ID: store.NewID(), StudentID: f.Mika.ID, SourceKind: kind,
				SourceID: src, Amount: amount, LocalDate: day, CreatedAt: now})
			return
		})
		return ok
	}
	if !pay(store.XPFromTask, task.ID, 12, "2026-03-07") || pay(store.XPFromTask, task.ID, 12, "2026-03-07") {
		t.Fatal("task XP paid other than once")
	}
	pay(store.XPFromDrill, "d1", 2, "2026-03-07")
	pay(store.XPFromDrill, "d2", 1, "2026-03-07")
	pay(store.XPFromDrill, "d3", 3, "2026-03-08")
	if n, err := st.SumXP(ctx, f.Mika.ID, store.XPFromDrill, "2026-03-07"); err != nil || n != 3 {
		t.Fatalf("drill XP on the 7th: %d %v", n, err)
	}
	if n, err := st.SumXP(ctx, f.Theo.ID, store.XPFromDrill, "2026-03-07"); err != nil || n != 0 {
		t.Fatalf("Theo's drill XP: %d %v", n, err)
	}
	if xs, err := st.ListXPEvents(ctx, f.Mika.ID); err != nil || len(xs) != 4 || xs[0].Amount != 12 {
		t.Fatalf("xp events %+v %v", xs, err)
	}

	// XP since an instant: strictly after it.
	if n, err := st.SumXPSince(ctx, f.Mika.ID, now.Add(-time.Second)); err != nil || n != 18 {
		t.Fatalf("XP since before: %d %v", n, err)
	}
	if n, err := st.SumXPSince(ctx, f.Mika.ID, now); err != nil || n != 0 {
		t.Fatalf("XP since now: %d %v", n, err)
	}
	// Tasks of a kind, newest first.
	quiz := store.Task{ID: store.NewID(), StudentID: f.Mika.ID, Kind: store.TaskQuiz, PackVersion: "2026.11.1",
		Status: store.TaskPassed, State: json.RawMessage(`{}`), StartedAt: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour), FinishedAt: &done}
	tx(func(x store.Tx) error { return x.CreateTask(ctx, quiz) })
	if ts, err := st.ListTasksOfKind(ctx, f.Mika.ID, store.TaskQuiz, 1); err != nil || len(ts) != 1 || ts[0].ID != quiz.ID || ts[0].TopicID != "" {
		t.Fatalf("quiz tasks %+v %v", ts, err)
	}
	if ts, err := st.ListTasksOfKind(ctx, f.Mika.ID, store.TaskLesson, 0); err != nil || len(ts) != 2 || ts[0].ID != second.ID {
		t.Fatalf("lesson tasks %+v %v", ts, err)
	}
	if ts, _ := st.ListTasksOfKind(ctx, f.Mika.ID, store.TaskDiagnostic, 0); len(ts) != 0 {
		t.Fatal("a diagnostic task appeared")
	}

	// daily_totals upserts and lists a range.
	for _, d := range []store.DailyTotal{
		{StudentID: f.Mika.ID, LocalDate: "2026-03-06", XP: 20, Goal: 15, GoalMet: true},
		{StudentID: f.Mika.ID, LocalDate: "2026-03-07", XP: 5, Goal: 15},
		{StudentID: f.Mika.ID, LocalDate: "2026-03-07", XP: 17, Goal: 15, GoalMet: true},
		{StudentID: f.Mika.ID, LocalDate: "2026-03-09", XP: 3, Goal: 15},
	} {
		tx(func(x store.Tx) error { return x.PutDailyTotal(ctx, d) })
	}
	days, err := st.ListDailyTotals(ctx, f.Mika.ID, "2026-03-07", "2026-03-09")
	must(t, err)
	if len(days) != 2 || days[0].XP != 17 || !days[0].GoalMet || days[1].LocalDate != "2026-03-09" {
		t.Fatalf("days %+v", days)
	}
	if d, err := st.GetDailyTotal(ctx, f.Mika.ID, "2026-03-06"); err != nil || d.XP != 20 {
		t.Fatalf("day %+v %v", d, err)
	}
	if _, err := st.GetDailyTotal(ctx, f.Mika.ID, "2026-03-08"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing day: %v", err)
	}
	tx(func(x store.Tx) error {
		return x.ReplaceDailyTotals(ctx, f.Mika.ID, []store.DailyTotal{{LocalDate: "2026-03-07", XP: 12, Goal: 15}})
	})
	if days, _ := st.ListDailyTotals(ctx, f.Mika.ID, "2026-01-01", "2026-12-31"); len(days) != 1 || days[0].XP != 12 {
		t.Fatalf("replaced days %+v", days)
	}

	// Deleting the student removes every learning row.
	must(t, st.Tx(ctx, "", func(x store.Tx) error { return x.DeleteUser(ctx, f.Mika.ID) }))
	if ts, _ := st.ListOpenTasks(ctx, f.Mika.ID); len(ts) != 0 {
		t.Error("tasks survived")
	}
	if _, err := st.GetAttempt(ctx, att.ID); !errors.Is(err, store.ErrNotFound) {
		t.Error("attempts survived")
	}
	if evs, _ := st.ListMasteryEvents(ctx, f.Mika.ID); len(evs) != 0 {
		t.Error("events survived")
	}
	if xs, _ := st.ListXPEvents(ctx, f.Mika.ID); len(xs) != 0 {
		t.Error("xp survived")
	}
	if ts, _ := st.ListOpenTasks(ctx, f.Theo.ID); len(ts) != 1 {
		t.Error("Theo's task went too")
	}
}
