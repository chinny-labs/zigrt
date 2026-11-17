package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/chinny-labs/zigrt/internal/store"
	"github.com/chinny-labs/zigrt/internal/store/postgres"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
	"github.com/chinny-labs/zigrt/internal/store/storetest"
)

// ZIGRT_TEST_POSTGRES_URL points at a server where the test user may
// create databases. Each test gets its own database, dropped afterwards.
func baseURL(t *testing.T) string {
	u := os.Getenv("ZIGRT_TEST_POSTGRES_URL")
	if u == "" {
		t.Skip("ZIGRT_TEST_POSTGRES_URL not set")
	}
	return u
}

var opened atomic.Int64

func open(t *testing.T) store.Store {
	t.Helper()
	base := baseURL(t)
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// A test may open several databases (an export and its import), so each
	// gets a number as well as the test's name.
	name := fmt.Sprintf("zt%d_%s", opened.Add(1), strings.ToLower(strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())))
	if len(name) > 63 {
		name = name[:63]
	}
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	st, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.Close()
		if db, err := sql.Open("pgx", base); err == nil {
			_, _ = db.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			db.Close()
		}
	})
	return st
}

func TestContract(t *testing.T) { storetest.Run(t, open) }

func TestRebind(t *testing.T) {
	got := postgres.Dialect{}.Rebind(`SELECT * FROM t WHERE a = ? AND b IN (?, ?)`)
	if got != `SELECT * FROM t WHERE a = $1 AND b IN ($2, $3)` {
		t.Fatal(got)
	}
}

// A household moves between engines in either direction with nothing lost:
// SQLite to Postgres and back give the same dump.
func TestCrossEngine(t *testing.T) {
	baseURL(t)
	ctx := context.Background()
	lite := func(t *testing.T) store.Store {
		t.Helper()
		st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), fmt.Sprintf("x%d.db", opened.Add(1))), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return st
	}
	src, _ := storetest.FilledStore(t, lite)
	dump := func(st store.Store) string {
		t.Helper()
		var b bytes.Buffer
		if err := st.Export(ctx, &b, store.ExportHeader{App: "test"}); err != nil {
			t.Fatal(err)
		}
		_, rest, _ := strings.Cut(b.String(), "\n")
		return rest
	}
	want := dump(src)
	var full bytes.Buffer
	if err := src.Export(ctx, &full, store.ExportHeader{App: "test"}); err != nil {
		t.Fatal(err)
	}
	pg := open(t)
	if _, err := pg.Import(ctx, bytes.NewReader(full.Bytes())); err != nil {
		t.Fatal(err)
	}
	if got := dump(pg); got != want {
		t.Fatal("SQLite to Postgres changed the data")
	}
	var fromPG bytes.Buffer
	if err := pg.Export(ctx, &fromPG, store.ExportHeader{App: "test"}); err != nil {
		t.Fatal(err)
	}
	back := lite(t)
	if _, err := back.Import(ctx, bytes.NewReader(fromPG.Bytes())); err != nil {
		t.Fatal(err)
	}
	if got := dump(back); got != want {
		t.Fatal("Postgres back to SQLite changed the data")
	}
}
