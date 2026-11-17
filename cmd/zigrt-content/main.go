// SPDX-License-Identifier: Apache-2.0

// Command zigrt-content is the authoring tool for Zigrt content packs (plan
// section 4.8). It uses the same problem and content packages as the app, so
// a template behaves the same here, in the curriculum CI and in a lesson.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/problem"
	"github.com/chinny-labs/zigrt/problem/expr"
)

const usage = `zigrt-content: check and build Zigrt content packs

Usage:
  zigrt-content [-pack DIR] lint [topic]           check structure, IDs, edges and template syntax
  zigrt-content [-pack DIR] check [-seeds N]       run every curriculum gate (CI runs this)
  zigrt-content [-pack DIR] sample TEMPLATE [-seeds 1-20] [-locale en]
                                                   print problems, answers and solutions
  zigrt-content [-pack DIR] graph [-strand S]      print the topic graph, flag problems
  zigrt-content [-pack DIR] ids [-write]           list every ID; -write adds them to released-ids.json
  zigrt-content [-pack DIR] build [-out dist]      build the pack file as the release job does
  zigrt-content funcs                              list the functions templates can use

-pack defaults to the current folder.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("zigrt-content", flag.ContinueOnError)
	global.SetOutput(stderr)
	global.Usage = func() { fmt.Fprint(stderr, usage) }
	dir := global.String("pack", ".", "the pack folder")
	if err := global.Parse(args); err != nil {
		return 2
	}
	if global.NArg() == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := global.Arg(0), global.Args()[1:]
	var err error
	switch cmd {
	case "funcs":
		for _, line := range expr.FuncDoc() {
			fmt.Fprintln(stdout, line)
		}
		return 0
	case "lint":
		err = lint(*dir, rest, stdout)
	case "check":
		err = check(*dir, rest, stdout)
	case "sample":
		err = sample(*dir, rest, stdout)
	case "graph":
		err = graph(*dir, rest, stdout)
	case "ids":
		err = ids(*dir, rest, stdout)
	case "build":
		err = build(*dir, rest, stdout)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "zigrt-content: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		var fe failures
		if errors.As(err, &fe) {
			for _, e := range fe {
				fmt.Fprintln(stderr, e)
			}
			fmt.Fprintf(stderr, "%d %s\n", len(fe), plural(len(fe), "problem", "problems"))
		} else {
			fmt.Fprintln(stderr, "zigrt-content:", err)
		}
		return 1
	}
	return 0
}

// failures is a list of problems to print one per line.
type failures []error

func (f failures) Error() string { return fmt.Sprintf("%d problems", len(f)) }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// open loads the pack, turning a joined load error into one line per problem.
func open(dir string) (*content.Pack, error) {
	p, err := content.Open(dir)
	if err != nil {
		var joined interface{ Unwrap() []error }
		if errors.As(err, &joined) {
			return nil, failures(joined.Unwrap())
		}
		return nil, err
	}
	return p, nil
}

func lint(dir string, args []string, out io.Writer) error {
	p, err := open(dir)
	if err != nil {
		var fe failures
		if errors.As(err, &fe) && len(args) == 1 {
			// Only the problems that mention the topic.
			var mine failures
			for _, e := range fe {
				if strings.Contains(e.Error(), args[0]) {
					mine = append(mine, e)
				}
			}
			if len(mine) == 0 {
				fmt.Fprintf(out, "%s: ok (the pack has %d other problems; run lint with no topic)\n", args[0], len(fe))
				return nil
			}
			return mine
		}
		return err
	}
	if len(args) == 1 {
		t := p.Topic(args[0])
		if t == nil {
			return fmt.Errorf("there is no topic %q", args[0])
		}
		n := 0
		for _, k := range t.KPs {
			n += len(k.Templates)
		}
		fmt.Fprintf(out, "%s: ok, %d knowledge points, %d templates\n", t.ID, len(t.KPs), n)
		return nil
	}
	fmt.Fprintf(out, "ok: %d topics, %d templates\n", len(p.Topics), len(p.Templates()))
	return nil
}

func check(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	seeds := fs.Int("seeds", problem.GateSeeds, "seeds to try per template")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := open(dir)
	if err != nil {
		return err
	}
	if errs := p.Gate(*seeds); len(errs) > 0 {
		return failures(errs)
	}
	fmt.Fprintf(out, "ok: %d topics and %d templates pass every gate at %d seeds\n", len(p.Topics), len(p.Templates()), *seeds)
	return nil
}

// parseSeeds reads "7" or "1-20".
func parseSeeds(s string) (from, to uint64, err error) {
	a, b, isRange := strings.Cut(s, "-")
	if from, err = strconv.ParseUint(a, 10, 64); err != nil {
		return 0, 0, fmt.Errorf("seeds %q: write a number or a range like 1-20", s)
	}
	to = from
	if isRange {
		if to, err = strconv.ParseUint(b, 10, 64); err != nil || to < from {
			return 0, 0, fmt.Errorf("seeds %q: write a number or a range like 1-20", s)
		}
	}
	if to-from >= 1000 {
		return 0, 0, errors.New("sample at most 1000 seeds at a time")
	}
	return from, to, nil
}

func sample(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sample", flag.ContinueOnError)
	seeds := fs.String("seeds", "1-5", "a seed or a range of seeds")
	locale := fs.String("locale", problem.DefaultLocale, "the locale to show")
	// Allow the template before or after the flags.
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" && fs.NArg() == 1 {
		id = fs.Arg(0)
	}
	if id == "" {
		return errors.New("sample needs a template ID")
	}
	from, to, err := parseSeeds(*seeds)
	if err != nil {
		return err
	}
	p, err := open(dir)
	if err != nil {
		return err
	}
	tpl := p.Template(id)
	if tpl == nil {
		return fmt.Errorf("there is no template %q", id)
	}
	for seed := from; seed <= to; seed++ {
		pr, err := tpl.Generate(seed, *locale)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "seed %d\n  %s\n", seed, pr.Prompt)
		if show := layout(pr); show != "" {
			fmt.Fprintf(out, "  [%s]\n", show)
		}
		for i, c := range pr.Choices {
			mark := " "
			if i == pr.Answer.Choice {
				mark = "*"
			}
			fmt.Fprintf(out, "  %s %d. %s\n", mark, i, c)
		}
		fmt.Fprintf(out, "  answer: %s\n", pr.Answer.Text(pr))
		for i, s := range pr.Solution {
			fmt.Fprintf(out, "  %d. %s\n", i+1, s)
		}
		fmt.Fprintln(out)
	}
	return nil
}

// layout describes what the problem draws besides its prompt.
func layout(p *problem.Problem) string {
	switch p.Layout {
	case problem.LayoutAcross, problem.LayoutStacked:
		return fmt.Sprintf("%s: %s %s %s", p.Layout, p.Parts[0], p.Op, p.Parts[1])
	case problem.LayoutLongDivision:
		return fmt.Sprintf("%s: %s ) %s", p.Layout, p.Parts[1], p.Parts[0])
	case problem.LayoutMissing:
		parts := slices.Clone(p.Parts)
		parts[p.Blank] = "□"
		return fmt.Sprintf("%s: %s %s %s = %s", p.Layout, parts[0], p.Op, parts[1], parts[2])
	case problem.LayoutFigure:
		keys := make([]string, 0, len(p.FigArgs))
		for k := range p.FigArgs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		var kv []string
		for _, k := range keys {
			kv = append(kv, k+"="+p.FigArgs[k])
		}
		return fmt.Sprintf("figure %s: %s", p.Figure, strings.Join(kv, " "))
	}
	return ""
}

// longChain is the prerequisite depth graph flags as worth a look.
const longChain = 15

func graph(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	strand := fs.String("strand", "", "only this strand")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := open(dir)
	if err != nil {
		return err
	}
	depth := map[string]int{}
	var depthOf func(id string) int
	depthOf = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		d := 1
		for _, r := range p.Topic(id).Requires {
			d = max(d, depthOf(r)+1)
		}
		depth[id] = d
		return d
	}
	inCourse := map[string]bool{}
	for _, c := range p.Manifest.Courses {
		for _, t := range p.CourseTopics(c.ID) {
			inCourse[t.ID] = true
		}
	}
	var flags []string
	for _, g := range content.Grades {
		var lines []string
		for _, t := range p.Topics {
			if t.Grade != g || (*strand != "" && t.Strand != *strand) {
				continue
			}
			line := fmt.Sprintf("  %-28s %-14s depth %d", t.ID, t.Strand, depthOf(t.ID))
			if len(t.Requires) > 0 {
				line += "  ← " + strings.Join(t.Requires, ", ")
			}
			lines = append(lines, line)
			if !inCourse[t.ID] {
				flags = append(flags, fmt.Sprintf("%s is in no course, so no student can reach it", t.ID))
			}
			if depthOf(t.ID) > longChain {
				flags = append(flags, fmt.Sprintf("%s sits at the end of a chain of %d prerequisites", t.ID, depthOf(t.ID)))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(out, "Grade %s\n%s\n", g, strings.Join(lines, "\n"))
		}
	}
	for _, f := range flags {
		fmt.Fprintln(out, "flag:", f)
	}
	return nil
}

func ids(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ids", flag.ContinueOnError)
	write := fs.Bool("write", false, "add the IDs to released-ids.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := open(dir)
	if err != nil {
		return err
	}
	if !*write {
		for _, id := range p.IDs() {
			fmt.Fprintln(out, id)
		}
		return nil
	}
	all := slices.Concat(p.Released, p.IDs())
	slices.Sort(all)
	all = slices.Compact(all)
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "released-ids.json"), append(b, '\n'), 0o644); err != nil { //nolint:gosec // a public file in the pack
		return err
	}
	fmt.Fprintf(out, "released-ids.json: %d IDs (%d new)\n", len(all), len(all)-len(p.Released))
	return nil
}

func build(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	outDir := fs.String("out", "dist", "the folder to write to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := open(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		return err
	}
	name := filepath.Join(*outDir, fmt.Sprintf("%s-%s.zip", p.Manifest.ID, p.Manifest.Version))
	f, err := os.Create(name) //nolint:gosec // the path is the author's own
	if err != nil {
		return err
	}
	sum, err := content.Build(os.DirFS(dir), f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s  %s\n", sum, filepath.Base(name))
	if err := os.WriteFile(name+".sha256", []byte(line), 0o644); err != nil { //nolint:gosec // a public checksum
		return err
	}
	fmt.Fprint(out, line)
	return nil
}
