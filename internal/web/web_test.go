package web

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/config"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
)

const setupToken = "test-setup-token"

// app is a server on a fresh SQLite file plus a browser-like client.
type app struct {
	t   *testing.T
	svc *service.Service
	ts  *httptest.Server
	c   *http.Client
}

func newApp(t *testing.T) *app {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "zigrt.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, nil)
	srv, err := New(ctx, svc, config.Config{InsecureCookies: true, Timezone: "UTC"}, nil, setupToken)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	a := &app{t: t, svc: svc, ts: ts}
	a.c = a.client()
	return a
}

// client is a fresh browser: its own cookies, redirects not followed.
func (a *app) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (a *app) get(c *http.Client, path string) (*http.Response, string) {
	a.t.Helper()
	resp, err := c.Get(a.ts.URL + path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

var csrfRE = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

// post loads formPage for its CSRF token, then posts form to path.
func (a *app) post(c *http.Client, formPage, path string, form url.Values) (*http.Response, string) {
	a.t.Helper()
	_, body := a.get(c, formPage)
	m := csrfRE.FindStringSubmatch(body)
	if m == nil {
		a.t.Fatalf("no CSRF token on %s", formPage)
	}
	form.Set("_csrf", m[1])
	resp, err := c.PostForm(a.ts.URL+path, form)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (a *app) setUp() {
	a.t.Helper()
	resp, body := a.post(a.c, "/setup", "/setup", url.Values{
		"setup_token": {setupToken}, "display_name": {"Sam"}, "email": {"sam@example.com"},
		"password": {"correct horse battery"}, "timezone": {"America/New_York"},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/parent/students/new" {
		a.t.Fatalf("setup: %d %s\n%s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
}

func (a *app) addStudent(name, pin string) string {
	a.t.Helper()
	resp, body := a.post(a.c, "/parent/students/new", "/parent/students", url.Values{"display_name": {name}, "pin": {pin}})
	if resp.StatusCode != http.StatusSeeOther {
		a.t.Fatalf("add student: %d\n%s", resp.StatusCode, body)
	}
	list, err := a.svc.Students(context.Background())
	if err != nil {
		a.t.Fatal(err)
	}
	for _, s := range list {
		if s.DisplayName == name {
			return s.ID
		}
	}
	a.t.Fatalf("student %q not created", name)
	return ""
}

func TestFirstRun(t *testing.T) {
	a := newApp(t)
	// Before setup every page leads to /setup, but health checks answer.
	for _, p := range []string{"/", "/parent", "/parent/login", "/pin/x"} {
		if resp, _ := a.get(a.c, p); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/setup" {
			t.Fatalf("GET %s before setup: %d %s", p, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	for _, p := range []string{"/healthz", "/readyz", "/static/app.css", "/static/fonts/fredoka-latin.woff2"} {
		if resp, _ := a.get(a.c, p); resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", p, resp.StatusCode)
		}
	}
	// A wrong token is refused and creates nothing.
	resp, body := a.post(a.c, "/setup", "/setup", url.Values{
		"setup_token": {"nope"}, "display_name": {"Sam"}, "email": {"sam@example.com"},
		"password": {"correct horse battery"}, "timezone": {"UTC"},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "setup token doesn&#39;t match") {
		t.Fatalf("wrong token: %d\n%s", resp.StatusCode, body)
	}
	if strings.Contains(body, "correct horse") {
		t.Fatal("the password was echoed back")
	}
	if need, _ := a.svc.NeedsSetup(context.Background()); !need {
		t.Fatal("a wrong token set the app up")
	}
	// A short password is explained on the form.
	resp, body = a.post(a.c, "/setup", "/setup", url.Values{
		"setup_token": {setupToken}, "display_name": {"Sam"}, "email": {"sam@example.com"},
		"password": {"short"}, "timezone": {"UTC"},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "at least 12 characters") {
		t.Fatalf("short password: %d\n%s", resp.StatusCode, body)
	}
	a.setUp()
	// Setup is closed afterwards, and the parent is signed in.
	if resp, _ := a.get(a.c, "/setup"); resp.Header.Get("Location") != "/parent/login" {
		t.Fatalf("setup still open: %d", resp.StatusCode)
	}
	if resp, body := a.get(a.c, "/parent"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "No students yet") {
		t.Fatalf("dashboard: %d\n%s", resp.StatusCode, body)
	}
}

func TestStudentSignIn(t *testing.T) {
	a := newApp(t)
	a.setUp()
	id := a.addStudent("Mika", "2468")
	kid := a.client()

	_, body := a.get(kid, "/")
	if !strings.Contains(body, "Who&#39;s climbing today?") || !strings.Contains(body, `href="/pin/`+id+`"`) {
		t.Fatalf("picker:\n%s", body)
	}
	// The PIN page works without JavaScript: a real field and a submit button.
	_, body = a.get(kid, "/pin/"+id)
	if !strings.Contains(body, `name="pin"`) || !strings.Contains(body, `type="submit"`) {
		t.Fatalf("PIN page:\n%s", body)
	}
	resp, body := a.post(kid, "/pin/"+id, "/pin/"+id, url.Values{"pin": {"0000"}})
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "That PIN isn&#39;t right.") {
		t.Fatalf("wrong PIN: %d\n%s", resp.StatusCode, body)
	}
	resp, _ = a.post(kid, "/pin/"+id, "/pin/"+id, url.Values{"pin": {"2468"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("right PIN: %d", resp.StatusCode)
	}
	if _, body = a.get(kid, "/"); !strings.Contains(body, "Hi Mika. Ready to climb?") {
		t.Fatalf("home:\n%s", body)
	}
	// A student can't open parent pages.
	if resp, _ := a.get(kid, "/parent"); resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/parent/login") {
		t.Fatalf("student on /parent: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = a.post(kid, "/", "/logout", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if _, body = a.get(kid, "/"); !strings.Contains(body, "Who&#39;s climbing today?") {
		t.Fatal("still signed in after logout")
	}
	if resp, _ := a.get(kid, "/pin/not-a-student"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown student: %d", resp.StatusCode)
	}
}

func TestLockAndUnlock(t *testing.T) {
	a := newApp(t)
	a.setUp()
	id := a.addStudent("Theo", "13579")
	ctx := context.Background()
	// Ten wrong PINs, stepping the clock past each backoff.
	now := time.Now()
	a.svc.Now = func() time.Time { return now }
	for range auth.StudentLockAfter {
		_, _ = a.svc.LoginStudent(ctx, id, "0000", "")
		now = now.Add(time.Hour)
	}
	kid := a.client()
	_, body := a.get(kid, "/pin/"+id)
	if !strings.Contains(body, "This is locked, Theo.") || strings.Contains(body, `name="pin"`) {
		t.Fatalf("locked PIN page:\n%s", body)
	}
	// The parent sees it and unlocks.
	if _, body = a.get(a.c, "/parent"); !strings.Contains(body, "Locked") {
		t.Fatalf("dashboard doesn't show the lock:\n%s", body)
	}
	page := "/parent/students/" + id
	resp, _ := a.post(a.c, page, page+"/unlock", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unlock: %d", resp.StatusCode)
	}
	if _, body = a.get(a.c, page); strings.Contains(body, "locked out") || !strings.Contains(body, "Unlocked.") {
		t.Fatalf("after unlock:\n%s", body)
	}
	// A bad new PIN is explained; a good one works for sign-in.
	a.post(a.c, page, page+"/pin", url.Values{"pin": {"12"}})
	if _, body = a.get(a.c, page); !strings.Contains(body, "PIN must be 4 to 6 digits.") {
		t.Fatalf("bad PIN message:\n%s", body)
	}
	a.post(a.c, page, page+"/pin", url.Values{"pin": {"424242"}})
	resp, _ = a.post(kid, "/pin/"+id, "/pin/"+id, url.Values{"pin": {"424242"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in with the new PIN: %d", resp.StatusCode)
	}
}

func TestSecurity(t *testing.T) {
	a := newApp(t)
	a.setUp()
	resp, _ := a.get(a.c, "/parent")
	for h, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "no-store",
	} {
		if got := resp.Header.Get(h); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", h, got, want)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); strings.Contains(csp, "unsafe") {
		t.Errorf("CSP allows something unsafe: %s", csp)
	}
	// A post without the CSRF token is refused.
	r, err := a.c.PostForm(a.ts.URL+"/parent/students", url.Values{"display_name": {"June"}, "pin": {"1234"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("post without CSRF: %d", r.StatusCode)
	}
	// So is one with another browser's token.
	other := a.client()
	_, body := a.get(other, "/parent/login")
	stolen := csrfRE.FindStringSubmatch(body)[1]
	r, err = a.c.PostForm(a.ts.URL+"/parent/students", url.Values{"_csrf": {stolen}, "display_name": {"June"}, "pin": {"1234"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("post with a foreign CSRF token: %d", r.StatusCode)
	}
	if list, _ := a.svc.Students(context.Background()); len(list) != 0 {
		t.Fatalf("a refused post created %d students", len(list))
	}
	// Signed-out visitors can't reach parent pages, and next stays on-site.
	if resp, _ := a.get(other, "/parent/students/new"); resp.Header.Get("Location") != "/parent/login?next=%2Fparent%2Fstudents%2Fnew" {
		t.Fatalf("guard: %s", resp.Header.Get("Location"))
	}
	resp, _ = a.post(other, "/parent/login", "/parent/login", url.Values{
		"email": {"sam@example.com"}, "password": {"correct horse battery"}, "next": {"//evil.example/"},
	})
	if resp.Header.Get("Location") != "/parent" {
		t.Fatalf("open redirect: %s", resp.Header.Get("Location"))
	}
	if resp, body := a.get(a.c, "/nope"); resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "Nothing here") {
		t.Fatalf("404: %d", resp.StatusCode)
	}
}

// TestNoInlineStyle keeps the templates inside the CSP: no style attributes,
// no inline scripts, no event handlers, nothing loaded from another origin.
func TestNoInlineStyle(t *testing.T) {
	bad := regexp.MustCompile(`(?i)\sstyle\s*=|<style|<script(?:\s[^>]*)?>[^<]|\son[a-z]+\s*=|https?://`)
	files, _ := fs.Glob(templateFS, "templates/*.html")
	if len(files) < 5 {
		t.Fatalf("found %d templates", len(files))
	}
	for _, f := range files {
		b, _ := fs.ReadFile(templateFS, f)
		if m := bad.Find(b); m != nil {
			t.Errorf("%s contains %q", f, m)
		}
	}
	css, _ := fs.ReadFile(staticFS, "static/app.css")
	if regexp.MustCompile(`https?://`).Match(css) {
		t.Error("app.css loads something from another origin")
	}
}
