package metrics_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinny-labs/zigrt/internal/metrics"
	"github.com/chinny-labs/zigrt/internal/pack"
	"github.com/chinny-labs/zigrt/internal/service"
	"github.com/chinny-labs/zigrt/internal/sim"
	"github.com/chinny-labs/zigrt/internal/store/sqlite"
)

func TestScrape(t *testing.T) {
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
	end := time.Now()
	if _, err := sim.Demo(ctx, svc, "UTC", "correct horse battery", 3, end, 1); err != nil {
		t.Fatal(err)
	}

	m := metrics.New(svc)
	m.Request("GET /parent", 200, 30*time.Millisecond)
	m.Request("", 404, time.Millisecond)
	m.Job("rollover", nil)
	m.Job("email", errors.New("smtp down"))
	m.Emails(2, 1)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{
		`zigrt_http_requests_total{code="200",route="GET /parent"} 1`,
		`zigrt_http_requests_total{code="404",route="unmatched"} 1`,
		`zigrt_http_request_duration_seconds_bucket{route="GET /parent",le="0.05"} 1`,
		`zigrt_job_runs_total{job="email",result="error"} 1`,
		`zigrt_job_runs_total{job="rollover",result="ok"} 1`,
		`zigrt_emails_total{result="failed"} 1`,
		`zigrt_emails_total{result="sent"} 2`,
		`zigrt_students 2`,
		`zigrt_active_students 2`,
		`zigrt_email_outbox{state="pending"} 0`,
		`zigrt_content_pack_info{pack="zigrt-k5",version="` + p.Manifest.Version + `"} 1`,
		`zigrt_build_info{version="dev"} 1`,
		`go_goroutines`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("no %s", want)
		}
	}
	if t.Failed() {
		t.Log(string(body))
	}
}
