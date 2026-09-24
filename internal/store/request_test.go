package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

type requestStore interface {
	FileRequest(protocol.ApprovalRequest) (FileOutcome, error)
	Request(id string) (protocol.ApprovalRequest, error)
	ListRequests(orgID string, status protocol.RequestStatus, now time.Time) ([]protocol.ApprovalRequest, error)
	ResolveRequest(id string, status protocol.RequestStatus, humanID, approvalID string, at time.Time) (protocol.ApprovalRequest, bool, error)
	ApproveRequest(id string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, bool, error)
	ApproveGrant(grantID string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, error)
	CancelRequestsForAgent(agentID string, at time.Time) error
	ExpireStaleRequests(now time.Time) ([]protocol.ApprovalRequest, error)
	Sweep(olderThan time.Time) (SweepReport, error)
	Audit() ([]protocol.AuditEvent, error)
}

func requestStores(t *testing.T) map[string]requestStore {
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
	stores := map[string]requestStore{"memory": NewMemory(), "sqlite": sq}
	if os.Getenv("PG_TEST_DSN") != "" {
		pg := openTestPostgres(t)
		t.Cleanup(func() { pg.Close() })
		stores["postgres"] = pg
	}
	return stores
}

func openRequest(agent string) protocol.ApprovalRequest {
	return protocol.ApprovalRequest{
		ID:        "req-" + agent,
		OrgID:     "org",
		AgentID:   agent,
		ItemID:    "github",
		GrantID:   "grant-" + agent,
		Action:    protocol.ActionFetch,
		Status:    protocol.RequestOpen,
		CreatedAt: time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func putTestGrant(t *testing.T, s Store, id string, exp *time.Time) {
	t.Helper()
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "owner-1"}
	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "a", OrgID: "org", Owner: owner}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{ID: "github", OrgID: "org", Name: "github", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGrant(protocol.Grant{
		ID: id, OrgID: "org", AgentID: "a", ItemID: "github",
		Level: protocol.Level1, ExpiresAt: exp,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFileRequestDedupesOpen(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			if !out.Created || out.Request.Status != protocol.RequestOpen || out.ExpiredID != "" {
				t.Fatalf("first file: %+v", out)
			}
			again, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			if again.Created || again.Request.ID != out.Request.ID {
				t.Fatalf("refile must reuse the open request: %+v", again)
			}
		})
	}
}

func TestFileRequestRefiresAfterExpiry(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			stale := openRequest("a")
			stale.ExpiresAt = time.Now().Add(-time.Minute)
			if _, err := s.FileRequest(stale); err != nil {
				t.Fatal(err)
			}
			fresh := openRequest("a")
			fresh.ID = "req-a-2"
			out, err := s.FileRequest(fresh)
			if err != nil {
				t.Fatal(err)
			}
			if !out.Created || out.Request.ID != "req-a-2" {
				t.Fatalf("expired open must not block a fresh file: %+v", out)
			}
			if out.ExpiredID != "req-a" {
				t.Fatalf("the expired ask must be named for audit, got %q", out.ExpiredID)
			}
			old, err := s.Request("req-a")
			if err != nil {
				t.Fatal(err)
			}
			if old.Status != protocol.RequestExpired {
				t.Fatalf("stale open must be marked expired, got %s", old.Status)
			}
		})
	}
}

func TestResolveRequestFirstWriteWins(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			won, ok, err := s.ResolveRequest(out.Request.ID, protocol.RequestApproved, "owner-1", "appr-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !ok || won.Status != protocol.RequestApproved || won.ResolvedBy != "owner-1" || won.ApprovalID != "appr-1" {
				t.Fatalf("first resolve must win: %+v ok=%v", won, ok)
			}
			lost, ok, err := s.ResolveRequest(out.Request.ID, protocol.RequestDenied, "owner-2", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Fatalf("second resolve must lose, got %+v", lost)
			}
			got, err := s.Request(out.Request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.ResolvedBy != "owner-1" {
				t.Fatalf("resolved_by must name the winner, got %q", got.ResolvedBy)
			}
		})
	}
}

