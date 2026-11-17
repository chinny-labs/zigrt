package web

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// The parent side (P2 to P6). Every page is a plain form or link.

func studentPath(id string, rest ...string) string {
	return "/parent/students/" + url.PathEscape(id) + strings.Join(rest, "")
}

// --- dashboard (P2) ---------------------------------------------------------

type dashData struct {
	Cards []service.StudentCard
	Week  string // "Mon 9 to Sun 15 Nov"
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	cards, err := s.svc.Dashboard(r.Context(), *c.User)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := dashData{Cards: cards}
	if len(cards) > 0 {
		d.Week = weekLabel(cards[0].Week)
	}
	v := s.newView(w, r, "This week", d)
	v.Shell = "parent"
	s.render(w, r, http.StatusOK, "dashboard", v)
}

func weekLabel(week []service.DayBar) string {
	if len(week) < 7 {
		return ""
	}
	a, _ := time.Parse("2006-01-02", week[0].Date)
	b, _ := time.Parse("2006-01-02", week[6].Date)
	if a.Month() == b.Month() {
		return a.Format("Mon 2") + " to " + b.Format("Mon 2 Jan")
	}
	return a.Format("Mon 2 Jan") + " to " + b.Format("Mon 2 Jan")
}

// --- one student (P3) -------------------------------------------------------

type studentData struct {
	R      service.StudentReport
	Chart  template.HTML
	Locked bool
}

func (s *Server) studentPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	rep, err := s.svc.StudentReport(r.Context(), *c.User, r.PathValue("id"))
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	d := studentData{R: rep, Chart: weekChart(rep.Week), Locked: rep.Student.Locked}
	v := s.newView(w, r, rep.Student.DisplayName, d)
	v.Shell, v.Back, v.BackLabel = "parent", "/parent", "Everyone"
	s.render(w, r, http.StatusOK, "student", v)
}

// weekChart draws a week's XP as bars against the goal (P3), as SVG with
// classes only, so the page CSS colours it.
func weekChart(week []service.DayBar) template.HTML {
	if len(week) == 0 {
		return ""
	}
	top := 1
	for _, d := range week {
		top = max(top, d.XP, d.Goal)
	}
	const w, h, base, bw = 336.0, 150.0, 120.0, 36.0
	y := func(v int) float64 { return base - float64(v)/float64(top)*96 }
	var b strings.Builder
	label := "XP each day this week"
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, w, h, label)
	for i, d := range week {
		x := 6 + float64(i)*48
		class := "bar"
		switch {
		case d.Rest:
			class += " bar-rest"
		case d.Met:
			class += " bar-met"
		}
		if d.XP > 0 {
			fmt.Fprintf(&b, `<rect class="%s" x="%.0f" y="%.1f" width="%.0f" height="%.1f" rx="8"/>`, class, x, y(d.XP), bw, base-y(d.XP))
		} else {
			fmt.Fprintf(&b, `<rect class="bar bar-empty" x="%.0f" y="%.0f" width="%.0f" height="6" rx="3"/>`, x, base-6, bw)
		}
		if !d.Future {
			fmt.Fprintf(&b, `<text class="chart-num" x="%.0f" y="%.1f" text-anchor="middle">%d</text>`, x+bw/2, y(d.XP)-6, d.XP)
		}
		fmt.Fprintf(&b, `<text class="chart-day" x="%.0f" y="%.0f" text-anchor="middle">%s</text>`, x+bw/2, base+22, d.Weekday.String()[:1])
	}
	goal := week[len(week)-1].Goal
	fmt.Fprintf(&b, `<line class="chart-goal" x1="0" y1="%.1f" x2="%.0f" y2="%.1f"/>`, y(goal), w, y(goal))
	b.WriteString(`</svg>`)
	return template.HTML(b.String()) //nolint:gosec // numbers and fixed markup only
}

// --- a student's settings (P4) ----------------------------------------------

type settingsData struct {
	S        service.StudentView
	Courses  []content.Course
	Days     []dayToggle
	Goal     int
	Fluency  bool
	CourseID string
}

type dayToggle struct {
	Digit, Letter, Name string
	On                  bool
}

func (s *Server) studentSettingsPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	st, err := s.svc.Student(r.Context(), r.PathValue("id"))
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	s.renderSettings(w, r, http.StatusOK, st, "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, status int, st service.StudentView, msg string) {
	d := settingsData{S: st, Goal: st.Settings.DailyXPGoal, Fluency: st.Settings.FluencyEnabled, CourseID: st.Settings.CourseID}
	if p := s.svc.Pack(); p != nil {
		d.Courses = p.Manifest.Courses
	}
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday} {
		digit := fmt.Sprint(int(wd))
		d.Days = append(d.Days, dayToggle{Digit: digit, Letter: wd.String()[:1], Name: wd.String(), On: strings.Contains(st.Settings.RestDays, digit)})
	}
	v := s.newView(w, r, st.DisplayName+"'s settings", d)
	v.Shell, v.Back, v.BackLabel, v.Err = "parent", studentPath(st.ID), st.DisplayName, msg
	s.render(w, r, status, "student_settings", v)
}

