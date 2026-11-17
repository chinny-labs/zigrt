package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/chinny-labs/zigrt/content"
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
