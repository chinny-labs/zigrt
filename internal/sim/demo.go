package sim

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/problem"
)

// DemoLogin is how to sign in to the demo household.
type DemoLogin struct {
	Email, Password string
	Students        []DemoStudent
}

// DemoStudent is one demo child and their PIN.
type DemoStudent struct{ Name, PIN string }

// ErrNotEmpty means the database already has a household.
var ErrNotEmpty = errors.New("this database already has a household; the demo only fills an empty one")

// clock is a settable clock for the simulated weeks.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func (c *clock) set(t time.Time)         { c.mu.Lock(); c.t = t; c.mu.Unlock() }

// Demo fills an empty database with a household (plan 11.1): a parent, two
// students, and days of lessons and reviews ending at end, played through
// the real service. One student does every day and rarely slips; the other
// rests at weekends and misses about one in five. seed makes it repeatable.
func Demo(ctx context.Context, svc *service.Service, timezone, password string, days int, end time.Time, seed uint64) (DemoLogin, error) {
	if need, err := svc.NeedsSetup(ctx); err != nil {
		return DemoLogin{}, err
	} else if !need {
		return DemoLogin{}, ErrNotEmpty
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return DemoLogin{}, err
	}
	// Each simulated day starts at four in the afternoon, after school.
	y, m, d := end.In(loc).AddDate(0, 0, -days).Date()
	start := time.Date(y, m, d, 16, 0, 0, 0, loc)
	clk := &clock{t: start}
	realNow := svc.Now
	svc.Now = clk.Now
	defer func() { svc.Now = realNow }()

	login := DemoLogin{Email: "parent@demo.zigrt.invalid", Password: password}
	parent, err := svc.Setup(ctx, service.SetupInput{Timezone: timezone, DisplayName: "Sam", Email: login.Email, Password: password})
	if err != nil {
		return login, fmt.Errorf("demo parent: %w", err)
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x5EED)) //nolint:gosec // a repeatable demo, not a secret
	type kid struct {
		s        *Student
		weekends bool
	}
	var kids []kid
	for _, k := range []struct {
		name, avatar, pin string
		miss              int // misses one answer in miss
		weekends          bool
	}{
		{"Ada", "sun", "2468", 15, true},
		{"Ben", "blue", "1357", 5, false},
	} {
		u, err := svc.CreateStudent(ctx, parent, service.StudentInput{DisplayName: k.name, Avatar: k.avatar, PIN: k.pin})
		if err != nil {
			return login, fmt.Errorf("demo student: %w", err)
		}
		login.Students = append(login.Students, DemoStudent{Name: k.name, PIN: k.pin})
		miss := k.miss
		kids = append(kids, kid{weekends: k.weekends, s: &Student{
			Svc: svc, User: u, Clock: clk,
			Right: func(Question) bool { return rng.IntN(miss) != 0 },
			Think: func(pr *problem.Problem) time.Duration {
				if t := svc.Pack().TopicOf(pr.Template); t != nil && t.FluencySeconds > 0 {
					return time.Duration(2+rng.IntN(3)) * time.Second // facts come quickly
				}
				return time.Duration(5+rng.IntN(20)) * time.Second
			},
		}})
	}
	for day := 0; day < days; day++ {
		today := start.AddDate(0, 0, day)
		for _, k := range kids {
			clk.set(today.Add(time.Duration(rng.IntN(90)) * time.Minute))
			if wd := today.Weekday(); !k.weekends && (wd == time.Saturday || wd == time.Sunday) {
				continue
			}
			if _, err := k.s.Day(ctx); err != nil {
				return login, fmt.Errorf("demo day %d, %s: %w", day+1, k.s.User.DisplayName, err)
			}
		}
	}
	return login, nil
}
