// Package storetest is the store contract suite (plan §8.3): every behaviour
// test runs against both SQLite and Postgres. This suite, not the interface,
// is the real definition of "pluggable".
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Opener returns a fresh, empty, migrated store for one test.
type Opener func(t *testing.T) store.Store

// Clock is a settable test clock.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func NewClock() *Clock { return &Clock{t: time.Date(2026, 3, 7, 15, 0, 0, 0, time.UTC)} }

func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *Clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// Fixture is a household with two parents and two students.
type Fixture struct {
	Svc         *service.Service
	Store       store.Store
	Clock       *Clock
	Sam, Alex   store.User
	Mika, Theo  store.User
	MikaPIN     string
	SamPassword string
}

// NewFixture sets up a household on a fresh store.
func NewFixture(t *testing.T, open Opener) *Fixture {
	t.Helper()
	ctx := context.Background()
	st := open(t)
	clk := NewClock()
	svc := service.New(st, nil)
	svc.Now = clk.Now
	f := &Fixture{Svc: svc, Store: st, Clock: clk, MikaPIN: "2468", SamPassword: "correct horse battery"}
	var err error
	f.Sam, err = svc.Setup(ctx, service.SetupInput{Timezone: "America/New_York", DisplayName: "Sam",
		Email: "Sam@Example.com", Password: f.SamPassword})
	must(t, err)
	f.Alex, err = svc.CreateParent(ctx, f.Sam, service.ParentInput{DisplayName: "Alex", Email: "alex@example.com", Password: "another long one"})
	must(t, err)
	f.Mika, err = svc.CreateStudent(ctx, f.Sam, service.StudentInput{DisplayName: "Mika", PIN: f.MikaPIN})
	must(t, err)
	f.Theo, err = svc.CreateStudent(ctx, f.Sam, service.StudentInput{DisplayName: "Theo", PIN: "13579"})
	must(t, err)
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Run runs the whole contract against one engine.
func Run(t *testing.T, open Opener) {
	for name, fn := range map[string]func(*testing.T, Opener){
		"Setup":          testSetup,
		"Parents":        testParents,
		"Students":       testStudents,
		"ParentLogin":    testParentLogin,
		"StudentLock":    testStudentLock,
		"Sessions":       testSessions,
		"ResetPIN":       testResetPIN,
		"DeleteUser":     testDeleteUser,
		"SettingsUpsert": testSettingsUpsert,
		"TimeRoundTrip":  testTimeRoundTrip,
	} {
		t.Run(name, func(t *testing.T) { fn(t, open) })
	}
}

func testSetup(t *testing.T, open Opener) {
	ctx := context.Background()
	st := open(t)
	svc := service.New(st, nil)
	if need, err := svc.NeedsSetup(ctx); err != nil || !need {
		t.Fatalf("fresh store: need=%v err=%v", need, err)
	}
	in := service.SetupInput{Timezone: "Europe/London", DisplayName: "Sam", Email: "sam@example.com", Password: "correct horse battery"}
	for _, bad := range []service.SetupInput{
		{Timezone: "Mars/Olympus", DisplayName: "Sam", Email: in.Email, Password: in.Password},
		{Timezone: in.Timezone, DisplayName: "", Email: in.Email, Password: in.Password},
		{Timezone: in.Timezone, DisplayName: "Sam", Email: "not-an-email", Password: in.Password},
		{Timezone: in.Timezone, DisplayName: "Sam", Email: in.Email, Password: "short"},
	} {
		var ue *service.UserError
		if _, err := svc.Setup(ctx, bad); !errors.As(err, &ue) {
			t.Fatalf("Setup(%+v) = %v, want a UserError", bad, err)
		}
	}
	p, err := svc.Setup(ctx, in)
	must(t, err)
	if !p.IsParent() || p.Email != "sam@example.com" {
		t.Fatalf("parent %+v", p)
	}
	h, err := st.GetHousehold(ctx)
	if err != nil || h.Timezone != "Europe/London" {
		t.Fatalf("household %+v, %v", h, err)
	}
	if need, _ := svc.NeedsSetup(ctx); need {
		t.Fatal("still needs setup")
	}
	if _, err := svc.Setup(ctx, in); !errors.Is(err, service.ErrAlreadySetUp) {
		t.Fatalf("second setup: %v", err)
	}
}

func testParents(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	var ue *service.UserError
	// Emails are unique, whatever the case.
	if _, err := f.Svc.CreateParent(ctx, f.Sam, service.ParentInput{DisplayName: "Dup", Email: "SAM@example.com", Password: "another long one"}); !errors.As(err, &ue) || ue.Field != "email" {
		t.Fatalf("duplicate email: %v", err)
	}
	// Students can't add parents.
	if _, err := f.Svc.CreateParent(ctx, f.Mika, service.ParentInput{DisplayName: "X", Email: "x@example.com", Password: "another long one"}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student created a parent: %v", err)
	}
	if n, err := f.Store.CountParents(ctx); err != nil || n != 2 {
		t.Fatalf("parents = %d, %v", n, err)
	}
	u, err := f.Store.GetUserByEmail(ctx, "  ALEX@example.com ")
	if err != nil || u.ID != f.Alex.ID {
		t.Fatalf("GetUserByEmail: %+v, %v", u, err)
	}
}

func testStudents(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	list, err := f.Svc.Students(ctx)
	must(t, err)
	if len(list) != 2 || list[0].DisplayName != "Mika" || list[1].DisplayName != "Theo" {
		t.Fatalf("students %+v", list)
	}
	for _, s := range list {
		if s.Settings.DailyXPGoal != store.DefaultDailyXPGoal || !s.Settings.FluencyEnabled || s.Locked || s.Email != "" {
			t.Fatalf("defaults %+v", s)
		}
	}
	if list[0].Avatar == list[1].Avatar || list[0].Avatar == "" {
		t.Fatalf("avatars %q %q", list[0].Avatar, list[1].Avatar)
	}
	var ue *service.UserError
	for _, pin := range []string{"123", "1234567", "12a4", ""} {
		if _, err := f.Svc.CreateStudent(ctx, f.Sam, service.StudentInput{DisplayName: "June", PIN: pin}); !errors.As(err, &ue) || ue.Field != "pin" {
			t.Fatalf("PIN %q: %v", pin, err)
		}
	}
	if _, err := f.Svc.CreateStudent(ctx, f.Theo, service.StudentInput{DisplayName: "June", PIN: "1234"}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student created a student: %v", err)
	}
	// A parent is not a student.
	if _, err := f.Svc.Student(ctx, f.Sam.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("Student(parent): %v", err)
	}
}

func testParentLogin(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	if _, err := f.Svc.LoginParent(ctx, "nobody@example.com", "whatever it is", ""); !errors.Is(err, service.ErrBadCredentials) {
		t.Fatalf("unknown email: %v", err)
	}
	res, err := f.Svc.LoginParent(ctx, "SAM@example.com", f.SamPassword, "Mac")
	must(t, err)
	u, sess, err := f.Svc.Authenticate(ctx, res.Token)
	if err != nil || u.ID != f.Sam.ID || sess.DeviceLabel != "Mac" {
		t.Fatalf("authenticate: %+v %+v %v", u, sess, err)
	}
	// Wrong passwords back off but never hard-lock a parent.
	var th *service.ThrottledError
	for i := 1; i <= auth.StudentLockAfter+2; i++ {
		_, err := f.Svc.LoginParent(ctx, "sam@example.com", "wrong password!", "")
		switch {
		case i < auth.BackoffAfter:
			if !errors.Is(err, service.ErrBadCredentials) {
				t.Fatalf("try %d: %v", i, err)
			}
		default:
			if !errors.As(err, &th) {
				t.Fatalf("try %d: %v", i, err)
			}
			f.Clock.Advance(th.Wait + time.Second)
		}
	}
	if _, err := f.Svc.LoginParent(ctx, "sam@example.com", f.SamPassword, ""); err != nil {
		t.Fatalf("right password after backoff: %v", err)
	}
	a, err := f.Store.GetAuthAttempts(ctx, f.Sam.ID)
	if err != nil || a.Failures != 0 || a.LockedAt != nil {
		t.Fatalf("attempts after success: %+v, %v", a, err)
	}
	// The command-line reset signs everyone out.
	must(t, f.Svc.ResetPassword(ctx, nil, "sam@example.com", "a brand new password"))
	if _, _, err := f.Svc.Authenticate(ctx, res.Token); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("old session after reset: %v", err)
	}
	if _, err := f.Svc.LoginParent(ctx, "sam@example.com", "a brand new password", ""); err != nil {
		t.Fatal(err)
	}
}