func (s *Server) studentSettingsSave(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	f := r.PostForm
	goal := 0
	_, _ = fmt.Sscan(f.Get("daily_xp_goal"), &goal)
	err := s.svc.UpdateStudent(r.Context(), *c.User, id, service.StudentUpdate{
		DisplayName: f.Get("display_name"), Avatar: f.Get("avatar"), CourseID: f.Get("course"),
		DailyXPGoal: goal, RestDays: strings.Join(f["rest"], ""), Fluency: f.Get("fluency") != "",
	})
	if err != nil {
		if msg := userMessage(err); msg != "" {
			st, serr := s.svc.Student(r.Context(), id)
			if serr != nil {
				s.parentFail(w, r, serr)
				return
			}
			s.renderSettings(w, r, http.StatusUnprocessableEntity, st, msg)
			return
		}
		s.parentFail(w, r, err)
		return
	}
	s.flash(w, "ok", "Saved.")
	http.Redirect(w, r, studentPath(id), http.StatusSeeOther) //nolint:gosec // a same-site path built from one escaped segment
}

func (s *Server) studentPlacement(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	if err := s.svc.RerunPlacement(r.Context(), *c.User, id); err != nil {
		s.parentFail(w, r, err)
		return
	}
	s.flash(w, "ok", "The placement check is at the top of their list next time they sign in.")
	http.Redirect(w, r, studentPath(id), http.StatusSeeOther) //nolint:gosec // a same-site path built from one escaped segment
}

// --- marking blocks placed or not -------------------------------------------

type blocksData struct {
	S     service.StudentView
	Tiers []service.TowerTier
}

func (s *Server) studentBlocksPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	st, err := s.svc.Student(r.Context(), id)
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	tv, err := s.svc.StudentTower(r.Context(), *c.User, id)
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	v := s.newView(w, r, "Mark blocks", blocksData{S: st, Tiers: tv.Tiers})
	v.Shell, v.Back, v.BackLabel = "parent", studentPath(id, "/settings"), "Settings"
	s.render(w, r, http.StatusOK, "student_blocks", v)
}

func (s *Server) studentBlockSave(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	mastered := r.PostFormValue("placed") == "1"
	if err := s.svc.OverrideTopic(r.Context(), *c.User, id, r.PathValue("topic"), mastered); err != nil {
		s.parentFail(w, r, err)
		return
	}
	s.flash(w, "ok", "Updated.")
	http.Redirect(w, r, studentPath(id, "/blocks"), http.StatusSeeOther) //nolint:gosec // a same-site path built from one escaped segment
}

// --- deleting a student -----------------------------------------------------

func (s *Server) studentDeletePage(w http.ResponseWriter, r *http.Request, _ *reqCtx) {
	st, err := s.svc.Student(r.Context(), r.PathValue("id"))
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	v := s.newView(w, r, "Delete "+st.DisplayName, st)
	v.Shell, v.Back, v.BackLabel = "parent", studentPath(st.ID, "/settings"), "Settings"
	s.render(w, r, http.StatusOK, "student_delete", v)
}

func (s *Server) studentDelete(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	if err := s.svc.DeleteStudent(r.Context(), *c.User, id, r.PostFormValue("confirm")); err != nil {
		s.fail(w, r, studentPath(id, "/delete"), err)
		return
	}
	s.flash(w, "ok", "Deleted, with all their history.")
	http.Redirect(w, r, "/parent", http.StatusSeeOther)
}

// --- a student's tower, read-only -------------------------------------------

func (s *Server) studentTowerPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	st, err := s.svc.Student(r.Context(), id)
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	tv, err := s.svc.StudentTower(r.Context(), *c.User, id)
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	d := s.towerData(r, tv, studentPath(id, "/tower"))
	v := s.newView(w, r, st.DisplayName+"'s tower", d)
	v.Shell, v.Back, v.BackLabel = "parent", studentPath(id), st.DisplayName
	s.render(w, r, http.StatusOK, "tower", v)
}

// --- household (P5) ---------------------------------------------------------

type householdData struct {
	H         service.HouseholdView
	Me        store.User
	Timezones []string
	Tokens    []store.APIToken
	BaseURL   string
	MCP       bool // the MCP server is on
}

