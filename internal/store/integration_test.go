//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CJUDGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CJUDGE_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + domain.NewID()
	if _, err = admin.Pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); admin.Pool.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); admin.Close() })
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatalf("migration not idempotent: %v", err)
	}
	return s
}

func fixture(t *testing.T, s *Store) (domain.Principal, domain.SubmissionRequest) {
	t.Helper()
	ctx := context.Background()
	p, _, err := s.CreatePrincipal(ctx, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	problem, err := s.CreateProblem(ctx, domain.Problem{Title: "sum", Checker: "tokens", Limits: domain.Limits{TimeMS: 1000, MemoryMB: 128, OutputKB: 64}, Tests: []domain.TestCase{{Input: "1 2", Expected: "3"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p, domain.SubmissionRequest{ProblemID: problem.ID, Language: "go", Source: "package main"}
}

func TestConcurrentIdempotency(t *testing.T) {
	s := testStore(t)
	p, req := fixture(t, s)
	ctx := context.Background()
	var wg sync.WaitGroup
	var created atomic.Int32
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, new, err := s.Enqueue(ctx, p.ID, "same", req, 100)
			if err != nil {
				t.Error(err)
				return
			}
			if new {
				created.Add(1)
			}
			ids <- sub.ID
		}()
	}
	wg.Wait()
	close(ids)
	if created.Load() != 1 {
		t.Fatalf("created %d copies", created.Load())
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("idempotency returned different jobs")
		}
	}
	req.Source = "changed"
	if _, _, err := s.Enqueue(ctx, p.ID, "same", req, 100); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if _, err := s.Submission(ctx, first, domain.NewID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-principal submission visible")
	}
}

func TestConcurrentCapacity(t *testing.T) {
	s := testStore(t)
	p, req := fixture(t, s)
	var wg sync.WaitGroup
	var count atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := s.Enqueue(context.Background(), p.ID, fmt.Sprint(i), req, 1)
			if err == nil {
				count.Add(1)
			} else if !errors.Is(err, domain.ErrQueueFull) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("capacity race: %d", count.Load())
	}
}

func TestClaimFencingAndRecovery(t *testing.T) {
	s := testStore(t)
	p, req := fixture(t, s)
	ctx := context.Background()
	sub, _, err := s.Enqueue(ctx, p.ID, "one", req, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Claim(ctx, time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx, time.Minute, 3); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("job claimed twice")
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE submissions SET lease_until=now()-interval '1 second' WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.Claim(ctx, time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempts != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatal("recovery did not fence attempt")
	}
	if err = s.Renew(ctx, first, time.Minute); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("stale renewal accepted")
	}
	if err = s.Complete(ctx, first, domain.Result{Verdict: domain.WrongAnswer}); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("stale verdict accepted")
	}
	if err = s.Retry(ctx, first, 3); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("stale retry accepted")
	}
	if err = s.Complete(ctx, second, domain.Result{Verdict: domain.Accepted, Passed: 1, Total: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Submission(ctx, sub.ID, p.ID)
	if err != nil || got.Result.Verdict != domain.Accepted {
		t.Fatalf("wrong final result: %+v %v", got, err)
	}
}

func TestFinalAttemptRecovery(t *testing.T) {
	s := testStore(t)
	p, req := fixture(t, s)
	ctx := context.Background()
	sub, _, _ := s.Enqueue(ctx, p.ID, "one", req, 100)
	if _, err := s.Claim(ctx, time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	s.Pool.Exec(ctx, `UPDATE submissions SET lease_until=now()-interval '1 second' WHERE id=$1`, sub.ID)
	if _, err := s.Claim(ctx, time.Minute, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	got, err := s.Submission(ctx, sub.ID, p.ID)
	if err != nil || got.State != "finished" || got.Result.Verdict != domain.SystemError {
		t.Fatalf("job stranded: %+v %v", got, err)
	}
}

func TestCredentialsAndSharedRateBudget(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p, token, err := s.CreatePrincipal(ctx, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Authenticate(ctx, token)
	if err != nil || got != p {
		t.Fatalf("authentication failed: %v", err)
	}
	var wg sync.WaitGroup
	var allowed atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.Allow(ctx, p.ID, 5)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("shared rate budget: %d", allowed.Load())
	}
	if err = s.RevokePrincipal(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("revoked token accepted")
	}
}
