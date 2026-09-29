package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

type Repository interface {
	Claim(context.Context, time.Duration, int) (domain.Job, error)
	Renew(context.Context, domain.Job, time.Duration) error
	Complete(context.Context, domain.Job, domain.Result) error
	Retry(context.Context, domain.Job, int) error
	Problem(context.Context, string) (domain.Problem, error)
}
type Evaluator interface {
	Evaluate(context.Context, domain.Submission, domain.Problem) (domain.Result, error)
}

type Worker struct {
	Repo        Repository
	Judge       Evaluator
	Log         *slog.Logger
	Concurrency int
	MaxAttempts int
	Lease       time.Duration
	JobTimeout  time.Duration
	Poll        time.Duration
}

// Run stops claims on cancellation and waits for active sandbox cleanup.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.loop(ctx) }()
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		job, err := w.Repo.Claim(claimCtx, w.Lease, w.MaxAttempts)
		cancel()
		if err == nil {
			w.process(ctx, job)
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) && ctx.Err() == nil {
			w.Log.Error("claim failed", "error", err)
		}
		timer := time.NewTimer(w.Poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) process(parent context.Context, job domain.Job) {
	budgetExpired := errors.New("whole-job budget exhausted")
	deadlineCtx, deadlineCancel := context.WithTimeoutCause(parent, w.JobTimeout, budgetExpired)
	defer deadlineCancel()
	ctx, cancel := context.WithCancel(deadlineCtx)
	defer cancel()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(ctx, w.Lease/3)
				err := w.Repo.Renew(renewCtx, job, w.Lease)
				renewCancel()
				if err != nil {
					w.Log.Warn("lease renewal failed", "submission_id", job.ID, "error", err)
					cancel()
					return
				}
			}
		}
	}()
	started := time.Now()
	result, err := w.evaluate(ctx, job)
	close(stop)
	<-done
	// The write has its own deadline so shutdown can safely release owned work.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if parent.Err() == nil && errors.Is(context.Cause(ctx), budgetExpired) {
		// Repeating a deterministic whole-job timeout repeats the same expensive
		// prefix. Preserve progress/provenance, but never publish partial acceptance.
		result.Verdict = domain.SystemError
		result.Diagnostic = "whole-job time budget exceeded"
		err = nil
	} else if err != nil || ctx.Err() != nil {
		w.Log.Warn("job interrupted", "submission_id", job.ID, "attempt", job.Attempts, "error", err)
		if retryErr := w.Repo.Retry(finishCtx, job, w.MaxAttempts); retryErr != nil {
			w.Log.Warn("retry publication failed", "submission_id", job.ID, "error", retryErr)
		}
		return
	}
	if err = w.Repo.Complete(finishCtx, job, result); err != nil {
		w.Log.Warn("verdict publication failed", "submission_id", job.ID, "error", err)
		return
	}
	w.Log.Info("job completed", "submission_id", job.ID, "attempt", job.Attempts, "verdict", result.Verdict, "duration_ms", time.Since(started).Milliseconds())
}

func (w *Worker) evaluate(ctx context.Context, job domain.Job) (result domain.Result, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("judge panic")
		}
	}()
	p, err := w.Repo.Problem(ctx, job.ProblemID)
	if err != nil {
		return result, err
	}
	return w.Judge.Evaluate(ctx, job.Submission, p)
}