func testStudentLock(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	res, err := f.Svc.LoginStudent(ctx, f.Mika.ID, f.MikaPIN, "iPad")
	must(t, err)
	// A parent can't sign in through the PIN pad.
	if _, err := f.Svc.LoginStudent(ctx, f.Sam.ID, "1234", ""); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("PIN sign-in as a parent: %v", err)
	}
	var th *service.ThrottledError
	for i := 1; i <= auth.StudentLockAfter; i++ {
		_, err := f.Svc.LoginStudent(ctx, f.Mika.ID, "0000", "")
		switch {
		case i == auth.StudentLockAfter:
			if !errors.Is(err, service.ErrLocked) {
				t.Fatalf("try %d: %v", i, err)
			}
		case i < auth.BackoffAfter:
			if !errors.Is(err, service.ErrBadPIN) {
				t.Fatalf("try %d: %v", i, err)
			}
		default:
			if !errors.As(err, &th) {
				t.Fatalf("try %d: %v", i, err)
			}
			f.Clock.Advance(th.Wait + time.Second)
		}
	}
	// Locked: even the right PIN fails, and the open session is gone.
	f.Clock.Advance(24 * time.Hour)
	if _, err := f.Svc.LoginStudent(ctx, f.Mika.ID, f.MikaPIN, ""); !errors.Is(err, service.ErrLocked) {
		t.Fatalf("right PIN while locked: %v", err)
	}
	if _, _, err := f.Svc.Authenticate(ctx, res.Token); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("session survived the lock: %v", err)
	}
	if v, err := f.Svc.Student(ctx, f.Mika.ID); err != nil || !v.Locked {
		t.Fatalf("view %+v, %v", v, err)
	}
	// Theo is unaffected, and only a parent can unlock.
	if _, err := f.Svc.LoginStudent(ctx, f.Theo.ID, "13579", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.Svc.Unlock(ctx, f.Theo, f.Mika.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student unlocked: %v", err)
	}
	must(t, f.Svc.Unlock(ctx, f.Alex, f.Mika.ID))
	if _, err := f.Svc.LoginStudent(ctx, f.Mika.ID, f.MikaPIN, ""); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	audit, err := f.Store.ListAudit(ctx, f.Mika.ID, 0)
	must(t, err)
	seen := map[string]bool{}
	for _, a := range audit {
		seen[a.Action] = true
	}
	if !seen["user.locked"] || !seen["user.unlock"] || !seen["user.create"] {
		t.Fatalf("audit %v", seen)
	}
}