func (s *Server) householdPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	h, err := s.svc.Household(r.Context(), *c.User)
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	tokens, err := s.svc.APITokens(r.Context(), *c.User)
	if err != nil {
		s.parentFail(w, r, err)
		return
	}
	v := s.newView(w, r, "Household", householdData{H: h, Me: *c.User, Timezones: commonZones(h.Household.Timezone),
		Tokens: tokens, BaseURL: s.baseURL(r), MCP: s.mcp != nil})
	v.Shell, v.Back, v.BackLabel = "parent", "/parent", "Everyone"
	s.render(w, r, http.StatusOK, "household", v)
}

// commonZones are the time zones offered in the household form, with the
// current one first if it is not among them.
func commonZones(current string) []string {
	zones := []string{"America/New_York", "America/Chicago", "America/Denver", "America/Phoenix", "America/Los_Angeles",
		"America/Anchorage", "Pacific/Honolulu", "America/Toronto", "America/Vancouver", "Europe/London", "Europe/Dublin",
		"Europe/Paris", "Europe/Berlin", "Asia/Kolkata", "Asia/Singapore", "Asia/Tokyo", "Australia/Sydney", "Pacific/Auckland", "UTC"}
	for _, z := range zones {
		if z == current {
			return zones
		}
	}
	return append([]string{current}, zones...)
}

// tokenCreated shows a new token's secret, once.
type tokenCreated struct {
	Secret  string
	Token   store.APIToken
	BaseURL string
	MCP     bool
}

func (s *Server) tokenCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	scopes := []string{service.ScopeRead}
	if r.PostFormValue("manage") != "" {
		scopes = append(scopes, service.ScopeManage)
	}
	secret, t, err := s.svc.CreateAPIToken(r.Context(), *c.User, service.TokenInput{Label: r.PostFormValue("label"), Scopes: scopes})
	if err != nil {
		s.fail(w, r, "/parent/household#tokens-h", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	v := s.newView(w, r, "Your new token", tokenCreated{Secret: secret, Token: t, BaseURL: s.baseURL(r), MCP: s.mcp != nil})
	v.Shell, v.Back, v.BackLabel = "parent", "/parent/household", "Household"
	s.render(w, r, http.StatusOK, "token_new", v)
}

func (s *Server) tokenRevoke(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.svc.RevokeAPIToken(r.Context(), *c.User, r.PathValue("id")); err != nil {
		s.parentFail(w, r, err)
		return
	}
	s.flash(w, "ok", "Token revoked. Anything using it stops working now.")
	http.Redirect(w, r, "/parent/household#tokens-h", http.StatusSeeOther)
}

// baseURL is the public URL: ZIGRT_BASE_URL, or what this request came to.
func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL
	}
	scheme := "https"
	if r.TLS == nil && s.cfg.InsecureCookies {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func (s *Server) householdSave(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.svc.UpdateHousehold(r.Context(), *c.User, r.PostFormValue("name"), r.PostFormValue("timezone")); err != nil {
		s.fail(w, r, "/parent/household", err)
		return
	}
	s.flash(w, "ok", "Saved.")
	http.Redirect(w, r, "/parent/household", http.StatusSeeOther)
}

func (s *Server) passwordSave(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.svc.ChangePassword(r.Context(), *c.User, r.PostFormValue("current"), r.PostFormValue("password")); err != nil {
		s.fail(w, r, "/parent/household", err)
		return
	}
	// Changing it ended every session, this one too.
	s.flash(w, "ok", "Your password is changed. Sign in with the new one.")
	http.Redirect(w, r, "/parent/login", http.StatusSeeOther)
}

func (s *Server) parentNewPage(w http.ResponseWriter, r *http.Request, _ *reqCtx) {
	v := s.newView(w, r, "Add a parent", nil)
	v.Shell, v.Back, v.BackLabel = "parent", "/parent/household", "Household"
	s.render(w, r, http.StatusOK, "parent_new", v)
}

func (s *Server) parentCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	f := r.PostForm
	p, err := s.svc.CreateParent(r.Context(), *c.User, service.ParentInput{DisplayName: f.Get("display_name"),
		Email: f.Get("email"), Password: f.Get("password")})
	if err != nil {
		if msg := userMessage(err); msg != "" {
			v := s.newView(w, r, "Add a parent", nil)
			v.Shell, v.Back, v.BackLabel, v.Err = "parent", "/parent/household", "Household", msg
			v.Form = f
			v.Form.Del("password")
			s.render(w, r, http.StatusUnprocessableEntity, "parent_new", v)
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.flash(w, "ok", p.DisplayName+" can sign in now.")
	http.Redirect(w, r, "/parent/household", http.StatusSeeOther)
}

func (s *Server) backupNow(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	path, err := s.svc.BackupNow(r.Context(), *c.User)
	if err != nil {
		s.log.Error("backup", "err", err)
		s.flash(w, "err", "The backup didn't work: "+err.Error())
	} else {
		s.flash(w, "ok", "Backed up to "+path+".")
	}
	http.Redirect(w, r, "/parent/household", http.StatusSeeOther)
}

func (s *Server) exportAll(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	name := "zigrt-export-" + s.svc.Now().Format("2006-01-02") + ".jsonl"
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := s.svc.ExportAll(r.Context(), *c.User, w); err != nil {
		s.log.Error("export", "err", err) // the download is already under way
	}
}

func (s *Server) packUpload(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	r.Body = http.MaxBytesReader(w, r.Body, service.MaxPackSize+1<<20)
	file, _, err := r.FormFile("pack")
	if err != nil {
		s.flash(w, "err", "Choose a content pack file (.zip) to upload.")
		http.Redirect(w, r, "/parent/household", http.StatusSeeOther)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, service.MaxPackSize+1))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p, err := s.svc.InstallPack(r.Context(), *c.User, data)
	if err != nil {
		s.fail(w, r, "/parent/household", err)
		return
	}
	s.flash(w, "ok", "Lessons updated to "+p.Manifest.Version+".")
	http.Redirect(w, r, "/parent/household", http.StatusSeeOther)
}

// --- notifications (P6) -----------------------------------------------------

type noteView struct {
	service.NotificationView
	Title, Body       string
	Action, ActionURL string
	When              string
}

func (s *Server) notificationsPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	ns, unread, err := s.svc.Notifications(r.Context(), *c.User, 50)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	out := make([]noteView, 0, len(ns))
	for _, n := range ns {
		out = append(out, s.noteText(n))
	}
	v := s.newView(w, r, "Notifications", struct {
		Notes  []noteView
		Unread int
	}{out, unread})
	v.Shell, v.Back, v.BackLabel = "parent", "/parent", "Everyone"
	s.render(w, r, http.StatusOK, "notifications", v)
}

