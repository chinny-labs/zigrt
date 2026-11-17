package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/sim"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/problem"
)

// HardestPack loads the 20-topic prototype pack from content/testdata.
func HardestPack(t *testing.T) *content.Pack {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	p, err := content.Open(filepath.Join(filepath.Dir(file), "..", "..", "..", "content", "testdata", "hardest"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// withLessons loads the prototype pack and makes problem seeds repeatable.
func (f *Fixture) withLessons(t *testing.T) *content.Pack {
	return f.withPack(HardestPack(t))
}

// withPack loads a pack and makes problem seeds repeatable. Students start
// from the bottom: the placement test is off unless a test turns it on.
func (f *Fixture) withPack(p *content.Pack) *content.Pack {
	f.Svc.SetPack(p)
	f.Svc.Learn.OfferPlacement = false
	var n uint64
	f.Svc.Seed = func() uint64 { n++; return n * 0x9E3779B97F4A7C15 }
	return p
}

func (f *Fixture) student(u store.User, right func(sim.Question) bool) *sim.Student {
	return &sim.Student{Svc: f.Svc, User: u, Clock: f.Clock, Right: right}
}

// testLessonFlow walks one lesson by hand: explanation, problems, feedback,
// a knowledge point passed, a repeated submit, and the finish.
func testLessonFlow(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)

	home, err := f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if home.NoContent || home.Active != nil || len(home.Tasks) == 0 || home.Today.Goal != store.DefaultDailyXPGoal {
		t.Fatalf("home %+v", home)
	}
	first := home.Tasks[0]
	if first.Kind != store.TaskLesson || first.Topic.ID != "elapsed-time" {
		t.Fatalf("first task %s %s, want the first topic in teaching order", first.Kind, first.Topic.ID)
	}
	if _, err := f.Svc.StartTask(ctx, f.Mika, store.TaskLesson, "long-division-2d"); !errors.Is(err, service.ErrNotOffered) {
		t.Fatalf("a locked topic: %v", err)
	}
	if _, err := f.Svc.StartTask(ctx, f.Sam, store.TaskLesson, "elapsed-time"); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("a parent: %v", err)
	}

	task, err := f.Svc.StartTask(ctx, f.Mika, store.TaskLesson, "elapsed-time")
	must(t, err)
	again, err := f.Svc.StartTask(ctx, f.Mika, store.TaskLesson, "elapsed-time")
	must(t, err)
	if again.ID != task.ID {
		t.Fatal("starting the task on screen made another")
	}
	v, err := f.Svc.Task(ctx, f.Mika, task.ID)
	must(t, err)
	if v.Phase != service.PhaseExplain || v.Example == nil || len(v.Example.Solution) == 0 || v.KPIndex != 0 || v.KPCount != 1 {
		t.Fatalf("explain %+v", v)
	}
	if _, err := f.Svc.Task(ctx, f.Theo, task.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("another student's task: %v", err)
	}

	v, err = f.Svc.Continue(ctx, f.Mika, task.ID)
	must(t, err)
	if v.Phase != service.PhaseProblem || v.Problem == nil || v.AttemptID == "" {
		t.Fatalf("problem %+v", v)
	}
	// A wrong answer shows the worked solution and the right answer.
	f.Clock.Advance(9 * time.Second)
	attempt := v.AttemptID
	v, err = f.Svc.SubmitAnswer(ctx, f.Mika, task.ID, attempt, v.Problem.Wrong())
	must(t, err)
	if v.Phase != service.PhaseFeedback || v.Feedback == nil || v.Feedback.Right || v.Feedback.Answer == "" || len(v.Feedback.Problem.Solution) == 0 {
		t.Fatalf("feedback %+v", v.Feedback)
	}
	// Sending the same answer again changes nothing.
	v2, err := f.Svc.SubmitAnswer(ctx, f.Mika, task.ID, attempt, problem.Response{Main: "1"})
	must(t, err)
	if v2.Phase != service.PhaseFeedback || v2.Feedback.Given != v.Feedback.Given {
		t.Fatalf("resubmit changed the task: %+v", v2.Feedback)
	}
	if as, _ := f.Store.ListAttempts(ctx, task.ID); len(as) != 1 || as[0].Correct || as[0].ResponseMS != 9000 {
		t.Fatalf("attempts %+v", as)
	}

	// Two right in a row pass the only knowledge point, and the lesson.
	for i := range 2 {
		v, err = f.Svc.Continue(ctx, f.Mika, task.ID)
		must(t, err)
		f.Clock.Advance(7 * time.Second)
		v, err = f.Svc.SubmitAnswer(ctx, f.Mika, task.ID, v.AttemptID, v.Problem.Right())
		must(t, err)
		if !v.Feedback.Right {
			t.Fatalf("answer %d marked wrong", i)
		}
	}
	// One miss is one retry, which still earns the bonus: 12 XP + 20%.
	if v.Task.Status != store.TaskPassed || v.Task.XPAwarded != 14 {
		t.Fatalf("finished task %+v", v.Task)
	}
	states, err := f.Store.ListTopicStates(ctx, f.Mika.ID)
	must(t, err)
	if len(states) != 1 || states[0].State != "mastered" || states[0].DueOn != "2026-03-08" {
		t.Fatalf("states %+v", states)
	}
	v, err = f.Svc.Continue(ctx, f.Mika, task.ID)
	must(t, err)
	if v.Phase != service.PhaseDone || v.Result == nil || !v.Result.Passed || v.Result.Today.XP != 14 {
		t.Fatalf("done %+v", v.Result)
	}
}

// testLessonPauses covers three misses pausing a lesson until tomorrow,
// setting a lesson aside for another task, and rushing.
func testLessonPauses(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)
	wrong := f.student(f.Mika, func(sim.Question) bool { return false })

	v, err := wrong.Do(ctx, store.TaskLesson, "elapsed-time")
	must(t, err)
	if v.Phase != service.PhasePaused || v.Task.Status != store.TaskPaused || v.Result == nil || v.Result.XP != 0 {
		t.Fatalf("after three misses: phase %s status %s", v.Phase, v.Task.Status)
	}
	home, err := f.Svc.Home(ctx, f.Mika)
	must(t, err)
	for _, n := range append(home.Tasks, v.Result.Next...) {
		if n.Topic.ID == "elapsed-time" {
			t.Fatal("a lesson paused today is offered again today")
		}
	}
	if _, err := f.Svc.StartTask(ctx, f.Mika, store.TaskLesson, "elapsed-time"); !errors.Is(err, service.ErrNotOffered) {
		t.Fatalf("restarting a paused lesson today: %v", err)
	}

	// Tomorrow it is offered first, as a lesson to resume, at the same place.
	f.Clock.Advance(24 * time.Hour)
	home, err = f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if len(home.Tasks) == 0 || home.Tasks[0].Topic.ID != "elapsed-time" || !home.Tasks[0].Resume {
		t.Fatalf("next day: %+v", home.Tasks)
	}
	resumed, err := f.Svc.StartTask(ctx, f.Mika, store.TaskLesson, "elapsed-time")
	must(t, err)
	if resumed.ID != v.Task.ID || resumed.Status != store.TaskActive {
		t.Fatalf("resume made a new task: %+v", resumed)
	}

	// Starting a different offered task sets the lesson aside, still open.
	home, err = f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if home.Active == nil || home.Active.TaskID != resumed.ID {
		t.Fatalf("active %+v", home.Active)
	}
	other := home.Tasks[1]
	if other.Topic.ID == "elapsed-time" {
		other = home.Tasks[0]
	}
	if _, err := f.Svc.StartTask(ctx, f.Mika, other.Kind, other.Topic.ID); err != nil {
		t.Fatal(err)
	}
	if t1, _ := f.Store.GetTask(ctx, resumed.ID); t1.Status != store.TaskPaused {
		t.Fatalf("set-aside lesson is %s", t1.Status)
	}
	home, err = f.Svc.Home(ctx, f.Mika)
	must(t, err)
	found := false
	for _, n := range home.Tasks {
		found = found || (n.Topic.ID == "elapsed-time" && n.Resume)
	}
	if !found {
		t.Fatal("a lesson set aside (not paused by misses) is not offered to resume")
	}

	must(t, f.Svc.CheckProjections(ctx, f.Mika.ID))
	must(t, f.Svc.CheckProjections(ctx, f.Theo.ID))
}

