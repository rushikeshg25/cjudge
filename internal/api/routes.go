package api

import "net/http"

func (s *Server) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /metrics", s.auth(true, s.serveMetrics))
	mux.HandleFunc("POST /v1/problems", s.auth(true, s.createProblem))
	mux.HandleFunc("GET /v1/problems", s.auth(false, s.listProblems))
	mux.HandleFunc("GET /v1/problems/{id}", s.auth(false, s.getProblem))
	mux.HandleFunc("POST /v1/submissions", s.auth(false, s.submit))
	mux.HandleFunc("GET /v1/submissions", s.auth(false, s.listSubmissions))
	mux.HandleFunc("GET /v1/submissions/{id}", s.auth(false, s.getSubmission))
}
