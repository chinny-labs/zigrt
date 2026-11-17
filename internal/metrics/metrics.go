// Package metrics is Zigrt's optional Prometheus endpoint (plan §10.3):
// requests, latency, job runs, email, and students. It is served on its own
// port (ZIGRT_METRICS_LISTEN) so a public ingress never exposes it.
package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/chinny-labs/zigrt/internal/buildinfo"
	"github.com/chinny-labs/zigrt/internal/service"
)

// Metrics holds the registry and the instruments.
type Metrics struct {
	Registry *prometheus.Registry
	requests *prometheus.CounterVec
	latency  *prometheus.HistogramVec
	jobs     *prometheus.CounterVec
	emails   *prometheus.CounterVec
}

// New builds the metrics. svc supplies the gauges read at each scrape.
func New(svc *service.Service) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry()}
	m.requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zigrt_http_requests_total", Help: "HTTP requests by route and status.",
	}, []string{"route", "code"})
	m.latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "zigrt_http_request_duration_seconds", Help: "HTTP request latency by route.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"route"})
	m.jobs = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zigrt_job_runs_total", Help: "Background job checks by job and result (ok or error); a job runs every minute and most runs find nothing to do.",
	}, []string{"job", "result"})
	m.emails = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zigrt_emails_total", Help: "Email delivery attempts by result (sent or failed).",
	}, []string{"result"})
	m.Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.latency, m.jobs, m.emails, &household{svc: svc})
	return m
}

// Handler serves the metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// Request records one HTTP request. route is the matched pattern, "" if
// none matched.
func (m *Metrics) Request(route string, code int, d time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	m.requests.WithLabelValues(route, strconv.Itoa(code)).Inc()
	m.latency.WithLabelValues(route).Observe(d.Seconds())
}

// Job records one run of a background job.
func (m *Metrics) Job(name string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	m.jobs.WithLabelValues(name, result).Inc()
}

// Emails records delivery attempts.
func (m *Metrics) Emails(sent, failed int) {
	m.emails.WithLabelValues("sent").Add(float64(sent))
	m.emails.WithLabelValues("failed").Add(float64(failed))
}

// household is read at scrape time: students, the outbox, the pack, the
// build.
type household struct{ svc *service.Service }

var (
	studentsDesc = prometheus.NewDesc("zigrt_students", "Students in the household.", nil, nil)
	activeDesc   = prometheus.NewDesc("zigrt_active_students", "Students who practised in the last seven days.", nil, nil)
	outboxDesc   = prometheus.NewDesc("zigrt_email_outbox", "Email not sent: pending (being retried) or failed (given up).", []string{"state"}, nil)
	packDesc     = prometheus.NewDesc("zigrt_content_pack_info", "The content pack in use.", []string{"pack", "version"}, nil)
	buildDesc    = prometheus.NewDesc("zigrt_build_info", "Zigrt's version.", []string{"version"}, nil)
)

func (h *household) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{studentsDesc, activeDesc, outboxDesc, packDesc, buildDesc} {
		ch <- d
	}
}

func (h *household) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch <- prometheus.MustNewConstMetric(buildDesc, prometheus.GaugeValue, 1, buildinfo.Version)
	if p := h.svc.Pack(); p != nil {
		ch <- prometheus.MustNewConstMetric(packDesc, prometheus.GaugeValue, 1, p.Manifest.ID, p.Manifest.Version)
	}
	if c, err := h.svc.Counts(ctx); err == nil {
		ch <- prometheus.MustNewConstMetric(studentsDesc, prometheus.GaugeValue, float64(c.Students))
		ch <- prometheus.MustNewConstMetric(activeDesc, prometheus.GaugeValue, float64(c.Active))
	} else {
		ch <- prometheus.NewInvalidMetric(studentsDesc, err)
	}
	if pending, failed, err := h.svc.Store.CountEmails(ctx, service.MaxEmailAttempts); err == nil {
		ch <- prometheus.MustNewConstMetric(outboxDesc, prometheus.GaugeValue, float64(pending), "pending")
		ch <- prometheus.MustNewConstMetric(outboxDesc, prometheus.GaugeValue, float64(failed), "failed")
	} else {
		ch <- prometheus.NewInvalidMetric(outboxDesc, err)
	}
}
