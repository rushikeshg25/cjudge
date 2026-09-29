//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

func TestParallelClaims(t *testing.T) {
	s := testStore(t)
	p, req := fixture(t, s)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if _, _, err := s.Enqueue(ctx, p.ID, fmt.Sprint(i), req, 100); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := s.Claim(ctx, time.Minute, 3)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- job.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatal("duplicate concurrent claim")
		}
		seen[id] = true
	}
	if len(seen) != 20 {
		t.Fatalf("only claimed %d jobs", len(seen))
	}
}

func TestDatabaseRoleBoundaries(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner, req := fixture(t, s)
	var schema string
	if err := s.Pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	apiRole, workerRole, operatorRole := "api_"+domain.NewID(), "worker_"+domain.NewID(), "operator_"+domain.NewID()
	data, err := os.ReadFile("../../deploy/roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.NewReplacer("cjudge_api", apiRole, "cjudge_worker", workerRole, "cjudge_operator", operatorRole, "public", schema).Replace(string(data))
	if _, err = s.Pool.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, role := range []string{apiRole, workerRole, operatorRole} {
			s.Pool.Exec(ctx, `DROP OWNED BY `+role)
			s.Pool.Exec(ctx, `DROP ROLE `+role)
		}
	})
	asRole := func(role string) *Store {
		cfg := s.Pool.Config()
		cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error { _, err := c.Exec(ctx, `SET ROLE `+role); return err }
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return &Store{Pool: pool}
	}
	// Simulate drift from a previous deployment, including column-level grants
	// that survive REVOKE ALL ON TABLE. Reapplying roles.sql must converge.
	if _, err = s.Pool.Exec(ctx, `GRANT ALL ON ALL TABLES IN SCHEMA `+schema+` TO `+apiRole+`,`+workerRole+`; GRANT SELECT(tests) ON problems TO `+apiRole+`; GRANT UPDATE(tests) ON problems TO `+workerRole); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	a := asRole(apiRole)
	w := asRole(workerRole)
	if err := a.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = a.PublicProblem(ctx, req.ProblemID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Problem(ctx, req.ProblemID); err == nil {
		t.Fatal("API can read hidden tests")
	}
	if _, _, err = a.CreatePrincipal(ctx, "bad", true); err == nil {
		t.Fatal("API can mint admin tokens")
	}
	if _, _, err = w.CreatePrincipal(ctx, "bad", true); err == nil {
		t.Fatal("worker can mint admin tokens")
	}
	if _, err = a.Pool.Exec(ctx, `UPDATE audit_events SET action='tampered'`); err == nil {
		t.Fatal("API can mutate audit")
	}
	if _, err = w.Pool.Exec(ctx, `UPDATE problems SET tests='[]'`); err == nil {
		t.Fatal("worker can mutate tests")
	}
	p := domain.Problem{AuthorID: owner.ID, Title: "role test", Checker: "exact", Limits: domain.Limits{TimeMS: 1000, MemoryMB: 128, OutputKB: 64}, Tests: []domain.TestCase{{}}}
	if _, err = a.CreateProblem(ctx, p); err != nil {
		t.Fatalf("API cannot publish: %v", err)
	}
	sub, _, err := a.Enqueue(ctx, owner.ID, "roles", req, 100)
	if err != nil {
		t.Fatal(err)
	}
	job, err := w.Claim(ctx, time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Renew(ctx, job, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = w.Complete(ctx, job, domain.Result{Verdict: domain.Accepted}); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Pool.Exec(ctx, `UPDATE submissions SET result='{"verdict":"wrong_answer"}' WHERE id=$1`, sub.ID); err == nil {
		t.Fatal("terminal verdict could be rewritten")
	}
	var n int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE entity_id=$1`, sub.ID).Scan(&n); err != nil || n != 3 {
		t.Fatalf("audit transition count=%d err=%v", n, err)
	}
	var leaked bool
	if err = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT FROM audit_events WHERE details::text LIKE '%package main%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("audit leaked source")
	}
}

func TestRetryBackoffAndAttemptBudget(t *testing.T) {
	s := testStore(t)
	p, req := fixture(t, s)
	ctx := context.Background()
	sub, _, err := s.Enqueue(ctx, p.ID, "retry", req, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Claim(ctx, time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Retry(ctx, first, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx, time.Minute, 2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("retry backoff bypassed")
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE submissions SET available_at=now()-interval '1 second' WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.Claim(ctx, time.Minute, 2)
	if err != nil || second.Attempts != 2 {
		t.Fatalf("retry attempt: %+v %v", second, err)
	}
	if err = s.Retry(ctx, second, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.Submission(ctx, sub.ID, p.ID)
	if err != nil || got.Result == nil || got.Result.Verdict != domain.SystemError {
		t.Fatalf("exhausted retry: %+v %v", got, err)
	}
}
