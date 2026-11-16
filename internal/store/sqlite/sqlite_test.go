package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
	"github.com/chinny-labs/zigrt/internal/store/storetest"
)

func open(t *testing.T) store.Store {
	t.Helper()
	st, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "zigrt.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestContract(t *testing.T) { storetest.Run(t, open) }

func TestPathFromURL(t *testing.T) {
	for in, want := range map[string]string{
		"sqlite:///data/zigrt.db": "/data/zigrt.db",
		"sqlite://rel.db":         "rel.db",
		"sqlite:rel.db":           "rel.db",
	} {
		if got, err := sqlite.PathFromURL(in); err != nil || got != want {
			t.Errorf("PathFromURL(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"sqlite://", "sqlite://:memory:", "postgres://x"} {
		if _, err := sqlite.PathFromURL(bad); err == nil {
			t.Errorf("PathFromURL(%q) accepted", bad)
		}
	}
}

func TestReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zigrt.db")
	st, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(ctx, "", func(tx store.Tx) error { return tx.PutSetting(ctx, "k", "v", false) }); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = sqlite.Open(ctx, path, nil) // migrations are idempotent
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if v, err := st.GetSetting(ctx, "k"); err != nil || v != "v" {
		t.Fatalf("got %q, %v", v, err)
	}
}

// TestBackupRestore is the restore path (plan §10.3): snapshot the live
// database, open the snapshot as if restored, and check the people match.
func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	f := storetest.NewFixture(t, open)
	st := f.Store.(*store.SQL)
	dir := filepath.Join(t.TempDir(), "backups")
	path := filepath.Join(dir, sqlite.NightlyName(f.Clock.Now()))
	if err := sqlite.Backup(ctx, st, path); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Backup(ctx, st, path); err == nil {
		t.Fatal("overwrote an existing backup")
	}
	restored, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a, _ := st.ListUsers(ctx, "")
	b, _ := restored.ListUsers(ctx, "")
	if len(a) != 4 || len(a) != len(b) {
		t.Fatalf("users %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("user %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	// Retention keeps the newest N.
	for _, d := range []string{"20260101", "20260102", "20260103"} {
		if err := os.WriteFile(filepath.Join(dir, "zigrt-"+d+".db"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := sqlite.Prune(dir, 2)
	if err != nil || len(removed) != 2 || removed[0] != "zigrt-20260102.db" {
		t.Fatalf("pruned %v, %v", removed, err)
	}
}
