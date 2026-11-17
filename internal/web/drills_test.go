package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/store"
)

// signInStudent adds a student and returns a client signed in as them.
func (a *app) signInStudent(name, pin string) (*http.Client, string) {
	a.t.Helper()
	id := a.addStudent(name, pin)
	c := a.client()
	resp, _ := a.post(c, "/pin/"+id, "/pin/"+id, url.Values{"pin": {pin}})
	if resp.StatusCode != http.StatusSeeOther {
		a.t.Fatalf("%s sign-in: %d", name, resp.StatusCode)
	}
	return c, id
}

var drillIDRE = regexp.MustCompile(`^/(?:drills|worksheets)/([^/]+)$`)

func (a *app) build(c *http.Client, form url.Values) (*http.Response, string) {
	a.t.Helper()
	return a.post(c, "/drills", "/drills", form)
}

func TestDrillFlow(t *testing.T) {
	a := newApp(t)
	a.setUp()
	kid, kidID := a.signInStudent("Mika", "2468")

	// The builder starts from the defaults: addition on, across, 20.
	_, body := a.get(kid, "/drills")
	for _, want := range []string{`name="op_add" value="1" checked`, `name="fmt_across" value="1" checked`, `name="count" value="20" checked`, "Start the drill"} {
		if !strings.Contains(body, want) {
			t.Fatalf("builder lacks %q", want)
		}
	}
	// Nothing chosen is explained, and the form comes back.
	resp, body := a.build(kid, url.Values{"kind": {"drill"}, "count": {"10"}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Choose at least one kind of problem") {
		t.Fatalf("empty build: %d", resp.StatusCode)
	}
	resp, _ = a.build(kid, url.Values{"kind": {"drill"}, "op_mul": {"1"}, "level_mul": {"facts5"}, "fmt_across": {"1"}, "count": {"10"}})
	m := drillIDRE.FindStringSubmatch(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || m == nil || !strings.HasPrefix(resp.Header.Get("Location"), "/drills/") {
		t.Fatalf("build: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	id := m[1]
	_, body = a.get(kid, "/drills/"+id)
	if strings.Count(body, "data-problem ") != 10 || !strings.Contains(body, `src="/static/drill.js"`) || !strings.Contains(body, "<noscript>") {
		t.Fatalf("drill page:\n%s", body)
	}
	d, err := a.svc.Drill(context.Background(), store.User{ID: kidID, Role: store.RoleStudent}, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range d.Problems {
		if p.Op != drill.Mul || p.X > 5 || p.Y > 5 {
			t.Fatalf("problem outside the chosen level: %+v", p)
		}
	}

	// The browser's marking is ignored: claim everything was right, send one wrong.
	answers := make([]map[string]any, 10)
	for i, p := range d.Problems {
		answers[i] = map[string]any{"main": strconv.Itoa(p.Answer()), "ms": 1200, "correct": true}
	}
	answers[3]["main"] = strconv.Itoa(d.Problems[3].Answer() + 1)
	aj, _ := json.Marshal(answers)
	resp, _ = a.post(kid, "/drills/"+id, "/drills/"+id+"/finish", url.Values{"answers": {string(aj)}, "duration_ms": {"61000"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/drills/"+id+"/results" {
		t.Fatalf("finish: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body = a.get(kid, "/drills/"+id+"/results")
	for _, want := range []string{"9 of 10 right.", "1:01", "1 to look at again", d.Problems[3].Equation(), "Drill just these"} {
		if !strings.Contains(body, want) {
			t.Fatalf("results lack %q:\n%s", want, body)
		}
	}
	// A finished drill sends you to its results.
	if resp, _ := a.get(kid, "/drills/"+id); resp.Header.Get("Location") != "/drills/"+id+"/results" {
		t.Fatalf("finished drill: %s", resp.Header.Get("Location"))
	}
	resp, _ = a.post(kid, "/drills/"+id+"/results", "/drills/"+id+"/retry", url.Values{})
	m = drillIDRE.FindStringSubmatch(resp.Header.Get("Location"))
	if m == nil || m[1] == id {
		t.Fatalf("retry: %s", resp.Header.Get("Location"))
	}
	if _, body = a.get(kid, "/drills/"+m[1]); strings.Count(body, "data-problem ") != 1 {
		t.Fatal("the retry doesn't hold exactly the one miss")
	}
	// The builder lists both, and remembers ×.
	_, body = a.get(kid, "/drills")
	if !strings.Contains(body, "9 of 10") || !strings.Contains(body, "Not finished") || !strings.Contains(body, `name="op_mul" value="1" checked`) {
		t.Fatalf("history:\n%s", body)
	}

	// Garbled answers are refused without saving anything.
	resp, _ = a.post(kid, "/drills/"+m[1], "/drills/"+m[1]+"/finish", url.Values{"answers": {"{not json"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbled answers: %d", resp.StatusCode)
	}
	// Stopping straight away (no answers) still finishes the drill.
	resp, _ = a.post(kid, "/drills/"+m[1], "/drills/"+m[1]+"/finish", url.Values{"answers": {""}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("stop: %d", resp.StatusCode)
	}
	if _, body = a.get(kid, "/drills/"+m[1]+"/results"); !strings.Contains(body, "0 of 1 right.") || !strings.Contains(body, "Not answered") {
		t.Fatalf("stopped results:\n%s", body)
	}

	// Another student can't see it; the parent can see the results but not act.
	other, _ := a.signInStudent("Theo", "13579")
	if resp, _ := a.get(other, "/drills/"+id+"/results"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("another student saw the results: %d", resp.StatusCode)
	}
	resp, body = a.get(a.c, "/drills/"+id+"/results")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "9 of 10 right.") || strings.Contains(body, "Drill just these") {
		t.Fatalf("parent results: %d", resp.StatusCode)
	}
	if resp, _ := a.get(other, "/drills/no-such"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown drill: %d", resp.StatusCode)
	}
	// Signed-out visitors are sent to the start.
	if resp, _ := a.get(a.client(), "/drills"); resp.Header.Get("Location") != "/" {
		t.Fatalf("signed out: %s", resp.Header.Get("Location"))
	}
}

func TestWorksheet(t *testing.T) {
	a := newApp(t)
	a.setUp()
	// Parents get the worksheet builder only.
	_, body := a.get(a.c, "/drills")
	if strings.Contains(body, "Start the drill") || !strings.Contains(body, "Make a worksheet") {
		t.Fatal("parent builder offers an on-screen drill")
	}
	resp, _ := a.build(a.c, url.Values{"kind": {"drill"}, "op_add": {"1"}, "fmt_across": {"1"}, "count": {"10"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("parent made a drill: %d", resp.StatusCode)
	}
	resp, _ = a.build(a.c, url.Values{"kind": {"worksheet"}, "title": {"Friday <sheet>"}, "op_div": {"1"}, "level_div": {"d3d2"},
		"div_rem": {"1"}, "fmt_across": {"1"}, "fmt_stacked": {"1"}, "count": {"30"}})
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/worksheets/") {
		t.Fatalf("worksheet: %d %s", resp.StatusCode, loc)
	}
	_, body = a.get(a.c, loc)
	for _, want := range []string{"Friday &lt;sheet&gt;", "sheet-name", "/ 30", "30.", `class="pr pr-ld"`, "Division (3-digit ÷ 2-digit, remainders)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("worksheet lacks %q", want)
		}
	}
	if strings.Contains(body, "answer-key") || strings.Contains(body, "<script src=\"/static/drill.js\"") {
		t.Fatal("answer key or drill script on a plain sheet")
	}
	// Options come from the query string, so they work without JavaScript.
	_, body = a.get(a.c, loc+"?opts=1&key=1&large=1")
	if !strings.Contains(body, "answer key") || !strings.Contains(body, "sheet-large") || strings.Contains(body, `class="sheet-name"`) {
		t.Fatal("options not applied")
	}
	// A new version is a new sheet with the same settings.
	resp, _ = a.post(a.c, loc, loc+"/again", url.Values{})
	if l := resp.Header.Get("Location"); !strings.HasPrefix(l, "/worksheets/") || l == loc {
		t.Fatalf("new version: %s", l)
	}
	// A worksheet can't be "finished" as a drill.
	r, _ := a.post(a.c, loc, "/drills"+strings.TrimPrefix(loc, "/worksheets")+"/finish", url.Values{"answers": {"[]"}})
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("finished a worksheet: %d", r.StatusCode)
	}
}
