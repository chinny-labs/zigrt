package storetest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/drill"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/store"
)

// FilledStore is a store with a household and a few weeks of everything:
// lessons, reviews, quizzes, drills, notifications, an assignment.
func FilledStore(t *testing.T, open Opener) (store.Store, *Fixture) {
	t.Helper()
	ctx := context.Background()
	f := NewFixture(t, open)
	f.withLessons(t)
	f.Svc.Learn.QuizEveryXP = 40
	for _, u := range []store.User{f.Mika, f.Theo} {
		s := f.student(u, nil)
		for range 6 {
			_, err := s.Day(ctx)
			must(t, err)
			f.Clock.Advance(24 * time.Hour)
		}
	}
	_, err := f.Svc.AssignDrill(ctx, f.Sam, f.Mika.ID, store.KindWorksheet, defaultDrill(), "Practice")
	must(t, err)
	must(t, f.Svc.Rollover(ctx))
	return f.Store, f
}

// Export(Import(Export(x))) is Export(x): every row survives, on this engine.
func testExportImport(t *testing.T, open Opener) {
	ctx := context.Background()
	src, f := FilledStore(t, open)
	var first bytes.Buffer
	must(t, src.Export(ctx, &first, store.ExportHeader{App: "test"}))
	lines := strings.Count(first.String(), "\n")
	if lines < 100 {
		t.Fatalf("only %d lines exported", lines)
	}
	if strings.Contains(first.String(), `"t":"sessions"`) {
		t.Fatal("sessions were exported")
	}

	dst := open(t)
	n, err := dst.Import(ctx, bytes.NewReader(first.Bytes()))
	must(t, err)
	if n != lines-1 {
		t.Fatalf("imported %d rows of %d", n, lines-1)
	}
	var second bytes.Buffer
	must(t, dst.Export(ctx, &second, store.ExportHeader{App: "test"}))
	if body(first.String()) != body(second.String()) {
		t.Fatal("the re-export differs from the first export")
	}
	// The imported household works: projections match the logs.
	svc := service.New(dst, nil)
	svc.SetPack(HardestPack(t))
	must(t, svc.CheckProjections(ctx, f.Mika.ID))

	if _, err := dst.Import(ctx, bytes.NewReader(first.Bytes())); !errors.Is(err, store.ErrNotEmpty) {
		t.Fatalf("import into a full database: %v", err)
	}
	if _, err := open(t).Import(ctx, strings.NewReader("{\"hello\":1}\n")); err == nil {
		t.Fatal("imported something that is not an export")
	}
}

// body drops the header line, whose time differs.
func body(dump string) string {
	_, rest, _ := strings.Cut(dump, "\n")
	return rest
}

func defaultDrill() drill.Settings { return drill.Default() }
