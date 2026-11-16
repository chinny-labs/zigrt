package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// --- builder (S13) ----------------------------------------------------------

// opRow is one operation's row in the builder form.
type opRow struct {
	Op       drill.Op
	Settings drill.OpSettings
	Levels   []drill.Level
	Labels   [2]string
}

type builderData struct {
	Ops      []opRow
	Formats  []formatChoice
	Counts   []int
	Count    int
	Title    string
	CanDrill bool // on-screen drills are for students
	Recent   []service.DrillSummary
}

type formatChoice struct {
	Format drill.Format
	On     bool
}

func (s *Server) builderData(r *http.Request, c *reqCtx, set drill.Settings, title string) (builderData, error) {
	d := builderData{Counts: drill.Counts, Count: set.Count, Title: title, CanDrill: c.User.IsStudent()}
	for _, op := range drill.Ops {
		d.Ops = append(d.Ops, opRow{Op: op, Settings: set.Ops[op], Levels: drill.Levels[op], Labels: drill.CustomLabels[op]})
	}
	for _, f := range drill.Formats {
		on := false
		for _, g := range set.Formats {
			on = on || f == g
		}
		d.Formats = append(d.Formats, formatChoice{Format: f, On: on})
	}
	var err error
	d.Recent, err = s.svc.RecentDrills(r.Context(), *c.User, 5)
	return d, err
}

func (s *Server) shell(c *reqCtx) string {
	if c.User.IsParent() {
		return "parent"
	}
	return "student"
}

func (s *Server) drillBuilder(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	set, err := s.svc.LastDrillSettings(r.Context(), *c.User)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderBuilder(w, r, c, http.StatusOK, set, "", "")
}

func (s *Server) renderBuilder(w http.ResponseWriter, r *http.Request, c *reqCtx, status int, set drill.Settings, title, msg string) {
	d, err := s.builderData(r, c, set, title)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	v := s.newView(w, r, "Make a drill", d)
	if !d.CanDrill {
		v.Title = "Make a worksheet"
		v.Back, v.BackLabel = "/parent", "Everyone"
	}
	v.Shell, v.Nav, v.Err = s.shell(c), "drills", msg
	s.render(w, r, status, "drills", v)
}

// parseDrillForm reads the builder form. Unticked boxes are simply absent,
// so everything starts off and the form turns things on.
func parseDrillForm(f url.Values) drill.Settings {
	num := func(name string) int { n, _ := strconv.Atoi(f.Get(name)); return n }
	set := drill.Settings{Ops: map[drill.Op]drill.OpSettings{}, Count: num("count")}
	for _, op := range drill.Ops {
		k := string(op)
		set.Ops[op] = drill.OpSettings{
			On:         f.Get("op_"+k) != "",
			Level:      f.Get("level_" + k),
			CustomA:    drill.Range{num(k + "_a_lo"), num(k + "_a_hi")},
			CustomB:    drill.Range{num(k + "_b_lo"), num(k + "_b_hi")},
			Remainders: op == drill.Div && f.Get("div_rem") != "",
		}
	}
	for _, fm := range drill.Formats {
		if f.Get("fmt_"+string(fm)) != "" {
			set.Formats = append(set.Formats, fm)
		}
	}
	return set
}

func (s *Server) drillCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	set := parseDrillForm(r.PostForm)
	kind := store.DrillKind(r.PostFormValue("kind"))
	d, err := s.svc.CreateDrill(r.Context(), *c.User, kind, set, r.PostFormValue("title"))
	if err != nil {
		msg := userMessage(err)
		if msg == "" {
			s.serverError(w, r, err)
			return
		}
		// Show the form again as it was filled in, cleaned where possible.
		if norm, nerr := drill.Normalize(set); nerr == nil || errors.Is(nerr, drill.ErrNothingChosen) {
			set = norm
		}
		s.renderBuilder(w, r, c, http.StatusUnprocessableEntity, set, r.PostFormValue("title"), msg)
		return
	}
	http.Redirect(w, r, drillPath(d), http.StatusSeeOther)
}

func drillPath(d service.DrillView) string {
	if d.Kind == store.KindWorksheet {
		return "/worksheets/" + url.PathEscape(d.ID)
	}
	return "/drills/" + url.PathEscape(d.ID)
}

// loadDrill fetches the drill in the URL, answering 404 itself when it isn't
// there or isn't the visitor's.
func (s *Server) loadDrill(w http.ResponseWriter, r *http.Request, c *reqCtx) (service.DrillView, bool) {
	d, err := s.svc.Drill(r.Context(), *c.User, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
		} else {
			s.serverError(w, r, err)
		}
		return d, false
	}
	return d, true
}

// --- on-screen drill (S14) and results (S15) --------------------------------

// problemView is a problem plus how to draw it.
type problemView struct {
	drill.Problem
	N     int    // 1-based position
	Mode  string // "drill" or "sheet"
	Total int
}