// testReviewsAndXP covers a review from start to end, drill XP and its daily
// cap, the streak, and a rebuild after the projections are lost.
func testReviewsAndXP(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)
	right := f.student(f.Mika, nil)

	v, err := right.Do(ctx, store.TaskLesson, "elapsed-time")
	must(t, err)
	if v.Task.XPAwarded != 14 {
		t.Fatalf("lesson XP %d", v.Task.XPAwarded)
	}
	// Not due today: the review is not offered.
	if _, err := f.Svc.StartTask(ctx, f.Mika, store.TaskReview, "elapsed-time"); !errors.Is(err, service.ErrNotOffered) {
		t.Fatalf("an early review: %v", err)
	}
	f.Clock.Advance(24 * time.Hour)
	home, err := f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if home.Tasks[0].Kind != store.TaskReview || home.Tasks[0].Topic.ID != "elapsed-time" || home.Today.Streak != 0 {
		t.Fatalf("next day: %+v streak %d", home.Tasks[0], home.Today.Streak)
	}
	// Rushed answers in a review: "slow down" shows, and no XP.
	task, err := f.Svc.StartTask(ctx, f.Mika, store.TaskReview, "elapsed-time")
	must(t, err)
	tv, err := f.Svc.Task(ctx, f.Mika, task.ID)
	slow := 0
	for err == nil && tv.Phase != service.PhaseDone {
		switch tv.Phase {
		case service.PhaseProblem:
			if tv.Total != 5 || tv.Asked > 4 {
				t.Fatalf("review progress %d of %d", tv.Asked, tv.Total)
			}
			f.Clock.Advance(400 * time.Millisecond)
			tv, err = f.Svc.SubmitAnswer(ctx, f.Mika, task.ID, tv.AttemptID, tv.Problem.Right())
		case service.PhaseSlow:
			slow++
			fallthrough
		default:
			tv, err = f.Svc.Continue(ctx, f.Mika, task.ID)
		}
	}
	must(t, err)
	if slow != 1 || tv.Task.Status != store.TaskPassed || tv.Task.XPAwarded != 0 || !tv.Result.Rushed {
		t.Fatalf("rushed review: slow %d, %+v, result %+v", slow, tv.Task, tv.Result)
	}
	states, _ := f.Store.ListTopicStates(ctx, f.Mika.ID)
	if states[0].Stage != 1 || states[0].DueOn != "2026-03-11" {
		t.Fatalf("after the review: %+v", states[0])
	}

	// Drills pay 1 XP per 10 right, at most 5 a day.
	finishDrill := func(n int) {
		t.Helper()
		set := drill.Default()
		set.Count = 50
		d, err := f.Svc.CreateDrill(ctx, f.Mika, store.KindDrill, set, "")
		must(t, err)
		answers := make([]service.Answer, len(d.Problems))
		for i, p := range d.Problems {
			if i < n {
				answers[i] = service.Answer{Main: p.AnswerText()}
			}
		}
		_, err = f.Svc.FinishDrill(ctx, f.Mika, d.ID, answers, time.Minute)
		must(t, err)
	}
	finishDrill(37)
	finishDrill(50)
	home, err = f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if home.Today.XP != 5 {
		t.Fatalf("drill XP today %d, want 3 + 2 (capped)", home.Today.XP)
	}

	// Meeting the goal (15) starts a streak; it survives into tomorrow.
	f.Clock.Advance(time.Hour)
	for home.Today.XP < 15 && len(home.Tasks) > 0 {
		_, err = right.Do(ctx, home.Tasks[0].Kind, home.Tasks[0].Topic.ID)
		must(t, err)
		home, err = f.Svc.Home(ctx, f.Mika)
		must(t, err)
	}
	if !home.Today.GoalMet || home.Today.Streak != 1 {
		t.Fatalf("goal met %v streak %d", home.Today.GoalMet, home.Today.Streak)
	}
	f.Clock.Advance(24 * time.Hour)
	home, _ = f.Svc.Home(ctx, f.Mika)
	if home.Today.Streak != 1 || home.Today.XP != 0 {
		t.Fatalf("tomorrow: streak %d xp %d", home.Today.Streak, home.Today.XP)
	}

	// Projections lost: a rebuild puts them back exactly.
	must(t, f.Svc.CheckProjections(ctx, f.Mika.ID))
	before, _ := f.Store.ListTopicStates(ctx, f.Mika.ID)
	must(t, f.Store.Tx(ctx, f.Mika.ID, func(tx store.Tx) error {
		if err := tx.ReplaceTopicStates(ctx, f.Mika.ID, nil); err != nil {
			return err
		}
		return tx.PutDailyTotal(ctx, store.DailyTotal{StudentID: f.Mika.ID, LocalDate: "2026-03-08", XP: 999, Goal: 15, GoalMet: true})
	}))
	if f.Svc.CheckProjections(ctx, f.Mika.ID) == nil {
		t.Fatal("CheckProjections missed lost projections")
	}
	n, err := f.Svc.Rebuild(ctx)
	must(t, err)
	if n != 2 {
		t.Errorf("rebuilt %d students", n)
	}
	must(t, f.Svc.CheckProjections(ctx, f.Mika.ID))
	after, _ := f.Store.ListTopicStates(ctx, f.Mika.ID)
	if len(after) != len(before) {
		t.Fatalf("rebuilt %d states, had %d", len(after), len(before))
	}
	for i := range after {
		after[i].UpdatedAt, before[i].UpdatedAt = time.Time{}, time.Time{}
		if after[i] != before[i] {
			t.Errorf("rebuilt %+v, had %+v", after[i], before[i])
		}
	}
}

