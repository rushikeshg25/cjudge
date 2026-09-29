package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rushikeshg25/cjudge/internal/domain"
)

func (s *Store) CreateProblem(ctx context.Context, p domain.Problem) (domain.Problem, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	p.ID = domain.NewID()
	limits, _ := json.Marshal(p.Limits)
	tests, _ := json.Marshal(p.Tests)
	err := s.Pool.QueryRow(ctx, `INSERT INTO problems(id,title,statement,checker,limits,tests) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at`, p.ID, p.Title, p.Statement, p.Checker, limits, tests).Scan(&p.CreatedAt)
	return p, err
}

func (s *Store) Problem(ctx context.Context, id string) (domain.Problem, error) {
	var p domain.Problem
	var limits, tests []byte
	err := s.Pool.QueryRow(ctx, `SELECT id,title,statement,checker,limits,tests,created_at FROM problems WHERE id=$1`, id).Scan(&p.ID, &p.Title, &p.Statement, &p.Checker, &limits, &tests, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(limits, &p.Limits); err != nil {
		return p, err
	}
	err = json.Unmarshal(tests, &p.Tests)
	return p, err
}

func (s *Store) Problems(ctx context.Context, before string, limit int) ([]domain.Problem, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,title,statement,checker,limits,created_at FROM problems WHERE ($1='' OR id < $1) ORDER BY id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Problem, 0)
	for rows.Next() {
		var p domain.Problem
		var limits []byte
		if err := rows.Scan(&p.ID, &p.Title, &p.Statement, &p.Checker, &limits, &p.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(limits, &p.Limits); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