func TestResolveRequestExpiredOpenLoses(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			stale := openRequest("a")
			stale.ExpiresAt = time.Now().Add(-time.Minute)
			out, err := s.FileRequest(stale)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.ResolveRequest(out.Request.ID, protocol.RequestApproved, "owner-1", "appr-1", time.Now()); err != nil {
				t.Fatal(err)
			} else if ok {
				t.Fatal("an expired request must not resolve")
			}
		})
	}
}

func TestApproveRequestAtomic(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs PutGrant + LiveApproval")
			}
			putTestGrant(t, full, "grant-a", nil)
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			appr := protocol.Approval{ID: "appr-1", HumanID: "owner-1", ExpiresAt: time.Now().Add(time.Hour)}
			resolved, won, err := s.ApproveRequest(out.Request.ID, appr, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !won || len(resolved) != 1 || resolved[0].ApprovalID != "appr-1" {
				t.Fatalf("approve must win and name its approval: won=%v %+v", won, resolved)
			}
			live, err := full.LiveApproval("grant-a", time.Now())
			if err != nil || live == nil || live.ID != "appr-1" {
				t.Fatalf("approval must be minted in the same commit: %v %v", live, err)
			}
			// Losing the race writes nothing: no second approval, no flip.
			appr2 := protocol.Approval{ID: "appr-2", HumanID: "owner-2", ExpiresAt: time.Now().Add(time.Hour)}
			_, won, err = s.ApproveRequest(out.Request.ID, appr2, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if won {
				t.Fatal("second approve must lose")
			}
			live, _ = full.LiveApproval("grant-a", time.Now())
			if live == nil || live.ID != "appr-1" || live.HumanID != "owner-1" {
				t.Fatalf("a lost approve must leave no side effects: %+v", live)
			}
		})
	}
}

func TestApproveRequestDeadGrantLoses(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs PutGrant")
			}
			dead := time.Now().Add(-time.Minute)
			putTestGrant(t, full, "grant-a", &dead)
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			appr := protocol.Approval{ID: "appr-1", HumanID: "owner-1", ExpiresAt: time.Now().Add(time.Hour)}
			_, won, err := s.ApproveRequest(out.Request.ID, appr, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if won {
				t.Fatal("an ask on a dead grant must not approve")
			}
			if live, _ := full.LiveApproval("grant-a", time.Now()); live != nil {
				t.Fatalf("a lost approve must not mint the approval: %+v", live)
			}
			got, _ := s.Request(out.Request.ID)
			if got.Status != protocol.RequestOpen {
				t.Fatalf("the ask stays open (expires on its own TTL), got %s", got.Status)
			}
		})
	}
}

func TestApproveGrantResolvesAsksAtomically(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs PutGrant + LiveApproval")
			}
			putTestGrant(t, full, "grant-a", nil)
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			appr := protocol.Approval{ID: "appr-1", HumanID: "owner-1", ExpiresAt: time.Now().Add(time.Hour)}
			resolved, err := s.ApproveGrant("grant-a", appr, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(resolved) != 1 || resolved[0].ID != out.Request.ID || resolved[0].Status != protocol.RequestApproved {
				t.Fatalf("grant approve must resolve open asks: %+v", resolved)
			}
			live, err := full.LiveApproval("grant-a", time.Now())
			if err != nil || live == nil || live.ID != "appr-1" {
				t.Fatalf("approval must land in the same commit: %v %v", live, err)
			}
		})
	}
}

func TestCancelRequestsForAgent(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.FileRequest(openRequest("a")); err != nil {
				t.Fatal(err)
			}
			b := openRequest("b")
			b.ID, b.GrantID = "req-b", "grant-b"
			if _, err := s.FileRequest(b); err != nil {
				t.Fatal(err)
			}
			if err := s.CancelRequestsForAgent("b", time.Now()); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Request("req-b")
			if got.Status != protocol.RequestCancelled {
				t.Fatalf("agent revoke must cancel its ask, got %s", got.Status)
			}
			got, _ = s.Request("req-a")
			if got.Status != protocol.RequestOpen {
				t.Fatalf("other agents' asks stay open, got %s", got.Status)
			}
		})
	}
}

