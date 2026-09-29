package judge

import (
	"context"
	"errors"
	"testing"

	"github.com/rushikeshg25/cjudge/internal/domain"
	"github.com/rushikeshg25/cjudge/internal/sandbox"
)

type fakeRunner struct {
	out      []sandbox.Outcome
	requests []sandbox.Request
	err      error
}

func (f *fakeRunner) Run(_ context.Context, r sandbox.Request) (sandbox.Outcome, error) {
	f.requests = append(f.requests, r)
	if f.err != nil {
		return sandbox.Outcome{}, f.err
	}
	o := f.out[0]
	f.out = f.out[1:]
	return o, nil
}

func TestVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  sandbox.Outcome
		want domain.Verdict
	}{
		{"accepted", sandbox.Outcome{Stdout: "3\n"}, domain.Accepted},
		{"wrong", sandbox.Outcome{Stdout: "4"}, domain.WrongAnswer},
		{"timeout", sandbox.Outcome{TimedOut: true}, domain.TimeLimit},
		{"memory", sandbox.Outcome{OOM: true, ExitCode: 137}, domain.MemoryLimit},
		{"output", sandbox.Outcome{OutputExceeded: true}, domain.OutputLimit},
		{"runtime", sandbox.Outcome{ExitCode: 1, Stderr: "secret test"}, domain.RuntimeError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{out: []sandbox.Outcome{{}, tc.out}}
			j := Judge{Runner: f, Languages: Languages("cpp", "py", "go"), Workspace: t.TempDir()}
			p := domain.Problem{Title: "sum", Checker: "tokens", Limits: domain.Limits{TimeMS: 1000, MemoryMB: 128, OutputKB: 64}, Tests: []domain.TestCase{{Input: "1 2", Expected: "3"}}}
			r, err := j.Evaluate(context.Background(), domain.Submission{Language: "go", Source: "code"}, p)
			if err != nil || r.Verdict != tc.want || r.Diagnostic != "" {
				t.Fatalf("got %+v %v", r, err)
			}
			if f.requests[0].Input != "" {
				t.Fatal("test input exposed to compiler")
			}
		})
	}
}

func TestInfrastructureError(t *testing.T) {
	j := Judge{Runner: &fakeRunner{err: errors.New("daemon down")}, Languages: Languages("cpp", "py", "go"), Workspace: t.TempDir()}
	p := domain.Problem{Title: "p", Checker: "exact", Limits: domain.Limits{1000, 128, 64}, Tests: []domain.TestCase{{}}}
	if _, err := j.Evaluate(context.Background(), domain.Submission{Language: "go"}, p); err == nil {
		t.Fatal("infrastructure failure became contestant verdict")
	}
}

func TestCheckers(t *testing.T) {
	if !matches("tokens", "1\r\n 2 ", "1 2") || matches("exact", "1\n", "1") || matches("tokens", "1 2 3", "1 2") {
		t.Fatal("checker mismatch")
	}
}