func testSessions(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	parent, err := f.Svc.LoginParent(ctx, "sam@example.com", f.SamPassword, "")
	must(t, err)
	student, err := f.Svc.LoginStudent(ctx, f.Mika.ID, f.MikaPIN, "")
	must(t, err)
	// Using a session slides its expiry; an idle parent session ends first.
	f.Clock.Advance(6 * 24 * time.Hour)
	if _, _, err := f.Svc.Authenticate(ctx, parent.Token); err != nil {
		t.Fatal(err)
	}
	f.Clock.Advance(6 * 24 * time.Hour)
	if _, _, err := f.Svc.Authenticate(ctx, parent.Token); err != nil {
		t.Fatalf("slid session expired: %v", err)
	}
	f.Clock.Advance(service.ParentSession + time.Minute)
	if _, _, err := f.Svc.Authenticate(ctx, parent.Token); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("idle parent session: %v", err)
	}
	if _, _, err := f.Svc.Authenticate(ctx, student.Token); err != nil {
		t.Fatalf("student session inside 30 days: %v", err)
	}
	must(t, f.Svc.Logout(ctx, student.Token))
	if _, _, err := f.Svc.Authenticate(ctx, student.Token); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("after logout: %v", err)
	}
	if _, _, err := f.Svc.Authenticate(ctx, "not-a-token"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("garbage token: %v", err)
	}
	// Housekeeping removes both dead sessions once they are a day old.
	if n, err := f.Svc.SweepSessions(ctx); err != nil || n != 0 {
		t.Fatalf("early sweep removed %d, %v", n, err)
	}
	f.Clock.Advance(25 * time.Hour)
	if n, err := f.Svc.SweepSessions(ctx); err != nil || n != 2 {
		t.Fatalf("sweep removed %d, %v", n, err)
	}
}

