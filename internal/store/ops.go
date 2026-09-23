package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ops_heartbeat is the dead-man's switch table: periodic jobs (backup,
// sweep) stamp a row on success; `veil monitor` pages when a beat goes
// stale or is missing. It is ops bookkeeping, not vault data — it lives
// outside EnsurePostgresSchema so jobs can create it before the schema
// exists on a fresh deployment.
const opsHeartbeatDDL = `CREATE TABLE IF NOT EXISTS ops_heartbeat (
	name TEXT PRIMARY KEY,
	at TIMESTAMPTZ NOT NULL
)`

// EnsureOpsHeartbeat creates the beat table. Idempotent.
func EnsureOpsHeartbeat(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, opsHeartbeatDDL)
	return err
}

// MarkHeartbeat upserts name's beat to now.
func MarkHeartbeat(ctx context.Context, pool *pgxpool.Pool, name string) error {
	if err := EnsureOpsHeartbeat(ctx, pool); err != nil {
		return err
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO ops_heartbeat(name, at) VALUES($1, now())
		 ON CONFLICT (name) DO UPDATE SET at = now()`, name)
	return err
}

// HeartbeatAge returns the age of name's last beat. ok is false when the
// job has never reported — a missing beat is itself a finding.
func HeartbeatAge(ctx context.Context, pool *pgxpool.Pool, name string) (age time.Duration, ok bool, err error) {
	if err := EnsureOpsHeartbeat(ctx, pool); err != nil {
		return 0, false, err
	}
	// Age in server time: the monitor host's clock is not the database's.
	var secs float64
	err = pool.QueryRow(ctx,
		`SELECT EXTRACT(EPOCH FROM now() - at) FROM ops_heartbeat WHERE name = $1`, name).Scan(&secs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("heartbeat %s: %w", name, err)
	}
	return time.Duration(secs * float64(time.Second)), true, nil
}

// OutboxLag reports audit relay backlog: queued rows and the age of the
// oldest one, measured in server time. Zero depth returns oldest=0.
func OutboxLag(ctx context.Context, pool *pgxpool.Pool) (depth int, oldest time.Duration, err error) {
	var secs *float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*), EXTRACT(EPOCH FROM now() - min(at)) FROM audit_outbox`).Scan(&depth, &secs); err != nil {
		return 0, 0, err
	}
	if secs != nil {
		oldest = time.Duration(*secs * float64(time.Second))
	}
	return depth, oldest, nil
}
