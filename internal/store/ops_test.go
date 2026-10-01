package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Heartbeats: missing is a finding, fresh beats report their age, and a
// re-mark advances the timestamp. PG_TEST_DSN-gated like the drill.
func TestOpsHeartbeat(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOpsHeartbeat(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ops_heartbeat WHERE name = 'test-job'`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := HeartbeatAge(ctx, pool, "test-job"); err != nil || ok {
		t.Fatalf("missing beat should be ok=false: %v %v", ok, err)
	}
	if err := MarkHeartbeat(ctx, pool, "test-job"); err != nil {
		t.Fatal(err)
	}
	age, ok, err := HeartbeatAge(ctx, pool, "test-job")
	if err != nil || !ok {
		t.Fatalf("fresh beat: %v %v", ok, err)
	}
	if age < 0 || age > time.Minute {
		t.Fatalf("fresh beat age %s", age)
	}

	// Re-mark advances the timestamp rather than erroring on the conflict.
	if _, err := pool.Exec(ctx,
		`UPDATE ops_heartbeat SET at = now() - interval '2 hours' WHERE name = 'test-job'`); err != nil {
		t.Fatal(err)
	}
	if err := MarkHeartbeat(ctx, pool, "test-job"); err != nil {
		t.Fatal(err)
	}
	age, _, err = HeartbeatAge(ctx, pool, "test-job")
	if err != nil {
		t.Fatal(err)
	}
	if age > time.Minute {
		t.Fatalf("re-mark did not advance: %s", age)
	}

	if _, _, err := OutboxLag(ctx, pool); err != nil {
		t.Fatalf("outbox lag: %v", err)
	}
}

// Usage-report lag is the monitor's view of the billing flusher: pending
// rows are deltas owed to the provider, stale rows are deltas from closed
// windows that a healthy flusher would already have drained.
func TestOpsUsageReportLag(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM usage_counters WHERE org_id LIKE 'lag-%'`); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonth := thisMonth.AddDate(0, -1, 0)
	for _, row := range []struct {
		org      string
		window   time.Time
		used     int64
		reported int64
	}{
		{"lag-current", thisMonth, 5, 2}, // pending, current window — normal lag
		{"lag-stale", lastMonth, 3, 0},   // pending, closed window — stuck
		{"lag-done", thisMonth, 4, 4},    // fully reported — invisible
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO usage_counters(org_id, window_start, used, reported)
			 VALUES($1, $2, $3, $4)`, row.org, row.window, row.used, row.reported); err != nil {
			t.Fatal(err)
		}
	}

	pending, stale, units, err := UsageReportLag(ctx, pool, thisMonth)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 2 || stale != 1 || units != 6 {
		t.Fatalf("lag = (%d, %d, %d), want (2, 1, 6)", pending, stale, units)
	}
}

// The wal-archive slot check distinguishes missing, attached, and
// retained-WAL-lost states — the monitor pages on all but a live stream.
func TestOpsWalSlotStatus(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx,
		`SELECT pg_drop_replication_slot('test_wal_slot')`); err != nil {
		// Absent slot is fine; only a real error should fail.
		t.Logf("pre-clean drop: %v", err)
	}
	if _, _, ok, err := WalSlotStatus(ctx, pool, "test_wal_slot"); err != nil || ok {
		t.Fatalf("missing slot should be ok=false: %v %v", ok, err)
	}
	if _, err := pool.Exec(ctx,
		`SELECT pg_create_physical_replication_slot('test_wal_slot', true)`); err != nil {
		t.Fatal(err)
	}
	active, status, ok, err := WalSlotStatus(ctx, pool, "test_wal_slot")
	if err != nil || !ok {
		t.Fatalf("created slot: %v %v", ok, err)
	}
	if active {
		t.Fatal("no receiver attached — slot must report inactive")
	}
	if status != "reserved" {
		t.Fatalf("fresh slot status = %q, want reserved", status)
	}
	if _, err := pool.Exec(ctx,
		`SELECT pg_drop_replication_slot('test_wal_slot')`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := WalSlotStatus(ctx, pool, "test_wal_slot"); err != nil || ok {
		t.Fatalf("dropped slot should be ok=false: %v %v", ok, err)
	}
}

// WalSlots is the multi-slot view the monitor pages on: it lists only
// wal_archive% slots so a standby (wal_archive_dr) is checked alongside the
// primary. SystemIdentifier is the arc-<sysid>/ namespace every offsite
// archive object is written under.
func TestOpsWalSlotsAndSysid(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	sysid, err := SystemIdentifier(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(sysid) == 0 || len(sysid) > 20 {
		t.Fatalf("sysid %q: want 1-20 decimal digits", sysid)
	}
	for _, c := range sysid {
		if c < '0' || c > '9' {
			t.Fatalf("sysid %q not decimal", sysid)
		}
	}

	for _, name := range []string{"wal_archive_t1", "wal_archive_dr_t"} {
		pool.Exec(ctx, `SELECT pg_drop_replication_slot('`+name+`')`)
	}
	pool.Exec(ctx, `SELECT pg_create_physical_replication_slot('wal_archive_t1', true)`)
	pool.Exec(ctx, `SELECT pg_create_physical_replication_slot('wal_archive_dr_t', true)`)
	defer func() {
		pool.Exec(ctx, `SELECT pg_drop_replication_slot('wal_archive_t1')`)
		pool.Exec(ctx, `SELECT pg_drop_replication_slot('wal_archive_dr_t')`)
	}()

	slots, err := WalSlots(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range slots {
		names[s.Name] = true
	}
	if !names["wal_archive_t1"] || !names["wal_archive_dr_t"] {
		t.Fatalf("WalSlots = %v, want both test slots", names)
	}
}

// The export cursor starts at zero, only moves forward, and the read
// cursor returns rows after it in sequence order.
func TestOpsAuditExport(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAuditExport(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM audit WHERE org_id = 'export-org'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM audit_export_cursor`); err != nil {
		t.Fatal(err)
	}

	cur, err := AuditExportCursor(ctx, pool)
	if err != nil || cur != 0 {
		t.Fatalf("fresh cursor = %d, %v — want 0", cur, err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	var ids [3]int64
	for i := range ids {
		err := pool.QueryRow(ctx,
			`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			 VALUES($1, 'export-org', 'agent-x', 'item-y', 'use', 'allow', 't', '') RETURNING id`,
			at.Add(time.Duration(i)*time.Second)).Scan(&ids[i])
		if err != nil {
			t.Fatal(err)
		}
	}

	rows, err := ExportableAudits(ctx, pool, ids[0], 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != ids[1] || rows[1].ID != ids[2] {
		t.Fatalf("export after %d = %+v", ids[0], rows)
	}
	if rows[0].OrgID != "export-org" || rows[0].Decision != "allow" {
		t.Fatalf("row fields lost: %+v", rows[0])
	}

	if err := SetAuditExportCursor(ctx, pool, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := SetAuditExportCursor(ctx, pool, ids[0]); err == nil {
		t.Fatal("regressing the cursor must fail")
	}
	cur, err = AuditExportCursor(ctx, pool)
	if err != nil || cur != ids[1] {
		t.Fatalf("cursor = %d, %v — want %d", cur, err, ids[1])
	}
}
