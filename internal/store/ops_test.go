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
