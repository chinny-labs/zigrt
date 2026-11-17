package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/config"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/problem"
)

func hardestPack(t *testing.T) *content.Pack {
	t.Helper()
	p, err := content.Open(filepath.Join("..", "..", "content", "testdata", "hardest"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var taskIDRE = regexp.MustCompile(`^/tasks/([^/?]+)$`)

// answerForm is what a student types for a response, field by field.
func answerForm(p *problem.Problem, r problem.Response, attempt string) url.Values {
	f := url.Values{"attempt": {attempt}}
	switch p.Answer.Type {
	case problem.Fraction:
		main := r.Main
		if whole, rest, ok := strings.Cut(main, " "); ok {
			f.Set("whole", whole)
			main = rest
		}
		num, den, _ := strings.Cut(main, "/")
		f.Set("num", num)
		f.Set("den", den)
	default:
		f.Set("main", r.Main)
		if r.Rem != "" {
			f.Set("rem", r.Rem)
		}
	}
	return f
}

// TestLessonPages walks a lesson through the pages as a browser without
// JavaScript would: forms and redirects only.
func TestLessonPages(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.svc.SetPack(hardestPack(t))
	// A clock the test moves: answers in no time at all are rushing.
	now := time.Date(2026, 3, 7, 15, 0, 0, 0, time.UTC)
	a.svc.Now = func() time.Time { return now }
	a.setUp()
	kid, kidID := a.signInStudent("Mika", "2468")
	mika, err := a.svc.Store.GetUser(ctx, kidID)
	if err != nil {
		t.Fatal(err)
	}

	_, body := a.get(kid, "/")
	for _, want := range []string{"Your tower", "0 blocks placed", "Today's climb", "0 of 15 XP", "Next step · New block", "Elapsed time", `name="topic" value="elapsed-time"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("home lacks %q:\n%s", want, body)
		}
	}
	resp, _ := a.post(kid, "/", "/tasks", url.Values{"kind": {"lesson"}, "topic": {"elapsed-time"}})
	m := taskIDRE.FindStringSubmatch(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || m == nil {
		t.Fatalf("start: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	page := "/tasks/" + m[1]

	// The explanation reveals the worked example a step at a time.
	_, body = a.get(kid, page)
	if !strings.Contains(body, "Let's learn it") || !strings.Contains(body, `href="`+page+`?step=2"`) || strings.Contains(body, "Now you try") {
		t.Fatalf("explain:\n%s", body)
	}
	_, body = a.get(kid, page+"?step=99")
	if !strings.Contains(body, "Now you try") || strings.Contains(body, "Show the next step") {
		t.Fatalf("all steps:\n%s", body)
	}

	answer := func(right bool) string {
		t.Helper()
		tv, err := a.svc.Task(ctx, mika, m[1])
		if err != nil || tv.Phase != service.PhaseProblem {
			t.Fatalf("task: %v %s", err, tv.Phase)
		}
		now = now.Add(8 * time.Second)
		r := tv.Problem.Wrong()
		if right {
			r = tv.Problem.Right()
		}
		resp, _ := a.post(kid, page, page+"/answer", answerForm(tv.Problem, r, tv.AttemptID))
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != page {
			t.Fatalf("answer: %d %s", resp.StatusCode, resp.Header.Get("Location"))
		}
		_, body := a.get(kid, page)
		return body
	}
	next := func() string {
		t.Helper()
		// ?step=99 shows the whole worked example, where the form is.
		resp, _ := a.post(kid, page+"?step=99", page+"/next", url.Values{})
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("next: %d", resp.StatusCode)
		}
		_, body := a.get(kid, page)
		return body
	}

	body = next()
	if !strings.Contains(body, `name="attempt"`) || !strings.Contains(body, `name="main"`) || !strings.Contains(body, ">Check<") || strings.Contains(body, "qans") {
		t.Fatalf("problem:\n%s", body)
	}
	body = answer(false)
	if !strings.Contains(body, "Not quite. Let's look together.") || !strings.Contains(body, "The answer is") || !strings.Contains(body, `class="steps"`) || !strings.Contains(body, "Try another one") {
		t.Fatalf("feedback, not yet:\n%s", body)
	}
	next()
	if body = answer(true); !strings.Contains(body, "That's right!") {
		t.Fatalf("feedback, right:\n%s", body)
	}
	next()
	answer(true)
	body = next()
	// Elapsed time is the prototype pack's only grade 3 topic, so placing it
	// finishes the tier: the milestone screen (S9b).
	for _, want := range []string{"Tier complete", "You finished Grade 3!", "Its block is placed.", "+14 XP", "shell-milestone", `href="/tower"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("done page lacks %q:\n%s", want, body)
		}
	}
	if _, body = a.get(kid, "/"); !strings.Contains(body, "1 block placed") || !strings.Contains(body, "14 of 15 XP") {
		t.Fatalf("home after:\n%s", body)
	}

	// The server chooses tasks: a locked topic is refused, kindly.
	resp, _ = a.post(kid, "/", "/tasks", url.Values{"kind": {"lesson"}, "topic": {"long-division-2d"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("locked topic: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, body = a.get(kid, "/"); !strings.Contains(body, "That isn&#39;t on your list right now.") {
		t.Fatal("no message for a locked topic")
	}
	// Parents cannot open or answer a child's task.
	if resp, _ := a.get(a.c, page); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/parent" {
		t.Fatalf("parent on a task: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	// Another student gets nothing.
	theo, _ := a.signInStudent("Theo", "13579")
	if resp, _ := a.get(theo, page); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("another student's task: %d", resp.StatusCode)
	}
}

// TestTaskRendering draws a problem of every answer type and layout in the
// prototype pack, before and after it is answered: the right inputs, a
// figure when there is one, no inline style, and no answer before it is
// submitted.
func TestTaskRendering(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	pack := hardestPack(t)
	a.svc.SetPack(pack)
	srv, err := New(ctx, a.svc, config.Config{InsecureCookies: true}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	render := func(tv service.TaskView) string {
		t.Helper()
		d := srv.taskData(tv, 0)
		v := &view{reqCtx: &reqCtx{CSRF: "tok"}, Title: d.Title, Shell: "task", D: d, Form: url.Values{}}
		var buf bytes.Buffer
		if err := srv.pages["task"].ExecuteTemplate(&buf, "layout", v); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	inputs := map[problem.AnswerType][]string{
		problem.Integer:           {`name="main" inputmode="numeric"`},
		problem.Decimal:           {`name="main" inputmode="decimal"`},
		problem.Fraction:          {`name="whole"`, `name="num"`, `name="den"`},
		problem.QuotientRemainder: {`name="main"`, `name="rem"`},
		problem.Comparison:        {`type="radio" name="main" value="&lt;"`, `value="="`, `value="&gt;"`},
		problem.Choice:            {`type="radio" name="main" value="0"`},
	}
	seenType, seenLayout := map[problem.AnswerType]bool{}, map[problem.Layout]bool{}
	for _, tpl := range pack.Templates() {
		p, err := tpl.Generate(3, service.Locale)
		if err != nil {
			t.Fatal(err)
		}
		seenType[p.Answer.Type], seenLayout[p.Layout] = true, true
		topic := pack.TopicOf(tpl.ID)
		tv := service.TaskView{Task: store.Task{ID: "t1", Kind: store.TaskLesson}, Topic: topic, Phase: service.PhaseProblem,
			Problem: p, AttemptID: "a1", KPCount: len(topic.KPs)}
		html := render(tv)
		for _, want := range inputs[p.Answer.Type] {
			if !strings.Contains(html, want) {
				t.Errorf("%s (%s): lacks %s", tpl.ID, p.Answer.Type, want)
			}
		}
		if p.Answer.Type == problem.Choice && strings.Count(html, `type="radio"`) != len(p.Choices) {
			t.Errorf("%s: %d options drawn, %d choices", tpl.ID, strings.Count(html, `type="radio"`), len(p.Choices))
		}
		if p.Layout == problem.LayoutFigure && !strings.Contains(html, `<svg class="fig"`) {
			t.Errorf("%s: no figure", tpl.ID)
		}
		if strings.Contains(html, "qans") || strings.Contains(html, "style=") {
			t.Errorf("%s: problem page shows an answer or an inline style", tpl.ID)
		}

		tv.Phase, tv.Problem = service.PhaseFeedback, nil
		tv.Feedback = &service.Feedback{Problem: p, Given: p.Wrong(), Answer: p.Answer.Text(p)}
		html = render(tv)
		if !strings.Contains(html, "qans") || !strings.Contains(html, "Not quite") || strings.Count(html, `class="step"`) != len(p.Solution) {
			t.Errorf("%s: feedback lacks the answer or the worked steps", tpl.ID)
		}
	}
	for ty := range inputs {
		if !seenType[ty] {
			t.Errorf("the prototype pack has no %s problem to draw", ty)
		}
	}
	// The prototype pack has no across or missing-number problems; the
	// sample pack's do (see the content tests).
	for _, l := range []problem.Layout{problem.LayoutText, problem.LayoutStacked, problem.LayoutLongDivision, problem.LayoutFigure} {
		if !seenLayout[l] {
			t.Errorf("no %s layout drawn", l)
		}
	}
}

func TestMathHTML(t *testing.T) {
	for in, want := range map[string]string{
		"3/4 ○ 2/3": `<span class="frac" role="math" aria-label="3 over 4"><span>3</span><span>4</span></span> ` + circle +
			` <span class="frac" role="math" aria-label="2 over 3"><span>2</span><span>3</span></span>`,
		"1 3/4":         `<span class="mixed">1<span class="frac" role="math" aria-label="3 over 4"><span>3</span><span>4</span></span></span>`,
		"<b>12</b> r 3": `&lt;b&gt;12&lt;/b&gt; r 3`,
	} {
		if got := string(mathHTML(in)); got != want {
			t.Errorf("mathHTML(%q) =\n%s\nwant\n%s", in, got, want)
		}
	}
}

// /readyz fails while no content pack can run (plan 4.7).
func TestReadyzContent(t *testing.T) {
	a := newApp(t)
	if resp, _ := a.get(a.c, "/readyz"); resp.StatusCode != http.StatusOK {
		t.Fatalf("with a pack: %d", resp.StatusCode)
	}
	a.svc.SetPack(nil)
	if resp, body := a.get(a.c, "/readyz"); resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "no content pack") {
		t.Fatalf("without a pack: %d %s", resp.StatusCode, body)
	}
	srv, err := New(context.Background(), a.svc, config.Config{InsecureCookies: true}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	a.svc.SetPack(hardestPack(t))
	srv.SetContentError(errors.New("content pack 2099.01.1 needs Zigrt 9.0.0 or later"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "needs Zigrt 9.0.0") {
		t.Fatalf("too new a pack: %d %s", rec.Code, rec.Body.String())
	}
}

// TestPlacementPages: a new student is offered the placement test first,
// can skip it, and a started one asks with "I haven't learned this yet".
func TestPlacementPages(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.svc.Learn.OfferPlacement = true
	now := time.Date(2026, 3, 7, 15, 0, 0, 0, time.UTC)
	a.svc.Now = func() time.Time { return now }
	a.setUp()
	kid, kidID := a.signInStudent("Mika", "2468")
	mika, _ := a.svc.Store.GetUser(ctx, kidID)

	_, body := a.get(kid, "/")
	for _, want := range []string{"Find your start", "Where does your tower start?", `name="kind" value="diagnostic"`, "Start from the bottom instead"} {
		if !strings.Contains(body, want) {
			t.Fatalf("home lacks %q:\n%s", want, body)
		}
	}
	resp, _ := a.post(kid, "/", "/tasks", url.Values{"kind": {"diagnostic"}, "topic": {""}})
	m := taskIDRE.FindStringSubmatch(resp.Header.Get("Location"))
	if m == nil {
		t.Fatalf("start: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	page := "/tasks/" + m[1]
	_, body = a.get(kid, page)
	if !strings.Contains(body, "Let's find where your tower starts.") || !strings.Contains(body, ">Start<") {
		t.Fatalf("intro:\n%s", body)
	}
	a.post(kid, page, page+"/next", url.Values{})
	_, body = a.get(kid, page)
	if !strings.Contains(body, "Finding your start") || !strings.Contains(body, page+"/not-learned") || !strings.Contains(body, "I haven't learned this yet") || strings.Contains(body, "qans") {
		t.Fatalf("question:\n%s", body)
	}
	tv, err := a.svc.Task(ctx, mika, m[1])
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = a.post(kid, page, page+"/not-learned", url.Values{"attempt": {tv.AttemptID}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("not learned: %d", resp.StatusCode)
	}
	if tv2, _ := a.svc.Task(ctx, mika, m[1]); tv2.Asked != 1 || tv2.AttemptID == tv.AttemptID {
		t.Fatalf("after not learned: asked %d", tv2.Asked)
	}

	// Another student starts from the bottom instead.
	theo, _ := a.signInStudent("Theo", "13579")
	resp, _ = a.post(theo, "/", "/placement/skip", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("skip: %d", resp.StatusCode)
	}
	if _, body = a.get(theo, "/"); strings.Contains(body, "Find your start") || !strings.Contains(body, "Next step · New block") {
		t.Fatalf("home after skipping:\n%s", body)
	}
}

// TestTowerPage: tiers, faces and blocks, and a block's story with its action.
func TestTowerPage(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	now := time.Date(2026, 3, 7, 15, 0, 0, 0, time.UTC)
	a.svc.Now = func() time.Time { return now }
	a.setUp()
	kid, kidID := a.signInStudent("Mika", "2468")
	mika, _ := a.svc.Store.GetUser(ctx, kidID)

	_, body := a.get(kid, "/tower")
	for _, want := range []string{"My tower", "0 blocks placed", "Grade 3 · 0 of 1", `class="face face-on`, "block-ready", "Needs mending", `href="/tower" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("tower lacks %q:\n%s", want, body)
		}
	}
	// A ready block's story offers to start it.
	_, body = a.get(kid, "/tower?grade=3&strand=measurement&topic=elapsed-time")
	if !strings.Contains(body, `class="card detail`) || !strings.Contains(body, "Start this block · 12 XP") || !strings.Contains(body, `name="topic" value="elapsed-time"`) {
		t.Fatalf("block detail:\n%s", body)
	}
	// Place it: the tier is finished, with its flag, for good.
	task, err := a.svc.StartTask(ctx, mika, "lesson", "elapsed-time")
	if err != nil {
		t.Fatal(err)
	}
	for v, err := a.svc.Continue(ctx, mika, task.ID); v.Phase != service.PhaseDone; {
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(9 * time.Second)
		if v.Phase == service.PhaseProblem {
			v, err = a.svc.SubmitAnswer(ctx, mika, task.ID, v.AttemptID, v.Problem.Right())
		} else {
			v, err = a.svc.Continue(ctx, mika, task.ID)
		}
	}
	_, body = a.get(kid, "/tower?grade=3")
	if !strings.Contains(body, "Grade 3 · complete") || !strings.Contains(body, "tier-finished") || !strings.Contains(body, "1 block placed") || !strings.Contains(body, "face-done") {
		t.Fatalf("finished tier:\n%s", body)
	}
	_, body = a.get(kid, "/tower?grade=3&strand=measurement&topic=elapsed-time")
	if !strings.Contains(body, "Placed today") || strings.Contains(body, "Start this block") {
		t.Fatalf("placed block:\n%s", body)
	}
}

// TestQuizResultsPage draws a finished quiz (S11): every problem, the
// misses opened up with their solutions.
func TestQuizResultsPage(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	pack := hardestPack(t)
	srv, err := New(ctx, a.svc, config.Config{InsecureCookies: true}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var results []service.Feedback
	for i, tpl := range pack.Templates()[:8] {
		p, _ := tpl.Generate(2, service.Locale)
		f := service.Feedback{Problem: p, Given: p.Right(), Right: i != 3, Answer: p.Answer.Text(p)}
		if !f.Right {
			f.Given = p.Wrong()
		}
		results = append(results, f)
	}
	done := time.Now()
	tv := service.TaskView{Task: store.Task{ID: "q1", Kind: store.TaskQuiz, Status: store.TaskPassed, XPAwarded: 13, FinishedAt: &done},
		Phase: service.PhaseDone, Asked: 8, Total: 8, QuizResults: results, Result: &service.TaskResult{Passed: true, XP: 13}}
	d := srv.taskData(tv, 0)
	v := &view{reqCtx: &reqCtx{CSRF: "x"}, Title: d.Title, Shell: "task", D: d, Form: url.Values{}}
	var buf bytes.Buffer
	if err := srv.pages["task"].ExecuteTemplate(&buf, "layout", v); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"Quiz done", "7 of 8 held firm.", "+13 XP", "You got it", "See how", "One block needs mending. It is next on your list."} {
		if !strings.Contains(html, want) {
			t.Errorf("quiz results lack %q", want)
		}
	}
	if strings.Count(html, `class="quiz-item`) != 8 || strings.Count(html, "<details") != 1 {
		t.Errorf("items %d, details %d", strings.Count(html, `class="quiz-item`), strings.Count(html, "<details"))
	}
}
