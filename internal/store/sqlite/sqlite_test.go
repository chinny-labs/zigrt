package sqlite_test

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"

	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
	"github.com/chinny-labs/zigrt/internal/store/storetest"
	"github.com/chinny-labs/zigrt/migrations"
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

// Migration 4 rebuilds mastery_events: a database with history keeps it.
func TestMigration4KeepsHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zigrt.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	fsys, _ := fs.Sub(migrations.SQLite, "sqlite")
	p, err := goose.NewProvider(database.DialectSQLite3, db, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, role, display_name, created_at) VALUES ('u1', 'student', 'Mika', '2026-03-07T15:00:00.000000Z')`,
		`INSERT INTO mastery_events (id, student_id, topic_id, kind, task_id, detail, created_at)
		 VALUES ('e1', 'u1', 'add', 'lesson_passed', 't1', '{"on":"2026-03-07","stage":0}', '2026-03-07T15:00:00.000000Z')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	st, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	evs, err := st.ListMasteryEvents(ctx, "u1")
	if err != nil || len(evs) != 1 || evs[0].Kind != "lesson_passed" || string(evs[0].Detail) != `{"on":"2026-03-07","stage":0}` {
		t.Fatalf("after migrating: %+v %v", evs, err)
	}
	err = st.Tx(ctx, "u1", func(tx store.Tx) error {
		return tx.InsertMasteryEvent(ctx, store.MasteryEvent{ID: "e2", StudentID: "u1", TopicID: "add", Kind: "quiz_missed", CreatedAt: time.Now()})
	})
	if err != nil {
		t.Fatalf("quiz_missed: %v", err)
	}
}
