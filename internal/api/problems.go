package api

import (
	"net/http"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

func (s *Server) createProblem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string            `json:"title"`
		Statement string            `json:"statement"`
		Checker   string            `json:"checker"`
		Limits    domain.Limits     `json:"limits"`
		Tests     []domain.TestCase `json:"tests"`
	}
	if !decode(w, r, &req, 26<<20) {
		return
	}
	p := domain.Problem{Title: req.Title, Statement: req.Statement, Checker: req.Checker, Limits: req.Limits, Tests: req.Tests}
	if err := p.Validate(); err != nil {
		writeError(w, 422, "validation_error", err.Error())
		return
	}
	p, err := s.Repo.CreateProblem(r.Context(), p)
	if err != nil {
		s.failure(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/problems/"+p.ID)
	writeJSON(w, 201, p)
}

func (s *Server) getProblem(w http.ResponseWriter, r *http.Request) {
	if !domain.ValidID(r.PathValue("id")) {
		writeError(w, 404, "not_found", "resource not found")
		return
	}
	p, err := s.Repo.Problem(r.Context(), r.PathValue("id"))
	if err != nil {
		s.failure(w, r, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) listProblems(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := page(w, r)
	if !ok {
		return
	}
	items, err := s.Repo.Problems(r.Context(), before, limit+1)
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
