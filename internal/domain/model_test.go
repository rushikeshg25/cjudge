package domain

import "testing"

func TestValidation(t *testing.T) {
	p := Problem{Title: "sum", Checker: "tokens", Limits: Limits{1000, 128, 64}, Tests: []TestCase{{"1 2", "3"}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Tests = nil
	if p.Validate() == nil {
		t.Fatal("empty tests accepted")
	}
	s := SubmissionRequest{NewID(), "go", "package main"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Language = "sh"
	if s.Validate() == nil {
		t.Fatal("unsupported language accepted")
	}
	if ValidID("../../etc/passwd") {
		t.Fatal("unsafe id accepted")
	}
}
