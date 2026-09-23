package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

func testStores(t *testing.T) []Store {
	t.Helper()
	out := []Store{NewMemory()}

	dir := t.TempDir()
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	sql, err := OpenSQLite(filepath.Join(dir, "vault.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, sql)

	if dsn := os.Getenv("PG_TEST_DSN"); dsn != "" {
		pg := openTestPostgres(t)
		out = append(out, pg)
	}
	return out
}

func seedSessionStore(t *testing.T, s Store) (hash []byte, itemID string) {
	t.Helper()
	org := protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"}
	item := protocol.Item{
		ID:    "stripe",
		OrgID: "org",
		Name:  "stripe",
		Kind:  protocol.ItemAPIKey,
		Owner: org,
		URIs:  []string{"https://api.stripe.com"},
	}
	if err := s.PutItem(item, Secret("sk_live_secret")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org", Owner: org}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGrant(protocol.Grant{
		ID:      "claude:stripe",
		OrgID:   "org",
		AgentID: "claude",
		ItemID:  "stripe",
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}

	sess := protocol.Session{
		ID:        "ses_1",
		OrgID:     "org",
		AgentID:   "claude",
		CreatedAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		TTL:       int64(time.Hour.Seconds()),
		MaxTTL:    int64((2 * time.Hour).Seconds()),
		MaxUses:   0,
		Uses:      0,
	}
	token := "ses_01010101010101010101010101010101"
	sum := sha256.Sum256([]byte(token))
	hash = sum[:]
	if err := s.PutSession(sess, hash); err != nil {
		t.Fatal(err)
	}
	return hash, item.ID
}

func TestSessionLifecycle(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, _ := seedSessionStore(t, s)

			byHash, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			if byHash.ID != "ses_1" || byHash.AgentID != "claude" {
				t.Fatalf("byHash %+v", byHash)
			}

			byID, err := s.SessionByID(byHash.ID)
			if err != nil {
				t.Fatal(err)
			}
			if byID.ID != byHash.ID {
				t.Fatalf("byID %+v", byID)
			}

			list, err := s.ListSessions()
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 || list[0].ID != byHash.ID {
				t.Fatalf("list %+v", list)
			}

			first := time.Now().UTC()
			if err := s.RevokeSession(byHash.ID, first); err != nil {
				t.Fatal(err)
			}
			if err := s.RevokeSession(byHash.ID, first.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			got, err := s.SessionByID(byHash.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.RevokedAt == nil || got.RevokedAt.UTC().Format(time.RFC3339) != first.UTC().Format(time.RFC3339) {
				t.Fatalf("revoked_at not preserved: %+v", got.RevokedAt)
			}

			_, err = s.UseAuthSession(hash, "stripe", time.Now())
			if !errors.Is(err, ErrSessionRevoked) && !errors.Is(err, ErrNotFound) {
				t.Fatalf("use revoked session: %v", err)
			}
		})
	}
}

func TestSessionRenew(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, _ := seedSessionStore(t, s)

			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}

			renewed, err := s.RenewSession(sess.ID, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !renewed.ExpiresAt.After(sess.ExpiresAt) {
				t.Fatalf("expires_at did not advance: %v -> %v", sess.ExpiresAt, renewed.ExpiresAt)
			}
			if renewed.RenewedAt == nil {
				t.Fatal("renewed_at not set")
			}

			// Repeated renews are capped at created_at + MaxTTL.
			for i := 0; i < 10; i++ {
				if _, err := s.RenewSession(sess.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			final, err := s.SessionByID(sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			maxExpires := sess.CreatedAt.Add(time.Duration(sess.MaxTTL) * time.Second)
			if final.ExpiresAt.After(maxExpires.Add(time.Second)) {
				t.Fatalf("expires_at exceeded max_ttl: %v > %v", final.ExpiresAt, maxExpires)
			}
		})
	}
}

func TestSessionUseAuthSessionAndConsume(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, itemID := seedSessionStore(t, s)

			now := time.Now()
			auth, err := s.UseAuthSession(hash, itemID, now)
			if err != nil {
				t.Fatal(err)
			}
			if auth.Agent.ID != "claude" || auth.Item.ID != itemID || auth.Grant == nil {
				t.Fatalf("auth %+v", auth)
			}

			agent, err := s.ConsumeSession(hash, now)
			if err != nil {
				t.Fatal(err)
			}
			if agent.ID != "claude" {
				t.Fatalf("agent %+v", agent)
			}

			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			if sess.Uses != 1 {
				t.Fatalf("uses=%d", sess.Uses)
			}
		})
	}
}

func TestSessionMaxUses(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, itemID := seedSessionStore(t, s)

			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			sess.MaxUses = 3
			sess.Uses = 0
			if err := s.PutSession(sess, hash); err != nil {
				t.Fatal(err)
			}

			for i := 0; i < 3; i++ {
				if _, err := s.ConsumeSession(hash, time.Now()); err != nil {
					t.Fatalf("iter %d: %v", i, err)
				}
			}

			_, err = s.ConsumeSession(hash, time.Now())
			if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrDenied) {
				t.Fatalf("expected exhaustion, got %v", err)
			}

			_, err = s.UseAuthSession([]byte(hash), itemID, time.Now())
			if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrDenied) {
				t.Fatalf("UseAuthSession should fail exhausted session: %v", err)
			}
		})
	}
}

func TestSessionInvalidItemDoesNotConsume(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, _ := seedSessionStore(t, s)

			// UseAuthSession is read-only and does not validate item existence.
			// It must not consume a use.
			_, err := s.UseAuthSession(hash, "missing-item", time.Now())
			if err != nil {
				t.Fatal(err)
			}

			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			if sess.Uses != 0 {
				t.Fatalf("uses consumed for missing item: %d", sess.Uses)
			}
		})
	}
}