// testSimulatedWeeks is phase 3's "done" test: students work through the
// prototype pack over simulated weeks, and every review lands on the
// schedule the rules give, on both engines.
func testSimulatedWeeks(t *testing.T, open Opener) {
	simulatedWeeks(t, open, HardestPack(t), 60)
}

// testSampleWeeks does the same with the sample pack the app embeds: 30
// topics from kindergarten to grade 5.
func testSampleWeeks(t *testing.T, open Opener) {
	p, err := pack.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	simulatedWeeks(t, open, p, 90)
}

func simulatedWeeks(t *testing.T, open Opener, p *content.Pack, days int) {
	ctx := context.Background()
	f := NewFixture(t, open)
	pack := f.withPack(p)

	// Mika answers everything right; Theo misses about one in six.
	rng := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // a repeatable script, not a secret
	students := []*sim.Student{
		f.student(f.Mika, nil),
		f.student(f.Theo, func(sim.Question) bool { return rng.IntN(6) != 0 }),
	}
	// Facts come quickly to a child who knows them; everything else takes
	// a few seconds of work.
	for _, s := range students {
		s.Think = sim.ThinkTime(pack, 3*time.Second, 8*time.Second)
	}
	maxReviews := 0
	start := f.Clock.Now()
	for d := range days {
		for _, s := range students {
			day, err := s.Day(ctx)
			if err != nil {
				t.Fatalf("day %d, %s: %v", d, s.User.DisplayName, err)
			}
			maxReviews = max(maxReviews, day.Reviews)
			if err := f.Svc.CheckProjections(ctx, s.User.ID); err != nil {
				t.Fatalf("day %d, %s: %v", d, s.User.DisplayName, err)
			}
		}
		f.Clock.Advance(start.Add(time.Duration(d+1) * 24 * time.Hour).Sub(f.Clock.Now()))
	}
	if maxReviews > 8 {
		t.Errorf("a day had %d reviews", maxReviews)
	}

	cfg := f.Svc.Learn
	for _, s := range students {
		states, err := f.Store.ListTopicStates(ctx, s.User.ID)
		must(t, err)
		mastered := 0
		for _, st := range states {
			if st.State == "mastered" {
				mastered++
			}
		}
		t.Logf("%s: %d of %d topics mastered", s.User.DisplayName, mastered, len(pack.Topics))
		if mastered < len(pack.Topics)-1 {
			t.Errorf("%s mastered %d of %d topics in %d days", s.User.DisplayName, mastered, len(pack.Topics), days)
		}
		checkSchedule(t, f.Store, s.User, cfg, 20)
	}
	// Theo's misses failed some reviews, and the schedule recovered.
	evs, _ := f.Store.ListMasteryEvents(ctx, f.Theo.ID)
	fails := 0
	for _, e := range evs {
		if e.Kind == "review_failed" {
			fails++
		}
	}
	t.Logf("Theo failed %d reviews; the busiest day had %d reviews", fails, maxReviews)
	if fails == 0 {
		t.Error("Theo never failed a review; the forgetful script is not exercising failures")
	}
}

