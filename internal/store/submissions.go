package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rushikeshg25/cjudge/internal/domain"
)

const submissionColumns = `id,owner_id,problem_id,language,source,state,attempts,result,created_at,updated_at`

func scanSubmission(row pgx.Row) (domain.Submission, error) {
	var s domain.Submission
	var result []byte
	err := row.Scan(&s.ID, &s.OwnerID, &s.ProblemID, &s.Language, &s.Source, &s.State, &s.Attempts, &result, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if result != nil {
		err = json.Unmarshal(result, &s.Result)
	}
	return s, err
}

// Enqueue serializes admission, making capacity and idempotency atomic across APIs.
func (s *Store) Enqueue(ctx context.Context, owner, key string, req domain.SubmissionRequest, capacity int) (domain.Submission, bool, error) {
	var zero domain.Submission
	if err := req.Validate(); err != nil {
		return zero, false, err
	}
	body, _ := json.Marshal(req)
	hash := sha256.Sum256(body)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return zero, false, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(87652302)`); err != nil {
		return zero, false, err
	}
	var id string
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT id,request_hash FROM submissions WHERE owner_id=$1 AND idempotency_key=$2`, owner, key).Scan(&id, &previous)
	if err == nil {
		if !bytes.Equal(previous, hash[:]) {
			return zero, false, domain.ErrConflict
		}
		existing, err := scanSubmission(tx.QueryRow(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE id=$1`, id))
		return existing, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM problems WHERE id=$1)`, req.ProblemID).Scan(&exists); err != nil {
		return zero, false, err
	}
	if !exists {
		return zero, false, domain.ErrNotFound
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM submissions WHERE state IN ('queued','running')`).Scan(&count); err != nil {
		return zero, false, err
	}
	if count >= capacity {
		return zero, false, domain.ErrQueueFull
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM submissions WHERE owner_id=$1 AND state IN ('queued','running')`, owner).Scan(&count); err != nil {
		return zero, false, err
	}
	if count >= 50 {
		return zero, false, domain.ErrQueueFull
	}
	sub, err := scanSubmission(tx.QueryRow(ctx, `INSERT INTO submissions(id,owner_id,problem_id,language,source,idempotency_key,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+submissionColumns, domain.NewID(), owner, req.ProblemID, req.Language, req.Source, key, hash[:]))
	if err != nil {
		return zero, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, false, err
	}
	return sub, true, nil
}

func (s *Store) Submission(ctx context.Context, id, owner string) (domain.Submission, error) {
	return scanSubmission(s.Pool.QueryRow(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE id=$1 AND owner_id=$2`, id, owner))
}

func (s *Store) Submissions(ctx context.Context, owner, before string, limit int) ([]domain.Submission, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE owner_id=$1 AND ($2='' OR id<$2) ORDER BY id DESC LIMIT $3`, owner, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Submission, 0)
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, sub)
	}
	return items, rows.Err()
}
