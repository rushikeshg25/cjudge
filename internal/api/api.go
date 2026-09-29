package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

type Repository interface {
	Ping(context.Context) error
	Authenticate(context.Context, string) (domain.Principal, error)
	CreateProblem(context.Context, domain.Problem) (domain.Problem, error)
	Problem(context.Context, string) (domain.Problem, error)
	Problems(context.Context, string, int) ([]domain.Problem, error)
	Enqueue(context.Context, string, string, domain.SubmissionRequest, int) (domain.Submission, bool, error)
	Submission(context.Context, string, string) (domain.Submission, error)
	Submissions(context.Context, string, string, int) ([]domain.Submission, error)
	QueueStats(context.Context) (map[string]int64, error)
	Allow(context.Context, string, int) (bool, error)
	WorkerStats(context.Context) (int64, int64, int64, error)
}

type Server struct {
	Repo              Repository
	Log               *slog.Logger
	QueueLimit        int
	RequestsPerMinute int
	metrics           metrics
}

type principalKey struct{}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.Repo.Ping(ctx); err != nil {
			writeError(w, 503, "unavailable", "database unavailable")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	s.register(mux)
	return s.middleware(mux)
}

func (s *Server) auth(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, 401, "unauthorized", "bearer token required")
			return
		}
		p, err := s.Repo.Authenticate(r.Context(), strings.TrimPrefix(header, "Bearer "))
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, 401, "unauthorized", "invalid credential")
			return
		}
		if err != nil {
			s.failure(w, r, err)
			return
		}
		if admin && !p.Admin {
			writeError(w, 403, "forbidden", "administrator required")
			return
		}
		allowed, err := s.Repo.Allow(r.Context(), p.ID, s.RequestsPerMinute)
		if err != nil {
			s.failure(w, r, err)
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "60")
			writeError(w, 429, "rate_limited", "request budget exhausted")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	}
}

func principal(r *http.Request) domain.Principal {
	return r.Context().Value(principalKey{}).(domain.Principal)
}

func decode(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	if ct := strings.Split(r.Header.Get("Content-Type"), ";")[0]; ct != "application/json" {
		writeError(w, 415, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		decodeError(w, err)
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		decodeError(w, err)
		return false
	}
	return true
}

func decodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, 413, "payload_too_large", "request body exceeds limit")
		return
	}
	writeError(w, 400, "invalid_json", "body must be one valid JSON object with known fields")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *Server) failure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, 404, "not_found", "resource not found")
		return
	}
	s.Log.Error("request failed", "request_id", w.Header().Get("X-Request-ID"), "error", err)
	writeError(w, 503, "unavailable", "service temporarily unavailable")
}

func page(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	before := r.URL.Query().Get("before")
	if before != "" && !domain.ValidID(before) {
		writeError(w, 400, "invalid_cursor", "invalid before cursor")
		return "", 0, false
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, 400, "invalid_limit", "limit must be 1..100")
			return "", 0, false
		}
		limit = n
	}
	return before, limit, true
}
