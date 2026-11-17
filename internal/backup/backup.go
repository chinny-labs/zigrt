// Package backup runs Zigrt's nightly SQLite snapshots (plan §10.3):
// once a day at ZIGRT_BACKUP_SCHEDULE (household-agnostic, in TZ), a
// VACUUM INTO snapshot lands in /data/backups and only the newest
// ZIGRT_BACKUP_RETAIN are kept. Copy that folder off the box: a backup on
// the same disk is not a backup.
package backup

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
)

// Nightly is the scheduler job.
type Nightly struct {
	Store  *store.SQL
	Dir    string
	At     time.Duration // time of day, e.g. 3h15m
	Retain int
	Loc    *time.Location
	Now    func() time.Time
	Log    *slog.Logger
}

// Dir returns the backup folder next to a SQLite database file.
func Dir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "backups") }

// Run takes today's snapshot once the scheduled time has passed, if it
// doesn't exist yet. Missing the time (the box was off) just means it runs on
// the next tick.
func (n *Nightly) Run(ctx context.Context) error {
	now := n.Now().In(n.Loc)
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, n.Loc)
	if now.Before(midnight.Add(n.At)) {
		return nil
	}
	path := filepath.Join(n.Dir, sqlite.NightlyName(midnight))
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	start := time.Now()
	if err := sqlite.Backup(ctx, n.Store, path); err != nil {
		return err
	}
	removed, err := sqlite.Prune(n.Dir, n.Retain)
	n.Log.Info("backup written", "path", path, "dur_ms", time.Since(start).Milliseconds(), "pruned", len(removed))
	return err
}

// OnDemand is "back up now" and the latest-backup line on the household
// page, for SQLite.
type OnDemand struct {
	Store *store.SQL
	Dir   string
}

// Now writes a snapshot named for this moment.
func (o *OnDemand) Now(ctx context.Context) (string, error) {
	path := filepath.Join(o.Dir, sqlite.OnDemandName(time.Now().UTC()))
	return path, sqlite.Backup(ctx, o.Store, path)
}

// Latest reports when the newest snapshot was written and how many there
// are.
func (o *OnDemand) Latest() (*time.Time, int, error) {
	entries, err := os.ReadDir(o.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var latest *time.Time
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, sqlite.BackupPrefix) || !strings.HasSuffix(name, ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, 0, err
		}
		n++
		if t := info.ModTime(); latest == nil || t.After(*latest) {
			latest = &t
		}
	}
	return latest, n, nil
}
