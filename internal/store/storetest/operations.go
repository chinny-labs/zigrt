package storetest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/content"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// packFile builds the prototype pack as a file with another version and
// minimum app version, or another ID.
func packFile(t *testing.T, id, version, minApp string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(file), "..", "..", "..", "content", "testdata", "hardest")
	dir := t.TempDir()
	must(t, os.CopyFS(dir, os.DirFS(src)))
	m, err := os.ReadFile(filepath.Join(dir, "manifest.yaml")) //nolint:gosec // a test's own temporary copy
	must(t, err)
	s := string(m)
	s = strings.Replace(s, "id: zigrt-hardest", "id: "+id, 1)
	s = strings.Replace(s, "version: 2026.11.0", "version: "+version, 1)
	s = strings.Replace(s, "min_app_version: 0.1.0", "min_app_version: "+minApp, 1)
	must(t, os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(s), 0o600)) //nolint:gosec // a test's own temporary copy
	var b bytes.Buffer
	_, err = content.Build(os.DirFS(dir), &b)
	must(t, err)
	return b.Bytes()
}

type fakeBackups struct{ taken int }

func (f *fakeBackups) Now(context.Context) (string, error) {
	f.taken++
	return "/data/backups/zigrt-x.db", nil
}

func (f *fakeBackups) Latest() (*time.Time, int, error) {
	t := time.Date(2026, 3, 7, 3, 15, 0, 0, time.UTC)
	return &t, f.taken, nil
}

// testHouseholdPage covers the household page's actions: settings, a
// password change, backups, export, and installing a newer pack.
func testHouseholdPage(t *testing.T, open Opener) {
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)
	svc := f.Svc
	svc.AppVersion, svc.ContentDir, svc.PackSource = "0.4.0", t.TempDir(), "embedded"
	must(t, svc.NotePack(ctx))

	var ue *service.UserError
	h, err := svc.Household(ctx, f.Sam)
	must(t, err)
	if len(h.Parents) != 2 || h.Pack.Version != "2026.11.0" || h.Pack.Source != "embedded" || h.Backups || h.AppVersion != "0.4.0" {
		t.Fatalf("household %+v", h)
	}
	if err := svc.UpdateHousehold(ctx, f.Sam, "The Chins", "Mars/Base"); !errors.As(err, &ue) {
		t.Fatalf("bad time zone: %v", err)
	}
	must(t, svc.UpdateHousehold(ctx, f.Sam, "The Chins", "America/Los_Angeles"))
	if h, _ = svc.Household(ctx, f.Sam); h.Household.Name != "The Chins" || h.Household.Timezone != "America/Los_Angeles" {
		t.Fatalf("after update %+v", h.Household)
	}

	if err := svc.ChangePassword(ctx, f.Sam, "wrong", "a brand new password"); !errors.As(err, &ue) {
		t.Fatalf("wrong current password: %v", err)
	}
	must(t, svc.ChangePassword(ctx, f.Sam, f.SamPassword, "a brand new password"))
	if _, err := svc.LoginParent(ctx, "sam@example.com", "a brand new password", "test"); err != nil {
		t.Fatalf("sign in with the new password: %v", err)
	}

	if _, err := svc.BackupNow(ctx, f.Sam); err == nil {
		t.Fatal("a backup with no backups")
	}
	fb := &fakeBackups{}
	svc.Backups = fb
	_, err = svc.BackupNow(ctx, f.Sam)
	must(t, err)
	if h, _ = svc.Household(ctx, f.Sam); !h.Backups || h.LastBackup == nil || h.BackupsKept != 1 {
		t.Fatalf("backups %+v", h)
	}

	var dump bytes.Buffer
	must(t, svc.ExportAll(ctx, f.Sam, &dump))
	if !strings.HasPrefix(dump.String(), `{"zigrt_export":1,"app":"0.4.0"`) || !strings.Contains(dump.String(), `"t":"users"`) {
		t.Fatalf("export starts %q", dump.String()[:80])
	}
	if err := svc.ExportAll(ctx, f.Mika, &dump); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("a student exporting: %v", err)
	}

	// Installing a pack: only a newer release of the same curriculum, that
	// this Zigrt can run.
	for name, data := range map[string][]byte{
		"not a pack":    []byte("hello"),
		"same version":  packFile(t, "zigrt-hardest", "2026.11.0", "0.1.0"),
		"older":         packFile(t, "zigrt-hardest", "2025.01.1", "0.1.0"),
		"another pack":  packFile(t, "someone-else", "2099.01.1", "0.1.0"),
		"needs a newer": packFile(t, "zigrt-hardest", "2099.01.1", "9.0.0"),
	} {
		if _, err := svc.InstallPack(ctx, f.Sam, data); !errors.As(err, &ue) {
			t.Errorf("%s: %v", name, err)
		}
	}
	p, err := svc.InstallPack(ctx, f.Sam, packFile(t, "zigrt-hardest", "2099.01.1", "0.4.0"))
	must(t, err)
	if p.Manifest.Version != "2099.01.1" || svc.Pack().Manifest.Version != "2099.01.1" || svc.PackSource != "uploaded" {
		t.Fatalf("after install: %s running", svc.Pack().Manifest.Version)
	}
	if _, err := os.Stat(filepath.Join(svc.ContentDir, "zigrt-hardest-2099.01.1.zip")); err != nil {
		t.Fatalf("pack file: %v", err)
	}
	if h, _ = svc.Household(ctx, f.Sam); len(h.Pack.Uploaded) != 1 || h.Pack.Uploaded[0].InstalledBy != f.Sam.ID {
		t.Fatalf("uploaded %+v", h.Pack.Uploaded)
	}
	ns, _, _ := svc.Notifications(ctx, f.Alex, 0)
	if !slices.ContainsFunc(ns, func(n service.NotificationView) bool { return n.Kind == service.NotePackUpdated }) {
		t.Fatal("parents were not told about the new lessons")
	}
	if audit, _ := f.Store.ListAudit(ctx, "", 20); !slices.ContainsFunc(audit, func(a store.AuditEntry) bool { return a.Action == "household.pack" }) {
		t.Fatal("the install was not audited")
	}
}
