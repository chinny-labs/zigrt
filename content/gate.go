// SPDX-License-Identifier: Apache-2.0

package content

import (
	"fmt"
	"io/fs"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/chinny-labs/zigrt/problem"
)

// IDs lists every topic, knowledge point and template ID in the pack, sorted.
// A release appends them to released-ids.json.
func (p *Pack) IDs() []string {
	out := make([]string, 0, len(p.ids))
	for id, what := range p.ids {
		if what != "strand" {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// Templates lists every template in teaching order.
func (p *Pack) Templates() []*problem.Template {
	var out []*problem.Template
	for _, t := range p.Topics {
		for _, k := range t.KPs {
			out = append(out, k.Templates...)
		}
	}
	return out
}

// maxSentenceWords is the longest sentence an explanation may have, by
// grade: short for the youngest readers.
var maxSentenceWords = map[string]int{"K": 10, "1": 12, "2": 14, "3": 16, "4": 18, "5": 20}

// Gate runs the gates of plan section 4.6 that Load does not: ids, generate,
// answers, solutions, readability and subresources. seeds is how many seeds
// to try per template (problem.GateSeeds in CI).
func (p *Pack) Gate(seeds int) []error {
	var errs []error
	errs = append(errs, p.gateIDs()...)
	errs = append(errs, p.gateTemplates(seeds)...)
	errs = append(errs, p.gateReadability()...)
	errs = append(errs, p.gateSubresources()...)
	for _, name := range p.Figures {
		if _, err := p.StaticFigure(name); err != nil {
			errs = append(errs, fmt.Errorf("subresources: figures/%s.svg: %w", name, err))
		}
	}
	return errs
}

func (p *Pack) gateIDs() []error {
	var errs []error
	moved := map[string]bool{}
	for _, tr := range p.Transitions {
		moved[tr.From] = true
	}
	for _, id := range p.Released {
		if _, ok := p.ids[id]; !ok && !moved[id] {
			errs = append(errs, fmt.Errorf("ids: %s was released and is gone; add a transition, or put it back", id))
		}
	}
	return errs
}

// GateTemplates runs the template gates on some templates, in parallel.
func GateTemplates(tpls []*problem.Template, seeds int) []error {
	results := make([][]error, len(tpls))
	var wg sync.WaitGroup
	work := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range work {
				results[i] = tpls[i].Gate(seeds)
			}
		})
	}
	for i := range tpls {
		work <- i
	}
	close(work)
	wg.Wait()
	var errs []error
	for _, r := range results {
		errs = append(errs, r...)
	}
	return errs
}

func (p *Pack) gateTemplates(seeds int) []error {
	return GateTemplates(p.Templates(), seeds)
}

var sentenceEnd = regexp.MustCompile(`[.!?]+(\s|$)`)

func (p *Pack) gateReadability() []error {
	var errs []error
	for _, t := range p.Topics {
		limit := maxSentenceWords[t.Grade]
		for _, k := range t.KPs {
			for loc, text := range k.Explain {
				for _, s := range sentenceEnd.Split(text, -1) {
					if n := len(strings.Fields(s)); n > limit {
						errs = append(errs, fmt.Errorf("readability: %s (%s): a sentence has %d words; grade %s allows %d: %q", k.ID, loc, n, t.Grade, limit, strings.TrimSpace(s)))
					}
				}
			}
		}
	}
	return errs
}

var (
	urlRE   = regexp.MustCompile(`(?i)\b(https?|ftp)://|\bwww\.`)
	xmlnsRE = regexp.MustCompile(`xmlns(:\w+)?\s*=\s*"[^"]*"`)
	linkRE  = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*["'][^#"']`)
)

func (p *Pack) gateSubresources() []error {
	var errs []error
	// Only what Build puts in the pack file is checked, so a pack folder
	// inside a git checkout (.git/config holds the remote's URL) passes.
	_ = fs.WalkDir(p.fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !packFiles(name, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || name == "manifest.yaml" || strings.HasPrefix(d.Name(), "LICENSE") || strings.HasSuffix(name, ".md") {
			return nil
		}
		b, err := fs.ReadFile(p.fsys, name)
		if err != nil {
			return err
		}
		text := xmlnsRE.ReplaceAllString(string(b), "")
		if urlRE.MatchString(text) || linkRE.MatchString(text) {
			errs = append(errs, fmt.Errorf("subresources: %s links outside the pack; everything must ship in it", name))
		}
		return nil
	})
	return errs
}
