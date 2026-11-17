package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/chinny-labs/zigrt/internal/api"
	"github.com/chinny-labs/zigrt/internal/store"
)

var secretRE = regexp.MustCompile(`id="secret" class="token-secret" value="(zg_[^"]+)"`)

// TestTokenPages makes a token on the household page, uses it on the API
// mounted behind the real server (no cookie, no CSRF), and revokes it.
func TestTokenPages(t *testing.T) {
	a := newApp(t)
	a.setUp()
	a.addStudent("Theo", "13579")
	ap := api.New(a.svc, api.Options{ClientIP: a.srv.ClientIP})
	a.srv.MountAPI(ap, ap.MCPHandler())

	_, body := a.get(a.c, "/parent/household")
	if !strings.Contains(body, "API tokens") || !strings.Contains(body, "/mcp</code>") {
		t.Fatalf("household page lacks tokens:\n%s", body)
	}
	resp, body := a.post(a.c, "/parent/household", "/parent/tokens", url.Values{"label": {"Home Assistant"}})
	m := secretRE.FindStringSubmatch(body)
	if resp.StatusCode != http.StatusOK || m == nil || resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(body, "reads progress only") {
		t.Fatalf("token page: %d\n%s", resp.StatusCode, body)
	}
	secret := m[1]

	// The API through the web server: a bearer token, no cookie or CSRF.
	apiGet := func(path, tok string) int {
		req, _ := http.NewRequest("GET", a.ts.URL+path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if c := apiGet("/api/v1/students", secret); c != 200 {
		t.Fatalf("API with the new token: %d", c)
	}
	if c := apiGet("/api/v1/students", ""); c != 401 {
		t.Fatalf("API without a token: %d", c)
	}
	req, _ := http.NewRequest("PATCH", a.ts.URL+"/api/v1/students/x/settings", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
		t.Fatalf("a read token patching: %v %d", err, r.StatusCode)
	}

	// Listed with no secret, then revoked.
	_, body = a.get(a.c, "/parent/household")
	if !strings.Contains(body, "Home Assistant") || strings.Contains(body, secret) || !strings.Contains(body, "used ") {
		t.Fatalf("token list:\n%s", body)
	}
	toks, _ := a.svc.APITokens(t.Context(), a.parent())
	resp, _ = a.post(a.c, "/parent/household", "/parent/tokens/"+toks[0].ID+"/revoke", url.Values{})
	if resp.StatusCode != http.StatusSeeOther || apiGet("/api/v1/students", secret) != 401 {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	resp, body = a.post(a.c, "/parent/household", "/parent/tokens", url.Values{"label": {""}})
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("a token with no name: %d\n%s", resp.StatusCode, body)
	}
	// A student can't reach the token pages.
	kid, _ := a.signInStudent("Mika", "2468")
	if resp, _ := a.get(kid, "/parent/household"); resp.StatusCode == http.StatusOK {
		t.Fatal("a student saw the household page")
	}
}

// parent is the household's first parent.
func (a *app) parent() store.User {
	a.t.Helper()
	ps, err := a.svc.Store.ListUsers(a.t.Context(), store.RoleParent)
	if err != nil || len(ps) == 0 {
		a.t.Fatal("no parent", err)
	}
	return ps[0]
}
