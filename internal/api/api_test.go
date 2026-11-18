package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/api"
	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/sim"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
)

// env is a demo household (two students, two weeks of history) behind the
// API and MCP, with a read token and a manage token.
type env struct {
	t      *testing.T
	svc    *service.Service
	api    *api.API
	srv    *httptest.Server
	parent store.User
	secret map[string]string // "read", "manage"
	clock  *clock
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// end is when the demo's last day finishes: a Wednesday evening.
var end = time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "zigrt.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, nil)
	p, err := pack.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPack(p)
	login, err := sim.Demo(ctx, svc, "UTC", "correct horse battery", 14, end, 7)
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: end}
	svc.Now = clk.Now
	parent, err := st.GetUserByEmail(ctx, login.Email)
	if err != nil {
		t.Fatal(err)
	}
	a := api.New(svc, api.Options{BaseURL: "https://zigrt.example"})
	mux := http.NewServeMux()
	mux.Handle(api.Prefix+"/", a)
	mux.Handle(api.MCPPath, a.MCPHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := &env{t: t, svc: svc, api: a, srv: srv, parent: parent, secret: map[string]string{}, clock: clk}
	for name, scopes := range map[string][]string{"read": nil, "manage": {service.ScopeManage}} {
		secret, _, err := svc.CreateAPIToken(ctx, parent, service.TokenInput{Label: name, Scopes: scopes})
		if err != nil {
			t.Fatal(err)
		}
		e.secret[name] = secret
	}
	return e
}

