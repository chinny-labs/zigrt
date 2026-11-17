// Command zigrt is the whole app: one binary, one container (plan §9).
//
//	zigrt [serve]                  run the web app (default)
//	zigrt migrate                  apply database migrations and exit
//	zigrt healthcheck              exit 0 if the local server answers /healthz
//	zigrt reset-password <email>   set a new parent password
//	zigrt backup                   snapshot the SQLite database now
//	zigrt rebuild                  recompute progress from the history
//	zigrt demo [-days n]           fill an empty database with a demo household
//	zigrt preview [-pack dir] [topic...]  render a pack's lessons to static pages
//	zigrt simulate [-profile p] [-days n]  play a scripted student through a pack
//	zigrt version                  print the version
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/backup"
	"github.com/chinny-labs/zigrt/internal/buildinfo"
	"github.com/chinny-labs/zigrt/internal/config"
	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/scheduler"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/engine"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
	"github.com/chinny-labs/zigrt/internal/web"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = migrate()
	case "healthcheck":
		err = healthcheck()
	case "reset-password":
		err = resetPassword(args)
	case "backup":
		err = backupNow()
	case "rebuild":
		err = rebuild()
	case "demo":
		err = demo(args)
	case "preview":
		err = preview(args)
	case "simulate":
		err = simulate(args)
	case "version", "--version", "-v":
		fmt.Printf("zigrt %s (%s)\n", buildinfo.Version, buildinfo.ShortCommit())
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "zigrt:", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: zigrt [command]

  serve                          run the web app (default)
  migrate                        apply database migrations and exit
  healthcheck                    exit 0 if the local server answers /healthz
  reset-password <email>         set a new parent password (prints one, or reads --password-stdin)
  backup                         snapshot the SQLite database into the backups folder now
  rebuild                        recompute every student's progress from the history
  demo [-days 28]                fill an empty database with a demo household and its history
  preview [-pack .] [-out preview] [-seeds 3] [topic...]
                                 render a content pack's lessons to static pages, for authors
  simulate [-pack file] [-profile forgetful] [-days 60] [-seed 1]
                                 play a scripted student through a pack on a throwaway database
  version                        print the version
