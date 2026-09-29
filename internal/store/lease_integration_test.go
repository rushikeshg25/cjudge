//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

func TestLeaseExpiryWhileWaitingForRowLock(t *testing.T) {
	for _, operation := range []string{"renew", "complete", "retry"} {
		t.Run(operation, func(t *testing.T) {
			s := testStore(t)
			p, req := fixture(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, _, err := s.Enqueue(ctx, p.ID, "lease", req, 100); err != nil {
				t.Fatal(err)
			}
			job, err := s.Claim(ctx, 2*time.Second, 3)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			var locker int
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM submissions WHERE id=$1 FOR UPDATE`, job.ID).Scan(&locker); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "renew":
					done <- s.Renew(ctx, job, time.Minute)
				case "complete":
					done <- s.Complete(ctx, job, domain.Result{Verdict: domain.Accepted})
				case "retry":
					done <- s.Retry(ctx, job, 3)
				}
			}()
			// Prove the mutation started and is blocked before allowing expiry.
			for {
				var blocked bool
				if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, locker).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("mutation did not wait: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			for {
				var expired bool
				if err := s.Pool.QueryRow(ctx, `SELECT lease_until<=clock_timestamp() FROM submissions WHERE id=$1`, job.ID).Scan(&expired); err != nil {
					t.Fatal(err)
				}
				if expired {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, domain.ErrLeaseLost) {
				t.Fatalf("expired lease mutation accepted: %v", err)
			}
			got, err := s.Submission(ctx, job.ID, p.ID)
			if err != nil || got.State != "running" || got.Result != nil {
				t.Fatalf("expired mutation changed job: %+v %v", got, err)
			}
		})
	}
}
