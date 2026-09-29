package api

import (
	"errors"
	"net/http"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if !validKey(key) {
		writeError(w, 400, "invalid_idempotency_key", "Idempotency-Key must be 1..128 visible ASCII characters")
		return
	}
	var req domain.SubmissionRequest
	if !decode(w, r, &req, 400<<10) {
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, 422, "validation_error", err.Error())
		return
	}
	sub, created, err := s.Repo.Enqueue(r.Context(), principal(r).ID, key, req, s.QueueLimit)
	if errors.Is(err, domain.ErrConflict) {
		writeError(w, 409, "idempotency_conflict", "key already used for another request")
		return
	}
	if errors.Is(err, domain.ErrQueueFull) {
		w.Header().Set("Retry-After", "5")
		writeError(w, 503, "queue_full", "judge queue at capacity")
		return
	}
	if err != nil {
		s.failure(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/submissions/"+sub.ID)
	status := 200
	if created {
		status = 202
	}
	writeJSON(w, status, sub)
}

func validKey(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func (s *Server) getSubmission(w http.ResponseWriter, r *http.Request) {
	if !domain.ValidID(r.PathValue("id")) {
		writeError(w, 404, "not_found", "resource not found")
		return
	}
	sub, err := s.Repo.Submission(r.Context(), r.PathValue("id"), principal(r).ID)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	writeJSON(w, 200, sub)
}

func (s *Server) listSubmissions(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := page(w, r)
	if !ok {
		return
	}
	items, err := s.Repo.Submissions(r.Context(), principal(r).ID, before, limit+1)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}
