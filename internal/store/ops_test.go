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
