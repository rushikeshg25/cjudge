package api

import (
	"context"
	"net/http"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
		r.ResponseWriter.WriteHeader(status)
	}
}
func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(200)
	}
	return r.ResponseWriter.Write(b)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Request-ID", domain.NewID())
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		rr := &responseRecorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.Log.Error("handler panic", "request_id", w.Header().Get("X-Request-ID"))
				if rr.status == 0 {
					writeError(rr, 500, "internal_error", "internal error")
				}
			}
			s.Log.Info("http request", "method", r.Method, "route", r.Pattern, "status", rr.status, "duration_ms", time.Since(started).Milliseconds(), "request_id", w.Header().Get("X-Request-ID"))
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		next.ServeHTTP(rr, r.WithContext(ctx))
	})
}
