package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

type fakeRepo struct {
	renewErr               error
	result                 domain.Result
	complete, retry, renew int
}

func (f *fakeRepo) Claim(context.Context, time.Duration, int) (domain.Job, error) {
	return domain.Job{}, domain.ErrNotFound
}
func (f *fakeRepo) Renew(context.Context, domain.Job, time.Duration) error {
	f.renew++
	return f.renewErr
}
func (f *fakeRepo) Complete(_ context.Context, _ domain.Job, r domain.Result) error {
	f.result = r
	f.complete++
	return nil
}
func (f *fakeRepo) Retry(context.Context, domain.Job, int) error { f.retry++; return nil }
func (f *fakeRepo) Problem(context.Context, string) (domain.Problem, error) {
	return domain.Problem{}, nil
}

type evaluatorFunc func(context.Context, domain.Submission, domain.Problem) (domain.Result, error)

func (f evaluatorFunc) Evaluate(c context.Context, s domain.Submission, p domain.Problem) (domain.Result, error) {
	return f(c, s, p)
}

func testWorker(repo *fakeRepo, evaluator evaluatorFunc) *Worker {
	return &Worker{Repo: repo, Judge: evaluator, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Concurrency: 2, MaxAttempts: 3, Lease: 60 * time.Millisecond, JobTimeout: time.Second, Poll: time.Millisecond}
}

func TestLostLeaseCancelsEvaluation(t *testing.T) {
	repo := &fakeRepo{renewErr: domain.ErrLeaseLost}
	w := testWorker(repo, func(ctx context.Context, _ domain.Submission, _ domain.Problem) (domain.Result, error) {
		<-ctx.Done()
		return domain.Result{Verdict: domain.Accepted}, ctx.Err()
	})
	w.process(context.Background(), domain.Job{})
	if repo.complete != 0 || repo.retry != 1 || repo.renew != 1 {
		t.Fatalf("lease loss incorrectly handled: %+v", repo)
	}
}

func TestJobOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name            string
		fn              evaluatorFunc
		complete, retry int
	}{
		{"accepted", func(context.Context, domain.Submission, domain.Problem) (domain.Result, error) {
			return domain.Result{Verdict: domain.Accepted}, nil
		}, 1, 0},
		{"infra", func(context.Context, domain.Submission, domain.Problem) (domain.Result, error) {
			return domain.Result{}, errors.New("offline")
		}, 0, 1},
		{"panic", func(context.Context, domain.Submission, domain.Problem) (domain.Result, error) {
			panic("executor panic")
		}, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			testWorker(repo, tc.fn).process(context.Background(), domain.Job{})
			if repo.complete != tc.complete || repo.retry != tc.retry {
				t.Fatalf("unexpected writes: %+v", repo)
			}
		})
	}
}

func TestIdleShutdown(t *testing.T) {
	w := testWorker(&fakeRepo{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown hung")
	}
}

func TestWholeJobDeadlineIsTerminal(t *testing.T) {
	repo := &fakeRepo{}
	w := testWorker(repo, func(ctx context.Context, _ domain.Submission, _ domain.Problem) (domain.Result, error) {
		<-ctx.Done()
		return domain.Result{Image: "sha256:fixture", Total: 10, Passed: 2}, ctx.Err()
	})
	w.JobTimeout = 30 * time.Millisecond
	w.process(context.Background(), domain.Job{})
	if repo.complete != 1 || repo.retry != 0 || repo.result.Verdict != domain.SystemError || repo.result.Total != 10 || repo.result.Image != "sha256:fixture" {
		t.Fatalf("deadline retried or provenance lost: %+v", repo)
	}
}
func TestShutdownStillRetries(t *testing.T) {
	repo := &fakeRepo{}
	parent, cancel := context.WithCancel(context.Background())
	w := testWorker(repo, func(ctx context.Context, _ domain.Submission, _ domain.Problem) (domain.Result, error) {
		cancel()
		<-ctx.Done()
		return domain.Result{}, ctx.Err()
	})
	w.process(parent, domain.Job{})
	if repo.complete != 0 || repo.retry != 1 {
		t.Fatalf("shutdown finalized: %+v", repo)
	}
}
