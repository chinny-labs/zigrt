package web

import (
	"bytes"
	"context"
	"net/http"
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
	for _, want := range []string{"You placed a new block!", "+14 XP", "12, plus 2 for a clean run", "14 of 15 XP", "Keep climbing"} {
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