// checkSchedule replays a student's event log and checks every review
// against the state before it: never early, a pass moves one stage up and
// out by that stage's interval, a fail drops two stages and comes back
// tomorrow (or reopens the lesson after two in a row).
func checkSchedule(t *testing.T, st store.Store, u store.User, cfg learning.Config, minReviews int) {
	t.Helper()
	evs, err := st.ListMasteryEvents(context.Background(), u.ID)
	must(t, err)
	type state struct {
		stage int
		due   learning.Date
		fails int
	}
	cur := map[string]state{}
	reviews, highest := 0, 0
	for _, e := range evs {
		var d struct {
			On    string `json:"on"`
			Stage int    `json:"stage"`
			Due   string `json:"due"`
		}
		must(t, json.Unmarshal(e.Detail, &d))
		on, _ := learning.ParseDate(d.On)
		due, _ := learning.ParseDate(d.Due)
		prev := cur[e.TopicID]
		switch e.Kind {
		case "review_passed", "review_failed":
			reviews++
			if on < prev.due {
				t.Errorf("%s: %s reviewed on %s, before it was due on %s", u.DisplayName, e.TopicID, d.On, prev.due)
			}
			if e.Kind == "review_passed" {
				if d.Stage != prev.stage+1 || due != on.Add(cfg.Interval(d.Stage)) {
					t.Errorf("%s: %s passed at stage %d on %s: now stage %d due %s", u.DisplayName, e.TopicID, prev.stage, d.On, d.Stage, d.Due)
				}
				highest = max(highest, d.Stage)
				prev.fails = 0
			} else {
				prev.fails++
				if prev.fails < cfg.FailsToReopen && (d.Stage != max(0, prev.stage-cfg.FailStageDrop) || due != on.Add(1)) {
					t.Errorf("%s: %s failed at stage %d on %s: now stage %d due %s", u.DisplayName, e.TopicID, prev.stage, d.On, d.Stage, d.Due)
				}
			}
		case "reopened":
			prev = state{}
			cur[e.TopicID] = prev
			continue
		}
		if d.Due != "" {
			prev.stage, prev.due = d.Stage, due
		}
		if e.Kind == "lesson_passed" {
			prev.fails = 0
		}
		cur[e.TopicID] = prev
	}
	t.Logf("%s: %d reviews checked, highest stage %d", u.DisplayName, reviews, highest)
	if reviews < minReviews || highest < 2 {
		t.Errorf("%s: %d reviews, highest stage %d: the run is too short to test the schedule", u.DisplayName, reviews, highest)
	}
}