// call sends a request with the named token ("" for none, or a literal
// secret) and decodes the JSON reply into out.
func (e *env) call(token, method, path string, body, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+api.Prefix+path, rd)
	if s, ok := e.secret[token]; ok {
		req.Header.Set("Authorization", "Bearer "+s)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: %v\n%s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

type apiError struct {
	Error struct{ Code, Message, Field string }
}

func (e *env) students() []api.Student {
	e.t.Helper()
	var out struct{ Students []api.Student }
	if c := e.call("read", "GET", "/students", nil, &out); c != 200 {
		e.t.Fatalf("list students: %d", c)
	}
	return out.Students
}

func TestReads(t *testing.T) {
	e := newEnv(t)
	students := e.students()
	if len(students) != 2 {
		t.Fatalf("got %d students, want the demo's 2", len(students))
	}
	for _, s := range students {
		if s.Placed == 0 || s.Week.XP == 0 || len(s.Week.Days) != 7 || s.LastActive == nil {
			t.Errorf("%s: an empty summary after two weeks: %+v", s.Name, s)
		}
		if s.Today.Streak == 0 && s.Today.GoalMet {
			t.Errorf("%s: goal met but no streak", s.Name)
		}
		if s.Week.Days[0].Date != "2026-11-16" || !s.Week.Days[6].Future {
			t.Errorf("%s: the week should run Monday 16 to a future Sunday: %+v", s.Name, s.Week.Days)
		}

		var one api.Student
		if c := e.call("read", "GET", "/students/"+s.ID, nil, &one); c != 200 || one.ID != s.ID || one.Week.XP != s.Week.XP {
			t.Errorf("get %s: %d %+v", s.Name, c, one)
		}

		var topics struct{ Topics []api.Topic }
		e.call("read", "GET", "/students/"+s.ID+"/topics", nil, &topics)
		placed := 0
		for _, tp := range topics.Topics {
			if tp.State == service.BlockPlaced || tp.State == service.BlockMend {
				placed++
				if tp.FirstPlaced == nil || tp.NextReview == "" {
					t.Errorf("%s: placed %s without a date or next review", s.Name, tp.ID)
				}
			}
		}
		if want := len(e.svc.Pack().Topics); len(topics.Topics) != want || placed != s.Placed {
			t.Errorf("%s: %d topics with %d placed, want %d with %d", s.Name, len(topics.Topics), placed, want, s.Placed)
		}

		var act struct{ Activity []api.Activity }
		e.call("read", "GET", "/students/"+s.ID+"/activity", nil, &act)
		if len(act.Activity) == 0 || act.Activity[0].At.Before(act.Activity[len(act.Activity)-1].At) {
			t.Errorf("%s: activity empty or not newest first", s.Name)
		}

		var st struct {
			Days      int
			Struggles []api.Struggle
		}
		if c := e.call("read", "GET", "/students/"+s.ID+"/struggles?days=14", nil, &st); c != 200 || st.Days != 14 {
			t.Errorf("%s struggles: %d %+v", s.Name, c, st)
		}
		for _, x := range st.Struggles {
			if x.Stops+x.FailedReviews < 2 || x.Topic.Title == "" {
				t.Errorf("%s: %+v isn't a repeated failure", s.Name, x)
			}
		}
	}

	var sys api.System
	if e.call("read", "GET", "/system", nil, &sys); sys.PackID != "zigrt-k5" || sys.Topics != len(e.svc.Pack().Topics) || !sys.Healthy {
		t.Errorf("system: %+v", sys)
	}

	var bad apiError
	if c := e.call("read", "GET", "/students/nobody", nil, &bad); c != 404 || bad.Error.Code != "not_found" {
		t.Errorf("unknown student: %d %+v", c, bad)
	}
	if c := e.call("read", "GET", "/students/"+e.parent.ID, nil, &bad); c != 404 {
		t.Errorf("a parent's ID as a student: %d", c)
	}
	if c := e.call("read", "GET", "/students/"+students[0].ID+"/struggles?days=0", nil, &bad); c != 422 || bad.Error.Field != "days" {
		t.Errorf("days=0: %d %+v", c, bad)
	}
}

func TestAuth(t *testing.T) {
	e := newEnv(t)
	var bad apiError
	for _, tc := range []struct {
		token, code string
		status      int
	}{
		{"", "unauthorized", 401},
		{"zg_" + strings.Repeat("a", 43), "unauthorized", 401},
		{"not-a-zigrt-token", "unauthorized", 401},
	} {
		if c := e.call(tc.token, "GET", "/students", nil, &bad); c != tc.status || bad.Error.Code != tc.code {
			t.Errorf("token %q: %d %+v", tc.token, c, bad)
		}
	}
	// The spec needs no token.
	if c := e.call("", "GET", "/openapi.json", nil, nil); c != 200 {
		t.Errorf("openapi.json: %d", c)
	}
	// A read token can't change anything.
	id := e.students()[0].ID
	if c := e.call("read", "PATCH", "/students/"+id+"/settings", map[string]any{"daily_xp_goal": 20}, &bad); c != 403 || bad.Error.Code != "insufficient_scope" {
		t.Errorf("read token patching: %d %+v", c, bad)
	}
	// A revoked token stops at once.
	toks, _ := e.svc.APITokens(context.Background(), e.parent)
	for _, tk := range toks {
		if tk.Label == "read" {
			if err := e.svc.RevokeAPIToken(context.Background(), e.parent, tk.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if c := e.call("read", "GET", "/students", nil, &bad); c != 401 {
		t.Errorf("revoked token: %d", c)
	}
	// Ten bad tokens from one address block it for a while, good token or not.
	for range 10 {
		e.call("zg_wrong", "GET", "/students", nil, nil)
	}
	if c := e.call("manage", "GET", "/students", nil, &bad); c != 429 || bad.Error.Code != "rate_limited" {
		t.Errorf("after ten bad tokens: %d %+v", c, bad)
	}
	e.clock.Advance(2 * time.Minute)
	if c := e.call("manage", "GET", "/students", nil, nil); c != 200 {
		t.Errorf("after the block ends: %d", c)
	}
	// Last used is recorded.
	toks, _ = e.svc.APITokens(context.Background(), e.parent)
	if len(toks) != 1 || toks[0].LastUsedAt == nil {
		t.Errorf("tokens after use: %+v", toks)
	}
}

func TestManage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s := e.students()[0]

	var got api.Student
	if c := e.call("manage", "PATCH", "/students/"+s.ID+"/settings", map[string]any{"daily_xp_goal": 25, "rest_days": []int{6, 0}}, &got); c != 200 {
		t.Fatalf("patch: %d", c)
	}
	if got.DailyXPGoal != 25 || fmt.Sprint(got.RestDays) != "[0 6]" || got.Fluency != s.Fluency || got.Name != s.Name || got.CourseID != s.CourseID {
		t.Errorf("after patch: %+v", got)
	}
	var bad apiError
	for _, tc := range []struct {
		body  map[string]any
		code  int
		field string
	}{
		{map[string]any{"daily_xp_goal": 500}, 422, "daily_xp_goal"},
		{map[string]any{"rest_days": []int{0, 1, 2, 3, 4, 5, 6}}, 422, "rest_days"},
		{map[string]any{"rest_days": []int{9}}, 422, "rest_days"},
		{map[string]any{"course_id": "nope"}, 422, "course"},
		{map[string]any{"goal": 10}, 400, ""},
	} {
		bad = apiError{}
		if c := e.call("manage", "PATCH", "/students/"+s.ID+"/settings", tc.body, &bad); c != tc.code || bad.Error.Field != tc.field {
			t.Errorf("%v: %d %+v", tc.body, c, bad)
		}
	}
	// The change is audited as the parent, naming the token.
	log, err := e.svc.Store.ListAudit(ctx, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range log {
		if a.Action == "student.settings" && a.ActorID == e.parent.ID && strings.Contains(string(a.Detail), `"via_token"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("no audit row naming the token: %+v", log)
	}

	var ws api.Worksheet
	req := map[string]any{"title": "Friday", "settings": map[string]any{
		"ops": map[string]any{"add": map[string]any{"on": true, "level": "sum20"}}, "formats": []string{"across"}, "count": 20}}
	if c := e.call("manage", "POST", "/worksheets", req, &ws); c != 201 {
		t.Fatalf("worksheet: %d", c)
	}
	if len(ws.Problems) != 20 || ws.Title != "Friday" || ws.URL != "https://zigrt.example/worksheets/"+ws.ID || ws.About == "" {
		t.Errorf("worksheet: %+v", ws)
	}
	for _, p := range ws.Problems {
		if p.Op != "add" || p.X+p.Y != p.Z || p.Z > 20 {
			t.Errorf("not a sum to 20: %+v", p)
		}
	}
	bad = apiError{}
	if c := e.call("manage", "POST", "/worksheets", map[string]any{"settings": map[string]any{"ops": map[string]any{}, "formats": []string{}, "count": 20}}, &bad); c != 422 {
		t.Errorf("an empty worksheet: %d %+v", c, bad)
	}
	if c := e.call("read", "POST", "/worksheets", req, &bad); c != 403 {
		t.Errorf("read token making a worksheet: %d", c)
	}
}

func TestTokens(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, in := range []service.TokenInput{{Label: ""}, {Label: "x", Scopes: []string{"money"}}} {
		if _, _, err := e.svc.CreateAPIToken(ctx, e.parent, in); err == nil {
			t.Errorf("%+v: made a token", in)
		}
	}
	students, _ := e.svc.Students(ctx)
	if _, _, err := e.svc.CreateAPIToken(ctx, students[0].User, service.TokenInput{Label: "kid"}); err == nil {
		t.Error("a student made a token")
	}
	secret, tk, err := e.svc.CreateAPIToken(ctx, e.parent, service.TokenInput{Label: "ha", Scopes: []string{"manage", "read", "manage"}})
	if err != nil || !strings.HasPrefix(secret, service.TokenPrefix) || fmt.Sprint(tk.Scopes) != "[read manage]" {
		t.Fatalf("%q %+v %v", secret, tk, err)
	}
	stored, _ := e.svc.Store.GetAPIToken(ctx, tk.ID)
	if strings.Contains(stored.TokenHash, secret[3:]) || stored.TokenHash == secret {
		t.Error("the secret is stored")
	}
	// Up to the cap, then refused.
	for i := len(e.secret) + 1; i < service.MaxTokensPerParent; i++ {
		if _, _, err := e.svc.CreateAPIToken(ctx, e.parent, service.TokenInput{Label: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := e.svc.CreateAPIToken(ctx, e.parent, service.TokenInput{Label: "one more"}); err == nil {
		t.Error("made more than the cap")
	}
	// Another parent can't revoke someone else's.
	other, err := e.svc.CreateParent(ctx, e.parent, service.ParentInput{DisplayName: "Alex", Email: "alex@example.invalid", Password: "another long password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RevokeAPIToken(ctx, other, tk.ID); err == nil {
		t.Error("another parent revoked the token")
	}
}
