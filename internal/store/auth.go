package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/rushikeshg25/cjudge/internal/domain"
)

// CreatePrincipal returns a secret once; only its hash is persisted.
func (s *Store) CreatePrincipal(ctx context.Context, name string, admin bool) (domain.Principal, string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return domain.Principal{}, "", fmt.Errorf("name must be 1..100 bytes")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return domain.Principal{}, "", err
	}
	token := "cj_" + base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(token))
	p := domain.Principal{ID: domain.NewID(), Admin: admin}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return p, "", err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `INSERT INTO principals(id,name,admin) VALUES($1,$2,$3)`, p.ID, name, admin); err != nil {
		return p, "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO api_keys(hash,principal_id) VALUES($1,$2)`, hash[:], p.ID); err != nil {
		return p, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, "", err
	}
	return p, token, nil
}

func (s *Store) Authenticate(ctx context.Context, token string) (domain.Principal, error) {
	var p domain.Principal
	if len(token) != 46 || !strings.HasPrefix(token, "cj_") {
		return p, domain.ErrNotFound
	}
	hash := sha256.Sum256([]byte(token))
	err := s.Pool.QueryRow(ctx, `SELECT p.id,p.admin FROM api_keys k JOIN principals p ON p.id=k.principal_id WHERE k.hash=$1 AND NOT k.revoked`, hash[:]).Scan(&p.ID, &p.Admin)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	return p, err
}

func (s *Store) RevokePrincipal(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE api_keys SET revoked=true WHERE principal_id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