// testDemo fills an empty household with the demo and refuses a second time.
func testDemo(t *testing.T, open Opener) {
	ctx := context.Background()
	st := open(t)
	svc := service.New(st, nil)
	p, err := pack.Embedded()
	must(t, err)
	svc.SetPack(p)
	end := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	login, err := sim.Demo(ctx, svc, "America/Los_Angeles", "a demo password", 21, end, 1)
	must(t, err)
	if len(login.Students) != 2 || login.Email == "" {
		t.Fatalf("login %+v", login)
	}
	if _, err := sim.Demo(ctx, svc, "America/Los_Angeles", "x", 21, end, 1); !errors.Is(err, sim.ErrNotEmpty) {
		t.Fatalf("second demo: %v", err)
	}
	if svc.Now().Year() == 2026 && svc.Now().Month() == 10 {
		t.Error("the demo left the service on its simulated clock")
	}
	students, err := st.ListUsers(ctx, store.RoleStudent)
	must(t, err)
	for _, u := range students {
		states, _ := st.ListTopicStates(ctx, u.ID)
		xp, _ := st.ListXPEvents(ctx, u.ID)
		if len(states) < 5 || len(xp) < 10 {
			t.Errorf("%s: %d topic states, %d XP events after 21 days", u.DisplayName, len(states), len(xp))
		}
		must(t, svc.CheckProjections(ctx, u.ID))
		checkSchedule(t, st, u, svc.Learn, 10)
	}
	if _, err := svc.LoginParent(ctx, login.Email, login.Password, "test"); err != nil {
		t.Errorf("demo parent sign-in: %v", err)
	}
}

