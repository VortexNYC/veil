package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// partPool opens a pool against a fresh schema without running the full
// Postgres store — for driving EnsurePostgresSchema directly.
func partPool(t *testing.T, dsn, schema string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	conn, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schema)); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// TestPostgresAuditPartitionFresh: a fresh schema gets a partitioned audit
// table, monthly partitions for the current horizon, and inserts route to
// the right month.
func TestPostgresAuditPartitionFresh(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool := partPool(t, dsn, "audit_fresh")
	defer pool.Close()
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := pool.QueryRow(ctx, `SELECT relkind::text FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND c.relname='audit'`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "p" {
		t.Fatalf("audit relkind %q, want partitioned 'p'", kind)
	}
	// Current and next month's partitions exist.
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		m := now.AddDate(0, i, 0)
		name := auditPartitionName(m)
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname=current_schema() AND c.relname=$1)`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("partition %s missing", name)
		}
	}
	// An insert in this month lands in this month's partition.
	if _, err := pool.Exec(ctx, `INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES ($1, 'org', 'a', 'i', 'fetch', 'allow', '', '')`, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, auditPartitionName(now))).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("month partition holds %d rows, want 1", n)
	}
	// A row from two months ago routes to the DEFAULT catch-all.
	if _, err := pool.Exec(ctx, `INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES ($1, 'org', 'a', 'i', 'fetch', 'deny', '', '')`, now.AddDate(0, -2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_default`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("default partition holds %d rows, want 1", n)
	}
	// Rerun is idempotent.
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
}

// TestPostgresAuditPartitionLegacy: a pre-partition audit table migrates in
// place — rows preserved, table partitioned, sequence continues, the legacy
// table is gone.
func TestPostgresAuditPartitionLegacy(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool := partPool(t, dsn, "audit_legacy_schema")
	defer pool.Close()

	// Old shape: plain table, identity PK, plus one pre-migration row in a
	// month that has no partition — it should land in audit_default.
	old := time.Now().UTC().AddDate(0, -3, 0)
	for _, q := range []string{
		`CREATE TABLE audit (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			at TIMESTAMPTZ NOT NULL,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			action TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason TEXT NOT NULL,
			approval_id TEXT NOT NULL
		)`,
		`CREATE INDEX idx_audit_agent_at ON audit(agent_id, at)`,
		`CREATE INDEX idx_audit_at ON audit(at)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES ($1, 'org', 'a', 'i', 'fetch', 'allow', 'legacy-row', '')`, old); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := pool.QueryRow(ctx, `SELECT relkind::text FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND c.relname='audit'`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "p" {
		t.Fatalf("post-migration audit relkind %q", kind)
	}
	var reason string
	if err := pool.QueryRow(ctx, `SELECT reason FROM audit`).Scan(&reason); err != nil {
		t.Fatalf("migrated row missing: %v", err)
	}
	if reason != "legacy-row" {
		t.Fatalf("migrated row reason %q", reason)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_default`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("legacy row did not land in audit_default (%d)", n)
	}
	var gone bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND c.relname='audit_legacy')`).Scan(&gone); err != nil {
		t.Fatal(err)
	}
	if !gone {
		t.Fatal("audit_legacy still exists after migration")
	}
	// The partitioned parent's audit indexes exist (the legacy ones were
	// dropped with the table; the index phase must have recreated them).
	var idx int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes i
		JOIN pg_namespace n ON n.nspname=i.schemaname
		WHERE i.schemaname=current_schema() AND i.tablename='audit'
		AND i.indexname IN ('idx_audit_agent_at','idx_audit_at')`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 2 {
		t.Fatalf("partitioned audit has %d idx_audit_* indexes, want 2", idx)
	}
	// New inserts get fresh sequence ids above the copied row.
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES ($1, 'org', 'a', 'i', 'fetch', 'allow', 'new', '') RETURNING id`, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id <= 1 {
		t.Fatalf("sequence did not advance: id %d", id)
	}
}

// TestPostgresAuditRetention: partitions older than the cutoff detach into
// standalone tables — rows still readable, detached from the parent.
func TestPostgresAuditRetention(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool := partPool(t, dsn, "audit_ret")
	defer pool.Close()
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Hand-create an old partition with a row.
	old := time.Now().UTC().AddDate(0, -4, 0)
	name := auditPartitionName(old)
	from := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF audit FOR VALUES FROM ('%s') TO ('%s')`,
		name, from.Format("2006-01-02"), to.Format("2006-01-02"))); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES ($1, 'org', 'a', 'i', 'fetch', 'allow', 'old-row', '')`, old); err != nil {
		t.Fatal(err)
	}
	// Cutoff = 90 days ago → the 4-month-old partition detaches.
	detached, err := DetachAuditPartitionsBefore(ctx, pool, time.Now().UTC().Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(detached) != 1 || detached[0] != name {
		t.Fatalf("detached %v, want [%s]", detached, name)
	}
	// The partition is now a standalone table: row readable, no longer a
	// partition of audit.
	var n int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, name)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("detached table lost its row")
	}
	var stillPartition bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid
		JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND c.relname=$1)`, name).Scan(&stillPartition); err != nil {
		t.Fatal(err)
	}
	if stillPartition {
		t.Fatal("detached table still attached to audit")
	}
	// Current-month partition must NOT detach.
	detached2, err := DetachAuditPartitionsBefore(ctx, pool, time.Now().UTC().Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(detached2) != 0 {
		t.Fatalf("current partitions detached: %v", detached2)
	}
}

// TestPostgresTrigramIndexes: EnsurePostgresSchema installs pg_trgm and the
// GIN trigram indexes on items, and a similarity query actually resolves
// (VEIL-9 — the index substrate for human item search).
func TestPostgresTrigramIndexes(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool := partPool(t, dsn, "test_trgm")
	defer pool.Close()
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var ext int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname='pg_trgm'`).Scan(&ext); err != nil {
		t.Fatal(err)
	}
	if ext != 1 {
		t.Fatal("pg_trgm extension not installed")
	}
	var idx int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes
		WHERE schemaname=current_schema() AND tablename='items'
		AND indexname IN ('idx_items_name_trgm','idx_items_uris_trgm','idx_items_tags_trgm','idx_items_login_trgm')`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 4 {
		t.Fatalf("items has %d trigram indexes, want 4", idx)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret, tags, login)
		VALUES ('i1', 'org', 'GitHub deploy token', 'login', 'agent', 'a1', '["github.com"]', '\x00', '["ci","prod"]', 'ci-bot')`); err != nil {
		t.Fatal(err)
	}
	var hits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE name % 'github' OR uris % 'github' OR tags % 'prod' OR login % 'ci'`).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("trigram similarity found %d rows, want 1", hits)
	}
}