func TestListRequestsFiltersLiveOpens(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.FileRequest(openRequest("a")); err != nil {
				t.Fatal(err)
			}
			stale := openRequest("b")
			stale.ID, stale.GrantID, stale.ExpiresAt = "req-b", "grant-b", time.Now().Add(-time.Minute)
			if _, err := s.FileRequest(stale); err != nil {
				t.Fatal(err)
			}
			open, err := s.ListRequests("org", protocol.RequestOpen, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(open) != 1 || open[0].ID != "req-a" {
				t.Fatalf("live opens only, got %+v", open)
			}
			expired, err := s.ExpireStaleRequests(time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(expired) != 1 || expired[0].ID != "req-b" {
				t.Fatalf("sweep returns the rows it expired for audit: %+v", expired)
			}
			exp, err := s.ListRequests("org", protocol.RequestExpired, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(exp) != 1 || exp[0].ID != "req-b" {
				t.Fatalf("sweep must mark stale opens expired, got %+v", exp)
			}
		})
	}
}

func TestApproveRequestDeadEdgesLose(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs agents/items/grants")
			}
			putTestGrant(t, full, "grant-a", nil)
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			// Agent revoked mid-ask → the approve edge is dead.
			if err := full.RevokeAgent("a", time.Now()); err != nil {
				t.Fatal(err)
			}
			appr := protocol.Approval{ID: "appr-1", HumanID: "owner-1", ExpiresAt: time.Now().Add(time.Hour)}
			_, won, err := s.ApproveRequest(out.Request.ID, appr, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if won {
				t.Fatal("an ask whose agent was revoked must not approve")
			}
			if live, _ := full.LiveApproval("grant-a", time.Now()); live != nil {
				t.Fatal("a lost approve must not mint the approval")
			}
			// Grant-scoped approve on the same dead edge fails outright.
			if _, err := s.ApproveGrant("grant-a", appr, time.Now()); err != ErrGrantNotLive {
				t.Fatalf("grant approve on a revoked agent must fail, got %v", err)
			}
		})
	}
}

func TestApproveRequestRaceFirstWriteWins(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs agents/items/grants")
			}
			putTestGrant(t, full, "grant-a", nil)
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			// N owners answer the same ask at once: exactly one wins, every
			// loser writes nothing — no duplicate approvals, no torn state.
			const racers = 8
			var wg sync.WaitGroup
			wins := make(chan bool, racers)
			for i := range racers {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					appr := protocol.Approval{
						ID: fmt.Sprintf("appr-%d", i), HumanID: fmt.Sprintf("owner-%d", i),
						ExpiresAt: time.Now().Add(time.Hour),
					}
					_, won, err := s.ApproveRequest(out.Request.ID, appr, time.Now())
					if err != nil {
						t.Errorf("approve racer %d: %v", i, err)
						return
					}
					wins <- won
				}(i)
			}
			wg.Wait()
			close(wins)
			n := 0
			for won := range wins {
				if won {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("%d winners, want exactly 1", n)
			}
			req, err := s.Request(out.Request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if req.Status != protocol.RequestApproved || req.ApprovalID == "" {
				t.Fatalf("ask must be approved once: %+v", req)
			}
			if live, _ := full.LiveApproval("grant-a", time.Now()); live == nil || live.ID != req.ApprovalID {
				t.Fatalf("exactly one approval minted, named by the ask: %+v vs %q", live, req.ApprovalID)
			}
		})
	}
}

func TestSweepAuditsRequestExpired(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			stale := openRequest("a")
			stale.ExpiresAt = time.Now().Add(-time.Minute)
			if _, err := s.FileRequest(stale); err != nil {
				t.Fatal(err)
			}
			rep, err := s.Sweep(time.Now().Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if rep.Requests != 1 {
				t.Fatalf("sweep must report the expired ask: %+v", rep)
			}
			events, err := s.Audit()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range events {
				if e.Action == protocol.ActionRequestExpired && e.Reason == stale.ID {
					found = true
				}
			}
			if !found {
				t.Fatalf("sweep must audit request_expired naming the ask, got %+v", events)
			}
		})
	}
}