// testPlacementFlow walks the placement test by hand: offered first to a
// new student, a welcome, questions across topics, "I haven't learned
// this", stopping and resuming, the end, and skipping.
func testPlacementFlow(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	p, err := pack.Embedded()
	must(t, err)
	f.withPack(p)
	f.Svc.Learn.OfferPlacement = true

	home, err := f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if len(home.Tasks) < 2 || home.Tasks[0].Kind != store.TaskDiagnostic || home.Tasks[0].Topic != nil || home.Tasks[1].Kind != store.TaskLesson {
		t.Fatalf("new student's tasks: %+v", home.Tasks)
	}
	task, err := f.Svc.StartTask(ctx, f.Mika, store.TaskDiagnostic, "")
	must(t, err)
	v, err := f.Svc.Task(ctx, f.Mika, task.ID)
	must(t, err)
	if v.Phase != service.PhaseIntro || v.Topic != nil {
		t.Fatalf("intro: %s", v.Phase)
	}
	v, err = f.Svc.Continue(ctx, f.Mika, task.ID)
	must(t, err)
	if v.Phase != service.PhaseProblem || v.ProblemTopic == nil || v.Total != 35 {
		t.Fatalf("first question: %s %v", v.Phase, v.ProblemTopic)
	}
	first := v.ProblemTopic.ID
	v, err = f.Svc.NotLearned(ctx, f.Mika, task.ID, v.AttemptID)
	must(t, err)
	if v.Phase != service.PhaseProblem || v.Asked != 1 || v.Feedback != nil {
		t.Fatalf("after not learned: %s asked %d", v.Phase, v.Asked)
	}
	if as, _ := f.Store.ListAttempts(ctx, task.ID); len(as) != 1 || as[0].Note != "not_learned" || as[0].TopicID != first {
		t.Fatalf("attempts %+v", as)
	}
	// Stopping part way: the test is kept and offered to resume.
	home, _ = f.Svc.Home(ctx, f.Mika)
	if home.Active == nil || home.Active.Kind != store.TaskDiagnostic {
		t.Fatalf("active %+v", home.Active)
	}
	_, err = f.Svc.StartTask(ctx, f.Mika, store.TaskLesson, home.Tasks[1].Topic.ID)
	must(t, err)
	home, _ = f.Svc.Home(ctx, f.Mika)
	if home.Tasks[0].Kind != store.TaskDiagnostic || !home.Tasks[0].Resume {
		t.Fatalf("placement set aside: %+v", home.Tasks[0])
	}
	again, err := f.Svc.StartTask(ctx, f.Mika, store.TaskDiagnostic, "")
	must(t, err)
	if again.ID != task.ID {
		t.Fatal("resuming made a new placement test")
	}
	// Answer everything right from here: every topic is placed except what
	// "not learned" ruled out.
	right := f.student(f.Mika, nil)
	v, err = right.Do(ctx, store.TaskDiagnostic, "")
	must(t, err)
	if v.Phase != service.PhaseDone || v.Task.Status != store.TaskPassed || len(v.Placed) == 0 || v.Task.XPAwarded != v.Asked/2 {
		t.Fatalf("done: %s %s placed %d xp %d asked %d", v.Phase, v.Task.Status, len(v.Placed), v.Task.XPAwarded, v.Asked)
	}
	for _, pt := range v.Placed {
		if pt.ID == first {
			t.Errorf("%s was placed after \"not learned\"", first)
		}
	}
	states, _ := f.Store.ListTopicStates(ctx, f.Mika.ID)
	if len(states) != len(v.Placed) || states[0].Stage != 2 {
		t.Fatalf("states %+v", states)
	}
	home, _ = f.Svc.Home(ctx, f.Mika)
	for _, n := range home.Tasks {
		if n.Kind == store.TaskDiagnostic {
			t.Fatal("placement offered again after it finished")
		}
	}
	must(t, f.Svc.CheckProjections(ctx, f.Mika.ID))

	// Theo skips it: it is not offered again, and lessons start at the bottom.
	must(t, f.Svc.SkipPlacement(ctx, f.Theo))
	home, _ = f.Svc.Home(ctx, f.Theo)
	if home.Tasks[0].Kind != store.TaskLesson || home.Tasks[0].Topic.ID != p.Topics[0].ID {
		t.Fatalf("after skipping: %+v", home.Tasks[0])
	}
	if err := f.Svc.SkipPlacement(ctx, f.Theo); !errors.Is(err, service.ErrNotOffered) {
		t.Fatalf("skipping twice: %v", err)
	}
}

