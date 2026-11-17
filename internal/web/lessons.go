package web

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/figure"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/problem"
)

// Lessons and reviews (S3, S6 to S11). Every step is a plain form and a
// redirect, so the pages work without JavaScript; the answer is never in a
// page before it is submitted (plan 10.2).

// studentOnly guards the learning pages: parents cannot answer for a child.
func (s *Server) studentOnly(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := rc(r)
		switch {
		case c.User == nil:
			http.Redirect(w, r, "/", http.StatusSeeOther)
		case !c.User.IsStudent():
			http.Redirect(w, r, "/parent", http.StatusSeeOther)
		default:
			h(w, r, c)
		}
	}
}

// --- the home page (S3) -----------------------------------------------------

type homeData struct {
	H         service.HomeView
	First     *taskCard
	Others    []taskCard
	Early     bool
	Tower     []tierView // top tier first
	Placement bool       // the placement test is offered and not started: show "start from the bottom"
}

// tierView is one tier of the home tower: done (blue), where the student
// is (sun) or still to come.
type tierView struct {
	Label, State string
	Width        int // 1 (top, narrowest) to 6
	Placed       int
	Total        int
}

func tower(tiers []service.Tier) []tierView {
	var out []tierView
	here := false
	for i, t := range tiers {
		tv := tierView{Label: gradeName(t.Grade), Placed: t.Placed, Total: t.Total, State: "idle"}
		switch {
		case t.Placed == t.Total:
			tv.State = "done"
		case !here:
			tv.State, here = "here", true
		}
		tv.Width = max(6-i, 1) // the youngest grade is the widest, at the bottom
		out = append([]tierView{tv}, out...)
	}
	return out
}

func gradeName(g string) string {
	if g == "K" {
		return "Kindergarten"
	}
	return "Grade " + g
}

// taskCard is one task on the home or results page.
type taskCard struct {
	Kind   store.TaskKind
	Topic  string
	Title  string
	Label  string // "New block", "Mend a block"
	XP     int
	TaskID string // continuing a task on screen
}

func (s *Server) card(n service.NextTask) taskCard {
	c := taskCard{Kind: n.Kind, TaskID: n.TaskID}
	if n.Topic != nil {
		c.Topic, c.Title = n.Topic.ID, n.Topic.Title.In(service.Locale)
	}
	switch {
	case n.Kind == store.TaskDiagnostic:
		c.Label, c.Title, c.XP = "Find your start", "Where does your tower start?", 0
		if n.Resume {
			c.Label = "Keep going"
		}
	case n.Kind == store.TaskQuiz:
		c.Label, c.Title, c.XP = "Quick check", "Are your new blocks firm?", s.svc.Learn.QuizBaseXP
	case n.Kind == store.TaskReview:
		c.Label, c.XP = "Mend a block", s.svc.Learn.ReviewBaseXP
	case n.Resume:
		c.Label, c.XP = "Keep building", lessonXP(n.Topic, s.svc.Learn.DefaultLessonXP)
	default:
		c.Label, c.XP = "New block", lessonXP(n.Topic, s.svc.Learn.DefaultLessonXP)
	}
	if n.Early {
		c.Label = "Mend early"
	}
	return c
}

func lessonXP(t *content.Topic, def int) int {
	if t != nil && t.XP > 0 {
		return t.XP
	}
	return def
}

func (s *Server) studentHome(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	h, err := s.svc.Home(r.Context(), *c.User)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := homeData{H: h, Early: h.Complete, Tower: tower(h.Tiers)}
	cards := make([]taskCard, 0, len(h.Tasks)+1)
	if h.Active != nil {
		cards = append(cards, s.card(*h.Active))
	}
	for _, n := range h.Tasks {
		if h.Active != nil && n.Kind == h.Active.Kind && (n.Topic == nil || h.Active.Topic == nil || n.Topic.ID == h.Active.Topic.ID) {
			continue
		}
		cards = append(cards, s.card(n))
	}
	if len(cards) > 0 {
		d.First, d.Others = &cards[0], cards[1:]
		d.Placement = cards[0].Kind == store.TaskDiagnostic && cards[0].TaskID == "" && !h.Tasks[0].Resume
	}
	v := s.newView(w, r, "Home", d)
	v.Shell, v.Nav = "student", "home"
	s.render(w, r, http.StatusOK, "home", v)
}

