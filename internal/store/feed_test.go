package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

// feedStore is the SIEM-feed contract: append, then pull org-scoped pages.
type feedStore interface {
	AppendAudits([]protocol.AuditEvent) error
	AuditFeed(orgID string, afterID int64, limit int) ([]protocol.AuditFeedEvent, error)
}

func feedStores(t *testing.T) map[string]feedStore {
	t.Helper()
	dir := t.TempDir()
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	sq, err := OpenSQLite(filepath.Join(dir, "vault.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sq.Close() })
	stores := map[string]feedStore{"memory": NewMemory(), "sqlite": sq}
	if os.Getenv("PG_TEST_DSN") != "" {
		pg := openTestPostgres(t)
		t.Cleanup(func() { pg.Close() })
		stores["postgres"] = pg
	}
	return stores
}

// The feed returns only the caller's org, in id order, resumable by cursor.
// A SIEM can replay every event by looping until a short page lands.
func TestAuditFeedContract(t *testing.T) {
	for name, s := range feedStores(t) {
		t.Run(name, func(t *testing.T) {
			events := []protocol.AuditEvent{
				{Time: time.Unix(1, 0), OrgID: "o", AgentID: "a1", ItemID: "i1", Action: protocol.ActionFetch, Decision: protocol.DecisionAllow},
				{Time: time.Unix(2, 0), OrgID: "other", AgentID: "x", ItemID: "i9", Action: protocol.ActionFetch, Decision: protocol.DecisionDeny},
				{Time: time.Unix(3, 0), OrgID: "o", AgentID: "a2", ItemID: "i2", Action: protocol.ActionEnv, Decision: protocol.DecisionAllow},
				{Time: time.Unix(4, 0), OrgID: "o", AgentID: "a1", ItemID: "i1", Action: protocol.ActionFetch, Decision: protocol.DecisionNeedApproval, Reason: "policy"},
			}
			if err := s.AppendAudits(events); err != nil {
				t.Fatal(err)
			}

			page, err := s.AuditFeed("o", 0, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) != 2 {
				t.Fatalf("page 1: got %d events %+v", len(page), page)
			}
			for _, e := range page {
				if e.OrgID != "o" {
					t.Fatalf("cross-org event leaked: %+v", e)
				}
			}
			if page[0].AgentID != "a1" || page[1].AgentID != "a2" {
				t.Fatalf("order wrong: %+v", page)
			}
			if page[0].ID >= page[1].ID {
				t.Fatalf("ids not ascending: %+v", page)
			}

			rest, err := s.AuditFeed("o", page[1].ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(rest) != 1 || rest[0].Reason != "policy" {
				t.Fatalf("page 2: got %+v", rest)
			}

			tail, err := s.AuditFeed("o", rest[0].ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(tail) != 0 {
				t.Fatalf("cursor past end should drain: %+v", tail)
			}
		})
	}
}

// Idempotent replay: re-polling the last seen id returns nothing new until
// a later append lands — the checkpoint contract.
func TestAuditFeedReplay(t *testing.T) {
	for name, s := range feedStores(t) {
		t.Run(name, func(t *testing.T) {
			first := []protocol.AuditEvent{
				{Time: time.Unix(1, 0), OrgID: "o", AgentID: "a1", Action: protocol.ActionFetch, Decision: protocol.DecisionAllow},
			}
			if err := s.AppendAudits(first); err != nil {
				t.Fatal(err)
			}
			page, err := s.AuditFeed("o", 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) != 1 {
				t.Fatalf("got %+v", page)
			}
			again, err := s.AuditFeed("o", page[0].ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(again) != 0 {
				t.Fatalf("replay at cursor returned %+v", again)
			}
			if err := s.AppendAudits([]protocol.AuditEvent{
				{Time: time.Unix(2, 0), OrgID: "o", AgentID: "a1", Action: protocol.ActionEnv, Decision: protocol.DecisionAllow},
			}); err != nil {
				t.Fatal(err)
			}
			grown, err := s.AuditFeed("o", page[0].ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(grown) != 1 || grown[0].Action != protocol.ActionEnv {
				t.Fatalf("post-append poll: %+v", grown)
			}
		})
	}
}