func (s *Server) notificationsRead(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.svc.MarkRead(r.Context(), *c.User, r.PostFormValue("id")); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/parent/notifications", http.StatusSeeOther)
}

// noteText puts a notification in the parent's words.
func (s *Server) noteText(n service.NotificationView) noteView {
	v := noteView{NotificationView: n, When: ago(s.svc.Now().Sub(n.CreatedAt))}
	name := "A student"
	if n.Student != nil {
		name = n.Student.DisplayName
		v.ActionURL = studentPath(n.Student.ID)
	}
	num := func(k string) int { f, _ := n.Data[k].(float64); return int(f) }
	str := func(k string) string { s, _ := n.Data[k].(string); return s }
	title := func(id string) string {
		if p := s.svc.Pack(); p != nil {
			if t := p.Topic(id); t != nil {
				return t.Title.In(service.Locale)
			}
		}
		return id
	}
	switch n.Kind {
	case service.NoteLocked:
		v.Title = name + " is locked out"
		v.Body = "Ten wrong PINs in a row. They stay locked until you unlock them."
		v.Action = "Unlock " + name
	case service.NoteLessonStopped:
		v.Title = "A lesson stopped twice for " + name
		v.Body = title(str("topic")) + ". Zigrt is mending the blocks under it first."
		v.Action = "See " + name
	case service.NoteGoalMet:
		v.Title = name + " met their goal"
		v.Body = fmt.Sprintf("%d of %d XP. Their streak is %d %s.", num("xp"), num("goal"), num("streak"), map[bool]string{true: "day", false: "days"}[num("streak") == 1])
	case service.NoteStreakLost:
		v.Title = name + "'s streak ended"
		v.Body = fmt.Sprintf("After %d days. A new one starts with today's goal.", num("days"))
	case service.NoteMilestone:
		if strand := str("strand"); strand != "" {
			v.Title = name + " finished a tier face"
			v.Body = fmt.Sprintf("Every %q block in %s is placed.", strand, gradeName(str("grade")))
		} else {
			v.Title = name + " finished " + gradeName(str("grade"))
			v.Body = fmt.Sprintf("All %d blocks in the tier are placed.", num("blocks"))
		}
	case service.NotePackUpdated:
		v.Title = "The lessons were updated"
		v.Body = fmt.Sprintf("Content pack %s is running now (it was %s).", str("version"), str("from"))
		v.Action, v.ActionURL = "Open Household", "/parent/household"
	default:
		v.Title = n.Kind
	}
	if v.Action == "" {
		v.ActionURL = ""
	}
	return v
}

// parentFail shows not found for a missing student, and the error page
// otherwise.
func (s *Server) parentFail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if msg := userMessage(err); msg != "" {
		s.renderStatus(w, r, http.StatusUnprocessableEntity, "Not quite", msg)
		return
	}
	s.serverError(w, r, err)
}
