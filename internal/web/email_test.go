package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/chinny-labs/zigrt/internal/store/storetest"
)

var linkRE = regexp.MustCompile(`https://zigrt\.example(/parent/reset/\S+)`)

// TestPasswordResetByEmail walks "Forgot your password?" to a new password,
// and the weekly email switch.
func TestPasswordResetByEmail(t *testing.T) {
	a := newApp(t)
	a.setUp()

	// Without email: the login page says how else.
	_, body := a.get(a.client(), "/parent/login")
	if strings.Contains(body, `href="/parent/forgot"`) || !strings.Contains(body, "zigrt reset-password") {
		t.Fatalf("login without mail:\n%s", body)
	}
	_, body = a.get(a.c, "/parent/household")
	if !strings.Contains(body, "can't send email yet") {
		t.Fatalf("household without mail:\n%s", body)
	}

	m := &storetest.FakeMail{}
	a.svc.Mail, a.svc.BaseURL = m, "https://zigrt.example"
	_, body = a.get(a.c, "/parent/household")
	if !strings.Contains(body, `name="weekly" value="1" checked`) {
		t.Fatalf("weekly email switch:\n%s", body)
	}
	// Goal-met notifications are off until a parent turns them on.
	if !strings.Contains(body, `name="goal_notes" value="1">`) {
		t.Fatalf("goal notes switch should start off:\n%s", body)
	}
	a.post(a.c, "/parent/household", "/parent/goal-notes", url.Values{"goal_notes": {"1"}})
	if on, _ := a.svc.GoalNotesOn(t.Context(), a.parent()); !on {
		t.Fatal("goal notes not turned on")
	}
	resp, _ := a.post(a.c, "/parent/household", "/parent/email", url.Values{})
	if on, _ := a.svc.WeeklyEmailOn(t.Context(), a.parent()); resp.StatusCode != http.StatusSeeOther || on {
		t.Fatalf("turning the weekly email off: %d on=%v", resp.StatusCode, on)
	}

	// A signed-out visitor asks for a link.
	c := a.client()
	_, body = a.get(c, "/parent/login")
	if !strings.Contains(body, `href="/parent/forgot"`) {
		t.Fatalf("no forgot link:\n%s", body)
	}
	for _, email := range []string{"nobody@example.com", "sam@example.com"} {
		resp, body = a.post(c, "/parent/forgot", "/parent/forgot", url.Values{"email": {email}})
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, "If that address belongs to a parent here") {
			t.Fatalf("forgot %s: %d\n%s", email, resp.StatusCode, body)
		}
	}
	if _, _, err := a.svc.SendOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	sent := m.Take()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(sent))
	}
	path := linkRE.FindStringSubmatch(sent[0].Body)[1]

	resp, body = a.get(c, path)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "New password") || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("reset page: %d\n%s", resp.StatusCode, body)
	}
	resp, body = a.post(c, path, path, url.Values{"password": {"short"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a short password: %d\n%s", resp.StatusCode, body)
	}
	resp, _ = a.post(c, path, path, url.Values{"password": {"a brand new long password"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/parent/login" {
		t.Fatalf("reset: %d", resp.StatusCode)
	}
	// The old session is gone; the link is used up.
	if resp, _ := a.get(a.c, "/parent"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("the old session still works: %d", resp.StatusCode)
	}
	_, body = a.get(c, path)
	if !strings.Contains(body, "expired or was already used") {
		t.Fatalf("a used link:\n%s", body)
	}
	resp, _ = a.post(c, "/parent/login", "/parent/login", url.Values{"email": {"sam@example.com"}, "password": {"a brand new long password"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign in with the new password: %d", resp.StatusCode)
	}
}

func TestLogPathHidesResetLinks(t *testing.T) {
	if got := logPath("/parent/reset/abc123"); got != "/parent/reset/…" {
		t.Fatal(got)
	}
	if got := logPath("/parent"); got != "/parent" {
		t.Fatal(got)
	}
}
