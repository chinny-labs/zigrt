package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestParentPages walks the parent side: dashboard, a student's page,
// settings, marking blocks, assigning a drill, the bell, the household page
// and its export, and deleting a student.
func TestParentPages(t *testing.T) {
	a := newApp(t)
	a.setUp()
	kid, kidID := a.signInStudent("Mika", "2468")
	theoID := a.addStudent("Theo", "13579")
	sp := "/parent/students/" + kidID

	_, body := a.get(a.c, "/parent")
	for _, want := range []string{"This week", "Mika", "Theo", "0 XP of", "0 new blocks placed", `href="/parent/notifications"`, `href="/parent/household"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard lacks %q:\n%s", want, body)
		}
	}
	_, body = a.get(a.c, sp)
	for _, want := range []string{`<svg class="chart"`, "XP each day", "What Mika did", "Assign a drill", sp + "/tower", sp + "/settings"} {
		if !strings.Contains(body, want) {
			t.Fatalf("student page lacks %q:\n%s", want, body)
		}
	}

	// Settings: rest days, goal, course.
	_, body = a.get(a.c, sp+"/settings")
	if !strings.Contains(body, `name="rest" value="6"`) || !strings.Contains(body, `<option value="k5" selected`) && !strings.Contains(body, `<option value="k5"`) {
		t.Fatalf("settings form:\n%s", body)
	}
	resp, _ := a.post(a.c, sp+"/settings", sp+"/settings", url.Values{"display_name": {"Mika"}, "avatar": {"sun"},
		"course": {"grade-5"}, "daily_xp_goal": {"25"}, "rest": {"0", "6"}, "fluency": {"1"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save settings: %d", resp.StatusCode)
	}
	if sv, _ := a.svc.Student(t.Context(), kidID); sv.Settings.DailyXPGoal != 25 || sv.Settings.RestDays != "06" || sv.Settings.CourseID != "grade-5" {
		t.Fatalf("saved %+v", sv.Settings)
	}
	resp, body = a.post(a.c, sp+"/settings", sp+"/settings", url.Values{"display_name": {"Mika"}, "daily_xp_goal": {"500"}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "from 1 to 120") {
		t.Fatalf("a bad goal: %d", resp.StatusCode)
	}

	// Marking a block placed.
	_, body = a.get(a.c, sp+"/blocks")
	if !strings.Contains(body, sp+"/blocks/long-division-2d") {
		t.Fatalf("blocks page:\n%s", body)
	}
	a.post(a.c, sp+"/blocks", sp+"/blocks/long-division-2d", url.Values{"placed": {"1"}})
	if _, body = a.get(a.c, sp+"/blocks"); !strings.Contains(body, "Take away") {
		t.Fatal("the block was not placed")
	}

	// Assigning a drill: it shows on Mika's home.
	_, body = a.get(a.c, "/drills?for="+kidID)
	if !strings.Contains(body, "Assign to Mika") || !strings.Contains(body, `name="for" value="`+kidID+`"`) {
		t.Fatalf("assign builder:\n%s", body)
	}
	resp, _ = a.post(a.c, "/drills?for="+kidID, "/drills", url.Values{"for": {kidID}, "kind": {"drill"}, "op_add": {"1"},
		"level_add": {"sums10"}, "fmt_across": {"1"}, "count": {"10"}, "title": {"Friday sums"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != sp {
		t.Fatalf("assign: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, body = a.get(kid, "/"); !strings.Contains(body, "From Sam: Friday sums") {
		t.Fatalf("Mika's home lacks the assignment:\n%s", body)
	}

	// The bell: Theo locks himself out.
	theo := a.client()
	now := time.Now()
	a.svc.Now = func() time.Time { return now }
	for range 10 { // an hour apart, so the throttle never makes him wait
		now = now.Add(time.Hour)
		a.post(theo, "/pin/"+theoID, "/pin/"+theoID, url.Values{"pin": {"0000"}})
	}
	if _, body = a.get(a.c, "/parent"); !strings.Contains(body, `<span class="badge" aria-hidden="true">1</span>`) || !strings.Contains(body, "Locked out") {
		t.Fatalf("no bell badge:\n%s", body)
	}
	_, body = a.get(a.c, "/parent/notifications")
	if !strings.Contains(body, "Theo is locked out") || !strings.Contains(body, "/parent/students/"+theoID+"/unlock") || !strings.Contains(body, "Mark all read") {
		t.Fatalf("notifications:\n%s", body)
	}
	a.post(a.c, "/parent/notifications", "/parent/notifications/read", url.Values{})
	if _, body = a.get(a.c, "/parent"); strings.Contains(body, `class="badge"`) {
		t.Fatal("still unread after marking all read")
	}

	// Household: the pack, an export download, a bad upload.
	_, body = a.get(a.c, "/parent/household")
	for _, want := range []string{"Sam (you)", "Hardest K-5 topics", "came with this version", "Export everything", "Add another parent"} {
		if !strings.Contains(body, want) {
			t.Fatalf("household lacks %q:\n%s", want, body)
		}
	}
	resp, body = a.get(a.c, "/parent/export")
	if resp.Header.Get("Content-Type") != "application/x-ndjson" || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") ||
		!strings.HasPrefix(body, `{"zigrt_export":1`) || !strings.Contains(body, `"display_name":"Mika"`) {
		t.Fatalf("export: %s %s", resp.Header, body[:min(len(body), 200)])
	}
	resp, body = a.upload(a.c, "/parent/household", "/parent/pack", "pack", "junk.zip", []byte("not a zip"))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("bad upload: %d %s", resp.StatusCode, body)
	}
	if _, body = a.get(a.c, "/parent/household"); !strings.Contains(body, "isn&#39;t a content pack") {
		t.Fatalf("no message for a bad pack:\n%s", body)
	}

	// Students can't open parent pages.
	if resp, _ := a.get(kid, sp+"/settings"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("a student on settings: %d", resp.StatusCode)
	}

	// Deleting Theo needs his name.
	tp := "/parent/students/" + theoID
	a.post(a.c, tp+"/delete", tp+"/delete", url.Values{"confirm": {"Mika"}})
	if _, err := a.svc.Student(t.Context(), theoID); err != nil {
		t.Fatal("deleted with the wrong name")
	}
	resp, _ = a.post(a.c, tp+"/delete", tp+"/delete", url.Values{"confirm": {"Theo"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/parent" {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if _, err := a.svc.Student(t.Context(), theoID); err == nil {
		t.Fatal("Theo is still there")
	}
}

// upload posts a multipart form with one file, after loading formPage for
// its CSRF token.
func (a *app) upload(c *http.Client, formPage, path, field, name string, data []byte) (*http.Response, string) {
	a.t.Helper()
	_, page := a.get(c, formPage)
	m := csrfRE.FindStringSubmatch(page)
	if m == nil {
		a.t.Fatalf("no CSRF token on %s", formPage)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("_csrf", m[1])
	fw, _ := mw.CreateFormFile(field, name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	resp, err := c.Post(a.ts.URL+path, mw.FormDataContentType(), &buf)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}
