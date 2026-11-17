package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chinny-labs/zigrt/internal/buildinfo"
	"github.com/chinny-labs/zigrt/problem"
)

// view is what every template receives.
type view struct {
	*reqCtx
	Title     string
	Shell     string // "student", "parent" or "" for bare pages
	Back      string // back-link target, if any
	BackLabel string
	Nav       string // active item of the student's bottom nav
	Script    string // an extra script from /static, for the one page that needs it
	FlashKind string
	Flash     string
	Err       string
	Form      url.Values
	Version   string
	SourceURL string
	Unread    int // a parent's unread notifications, for the bell
	D         any
}

// Val is a submitted form value, for refilling a form after an error.
func (v *view) Val(name string) string { return v.Form.Get(name) }

var funcs = template.FuncMap{
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	},
	"digits": func() []string { return []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"} },
	// wclass sizes an answer box by digit count, without an inline style.
	"wclass":    func(n int) string { return "w" + strconv.Itoa(min(max(n, 2), 8)) },
	"add":       func(a, b int) int { return a + b },
	"sub":       func(a, b int) int { return a - b },
	"list":      func(xs ...string) []string { return xs },
	"math":      mathHTML,
	"gradeOf":   gradeName,
	"stateWord": func(state string) string { return stateWords[state] },
	// since says how long ago a time was, the way the parent pages put it.
	"since": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return ago(time.Since(*t))
	},
	"kindWord": func(kind string) string {
		return map[string]string{"lesson": "Lesson", "review": "Review", "quiz": "Quiz", "placement": "Placement check",
			"paused": "Lesson, stopped for the day", "drill": "Drill", "worksheet": "Worksheet"}[kind]
	},
	// pair passes two values to a sub-template, as .A and .B.
	"pair": func(a, b any) struct{ A, B any } { return struct{ A, B any }{a, b} },
	// showPrompt says whether a problem's words are worth showing above its
	// drawing: a word problem or a figure's question, not "47 + 38" again.
	"showPrompt": func(p *problem.Problem) bool {
		return p != nil && p.Answer.Type != problem.Comparison &&
			(p.Layout == problem.LayoutText || p.Layout == problem.LayoutFigure || p.Layout == "")
	},
	// tone turns a stored avatar name into a CSS class, never trusting it.
	"tone": func(avatar string) string {
		switch avatar {
		case "sun", "blue", "sky":
			return "tone-" + avatar
		}
		return "tone-sky"
	},
}

func (s *Server) newView(w http.ResponseWriter, r *http.Request, title string, d any) *view {
	v := &view{reqCtx: rc(r), Title: title, D: d, Version: buildinfo.Version, SourceURL: buildinfo.SourceURL(), Form: url.Values{}}
	v.FlashKind, v.Flash = s.takeFlash(w, r)
	if c := v.reqCtx; c.User != nil && c.User.IsParent() {
		v.Unread, _ = s.svc.UnreadNotifications(r.Context(), *c.User)
	}
	return v
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, v *view) {
	t, ok := s.pages[page]
	if !ok {
		s.log.Error("render", "err", fmt.Errorf("no template %q", page))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		s.log.Error("render", "page", page, "err", err)
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderStatus shows a plain message page (errors, expired forms).
func (s *Server) renderStatus(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	v := s.newView(w, r, title, msg)
	s.render(w, r, status, "status", v)
}