// --- starting and moving through a task -------------------------------------

func (s *Server) taskStart(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	kind := store.TaskKind(r.PostForm.Get("kind"))
	t, err := s.svc.StartTask(r.Context(), *c.User, kind, r.PostForm.Get("topic"))
	if err != nil {
		s.taskFail(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(t.ID), http.StatusSeeOther) //nolint:gosec // same-site: a fixed prefix and an escaped ID
}

// assignmentStart opens a drill or worksheet a parent assigned.
func (s *Server) assignmentStart(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	d, err := s.svc.StartAssignment(r.Context(), *c.User, r.PathValue("id"))
	if err != nil {
		s.taskFail(w, r, err)
		return
	}
	s.toDrill(w, r, d, "")
}

// placementSkip starts a new student from the bottom of the tower.
func (s *Server) placementSkip(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := s.svc.SkipPlacement(r.Context(), *c.User); err != nil {
		s.taskFail(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// taskNotLearned answers a placement question with "I haven't learned this".
func (s *Server) taskNotLearned(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if _, err := s.svc.NotLearned(r.Context(), *c.User, id, r.PostForm.Get("attempt")); err != nil {
		s.taskFail(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(id), http.StatusSeeOther) //nolint:gosec // same-site: a fixed prefix and an escaped ID
}

func (s *Server) taskNext(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	id := r.PathValue("id")
	if _, err := s.svc.Continue(r.Context(), *c.User, id); err != nil {
		s.taskFail(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(id), http.StatusSeeOther) //nolint:gosec // same-site: a fixed prefix and an escaped ID
}

func (s *Server) taskAnswer(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	f := r.PostForm
	resp := problem.Response{Main: f.Get("main"), Rem: f.Get("rem")}
	if num, den := strings.TrimSpace(f.Get("num")), strings.TrimSpace(f.Get("den")); num != "" || den != "" {
		resp.Main = strings.TrimSpace(strings.TrimSpace(f.Get("whole")) + " " + num + "/" + den)
	} else if wh := strings.TrimSpace(f.Get("whole")); wh != "" {
		resp.Main = wh
	}
	if _, err := s.svc.SubmitAnswer(r.Context(), *c.User, id, f.Get("attempt"), resp); err != nil {
		s.taskFail(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(id), http.StatusSeeOther) //nolint:gosec // same-site: a fixed prefix and an escaped ID
}

// taskPath is a task's page. The ID is escaped, so the path stays on site.
func taskPath(id string) string { return "/tasks/" + url.PathEscape(id) }

// taskFail sends the student home with a kind word for the errors they can
// meet, and shows the error page for the rest.
func (s *Server) taskFail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrNotOffered):
		s.flash(w, "err", "That isn't on your list right now. Here's what's next.")
	case errors.Is(err, service.ErrTaskGone):
		s.flash(w, "err", "That lesson changed, so it starts fresh. Pick it from your list.")
	case errors.Is(err, service.ErrNoContent):
		s.flash(w, "err", "There are no lessons yet. You can make a drill.")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, service.ErrForbidden):
		s.notFound(w, r)
		return
	default:
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- the task page (S6 to S11) ----------------------------------------------

type taskData struct {
	V     service.TaskView
	Title string // the topic
	XP    int    // what the task is worth
	Segs  []string

	// explain
	Explain   string
	Example   template.HTML
	Steps     []string
	NextStep  int // the ?step= that shows one more, or 0 when all show
	AllShown  bool
	KPNumber  int
	FirstStep bool

	// problem
	Question   template.HTML // the heading: the prompt, or an instruction
	Body       template.HTML // the problem drawn, without its answer
	Input      string        // integer, decimal, fraction, quotient_remainder, comparison, choice
	Choices    []choiceView
	LongAnswer bool

	// feedback
	Answered template.HTML // the problem drawn with its answer
	Given    string
	NoteText string

	// done and paused
	BonusXP int
	Next    *taskCard

	// placement and quizzes
	Kind       string // lesson, review, diagnostic or quiz
	QuizItems  []quizItem
	QuizMissed int
	Placed     []string // placement: the topics placed
	Milestone  *milestoneView
}

// quizItem is one line of quiz results (S11).
type quizItem struct {
	Prompt template.HTML
	Answer template.HTML
	Right  bool
	Given  string
	Steps  []string
}

// milestoneView is a finished tier or face (S9b).
type milestoneView struct {
	Title          string // "Grade 2", or "Fractions in Grade 3"
	Tier           bool
	Blocks, Placed int
	Next           string // the tier above, if any
}

type choiceView struct {
	Value string
	Label template.HTML
}

func (s *Server) taskPage(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	tv, err := s.svc.Task(r.Context(), *c.User, r.PathValue("id"))
	if err != nil {
		s.taskFail(w, r, err)
		return
	}
	step, _ := strconv.Atoi(r.URL.Query().Get("step"))
	d := s.taskData(tv, step)
	v := s.newView(w, r, d.Title, d)
	v.Shell = "task"
	if d.Milestone != nil {
		v.Shell = "task shell-milestone" // the one full-blue screen (S9b)
	}
	s.render(w, r, http.StatusOK, "task", v)
}

func (s *Server) taskData(tv service.TaskView, step int) taskData {
	pack := s.svc.Pack()
	d := taskData{V: tv, Kind: string(tv.Task.Kind)}
	if tv.Topic != nil {
		d.Title = tv.Topic.Title.In(service.Locale)
	}
	switch tv.Task.Kind {
	case store.TaskDiagnostic:
		d.Title = "Finding your start"
	case store.TaskQuiz:
		d.Title, d.XP = "Quick check", s.svc.Learn.QuizBaseXP
		for i := range tv.Total {
			d.Segs = append(d.Segs, seg(i, tv.Asked))
		}
	case store.TaskReview:
		d.XP = s.svc.Learn.ReviewBaseXP
		for i := range tv.Total {
			d.Segs = append(d.Segs, seg(i, tv.Asked))
		}
	default:
		d.XP = lessonXP(tv.Topic, s.svc.Learn.DefaultLessonXP)
		for i := range tv.KPCount {
			d.Segs = append(d.Segs, seg(i, tv.KPIndex))
		}
		d.KPNumber = tv.KPIndex + 1
	}

	switch tv.Phase {
	case service.PhaseExplain:
		d.Explain = tv.KP.Explain.In(service.Locale)
		ex := tv.Example
		d.FirstStep = step <= 0
		step = min(max(step, 1), len(ex.Solution))
		d.Example = problemHTML(pack, ex, true)
		d.Steps = ex.Solution[:step]
		if step < len(ex.Solution) {
			d.NextStep = step + 1
		} else {
			d.AllShown = true
		}
	case service.PhaseProblem:
		p := tv.Problem
		d.Question, d.Body = mathHTML(question(p)), problemHTML(pack, p, false)
		d.Input = string(p.Answer.Type)
		for i, ch := range p.Choices {
			d.Choices = append(d.Choices, choiceView{Value: strconv.Itoa(i), Label: mathHTML(ch)})
		}
		d.LongAnswer = p.Layout == problem.LayoutLongDivision
	}
	if f := tv.Feedback; f != nil {
		d.Answered = problemHTML(pack, f.Problem, true)
		d.Given = given(f)
		switch f.Note {
		case problem.NoteSimplify:
			d.NoteText = "That's the right amount. Now write it in its simplest form."
		case problem.NoteUnreadable:
			d.NoteText = "We couldn't read that answer."
		}
	}
	if res := tv.Result; res != nil {
		if res.Passed {
			for i := range d.Segs {
				d.Segs[i] = "done"
			}
		}
		if tv.Task.Kind == store.TaskLesson && res.XP > d.XP {
			d.BonusXP = res.XP - d.XP
		}
		if m := res.Milestone; m != nil {
			mv := &milestoneView{Title: gradeName(m.Grade), Tier: m.Strand == nil, Blocks: m.Blocks, Placed: m.Placed}
			if m.Strand != nil {
				mv.Title = m.Strand.Title.In(service.Locale) + " in " + gradeName(m.Grade)
			}
			if m.TierNext != "" {
				mv.Next = gradeName(m.TierNext)
			}
			d.Milestone = mv
		}
		if len(res.Next) > 0 {
			c := s.card(res.Next[0])
			d.Next = &c
		}
	}
	for _, f := range tv.QuizResults {
		it := quizItem{Prompt: mathHTML(f.Problem.Prompt), Answer: mathHTML(f.Answer), Right: f.Right, Given: given(&f), Steps: f.Problem.Solution}
		switch {
		case f.Problem.Answer.Type == problem.Comparison:
			// 3/4 ○ 2/3 becomes 3/4 > 2/3, the sign in place of the box.
			it.Prompt = template.HTML(strings.Replace(string(it.Prompt), circle, `<b>`+template.HTMLEscapeString(f.Answer)+`</b>`, 1)) //nolint:gosec // escaped pack text and fixed markup
			it.Answer = ""
		case f.Problem.Layout == problem.LayoutMissing && strings.Contains(f.Problem.Prompt, "?"):
			it.Prompt = mathHTML(strings.Replace(f.Problem.Prompt, "?", f.Answer, 1))
			it.Answer = ""
		case f.Problem.Layout == problem.LayoutAcross || f.Problem.Layout == problem.LayoutStacked || f.Problem.Layout == problem.LayoutLongDivision:
			it.Prompt += " ="
		}
		if !f.Right {
			d.QuizMissed++
		}
		d.QuizItems = append(d.QuizItems, it)
	}
	for _, t := range tv.Placed {
		d.Placed = append(d.Placed, t.Title.In(service.Locale))
	}
	return d
}

// seg colours a progress segment: done, current or to come.
func seg(i, at int) string {
	switch {
	case i < at:
		return "done"
	case i == at:
		return "now"
	}
	return "todo"
}

// question is the heading for a problem: a word problem or a figure asks
// in its own words; a sum is just worked out.
func question(p *problem.Problem) string {
	switch {
	case p.Answer.Type == problem.Comparison:
		return "Which sign goes in the middle?"
	case p.Layout == problem.LayoutText || p.Layout == problem.LayoutFigure || p.Layout == "":
		return p.Prompt
	case p.Layout == problem.LayoutMissing:
		return "What number goes in the box?"
	}
	return "Work it out."
}

func given(f *service.Feedback) string {
	switch f.Problem.Answer.Type {
	case problem.Choice:
		if i, err := strconv.Atoi(f.Given.Main); err == nil && i >= 0 && i < len(f.Problem.Choices) {
			return f.Problem.Choices[i]
		}
		return ""
	case problem.QuotientRemainder:
		if strings.TrimSpace(f.Given.Rem) != "" {
			return f.Given.Main + " r " + f.Given.Rem
		}
	}
	return f.Given.Main
}

// --- drawing a problem ------------------------------------------------------

// problemHTML draws a problem in its layout. With answer false the answer
// place is an empty box: the page never carries the answer before it is
// submitted. Everything that comes from the pack is escaped.
func problemHTML(pack *content.Pack, p *problem.Problem, answer bool) template.HTML {
	var b strings.Builder
	ans := `<span class="qbox" aria-label="blank"></span>`
	if answer {
		ans = `<span class="qans">` + string(mathHTML(p.Answer.Text(p))) + `</span>`
	}
	part := func(i int) string {
		if i < len(p.Parts) {
			return string(mathHTML(p.Parts[i]))
		}
		return ""
	}
	op := `<span class="op">` + template.HTMLEscapeString(p.Op) + `</span>`
	switch p.Layout {
	case problem.LayoutAcross:
		b.WriteString(`<div class="pr pr-hz lp">` + part(0) + op + part(1) + `<span class="op">=</span>` + ans + `</div>`)
	case problem.LayoutStacked:
		b.WriteString(`<div class="pr pr-vt lp"><span></span>` + part(0) + op + part(1) +
			`<span class="bar"></span><span></span><span class="vt-ans">` + ans + `</span></div>`)
	case problem.LayoutMissing:
		parts := []string{part(0), part(1), part(2)}
		if p.Blank >= 0 && p.Blank < 3 {
			parts[p.Blank] = ans
		}
		b.WriteString(`<div class="pr pr-hz lp">` + parts[0] + op + parts[1] + `<span class="op">=</span>` + parts[2] + `</div>`)
	case problem.LayoutLongDivision:
		b.WriteString(`<div class="pr pr-ld lp"><span></span><span class="ld-ans">` + ans + `</span><span class="ld-divisor">` +
			part(1) + `</span><span class="ld-dividend">` + part(0) + `</span></div>`)
	case problem.LayoutFigure:
		b.WriteString(figureHTML(pack, p))
		if answer {
			b.WriteString(`<div class="pr lp lp-answer">` + ans + `</div>`)
		}
	default:
		if p.Answer.Type == problem.Comparison {
			b.WriteString(`<div class="pr pr-hz lp">` + strings.Replace(string(mathHTML(p.Prompt)), circle, ans, 1) + `</div>`)
		} else if answer {
			b.WriteString(`<div class="pr lp lp-answer">` + ans + `</div>`)
		}
	}
	return template.HTML(b.String()) //nolint:gosec // built from escaped pack text and fixed markup
}

// figureHTML draws a catalogue figure, or a static one from the pack (as
// figure.Clean rewrote it at load).
func figureHTML(pack *content.Pack, p *problem.Problem) string {
	var svg string
	var err error
	if _, ok := figure.Catalogue[p.Figure]; ok {
		svg, err = figure.Draw(p.Figure, figure.Args(p.FigArgs))
	} else if pack != nil {
		svg, err = pack.StaticFigure(p.Figure)
	}
	if err != nil || svg == "" {
		return ""
	}
	return `<div class="figure">` + svg + `</div>`
}

const circle = `<span class="qcmp" aria-label="which sign">○</span>`

var mathTokRE = regexp.MustCompile(`(\d+) (\d+)/(\d+)|(\d+)/(\d+)|○`)

// mathHTML writes pack text with fractions stacked (3/4, 1 3/4) and the
// comparison circle as a box. The text is escaped first.
func mathHTML(s string) template.HTML {
	esc := template.HTMLEscapeString(s)
	out := mathTokRE.ReplaceAllStringFunc(esc, func(m string) string {
		if m == "○" {
			return circle
		}
		sub := mathTokRE.FindStringSubmatch(m)
		if sub[1] != "" {
			return `<span class="mixed">` + sub[1] + frac(sub[2], sub[3]) + `</span>`
		}
		return frac(sub[4], sub[5])
	})
	return template.HTML(out) //nolint:gosec // escaped above; only fixed markup added
}

func frac(n, d string) string {
	return `<span class="frac" role="math" aria-label="` + n + ` over ` + d + `"><span>` + n + `</span><span>` + d + `</span></span>`
}
