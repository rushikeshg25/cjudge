package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Allow uses a shared fixed-minute budget across all API replicas.
func (s *Store) Allow(ctx context.Context, principal string, limit int) (bool, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `INSERT INTO rate_limits(principal_id,window_start,requests)
 VALUES($1,date_trunc('minute',now()),1) ON CONFLICT(principal_id) DO UPDATE SET
 window_start=date_trunc('minute',now()),
 requests=CASE WHEN rate_limits.window_start<date_trunc('minute',now()) THEN 1 ELSE rate_limits.requests+1 END
 WHERE rate_limits.window_start<date_trunc('minute',now()) OR rate_limits.requests<$2
 RETURNING requests`, principal, limit).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