// testQuizFlow: after enough XP a quiz comes before reviews; it shows no
// feedback until the end; a miss brings that topic's review due now.
func testQuizFlow(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	p, err := pack.Embedded()
	must(t, err)
	f.withPack(p)
	f.Svc.Learn.OfferQuizzes = true
	f.Svc.Learn.QuizEveryXP = 30 // three lessons' worth, to keep the test short
	stu := f.student(f.Mika, nil)
	stu.Think = sim.ThinkTime(p, 3*time.Second, 8*time.Second)
	for range 3 {
		home, err := f.Svc.Home(ctx, f.Mika)
		must(t, err)
		if home.Tasks[0].Kind == store.TaskQuiz {
			t.Fatal("a quiz before enough XP")
		}
		_, err = stu.Do(ctx, store.TaskLesson, home.Tasks[0].Topic.ID)
		must(t, err)
	}
	home, err := f.Svc.Home(ctx, f.Mika)
	must(t, err)
	if home.Tasks[0].Kind != store.TaskQuiz {
		t.Fatalf("after 3 lessons: %+v", home.Tasks[0])
	}
	task, err := f.Svc.StartTask(ctx, f.Mika, store.TaskQuiz, "")
	must(t, err)
	v, err := f.Svc.Task(ctx, f.Mika, task.ID)
	must(t, err)
	if v.Total != 8 || v.Phase != service.PhaseProblem {
		t.Fatalf("quiz: %d problems, %s", v.Total, v.Phase)
	}
	missed := v.ProblemTopic.ID
	for i := 0; v.Phase == service.PhaseProblem; i++ {
		f.Clock.Advance(9 * time.Second)
		resp := v.Problem.Right()
		if i == 0 {
			resp = v.Problem.Wrong()
		}
		v, err = f.Svc.SubmitAnswer(ctx, f.Mika, task.ID, v.AttemptID, resp)
		must(t, err)
		if v.Phase == service.PhaseFeedback {
			t.Fatal("feedback in a quiz before the end")
		}
	}
	if v.Phase != service.PhaseDone || len(v.QuizResults) != 8 || v.QuizResults[0].Right || !v.QuizResults[1].Right || v.Task.XPAwarded != 13 {
		t.Fatalf("results: %s, %d results, xp %d", v.Phase, len(v.QuizResults), v.Task.XPAwarded)
	}
	states, _ := f.Store.ListTopicStates(ctx, f.Mika.ID)
	today := learning.DateOf(f.Clock.Now(), mustLoc(t, "America/New_York")).String()
	for _, s := range states {
		if s.TopicID == missed && s.DueOn != today {
			t.Errorf("missed %s is due %s, want today %s", missed, s.DueOn, today)
		}
	}
	home, _ = f.Svc.Home(ctx, f.Mika)
	if home.Tasks[0].Kind != store.TaskReview || home.Tasks[0].Topic.ID != missed {
		t.Fatalf("after the quiz: %+v", home.Tasks[0])
	}
	must(t, f.Svc.CheckProjections(ctx, f.Mika.ID))
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	must(t, err)
	return loc
}

