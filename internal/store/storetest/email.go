package storetest

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/service"
)

// FakeMail records messages, failing the next Fail sends.
type FakeMail struct {
	mu   sync.Mutex
	Sent []FakeMessage
	Fail int
}

// FakeMessage is one message FakeMail took.
type FakeMessage struct{ To, Subject, Body string }

func (m *FakeMail) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail > 0 {
		m.Fail--
		return errors.New("421 try later")
	}
	m.Sent = append(m.Sent, FakeMessage{to, subject, body})
	return nil
}

// Take returns the messages sent so far and forgets them.
func (m *FakeMail) Take() []FakeMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.Sent
	m.Sent = nil
	return out
}

var resetLinkRE = regexp.MustCompile(`https://zigrt\.example/parent/reset/(\S+)`)

// testEmail covers reset links, the weekly summary and the outbox's
// retries.
func testEmail(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	svc := f.Svc
	if err := svc.RequestPasswordReset(ctx, "sam@example.com"); !errors.Is(err, service.ErrMailOff) {
		t.Fatalf("with no mail: %v", err)
	}
	m := &FakeMail{}
	svc.Mail, svc.BaseURL = m, "https://zigrt.example"
	send := func() (int, int) {
		t.Helper()
		sent, failed, err := svc.SendOutbox(ctx)
		must(t, err)
		return sent, failed
	}

	// A reset link: only for a parent, at most once in five minutes.
	must(t, svc.RequestPasswordReset(ctx, "nobody@example.com"))
	must(t, svc.RequestPasswordReset(ctx, "sam@example.com"))
	must(t, svc.RequestPasswordReset(ctx, "sam@example.com"))
	if s, _ := send(); s != 1 {
		t.Fatalf("sent %d, want one link", s)
	}
	msg := m.Take()[0]
	link := resetLinkRE.FindStringSubmatch(msg.Body)
	if !strings.EqualFold(msg.To, "sam@example.com") || link == nil || msg.Subject != "Reset your Zigrt password" {
		t.Fatalf("reset email: %+v", msg)
	}
	token := link[1]
	must(t, svc.CheckResetLink(ctx, token))
	sess, err := svc.LoginParent(ctx, "sam@example.com", f.SamPassword, "test")
	must(t, err)
	if err := svc.ResetWithLink(ctx, token, "short"); err == nil {
		t.Fatal("a short password was taken")
	}
	must(t, svc.ResetWithLink(ctx, token, "a brand new long password"))
	if _, _, err := svc.Authenticate(ctx, sess.Token); err == nil {
		t.Fatal("the old session survived a reset")
	}
	if _, err := svc.LoginParent(ctx, "sam@example.com", "a brand new long password", "test"); err != nil {
		t.Fatalf("the new password: %v", err)
	}
	if err := svc.ResetWithLink(ctx, token, "yet another long password"); err == nil {
		t.Fatal("a link worked twice")
	}
	// A link expires in an hour.
	f.Clock.Advance(service.ResetEvery)
	must(t, svc.RequestPasswordReset(ctx, "alex@example.com"))
	send()
	link = resetLinkRE.FindStringSubmatch(m.Take()[0].Body)
	f.Clock.Advance(service.ResetLinkLife)
	if err := svc.CheckResetLink(ctx, link[1]); err == nil {
		t.Fatal("a link outlived its hour")
	}

	// Retries: a failure waits, then goes; six failures give up.
	m.Fail = 1
	f.Clock.Advance(service.ResetEvery)
	must(t, svc.RequestPasswordReset(ctx, "alex@example.com"))
	if s, fl := send(); s != 0 || fl != 1 {
		t.Fatalf("first try: %d sent %d failed", s, fl)
	}
	if s, fl := send(); s+fl != 0 {
		t.Fatal("retried at once")
	}
	if pending, failed, _ := f.Store.CountEmails(ctx, service.MaxEmailAttempts); pending != 1 || failed != 0 {
		t.Fatalf("outbox: %d pending, %d failed", pending, failed)
	}
	f.Clock.Advance(time.Minute)
	if s, _ := send(); s != 1 {
		t.Fatal("not retried after a minute")
	}
	m.Take()
	m.Fail = 100
	f.Clock.Advance(service.ResetEvery)
	must(t, svc.RequestPasswordReset(ctx, "alex@example.com"))
	for range 10 {
		send()
		f.Clock.Advance(13 * time.Hour)
	}
	if pending, failed, _ := f.Store.CountEmails(ctx, service.MaxEmailAttempts); pending != 0 || failed != 1 {
		t.Fatalf("after giving up: %d pending, %d failed", pending, failed)
	}
	m.Fail = 0

	// The weekly email: Sunday from six in the evening, household time
	// (New York), once a week, for parents who want it.
	must(t, svc.SetWeeklyEmail(ctx, f.Alex, false))
	loc, _ := time.LoadLocation("America/New_York")
	now := f.Clock.Now().In(loc)
	sunday := time.Date(now.Year(), now.Month(), now.Day()+(7-int(now.Weekday()))%7, 17, 59, 0, 0, loc)
	if !sunday.After(now) {
		sunday = sunday.AddDate(0, 0, 7)
	}
	f.Clock.Advance(sunday.Sub(now))
	must(t, svc.WeeklyEmail(ctx))
	if s, _ := send(); s != 0 {
		t.Fatal("sent before six")
	}
	f.Clock.Advance(time.Minute)
	must(t, svc.WeeklyEmail(ctx))
	must(t, svc.WeeklyEmail(ctx))
	send()
	got := m.Take()
	if len(got) != 1 || !strings.EqualFold(got[0].To, f.Sam.Email) {
		t.Fatalf("weekly: %+v", got)
	}
	monday := sunday.AddDate(0, 0, -6).Format("2 January")
	for _, want := range []string{f.Mika.DisplayName, f.Theo.DisplayName, "XP of a", "No practice this week", "/parent/household"} {
		if !strings.Contains(got[0].Body, want) || got[0].Subject != "Zigrt: the week of "+monday {
			t.Fatalf("weekly email lacks %q or has subject %q:\n%s", want, got[0].Subject, got[0].Body)
		}
	}
	f.Clock.Advance(24 * time.Hour) // Monday: nothing more
	must(t, svc.WeeklyEmail(ctx))
	if s, _ := send(); s != 0 {
		t.Fatal("sent twice in a week")
	}

	// Old messages are forgotten.
	f.Clock.Advance(service.EmailKeep + time.Hour)
	n, err := svc.PruneOutbox(ctx)
	must(t, err)
	if n == 0 {
		t.Fatal("nothing pruned")
	}
}
