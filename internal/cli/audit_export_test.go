package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VortexNYC/veil/internal/store"
)

// Export batches JSONL objects named by range, advances the cursor only
// after the PUT lands, and a failed PUT leaves the cursor so the next run
// re-sends the identical object. PG-gated like the ops tests.
func TestAuditExportRun(t *testing.T) {
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
	if err := store.EnsurePostgresSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureAuditExport(ctx, pool); err != nil {
		t.Fatal(err)
	}
	org := "exportrun-org"
	if _, err := pool.Exec(ctx, `DELETE FROM audit WHERE org_id = $1`, org); err != nil {
		t.Fatal(err)
	}
	// Shared test DBs carry other orgs' audit rows — pin the cursor at the
	// current tail so this run exports exactly what this test inserts.
	var baseID int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM audit`).Scan(&baseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE audit_export_cursor SET last_id = $1 WHERE singleton`, baseID); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			 VALUES($1, $2, 'a', 'i', 'use', 'allow', '', '')`, at.Add(time.Duration(i)*time.Second), org); err != nil {
			t.Fatal(err)
		}
	}

	type gotPut struct {
		name string
		body []byte
	}
	var puts []gotPut
	failAt := -1
	put := func(name string, r io.Reader) error {
		b, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		puts = append(puts, gotPut{name, b})
		if failAt >= 0 && len(puts) > failAt {
			return fmt.Errorf("ingest down")
		}
		return nil
	}

	// batch=2 over 5 rows: 3 objects, cursor at the last exported id.
	n, err := runAuditExport(ctx, pool, put, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || len(puts) != 3 {
		t.Fatalf("exported %d in %d puts, want 5/3", n, len(puts))
	}
	for i, p := range puts {
		if !strings.HasPrefix(p.name, "audit-") || !strings.HasSuffix(p.name, ".jsonl") {
			t.Fatalf("object %d named %q", i, p.name)
		}
		lines := bufio.NewScanner(bytes.NewReader(p.body))
		cnt := 0
		for lines.Scan() {
			cnt++
			if !strings.Contains(lines.Text(), `"org_id":"exportrun-org"`) {
				t.Fatalf("object %d row missing fields: %s", i, lines.Text())
			}
		}
		want := 2
		if i == 2 {
			want = 1
		}
		if cnt != want {
			t.Fatalf("object %d has %d rows, want %d", i, cnt, want)
		}
	}
	cur, _ := store.AuditExportCursor(ctx, pool)
	var maxID int64
	if err := pool.QueryRow(ctx, `SELECT max(id) FROM audit WHERE org_id = $1`, org).Scan(&maxID); err != nil {
		t.Fatal(err)
	}
	if cur != maxID {
		t.Fatalf("cursor %d, want %d", cur, maxID)
	}

	// Nothing left — a second run exports zero without calling put.
	puts = nil
	if n, err := runAuditExport(ctx, pool, put, 2); err != nil || n != 0 || len(puts) != 0 {
		t.Fatalf("empty run: n=%d puts=%d err=%v", n, len(puts), err)
	}

	// PUT failure: two more rows, third batch fails → cursor holds at the
	// first landed batch and a retry re-sends only the unlanded range.
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			 VALUES(now(), $1, 'a', 'i', 'use', 'deny', '', '')`, org); err != nil {
			t.Fatal(err)
		}
	}
	puts = nil
	failAt = 0
	if _, err := runAuditExport(ctx, pool, put, 2); err == nil {
		t.Fatal("put failure must surface")
	}
	curAfter, _ := store.AuditExportCursor(ctx, pool)
	if curAfter != cur {
		t.Fatalf("cursor moved on failed put: %d -> %d", cur, curAfter)
	}
	failAt = -1
	puts = nil
	n, err = runAuditExport(ctx, pool, put, 2)
	if err != nil || n != 3 {
		t.Fatalf("retry: n=%d err=%v", n, err)
	}
}