// testProfiles is plan section 12's simulated students: each profile plays
// 60 days of the sample pack with placement and quizzes on. A student who
// knows grade 2 is placed there and no higher; review load stays bounded;
// no topic is left unreached; every review lands on schedule.
func testProfiles(t *testing.T, open Opener) {
	p, err := pack.Embedded()
	must(t, err)
	for _, prof := range sim.Profiles {
		t.Run(prof.Name, func(t *testing.T) {
			ctx := context.Background()
			svc := service.New(open(t), nil)
			svc.SetPack(p)
			svc.Learn.OfferPlacement, svc.Learn.OfferQuizzes = true, true
			rep, err := sim.Run(ctx, svc, prof, 60, 11)
			must(t, err)
			t.Logf("%s: placed %d, mastered %d of %d, %d lessons, %d reviews (max %d a day), %d quizzes",
				prof.Name, rep.Placed, rep.Mastered, rep.Topics, rep.Lessons, rep.Reviews, rep.ReviewsMax, rep.Quizzes)
			if rep.ReviewsMax > 10 {
				t.Errorf("a day had %d reviews", rep.ReviewsMax)
			}
			if len(rep.NeverReached) > 0 {
				t.Errorf("never reached: %v", rep.NeverReached)
			}
			if rep.Quizzes == 0 {
				t.Error("no quiz in 60 days")
			}
			students, err := svc.Store.ListUsers(ctx, store.RoleStudent)
			must(t, err)
			u := students[0]
			evs, err := svc.Store.ListMasteryEvents(ctx, u.ID)
			must(t, err)
			for _, e := range evs {
				if e.Kind != string(learning.Placed) {
					continue
				}
				if tp := p.Topic(e.TopicID); !prof.Knows(tp) {
					t.Errorf("placed %s (grade %s), which the student did not know", e.TopicID, tp.Grade)
				}
			}
			known := 0
			for _, tp := range p.Topics {
				if prof.Knows(tp) {
					known++
				}
			}
			if rep.Placed != known {
				t.Errorf("placed %d, knew %d", rep.Placed, known)
			}
			checkSchedule(t, svc.Store, u, svc.Learn, 30)
		})
	}
}
