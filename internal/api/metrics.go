package api

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type metrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	nanos    atomic.Uint64
}

func (m *metrics) record(status int, d time.Duration) {
	m.requests.Add(1)
	m.nanos.Add(uint64(d))
	if status >= 500 {
		m.errors.Add(1)
	}
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Repo.QueueStats(r.Context())
	if err != nil {
		s.failure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE cjudge_http_requests_total counter\ncjudge_http_requests_total %d\n# TYPE cjudge_http_errors_total counter\ncjudge_http_errors_total %d\n# TYPE cjudge_http_duration_seconds_total counter\ncjudge_http_duration_seconds_total %f\n", s.metrics.requests.Load(), s.metrics.errors.Load(), float64(s.metrics.nanos.Load())/float64(time.Second))
	fmt.Fprintf(w, "# TYPE cjudge_jobs gauge\ncjudge_jobs{state=\"queued\"} %d\ncjudge_jobs{state=\"running\"} %d\n", stats["queued"], stats["running"])
}
