package dpop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresCoordinator stores DPoP jti replay and token pins in Postgres so
// every origin replica shares bind-on-first-use state.
type PostgresCoordinator struct {
	pool *pgxpool.Pool
}

func NewPostgresCoordinator(pool *pgxpool.Pool) *PostgresCoordinator {
	if pool == nil {
		return nil
	}
	return &PostgresCoordinator{pool: pool}
}

func (p *PostgresCoordinator) PinnedJKT(tokHash string, now time.Time) string {
	if p == nil || p.pool == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var jkt string
	err := p.pool.QueryRow(ctx, `SELECT jkt FROM dpop_pins
		WHERE token_hash=$1 AND expires_at > $2`, tokHash, now.UTC()).Scan(&jkt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		return ""
	}
	return jkt
}

func (p *PostgresCoordinator) RecordProof(tokHash, jti, jkt string, now time.Time) error {
	if p == nil || p.pool == nil {
		return fmt.Errorf("dpop: postgres coordinator unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	jtiExp := now.Add(2 * clockSkewWindow)
	pinExp := now.Add(pinTTL)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing string
	err = tx.QueryRow(ctx, `SELECT jkt FROM dpop_pins
		WHERE token_hash=$1 AND expires_at > $2`, tokHash, now.UTC()).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && existing != jkt {
		return fmt.Errorf("dpop: jkt mismatch with pinned token key")
	}

	tag, err := tx.Exec(ctx, `INSERT INTO dpop_jtis (token_hash, jti, expires_at)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, tokHash, jti, jtiExp.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dpop: replayed jti")
	}

	_, err = tx.Exec(ctx, `INSERT INTO dpop_pins (token_hash, jkt, expires_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (token_hash) DO UPDATE SET
		  jkt=EXCLUDED.jkt, expires_at=EXCLUDED.expires_at`,
		tokHash, jkt, pinExp.UTC())
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
