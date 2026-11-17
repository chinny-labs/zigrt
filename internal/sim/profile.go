package sim

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/learning"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
	"github.com/chinny-labs/zigrt/problem"
)

// Profile is a kind of student to simulate (plan section 12): what they
// know at the start, how readily they learn, how much they forget.
type Profile struct {
	Name        string
	Description string
	KnowsUpTo   string  // grades up to and including this one are known at the start ("" for none)
	Learn       float64 // chance of a right answer in a lesson on a topic not known yet
	Forget      float64 // chance of a wrong answer in a review or quiz
}

// Profiles are the students `zigrt simulate` and the tests play.
var Profiles = []Profile{
	{Name: "steady", Description: "starts from nothing, learns well, rarely forgets", Learn: 0.95, Forget: 0.03},
	{Name: "forgetful", Description: "starts from nothing, forgets one answer in ten between reviews", Learn: 0.9, Forget: 0.1},
	{Name: "grade2", Description: "knows everything up to grade 2, forgets one in ten", KnowsUpTo: "2", Learn: 0.9, Forget: 0.1},
	{Name: "struggling", Description: "starts from nothing, often needs another go, forgets one in four", Learn: 0.7, Forget: 0.25},
}

// ProfileNamed finds a profile by name.
func ProfileNamed(name string) (Profile, bool) {
	i := slices.IndexFunc(Profiles, func(p Profile) bool { return p.Name == name })
	if i < 0 {
		return Profile{}, false
	}
	return Profiles[i], true
}

// Knows reports whether the profile knows a topic before any lesson.
func (p Profile) Knows(t *content.Topic) bool {
	if t == nil || p.KnowsUpTo == "" {
		return false
	}
	return slices.Index(content.Grades, t.Grade) <= slices.Index(content.Grades, p.KnowsUpTo)
}

// Student scripts a student of this profile. Fact topics are answered fast,
// everything else at the pace of real work.
func (p Profile) Student(svc *service.Service, u store.User, clock Clock, rng *rand.Rand) *Student {
	pack := svc.Pack()
	return &Student{
		Svc: svc, User: u, Clock: clock,
		Right: func(q Question) bool {
			switch q.Kind {
			case store.TaskDiagnostic:
				return p.Knows(q.Topic)
			case store.TaskLesson:
				return p.Knows(q.Topic) || rng.Float64() < p.Learn
			}
			return rng.Float64() >= p.Forget
		},
		NotLearned: func(q Question) bool { return !p.Knows(q.Topic) },
		Think: func(pr *problem.Problem) time.Duration {
			if t := pack.TopicOf(pr.Template); t != nil && t.FluencySeconds > 0 {
				return time.Duration(2+rng.IntN(3)) * time.Second
			}
			return time.Duration(6+rng.IntN(15)) * time.Second
		},
	}
}

// Report is what a simulation found.
type Report struct {
	Profile         Profile
	Days            int
	Topics          int            // in the course
	Placed          int            // by the placement test
	Mastered        int            // at the end
	MasteredByGrade map[string]int // at the end
	Lessons         int
	Reviews         int
	Quizzes         int
	ReviewsMax      int // reviews on the busiest day
	XP              int
	NeverReached    []string // topics in the course with no lesson or placement by the end
}

// Simulate plays one student of a profile through a pack for days, on a
// throwaway database, and reports how it went (`zigrt simulate`). It never
// touches a real database.
func Simulate(ctx context.Context, pack *content.Pack, prof Profile, days int, seed uint64) (Report, error) {
	dir, err := os.MkdirTemp("", "zigrt-simulate-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(dir)
	st, err := sqlite.Open(ctx, filepath.Join(dir, "sim.db"), nil)
	if err != nil {
		return Report{}, err
	}
	defer st.Close()
	svc := service.New(st, nil)
	svc.SetPack(pack)
	svc.Learn.OfferPlacement, svc.Learn.OfferQuizzes = true, true
	return Run(ctx, svc, prof, days, seed)
}

// Run plays one student of a profile on an empty store, days of 16:00
// sessions from 2026-01-05, and reports.
func Run(ctx context.Context, svc *service.Service, prof Profile, days int, seed uint64) (Report, error) {
	rep := Report{Profile: prof, Days: days, MasteredByGrade: map[string]int{}}
	start := time.Date(2026, 1, 5, 16, 0, 0, 0, time.UTC)
	clk := &clock{t: start}
	realNow := svc.Now
	svc.Now = clk.Now
	defer func() { svc.Now = realNow }()
	var n uint64
	realSeed := svc.Seed
	svc.Seed = func() uint64 { n++; return seed*0x9E3779B97F4A7C15 + n }
	defer func() { svc.Seed = realSeed }()

	parent, err := svc.Setup(ctx, service.SetupInput{Timezone: "UTC", DisplayName: "Sim", Email: "sim@zigrt.invalid", Password: "a simulation only"})
	if err != nil {
		return rep, err
	}
	u, err := svc.CreateStudent(ctx, parent, service.StudentInput{DisplayName: "Sim", PIN: "1234"})
	if err != nil {
		return rep, err
	}
	rng := rand.New(rand.NewPCG(seed, seed^0xC0FFEE)) //nolint:gosec // a repeatable simulation
	stu := prof.Student(svc, u, clk, rng)
	for d := range days {
		clk.set(start.AddDate(0, 0, d))
		day, err := stu.Day(ctx)
		if err != nil {
			return rep, fmt.Errorf("day %d: %w", d+1, err)
		}
		rep.Lessons += day.Lessons
		rep.Reviews += day.Reviews
		rep.Quizzes += day.Quizzes
		rep.ReviewsMax = max(rep.ReviewsMax, day.Reviews)
		if err := svc.CheckProjections(ctx, u.ID); err != nil {
			return rep, fmt.Errorf("day %d: the projections drifted from the logs: %w", d+1, err)
		}
	}

	pack := svc.Pack()
	states, err := svc.Store.ListTopicStates(ctx, u.ID)
	if err != nil {
		return rep, err
	}
	events, err := svc.Store.ListMasteryEvents(ctx, u.ID)
	if err != nil {
		return rep, err
	}
	reached := map[string]bool{}
	for _, s := range states {
		reached[s.TopicID] = true
		if s.State == string(learning.Mastered) {
			rep.Mastered++
			if t := pack.Topic(s.TopicID); t != nil {
				rep.MasteredByGrade[t.Grade]++
			}
		}
	}
	for _, e := range events {
		if e.Kind == string(learning.Placed) {
			rep.Placed++
		}
	}
	xp, err := svc.Store.SumXPSince(ctx, u.ID, time.Time{})
	if err != nil {
		return rep, err
	}
	rep.XP = xp
	set, err := svc.Store.GetStudentSettings(ctx, u.ID)
	if err != nil {
		return rep, err
	}
	course := pack.CourseTopics(set.CourseID)
	if len(course) == 0 && len(pack.Manifest.Courses) > 0 {
		course = pack.CourseTopics(pack.Manifest.Courses[0].ID)
	}
	rep.Topics = len(course)
	for _, t := range course {
		if !reached[t.ID] {
			rep.NeverReached = append(rep.NeverReached, t.ID)
		}
	}
	sort.Strings(rep.NeverReached)
	return rep, nil
}