func problemViews(ps []drill.Problem, mode string) []problemView {
	out := make([]problemView, len(ps))
	for i, p := range ps {
		out[i] = problemView{Problem: p, N: i + 1, Mode: mode, Total: len(ps)}
	}
	return out
}

type drillData struct {
	Drill    service.DrillView
	Problems []problemView
}

func (s *Server) drillPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d, ok := s.loadDrill(w, r, c)
	if !ok {
		return
	}
	switch {
	case d.Kind == store.KindWorksheet:
		http.Redirect(w, r, drillPath(d), http.StatusSeeOther)
	case d.Result != nil || d.OwnerID != c.User.ID:
		http.Redirect(w, r, drillPath(d)+"/results", http.StatusSeeOther)
	default:
		v := s.newView(w, r, "Drill", drillData{Drill: d, Problems: problemViews(d.Problems, "drill")})
		v.Script = "drill.js"
		s.render(w, r, http.StatusOK, "drill", v)
	}
}

func (s *Server) drillFinish(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var answers []service.Answer
	if raw := r.PostFormValue("answers"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &answers); err != nil {
			s.renderStatus(w, r, http.StatusBadRequest, "That didn't save", "The drill's answers arrived garbled. Go back and try again.")
			return
		}
	}
	ms, _ := strconv.ParseInt(r.PostFormValue("duration_ms"), 10, 64)
	d, err := s.svc.FinishDrill(r.Context(), *c.User, r.PathValue("id"), answers, time.Duration(ms)*time.Millisecond)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, service.ErrForbidden) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, drillPath(d)+"/results", http.StatusSeeOther)
}

type resultsData struct {
	Drill  service.DrillView
	Misses []service.Miss
	Time   string
	Mine   bool
}

func (s *Server) drillResults(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d, ok := s.loadDrill(w, r, c)
	if !ok {
		return
	}
	if d.Kind == store.KindWorksheet {
		http.Redirect(w, r, drillPath(d), http.StatusSeeOther)
		return
	}
	if d.Result == nil {
		if d.OwnerID == c.User.ID {
			http.Redirect(w, r, drillPath(d), http.StatusSeeOther)
			return
		}
		s.renderStatus(w, r, http.StatusOK, "Not finished yet", "This drill hasn't been finished, so there is nothing to show.")
		return
	}
	secs := d.Result.DurationMS / 1000
	v := s.newView(w, r, "Drill results", resultsData{
		Drill: d, Misses: d.Misses(), Mine: d.OwnerID == c.User.ID,
		Time: strconv.FormatInt(secs/60, 10) + ":" + twoDigits(secs%60),
	})
	v.Shell, v.Nav = s.shell(c), "drills"
	s.render(w, r, http.StatusOK, "drill_results", v)
}

func twoDigits(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

func (s *Server) drillRetry(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d, err := s.svc.RetryMisses(r.Context(), *c.User, r.PathValue("id"))
	s.afterNewDrill(w, r, d, err)
}

func (s *Server) drillAgain(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d, err := s.svc.NewVersion(r.Context(), *c.User, r.PathValue("id"))
	s.afterNewDrill(w, r, d, err)
}

func (s *Server) afterNewDrill(w http.ResponseWriter, r *http.Request, d service.DrillView, err error) {
	switch {
	case err == nil:
		http.Redirect(w, r, drillPath(d), http.StatusSeeOther)
	case errors.Is(err, store.ErrNotFound), errors.Is(err, service.ErrForbidden), errors.Is(err, service.ErrNoMisses):
		s.notFound(w, r)
	default:
		if msg := userMessage(err); msg != "" {
			s.renderStatus(w, r, http.StatusUnprocessableEntity, "That didn't work", msg)
			return
		}
		s.serverError(w, r, err)
	}
}

// --- worksheet (S16) --------------------------------------------------------

type sheetData struct {
	Drill     service.DrillView
	Problems  []problemView
	Cols      int
	Roomy     bool
	NameLine  bool
	AnswerKey bool
	Large     bool
	About     string
}

func (s *Server) worksheetPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d, ok := s.loadDrill(w, r, c)
	if !ok {
		return
	}
	q := r.URL.Query()
	sd := sheetData{
		Drill: d, Problems: problemViews(d.Problems, "sheet"), Cols: drill.Columns(d.Problems), About: drill.Describe(d.Settings),
		// With no options in the URL, the sheet starts with name and date lines.
		NameLine: q.Get("name") != "" || !q.Has("opts"), AnswerKey: q.Get("key") != "", Large: q.Get("large") != "",
	}
	for _, p := range d.Problems {
		sd.Roomy = sd.Roomy || p.NeedsRoom()
	}
	if sd.Large { // bigger type needs wider cells
		sd.Cols = max(2, sd.Cols-1)
	}
	v := s.newView(w, r, d.Title, sd)
	v.Shell, v.Nav = s.shell(c), "drills"
	v.Back, v.BackLabel = "/drills", "Make another"
	s.render(w, r, http.StatusOK, "worksheet", v)
}
