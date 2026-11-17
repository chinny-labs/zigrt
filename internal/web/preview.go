package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Preview writes a pack's lessons as static pages, drawn by the app's own
// templates, CSS and figures, for authors to open from disk (`zigrt
// preview`, ADR 13). For each topic it writes every knowledge point's
// explanation with its worked example, and seeds problems of every
// template, before and after answering. It returns the index page's path.
func Preview(p *content.Pack, out string, topicIDs []string, seeds int) (string, error) {
	pages, err := parseTemplates()
	if err != nil {
		return "", err
	}
	svc := service.New(nil, nil)
	svc.SetPack(p)
	s := &Server{svc: svc, pages: pages}

	topics := p.Topics
	if len(topicIDs) > 0 {
		topics = nil
		for _, id := range topicIDs {
			t := p.Topic(id)
			if t == nil {
				return "", fmt.Errorf("no topic %q in the pack", id)
			}
			topics = append(topics, t)
		}
	}
	if err := copyStatic(filepath.Join(out, "static")); err != nil {
		return "", err
	}

	type link struct{ Href, Text string }
	type topicLinks struct {
		Topic *content.Topic
		Href  string
		Links []link
	}
	var index []topicLinks
	for _, t := range topics {
		dir := filepath.Join(out, t.ID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", err
		}
		tl := topicLinks{Topic: t, Href: t.ID + "/index.html"}
		for k, kp := range t.KPs {
			base := service.TaskView{Task: store.Task{ID: "preview", Kind: store.TaskLesson, TopicID: t.ID},
				Topic: t, KP: kp, KPIndex: k, KPCount: len(t.KPs)}
			ex, err := kp.ExampleTemplate().Generate(1, service.Locale)
			if err != nil {
				return "", err
			}
			tv := base
			tv.Phase, tv.Example = service.PhaseExplain, ex
			name := fmt.Sprintf("kp%d-explain.html", k+1)
			if err := s.writePreview(filepath.Join(dir, name), tv, 1000); err != nil {
				return "", err
			}
			tl.Links = append(tl.Links, link{name, fmt.Sprintf("Step %d: the explanation", k+1)})
			for _, tpl := range kp.Templates {
				for seed := 1; seed <= seeds; seed++ {
					pr, err := tpl.Generate(uint64(seed), service.Locale)
					if err != nil {
						return "", err
					}
					tv := base
					tv.Phase, tv.Problem, tv.AttemptID = service.PhaseProblem, pr, "preview"
					q := fmt.Sprintf("%s-seed%d.html", tpl.ID, seed)
					if err := s.writePreview(filepath.Join(dir, q), tv, 0); err != nil {
						return "", err
					}
					tv.Phase, tv.Problem = service.PhaseFeedback, nil
					tv.Feedback = &service.Feedback{Problem: pr, Given: pr.Wrong(), Answer: pr.Answer.Text(pr)}
					a := fmt.Sprintf("%s-seed%d-answer.html", tpl.ID, seed)
					if err := s.writePreview(filepath.Join(dir, a), tv, 0); err != nil {
						return "", err
					}
					tl.Links = append(tl.Links, link{q, fmt.Sprintf("%s, seed %d", tpl.ID, seed)}, link{a, "the worked solution"})
				}
			}
		}
		if err := writeIndex(filepath.Join(dir, "index.html"), "../", t.Title.In(service.Locale), tl.Links); err != nil {
			return "", err
		}
		index = append(index, tl)
	}
	var top []link
	for _, tl := range index {
		top = append(top, link{tl.Href, fmt.Sprintf("%s (grade %s, %s)", tl.Topic.Title.In(service.Locale), tl.Topic.Grade, tl.Topic.ID)})
	}
	path := filepath.Join(out, "index.html")
	return path, writeIndex(path, "", p.Manifest.Title.In(service.Locale)+" "+p.Manifest.Version, top)
}

// writePreview renders one task screen to a file, with links made relative
// and the page's scripts left out (nothing posts anywhere).
func (s *Server) writePreview(path string, tv service.TaskView, step int) error {
	d := s.taskData(tv, step)
	v := &view{reqCtx: &reqCtx{CSRF: "preview"}, Title: d.Title, Shell: "task", D: d, Form: url.Values{}}
	var buf bytes.Buffer
	if err := s.pages["task"].ExecuteTemplate(&buf, "layout", v); err != nil {
		return err
	}
	html := strings.ReplaceAll(buf.String(), `"/static/`, `"../static/`)
	html = strings.ReplaceAll(html, `href="/"`, `href="index.html"`)
	html = strings.ReplaceAll(html, `<script src="../static/app.js" defer></script>`, "")
	return os.WriteFile(path, []byte(html), 0o644) //nolint:gosec // a page for the author to open
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · preview</title><link rel="stylesheet" href="{{.Up}}static/app.css"></head>
<body class="shell-bare"><div class="page"><main><div class="column stack">
<h1>{{.Title}}</h1>
{{if .Up}}<a class="quiet-link" href="{{.Up}}index.html">All topics</a>{{end}}
<div class="card stack-sm">{{range .Links}}<a class="rowlink" href="{{.Href}}">{{.Text}}</a>{{end}}</div>
</div></main></div></body></html>
`))

func writeIndex(path, up, title string, links any) error {
	var buf bytes.Buffer
	if err := indexTmpl.Execute(&buf, map[string]any{"Up": up, "Title": title, "Links": links}); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644) //nolint:gosec // a page for the author to open
}

// copyStatic writes the app's CSS and fonts (not its scripts) to dir.
func copyStatic(dir string) error {
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	return fs.WalkDir(static, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(name, ".js") {
			return err
		}
		b, err := fs.ReadFile(static, name)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644) //nolint:gosec // public assets
	})
}
