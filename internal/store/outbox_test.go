package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store/sqlc"
)

func outboxCount(t *testing.T, s *Postgres) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_outbox`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func auditCount(t *testing.T, s *Postgres) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func insertOutbox(t *testing.T, s *Postgres, itemID string) {
	t.Helper()
	err := s.sqlc.InsertAuditOutbox(context.Background(), sqlc.InsertAuditOutboxParams{
		At: time.Now().UTC(), OrgID: "org", AgentID: "claude", ItemID: itemID,
		Action: string(protocol.ActionFetch), Decision: string(protocol.DecisionAllow), Reason: "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPostgresAuditOutboxFallback: a write that fails against `audit` lands in
// the outbox durably, and the relay flush re-lands it once the table recovers.
func TestPostgresAuditOutboxFallback(t *testing.T) {
	s := openTestPostgres(t)
	defer s.Close()
	ctx := context.Background()

	// Take `audit` away — the partitioned-parent rename makes every insert
	// fail while audit_outbox stays writable.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE audit RENAME TO audit_gone`); err != nil {
		t.Fatal(err)
	}
	ev := protocol.AuditEvent{
		Time: time.Now().UTC(), OrgID: "org", AgentID: "claude", ItemID: "stripe",
		Action: protocol.ActionFetch, Decision: protocol.DecisionAllow, Reason: "ok",
	}
	if err := s.AppendAudit(ev); err != nil {
		t.Fatalf("append during outage: %v", err)
	}
	if err := s.AppendAudits([]protocol.AuditEvent{ev, ev}); err != nil {
		t.Fatalf("batch append during outage: %v", err)
	}
	if got := outboxCount(t, s); got != 3 {
		t.Fatalf("outbox rows %d, want 3", got)
	}

	if _, err := s.pool.Exec(ctx, `ALTER TABLE audit_gone RENAME TO audit`); err != nil {
		t.Fatal(err)
	}
	// Queued events are visible through Audit() before relay — durable and
	// observable, never double-counted once relayed (delete+insert are one tx).
	events, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("Audit() during outbox hold: %d events, want 3", len(events))
	}
	// The live relay may claim some rows first — assert the end state, not
	// which flush landed them.
	for i := 0; i < 8 && outboxCount(t, s) > 0; i++ {
		if _, err := s.FlushAuditOutbox(500); err != nil {
			t.Fatal(err)
		}
	}
	if got := outboxCount(t, s); got != 0 {
		t.Fatalf("outbox rows after flush %d, want 0", got)
	}
	if got := auditCount(t, s); got != 3 {
		t.Fatalf("audit rows after flush %d, want 3", got)
	}
	// Second flush is a no-op.
	if n, err := s.FlushAuditOutbox(500); err != nil || n != 0 {
		t.Fatalf("second flush %d %v", n, err)
	}
}

// TestPostgresAuditOutboxConcurrent: two stores racing FlushAuditOutbox take
// disjoint rows via SKIP LOCKED — every queued event lands exactly once.
func TestPostgresAuditOutboxConcurrent(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	kek := testMasterKey(t)
	s1 := rotateSchema(t, dsn, "outbox_race", kek)
	defer s1.Close()

	searchDSN := dsn
	if parsed, err := url.Parse(dsn); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		q := parsed.Query()
		q.Set("search_path", "outbox_race")
		parsed.RawQuery = q.Encode()
		searchDSN = parsed.String()
	} else {
		searchDSN = strings.TrimSpace(dsn) + " search_path=outbox_race"
	}
	s2, err := OpenPostgres(searchDSN, kek)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	const total = 200
	for i := 0; i < total; i++ {
		insertOutbox(t, s1, fmt.Sprintf("item-%d", i))
	}

	var wg sync.WaitGroup
	for _, st := range []*Postgres{s1, s2} {
		wg.Add(1)
		go func(st *Postgres) {
			defer wg.Done()
			for {
				n, err := st.FlushAuditOutbox(50)
				if err != nil {
					t.Errorf("flush: %v", err)
					return
				}
				if n < 50 {
					return
				}
			}
		}(st)
	}
	wg.Wait()
	if got := auditCount(t, s1); got != total {
		t.Fatalf("audit rows %d, want %d", got, total)
	}
	if got := outboxCount(t, s1); got != 0 {
		t.Fatalf("outbox leftover %d", got)
	}
}

// TestPostgresConsumeSessionAuditedOutbox: with `audit` broken the consume
// still commits — the event rides the same tx into the outbox via savepoint
// fallback, then the relay lands it. The use is audited and consumed exactly
// once.
func TestPostgresConsumeSessionAuditedOutbox(t *testing.T) {
	s := openTestPostgres(t)
	defer s.Close()
	ctx := context.Background()
	hash, _ := seedSessionStore(t, s)

	if _, err := s.pool.Exec(ctx, `ALTER TABLE audit RENAME TO audit_gone`); err != nil {
		t.Fatal(err)
	}
	ev := protocol.AuditEvent{
		Time: time.Now().UTC(), ItemID: "stripe",
		Action: protocol.ActionFetch, Decision: protocol.DecisionAllow, Reason: "ok",
	}
	ag, err := s.ConsumeSessionAudited(hash, time.Now().UTC(), ev)
	if err != nil {
		t.Fatalf("consume during audit outage: %v", err)
	}
	if ag.ID != "claude" {
		t.Fatalf("agent %q", ag.ID)
	}
	if got := outboxCount(t, s); got != 1 {
		t.Fatalf("outbox rows %d, want 1", got)
	}
	// The consume committed: uses incremented despite `audit` being gone —
	// the event is durable in the outbox, not lost.
	sess, err := s.SessionByHash(hash)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Uses != 1 {
		t.Fatalf("uses %d, want 1", sess.Uses)
	}

	if _, err := s.pool.Exec(ctx, `ALTER TABLE audit_gone RENAME TO audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FlushAuditOutbox(500); err != nil {
		t.Fatal(err)
	}
	if got := auditCount(t, s); got != 1 {
		t.Fatalf("audit rows %d, want 1", got)
	}
}