func TestSessionConcurrentConsume(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, _ := seedSessionStore(t, s)

			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			sess.MaxUses = 5
			sess.Uses = 0
			if err := s.PutSession(sess, hash); err != nil {
				t.Fatal(err)
			}

			var wg sync.WaitGroup
			success := make(chan struct{}, 20)
			fail := make(chan struct{}, 20)
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := s.ConsumeSession(hash, time.Now()); err == nil {
						success <- struct{}{}
					} else {
						fail <- struct{}{}
					}
				}()
			}
			wg.Wait()
			close(success)
			close(fail)

			successCount := 0
			for range success {
				successCount++
			}
			failCount := 0
			for range fail {
				failCount++
			}
			if successCount != 5 || failCount != 15 {
				t.Fatalf("success=%d fail=%d", successCount, failCount)
			}

			sess, err = s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			if sess.Uses != 5 {
				t.Fatalf("uses=%d", sess.Uses)
			}
		})
	}
}

func TestSessionAgentRevokeDeniesConsume(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, _ := seedSessionStore(t, s)

			if err := s.RevokeAgent("claude", time.Now()); err != nil {
				t.Fatal(err)
			}

			_, err := s.ConsumeSession(hash, time.Now())
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected denied for revoked agent, got %v", err)
			}
		})
	}
}

func TestSessionExpiry(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, itemID := seedSessionStore(t, s)

			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			sess.ExpiresAt = time.Now().UTC().Add(-time.Second)
			if err := s.PutSession(sess, hash); err != nil {
				t.Fatal(err)
			}

			_, err = s.UseAuthSession(hash, itemID, time.Now())
			if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrSessionExpired) {
				t.Fatalf("expected expired session to fail: %v", err)
			}
		})
	}
}

func TestSessionConsumeAudited(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, itemID := seedSessionStore(t, s)
			now := time.Now()

			// The event's OrgID/AgentID are filled from the consumed agent.
			evt := protocol.AuditEvent{
				Time: now, ItemID: itemID, Action: protocol.ActionFetch,
				Decision: protocol.DecisionAllow,
			}
			agent, err := s.ConsumeSessionAudited(hash, now, evt)
			if err != nil {
				t.Fatal(err)
			}
			if agent.ID != "claude" {
				t.Fatalf("agent %+v", agent)
			}
			sess, err := s.SessionByHash(hash)
			if err != nil {
				t.Fatal(err)
			}
			if sess.Uses != 1 {
				t.Fatalf("uses=%d", sess.Uses)
			}
			events, err := s.Audit()
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 {
				t.Fatalf("audit rows=%d", len(events))
			}
			got := events[0]
			if got.AgentID != "claude" || got.OrgID != "org" || got.ItemID != itemID || got.Decision != protocol.DecisionAllow {
				t.Fatalf("event %+v", got)
			}

			// A refused consume must not append an audit row.
			bad := sha256.Sum256([]byte("ses_99999999999999999999999999999999"))
			if _, err := s.ConsumeSessionAudited(bad[:], now, evt); !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected ErrNotFound: %v", err)
			}
			events, err = s.Audit()
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 {
				t.Fatalf("audit rows after failed consume=%d", len(events))
			}
		})
	}
}

// A failed audit insert must roll the consume back: no credential may be
// released without its audit row. Memory cannot inject the failure — its
// append is in-memory under the same lock — so this exercises the real
// transaction on the durable stores.
func TestSessionConsumeAuditedRollback(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			hash, itemID := seedSessionStore(t, s)
			now := time.Now()
			evt := protocol.AuditEvent{
				Time: now, ItemID: itemID, Action: protocol.ActionFetch,
				Decision: protocol.DecisionAllow,
			}

			switch st := s.(type) {
			case *SQLite:
				if _, err := st.db.Exec(`DROP TABLE audit`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := EnsureSQLiteSchema(st.db); err != nil {
						t.Fatal(err)
					}
				}()
				// No outbox on sqlite: audit-write failure still rolls the
				// consume back.
				if _, err := s.ConsumeSessionAudited(hash, now, evt); err == nil {
					t.Fatal("expected audit insert failure")
				}
				sess, err := s.SessionByHash(hash)
				if err != nil {
					t.Fatal(err)
				}
				if sess.Uses != 0 {
					t.Fatalf("consume not rolled back: uses=%d", sess.Uses)
				}
			case *Postgres:
				ctx := context.Background()
				if _, err := st.pool.Exec(ctx, `DROP TABLE audit`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := st.migrate(); err != nil {
						t.Fatal(err)
					}
				}()
				// audit down but audit_outbox up: the event commits with the
				// consume — durable in the outbox — and the use proceeds.
				if _, err := s.ConsumeSessionAudited(hash, now, evt); err != nil {
					t.Fatalf("outbox fallback should commit: %v", err)
				}
				sess, err := s.SessionByHash(hash)
				if err != nil {
					t.Fatal(err)
				}
				if sess.Uses != 1 {
					t.Fatalf("consume should commit with outbox: uses=%d", sess.Uses)
				}
				// Both down: the consume truly fails and rolls back.
				if _, err := st.pool.Exec(ctx, `DROP TABLE audit_outbox`); err != nil {
					t.Fatal(err)
				}
				if _, err := s.ConsumeSessionAudited(hash, now, evt); err == nil {
					t.Fatal("expected failure with audit and outbox down")
				}
				sess, err = s.SessionByHash(hash)
				if err != nil {
					t.Fatal(err)
				}
				if sess.Uses != 1 {
					t.Fatalf("double consume on total outage: uses=%d", sess.Uses)
				}
			default:
				t.Skip("no failure injection")
			}
		})
	}
}