func testResetPIN(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	res, err := f.Svc.LoginStudent(ctx, f.Mika.ID, f.MikaPIN, "")
	must(t, err)
	if err := f.Svc.ResetPIN(ctx, f.Mika, f.Mika.ID, "9999"); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student reset a PIN: %v", err)
	}
	if err := f.Svc.ResetPIN(ctx, f.Sam, f.Alex.ID, "9999"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("PIN reset on a parent: %v", err)
	}
	must(t, f.Svc.ResetPIN(ctx, f.Sam, f.Mika.ID, "975310"))
	if _, _, err := f.Svc.Authenticate(ctx, res.Token); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("old session after PIN reset: %v", err)
	}
	if _, err := f.Svc.LoginStudent(ctx, f.Mika.ID, f.MikaPIN, ""); !errors.Is(err, service.ErrBadPIN) {
		t.Fatalf("old PIN: %v", err)
	}
	if _, err := f.Svc.LoginStudent(ctx, f.Mika.ID, "975310", ""); err != nil {
		t.Fatal(err)
	}
}

func testDeleteUser(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	res, err := f.Svc.LoginStudent(ctx, f.Theo.ID, "13579", "")
	must(t, err)
	_, _ = f.Svc.LoginStudent(ctx, f.Theo.ID, "0000", "") // leaves an auth_attempts row
	must(t, f.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.DeleteUser(ctx, f.Theo.ID) }))
	if _, err := f.Store.GetUser(ctx, f.Theo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("user still there: %v", err)
	}
	if _, err := f.Store.GetStudentSettings(ctx, f.Theo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("settings still there: %v", err)
	}
	if _, _, err := f.Svc.Authenticate(ctx, res.Token); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("session still there: %v", err)
	}
	if list, _ := f.Svc.Students(ctx); len(list) != 1 || list[0].ID != f.Mika.ID {
		t.Fatalf("students %+v", list)
	}
}

func testSettingsUpsert(t *testing.T, open Opener) {
	ctx := context.Background()
	st := open(t)
	if _, err := st.GetSetting(ctx, "k"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing setting: %v", err)
	}
	for _, v := range []string{"one", "two"} {
		must(t, st.Tx(ctx, "", func(tx store.Tx) error { return tx.PutSetting(ctx, "k", v, true) }))
		if got, err := st.GetSetting(ctx, "k"); err != nil || got != v {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	// A failed transaction leaves nothing behind.
	boom := errors.New("boom")
	err := st.Tx(ctx, "", func(tx store.Tx) error {
		if err := tx.PutSetting(ctx, "k", "three", false); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if got, _ := st.GetSetting(ctx, "k"); got != "two" {
		t.Fatalf("rollback left %q", got)
	}
}

func testTimeRoundTrip(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	h, err := f.Store.GetHousehold(ctx)
	must(t, err)
	if !h.CreatedAt.Equal(f.Clock.Now()) || h.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at %v, want %v in UTC", h.CreatedAt, f.Clock.Now())
	}
	s := store.StudentSettings{UserID: f.Mika.ID, CourseID: "grade-2", DailyXPGoal: 20, RestDays: "06", FluencyEnabled: false}
	must(t, f.Store.Tx(ctx, "", func(tx store.Tx) error { return tx.PutStudentSettings(ctx, s) }))
	got, err := f.Store.GetStudentSettings(ctx, f.Mika.ID)
	if err != nil || got != s {
		t.Fatalf("settings %+v, %v", got, err)
	}
}
