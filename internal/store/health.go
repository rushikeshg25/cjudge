package store

import "context"

func (s *Store) Heartbeat(ctx context.Context, id string, slots int) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO worker_heartbeats(id,slots) VALUES($1,$2)
 ON CONFLICT(id) DO UPDATE SET slots=$2,last_seen=now()`, id, slots)
	return err
}

func (s *Store) WorkerStats(ctx context.Context) (int64, int64, int64, error) {
	var workers, slots, age int64
	err := s.Pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(slots),0) FROM worker_heartbeats WHERE last_seen>now()-interval '20 seconds'`).Scan(&workers, &slots)
	if err != nil {
		return 0, 0, 0, err
	}
	err = s.Pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(EPOCH FROM (now()-min(created_at)))::bigint,0) FROM submissions WHERE state='queued'`).Scan(&age)
	return workers, slots, age, err
}
