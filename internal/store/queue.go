package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

// Claim also recovers abandoned jobs. All clocks are supplied by PostgreSQL.
func (s *Store) Claim(ctx context.Context, lease time.Duration, maxAttempts int) (domain.Job, error) {
	_, err := s.Pool.Exec(ctx, `WITH expired AS (
 SELECT id FROM submissions WHERE attempts >= $1
 AND (state='queued' OR (state='running' AND lease_until<=clock_timestamp()))
 ORDER BY lease_until FOR UPDATE SKIP LOCKED LIMIT 100
 ) UPDATE submissions SET state='finished',lease_token=NULL,lease_until=NULL,
 result='{"verdict":"system_error","passed":0,"total":0,"time_ms":0}'::jsonb,updated_at=now()
 WHERE id IN (SELECT id FROM expired)`, maxAttempts)
	if err != nil {
		return domain.Job{}, err
	}
	token := domain.NewID()
	row := s.Pool.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM submissions WHERE attempts<$1 AND
 ((state='queued' AND available_at<=now()) OR (state='running' AND lease_until<=now()))
 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE submissions SET state='running',attempts=attempts+1,lease_token=$2,
 lease_until=clock_timestamp()+$3*interval '1 millisecond',updated_at=now()
 WHERE id IN (SELECT id FROM candidate) RETURNING `+submissionColumns, maxAttempts, token, lease.Milliseconds())
	sub, err := scanSubmission(row)
	return domain.Job{Submission: sub, LeaseToken: token}, err
}

func (s *Store) Renew(ctx context.Context, j domain.Job, lease time.Duration) error {
	return s.mutateLease(ctx, j, `UPDATE submissions SET lease_until=clock_timestamp()+$3*interval '1 millisecond',updated_at=now()
 WHERE id=$1 AND lease_token=$2 AND state='running' AND lease_until>clock_timestamp()`, lease.Milliseconds())
}

func (s *Store) Complete(ctx context.Context, j domain.Job, result domain.Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.mutateLease(ctx, j, `UPDATE submissions SET state='finished',result=$3,lease_token=NULL,lease_until=NULL,updated_at=now()
 WHERE id=$1 AND lease_token=$2 AND state='running' AND lease_until>clock_timestamp()`, body)
}

func (s *Store) Retry(ctx context.Context, j domain.Job, maxAttempts int) error {
	if j.Attempts >= maxAttempts {
		return s.Complete(ctx, j, domain.Result{Verdict: domain.SystemError})
	}
	delay := time.Duration(1<<min(j.Attempts, 6)) * time.Second
	return s.mutateLease(ctx, j, `UPDATE submissions SET state='queued',lease_token=NULL,lease_until=NULL,
 available_at=clock_timestamp()+$3*interval '1 millisecond',updated_at=now()
 WHERE id=$1 AND lease_token=$2 AND state='running' AND lease_until>clock_timestamp()`, delay.Milliseconds())
}

// mutateLease locks first, then evaluates expiry against wall time. An UPDATE
// predicate alone can be evaluated before waiting on a row lock; now() is also
// frozen at transaction start. Neither is a safe post-wait lease fence.
func (s *Store) mutateLease(ctx context.Context, j domain.Job, query string, value any) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM submissions WHERE id=$1 FOR UPDATE`, j.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, query, j.ID, j.LeaseToken, value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (s *Store) QueueStats(ctx context.Context) (map[string]int64, error) {
	rows, err := s.Pool.Query(ctx, `SELECT state,count(*) FROM submissions WHERE state!='finished' GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := map[string]int64{"queued": 0, "running": 0}
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		stats[state] = n
	}
	return stats, rows.Err()
}