`)
}

func setup() (config.Config, *slog.Logger, error) {
	cfg, err := config.Load(config.DefaultEnvFile)
	if err != nil {
		return cfg, nil, err
	}
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if os.Getenv("ZIGRT_DEBUG") != "" {
		opts.Level = slog.LevelDebug
	}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return cfg, slog.New(h), nil
}

func open(ctx context.Context, cfg config.Config, log *slog.Logger) (*store.SQL, error) {
	st, err := engine.Open(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", engine.Redact(cfg.DatabaseURL), err)
	}
	return st, nil
}

func serve() error {
	cfg, log, err := setup()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting zigrt", "version", buildinfo.Version, "commit", buildinfo.Commit,
		"database", engine.Redact(cfg.DatabaseURL), "listen", cfg.Listen)
	if cfg.InsecureCookies {
		log.Warn("ZIGRT_INSECURE_COOKIES is on: session cookies work over plain HTTP. Use only for local testing.")
	}
	if cfg.SecretKey == nil {
		log.Info("ZIGRT_SECRET_KEY is not set; using a generated key kept in the database")
	}

	st, err := open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := service.New(st, log)
	contentErr := loadContent(svc, cfg, log)

	token := ""
	if need, err := svc.NeedsSetup(ctx); err != nil {
		return err
	} else if need {
		token = auth.NewToken()[:24]
		log.Warn("first run: open /setup and enter this one-time setup token", "setup_token", token)
		fmt.Fprintf(os.Stderr, "\n  Zigrt setup token: %s\n  Open /setup in a browser and paste it to create the first parent account.\n\n", token)
	}

	srv, err := web.New(ctx, svc, cfg, log, token)
	if err != nil {
		return err
	}
	srv.SetContentError(contentErr)

	sched := &scheduler.Scheduler{Interval: time.Minute, Log: log, Jobs: []scheduler.Job{
		{Name: "housekeeping", Run: hourly(func(ctx context.Context) error {
			n, err := svc.SweepSessions(ctx)
			if n > 0 {
				log.Info("removed dead sessions", "count", n)
			}
			return err
		})},
	}}
	if path, err := sqlite.PathFromURL(cfg.DatabaseURL); err == nil {
		at, _ := config.ParseClock(cfg.BackupSchedule)
		loc, _ := time.LoadLocation(cfg.Timezone)
		nightly := &backup.Nightly{Store: st, Dir: backup.Dir(path), At: at, Retain: cfg.BackupRetain, Loc: loc, Now: time.Now, Log: log}
		sched.Jobs = append(sched.Jobs, scheduler.Job{Name: "backup", Run: nightly.Run})
		log.Info("nightly backups on", "dir", nightly.Dir, "at", cfg.BackupSchedule, "retain", cfg.BackupRetain)
	} else {
		log.Info("built-in backups are for SQLite; back up Postgres with pg_dump or your operator")
	}
	go sched.Run(ctx)

	hs := &http.Server{
		Addr: cfg.Listen, Handler: srv,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Listen)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadContent loads the embedded content pack, or a newer one from the
// content folder. It returns why no pack could be loaded, which keeps
// /readyz failing (plan 4.7).
func loadContent(svc *service.Service, cfg config.Config, log *slog.Logger) error {
	found, err := pack.Select(cfg.ContentDir, buildinfo.Version)
	for _, s := range found.Skip {
		log.Warn("content pack skipped", "reason", s)
	}
	if err != nil {
		log.Error("content pack not loaded: the app will not report ready", "err", err)
		return err
	}
	svc.SetPack(found.Pack)
	log.Info("content loaded", "pack", found.Pack.Manifest.ID, "version", found.Pack.Manifest.Version,
		"from", found.Source, "topics", len(found.Pack.Topics))
	return nil
}

// hourly wraps a job so it runs on the first tick and then once an hour,
// whatever the scheduler's own interval.
func hourly(run func(context.Context) error) func(context.Context) error {
	var last time.Time
	return func(ctx context.Context) error {
		if time.Since(last) < time.Hour {
			return nil
		}
		last = time.Now()
		return run(ctx)
	}
}

func migrate() error {
	cfg, log, err := setup()
	if err != nil {
		return err
	}
	st, err := open(context.Background(), cfg, log) // opening applies migrations
	if err != nil {
		return err
	}
	log.Info("migrations applied", "database", engine.Redact(cfg.DatabaseURL))
	return st.Close()
}

// healthcheck is for Docker's HEALTHCHECK: distroless has no curl.
func healthcheck() error {
	cfg, err := config.Load(config.DefaultEnvFile)
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return fmt.Errorf("ZIGRT_LISTEN: %w", err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", resp.Status)
	}
	return nil
}

func resetPassword(args []string) error {
	const use = "usage: zigrt reset-password [--password-stdin] <email>"
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	fromStdin := fs.Bool("password-stdin", false, "read the new password from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New(use)
	}
	email := fs.Arg(0)
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
	var pw string
	if *fromStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		pw = strings.TrimRight(line, "\r\n")
	} else {
		pw = randomPassword(20)
	}
	if err := service.New(st, log).ResetPassword(ctx, nil, email, pw); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no parent has the email %q", email)
		}
		return err
	}
	if *fromStdin {
		fmt.Printf("Password updated for %s.\n", email)
	} else {
		fmt.Printf("New password for %s: %s\n", email, pw)
	}
	return nil
}

func randomPassword(n int) string {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b strings.Builder
	for range n {
		i, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		b.WriteByte(alphabet[i.Int64()])
	}
	return b.String()
}

func backupNow() error {
	cfg, log, err := setup()
	if err != nil {
		return err
	}
	path, err := sqlite.PathFromURL(cfg.DatabaseURL)
	if err != nil {
		return errors.New("built-in backups are for SQLite; back up Postgres with pg_dump or your operator")
	}
	ctx := context.Background()
	st, err := open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()
	dest := filepath.Join(backup.Dir(path), sqlite.OnDemandName(time.Now()))
	if err := sqlite.Backup(ctx, st, dest); err != nil {
		return err
	}
	fmt.Println(dest)
	return nil
}
