package api

import "net/http"

func (s *Server) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/problems", s.auth(true, s.createProblem))
	mux.HandleFunc("GET /v1/problems", s.auth(false, s.listProblems))
	mux.HandleFunc("GET /v1/problems/{id}", s.auth(false, s.getProblem))
}
