package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/auth"
	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/store"
)

// Running the household (P5): its settings, its parents, the lessons in use,
// backups and export.

// Backups is the database's own backup, when it has one (SQLite). With
// Postgres the operator backs up, and Service.Backups is nil.
type Backups interface {
	// Now takes a snapshot and returns where it went.
	Now(ctx context.Context) (string, error)
	// Latest is when the newest snapshot was taken, and how many are kept.
	Latest() (*time.Time, int, error)
}

// HouseholdView is the household page.
type HouseholdView struct {
	Household   store.Household
	Parents     []store.User
	Pack        PackInfo
	Backups     bool // the app takes its own backups (SQLite)
	LastBackup  *time.Time
	BackupsKept int
	Engine      string
	AppVersion  string
}

// PackInfo is the content pack in use.
type PackInfo struct {
	Title, Version, Source string // Source: embedded, uploaded or the content folder
	Topics                 int
	Uploaded               []store.ContentPackRecord
}

// Household is the household page for a parent.
func (s *Service) Household(ctx context.Context, parent store.User) (HouseholdView, error) {
	var v HouseholdView
	if err := requireParent(parent); err != nil {
		return v, err
	}
	var err error
	if v.Household, err = s.Store.GetHousehold(ctx); err != nil {
		return v, err
	}
	if v.Parents, err = s.Store.ListUsers(ctx, store.RoleParent); err != nil {
		return v, err
	}
	if p := s.Pack(); p != nil {
		v.Pack = PackInfo{Title: p.Manifest.Title.In(Locale), Version: p.Manifest.Version, Topics: len(p.Topics), Source: s.PackSource}
		if v.Pack.Source == "" {
			v.Pack.Source = "embedded"
		}
	}
	if v.Pack.Uploaded, err = s.Store.ListContentPacks(ctx); err != nil {
		return v, err
	}
	v.Engine, v.AppVersion = s.Store.Dialect(), s.AppVersion
	if s.Backups != nil {
		v.Backups = true
		if v.LastBackup, v.BackupsKept, err = s.Backups.Latest(); err != nil {
			return v, err
		}
	}
	return v, nil
}

// UpdateHousehold renames the household and sets its time zone.
func (s *Service) UpdateHousehold(ctx context.Context, parent store.User, name, timezone string) error {
	if err := requireParent(parent); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return invalid("name", "Give the household a name of up to 60 characters.")
	}
	tz, err := cleanTimezone(timezone)
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, "", func(tx store.Tx) error {
		h, err := tx.GetHousehold(ctx)
		if err != nil {
			return err
		}
		h.Name, h.Timezone = name, tz
		if err := tx.UpdateHousehold(ctx, h); err != nil {
			return err
		}
		return s.audit(ctx, tx, parent.ID, "household.update", h.ID, map[string]any{"timezone": tz})
	})
}

// ChangePassword sets a parent's own password, given the current one. Their
// other sessions end.
func (s *Service) ChangePassword(ctx context.Context, parent store.User, current, next string) error {
	if err := requireParent(parent); err != nil {
		return err
	}
	u, err := s.Store.GetUser(ctx, parent.ID)
	if err != nil {
		return err
	}
	if !auth.Verify(current, u.PasswordHash) {
		return invalid("current", "That isn't your current password.")
	}
	return s.ResetPassword(ctx, &parent, u.Email, next)
}

// BackupNow takes a snapshot now, from the household page.
func (s *Service) BackupNow(ctx context.Context, parent store.User) (string, error) {
	if err := requireParent(parent); err != nil {
		return "", err
	}
	if s.Backups == nil {
		return "", errors.New("this Zigrt uses Postgres: back it up with your own tools")
	}
	path, err := s.Backups.Now(ctx)
	if err != nil {
		return "", err
	}
	return path, s.Store.Tx(ctx, "", func(tx store.Tx) error {
		return s.audit(ctx, tx, parent.ID, "household.backup", "", map[string]any{"file": filepath.Base(path)})
	})
}

// ExportAll writes the whole household as JSON Lines (plan 9.1): the same
// file `zigrt export` writes, which `zigrt import` loads on any engine.
func (s *Service) ExportAll(ctx context.Context, parent store.User, w io.Writer) error {
	if err := requireParent(parent); err != nil {
		return err
	}
	if err := s.Store.Tx(ctx, "", func(tx store.Tx) error {
		return s.audit(ctx, tx, parent.ID, "household.export", "", nil)
	}); err != nil {
		return err
	}
	return s.Store.Export(ctx, w, store.ExportHeader{App: s.AppVersion, ExportedAt: s.now()})
}

// MaxPackSize bounds an uploaded pack file.
const MaxPackSize = 16 << 20

// InstallPack checks an uploaded content pack and, if it is a newer
// release of the same curriculum that this Zigrt can run and that passes
// every gate, saves it to the content folder and switches to it at once.
func (s *Service) InstallPack(ctx context.Context, parent store.User, data []byte) (*content.Pack, error) {
	if err := requireParent(parent); err != nil {
		return nil, err
	}
	if len(data) > MaxPackSize {
		return nil, invalid("pack", "That file is too big to be a content pack.")
	}
	p, err := content.LoadFile(data)
	if err != nil {
		first, _, _ := strings.Cut(err.Error(), "\n")
		return nil, invalid("pack", "That isn't a content pack Zigrt can read: %s", first)
	}
	if cur := s.Pack(); cur != nil {
		if p.Manifest.ID != cur.Manifest.ID {
			return nil, invalid("pack", "That pack is %q; this Zigrt runs %q.", p.Manifest.ID, cur.Manifest.ID)
		}
		if !pack.Newer(p.Manifest.Version, cur.Manifest.Version) {
			return nil, invalid("pack", "Version %s is running already; that file is %s. Only a newer pack can be installed.",
				cur.Manifest.Version, p.Manifest.Version)
		}
	}
	if err := p.CheckApp(s.AppVersion); err != nil {
		return nil, invalid("pack", "%s. Upgrade Zigrt first.", capitalize(err.Error()))
	}
	if errs := p.Gate(100); len(errs) > 0 {
		return nil, invalid("pack", "That pack fails its checks (%d problems), starting with: %v", len(errs), errs[0])
	}
	if s.ContentDir == "" {
		return nil, errors.New("no content folder is set (ZIGRT_CONTENT_DIR)")
	}
	if err := os.MkdirAll(s.ContentDir, 0o750); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%s-%s.zip", p.Manifest.ID, p.Manifest.Version)
	tmp, err := os.CreateTemp(s.ContentDir, ".upload-*")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.ContentDir, name)); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	sum := sha256.Sum256(data)
	err = s.Store.Tx(ctx, "", func(tx store.Tx) error {
		if err := tx.PutContentPack(ctx, store.ContentPackRecord{Version: p.Manifest.Version, SHA256: hex.EncodeToString(sum[:]),
			File: name, InstalledBy: parent.ID, InstalledAt: s.now()}); err != nil {
			return err
		}
		return s.audit(ctx, tx, parent.ID, "household.pack", "", map[string]any{"version": p.Manifest.Version, "file": name})
	})
	if err != nil {
		return nil, err
	}
	s.SetPack(p)
	s.PackSource = "uploaded"
	return p, s.NotePack(ctx)
}
