package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/sim"
	"github.com/chinny-labs/zigrt/internal/web"
)

// rebuild recomputes topic_state and daily_totals from the logs (ADR 3).
func rebuild() error {
	cfg, log, err := setup()
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := service.New(st, log)
	if err := loadContent(svc, cfg, log); err != nil {
		return err
	}
	n, err := svc.Rebuild(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Rebuilt the progress of %d %s from the history.\n", n, plural(n, "student", "students"))
	return nil
}

// demo fills an empty database with a household and weeks of history.
func demo(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	days := fs.Int("days", 28, "days of history to simulate, ending today")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *days < 1 || *days > 365 {
		return errors.New("-days must be from 1 to 365")
	}
	cfg, log, err := setup()
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := service.New(st, log)
	if err := loadContent(svc, cfg, log); err != nil {
		return err
	}
	password := randomPassword(16)
	fmt.Printf("Simulating %d days for a demo household...\n", *days)
	login, err := sim.Demo(ctx, svc, cfg.Timezone, password, *days, time.Now(), uint64(time.Now().UnixNano())) //nolint:gosec // a demo seed, not a secret
	if err != nil {
		return err
	}
	fmt.Printf("\nDemo household ready.\n  Parent: %s, password %s\n", login.Email, login.Password)
	for _, s := range login.Students {
		fmt.Printf("  Student: %s, PIN %s\n", s.Name, s.PIN)
	}
	return nil
}

// preview renders a pack's lessons to static pages for authors (ADR 13).
func preview(args []string) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	dir := fs.String("pack", ".", "the pack folder or file")
	out := fs.String("out", "preview", "the folder to write the pages to")
	seeds := fs.Int("seeds", 3, "problems to draw from each template")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *seeds < 1 || *seeds > 50 {
		return errors.New("-seeds must be from 1 to 50")
	}
	p, err := content.Open(*dir)
	if err != nil {
		return err
	}
	index, err := web.Preview(p, *out, fs.Args(), *seeds)
	if err != nil {
		return err
	}
	fmt.Printf("Open %s in a browser.\n", index)
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// simulate plays a scripted student through a pack on a throwaway database
// and reports topics mastered, review load and topics never reached (plan
// 11.1). It is for tuning the rules and checking a pack before installing
// it; it never touches the real database.
func simulate(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	packPath := fs.String("pack", "", "a pack folder or file (default: the embedded pack)")
	profile := fs.String("profile", "forgetful", "the kind of student")
	days := fs.Int("days", 60, "days to simulate")
	seed := fs.Uint64("seed", 1, "seed, for a repeatable run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prof, ok := sim.ProfileNamed(*profile)
	if !ok {
		var names []string
		for _, p := range sim.Profiles {
			names = append(names, p.Name+" ("+p.Description+")")
		}
		return fmt.Errorf("no profile %q; choose from: %s", *profile, strings.Join(names, "; "))
	}
	if *days < 1 || *days > 730 {
		return errors.New("-days must be from 1 to 730")
	}
	var p *content.Pack
	var err error
	if *packPath == "" {
		p, err = pack.Embedded()
	} else {
		p, err = content.Open(*packPath)
	}
	if err != nil {
		return err
	}
	rep, err := sim.Simulate(context.Background(), p, prof, *days, *seed)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s, a %q student (%s), %d days:\n", p.Manifest.ID, p.Manifest.Version, prof.Name, prof.Description, rep.Days)
	fmt.Printf("  placed by the placement test  %d\n", rep.Placed)
	fmt.Printf("  mastered at the end           %d of %d", rep.Mastered, rep.Topics)
	var grades []string
	for _, g := range content.Grades {
		if n := rep.MasteredByGrade[g]; n > 0 {
			grades = append(grades, fmt.Sprintf("%s: %d", g, n))
		}
	}
	if len(grades) > 0 {
		fmt.Printf(" (%s)", strings.Join(grades, ", "))
	}
	fmt.Printf("\n  lessons %d, reviews %d (at most %d in a day), quizzes %d, XP %d\n", rep.Lessons, rep.Reviews, rep.ReviewsMax, rep.Quizzes, rep.XP)
	if len(rep.NeverReached) > 0 {
		fmt.Printf("  never reached (%d): %s\n", len(rep.NeverReached), strings.Join(rep.NeverReached, ", "))
	}
	return nil
}
