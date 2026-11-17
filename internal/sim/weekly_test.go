package sim_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/sim"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
	"github.com/chinny-labs/zigrt/internal/store/storetest"
)

// TestWeeklyEmailOnDemo reads the weekly email for a demo household whose
// history ends on a Sunday evening.
func TestWeeklyEmailOnDemo(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "z.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, nil)
	p, err := pack.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPack(p)
	loc, _ := time.LoadLocation("America/Los_Angeles")
	sunday := time.Date(2026, 11, 15, 19, 30, 0, 0, loc)
	if _, err := sim.Demo(ctx, svc, loc.String(), "correct horse battery", 21, sunday, 3); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return sunday }
	m := &storetest.FakeMail{}
	svc.Mail, svc.BaseURL = m, "https://zigrt.example"
	if err := svc.WeeklyEmail(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SendOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	got := m.Take()
	if len(got) != 1 {
		t.Fatalf("%d emails", len(got))
	}
	t.Logf("Subject: %s\n\n%s", got[0].Subject, got[0].Body)
	for _, want := range []string{"Zigrt: the week of 9 November", "Ada\n", "Ben\n", "Streak:", "placed this week", "goal was met on"} {
		if !strings.Contains(got[0].Subject+got[0].Body, want) {
			t.Errorf("no %q", want)
		}
	}
}
