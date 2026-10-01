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

// UsageReportLag reports billing-flusher backlog: pending rows are
// org-windows with unreported deltas, stale is the subset from closed
// windows (before currentWindow) — those have survived at least one full
// tick plus a window boundary, so they are stuck rather than merely queued.
// units is the total unreported usage.
func UsageReportLag(ctx context.Context, pool *pgxpool.Pool, currentWindow time.Time) (pending, stale int64, units int64, err error) {
	err = pool.QueryRow(ctx,
		`SELECT count(*),
		        count(*) FILTER (WHERE window_start < $1),
		        COALESCE(sum(used - reported), 0)
		 FROM usage_counters WHERE used > reported`, currentWindow).Scan(&pending, &stale, &units)
	return pending, stale, units, err
}

// audit_export_cursor is the offsite-archival watermark: one row, the last
// audit.id pushed through the backup-ingest worker. Outside the schema DDL
// for the same reason as ops_heartbeat — the cron creates it before first
// use.
const auditExportDDL = `CREATE TABLE IF NOT EXISTS audit_export_cursor (
	singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
	last_id BIGINT NOT NULL DEFAULT 0
)`

// EnsureAuditExport creates the cursor table and seeds the zero row.
// Idempotent.
func EnsureAuditExport(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, auditExportDDL); err != nil {
		return err
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO audit_export_cursor(singleton, last_id) VALUES(TRUE, 0) ON CONFLICT DO NOTHING`)
	return err
}

// AuditExportCursor returns the highest audit id already exported.
func AuditExportCursor(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	if err := EnsureAuditExport(ctx, pool); err != nil {
		return 0, err
	}
	var id int64
	if err := pool.QueryRow(ctx,
		`SELECT last_id FROM audit_export_cursor WHERE singleton`).Scan(&id); err != nil {
		return 0, fmt.Errorf("audit export cursor: %w", err)
	}
	return id, nil
}

// SetAuditExportCursor advances the watermark after a batch lands offsite.
// Moving backwards is refused — a regressed cursor re-exports history.
func SetAuditExportCursor(ctx context.Context, pool *pgxpool.Pool, lastID int64) error {
	if err := EnsureAuditExport(ctx, pool); err != nil {
		return err
	}
	res, err := pool.Exec(ctx,
		`UPDATE audit_export_cursor SET last_id = $1 WHERE singleton AND last_id < $1`, lastID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("audit export cursor regression: %d not ahead", lastID)
	}
	return nil
}

// WalSlotStatus reads the archive slot's liveness from the server's side.
// The wal-archive heartbeat proves the shipper loop is alive; this proves a
// pg_receivewal is actually attached — a dead receiver with a live shipper
// otherwise pages nothing while WAL silently stops flowing. ok=false means
// the slot is missing entirely; status='lost' means max_slot_wal_keep_size
// fired and retained WAL was dropped.
func WalSlotStatus(ctx context.Context, pool *pgxpool.Pool, name string) (active bool, status string, ok bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT active, coalesce(wal_status, '') FROM pg_replication_slots WHERE slot_name = $1`, name).
		Scan(&active, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", false, nil
		}
		return false, "", false, fmt.Errorf("wal slot %s: %w", name, err)
	}
	return active, status, true, nil
}

// SystemIdentifier is the cluster's stable identity (pg_control) — the
// arc-<sysid>/ namespace prefix every offsite archive object lives under.
func SystemIdentifier(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var id string
	if err := pool.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&id); err != nil {
		return "", fmt.Errorf("system identifier: %w", err)
	}
	return id, nil
}

// WalSlot is one replication slot's server-side liveness — the multi-slot
// view for monitor: primary + standby archivers each own a pinned slot.
type WalSlot struct {
	Name   string
	Active bool
	Status string
}

// WalSlots lists every archive slot (wal_archive, wal_archive_dr, …). The
// monitor pages when none is active or any shows wal_status='lost'.
func WalSlots(ctx context.Context, pool *pgxpool.Pool) ([]WalSlot, error) {
	rows, err := pool.Query(ctx,
		`SELECT slot_name, active, coalesce(wal_status, '') FROM pg_replication_slots
		 WHERE slot_name LIKE 'wal\_archive%' ORDER BY slot_name`)
	if err != nil {
		return nil, fmt.Errorf("wal slots: %w", err)
	}
	defer rows.Close()
	var out []WalSlot
	for rows.Next() {
		var s WalSlot
		if err := rows.Scan(&s.Name, &s.Active, &s.Status); err != nil {
			return nil, fmt.Errorf("wal slots: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AuditExportRow is one audit event on the export wire — the full row plus
// its sequence id so the archive is independently ordered and dedup-able.
type AuditExportRow struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	OrgID      string    `json:"org_id"`
	AgentID    string    `json:"agent_id"`
	ItemID     string    `json:"item_id"`
	Action     string    `json:"action"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason"`
	ApprovalID string    `json:"approval_id"`
}

// ExportableAudits returns up to limit audit rows with id > afterID in
// sequence order — the archive's read cursor. id is a shared sequence, so
// strictly increasing across partitions; the scan fans out over monthly
// partitions, bounded by limit.
func ExportableAudits(ctx context.Context, pool *pgxpool.Pool, afterID int64, limit int) ([]AuditExportRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, at, org_id, agent_id, item_id, action, decision, reason, approval_id
		 FROM audit WHERE id > $1 ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditExportRow
	for rows.Next() {
		var r AuditExportRow
		if err := rows.Scan(&r.ID, &r.At, &r.OrgID, &r.AgentID, &r.ItemID,
			&r.Action, &r.Decision, &r.Reason, &r.ApprovalID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
