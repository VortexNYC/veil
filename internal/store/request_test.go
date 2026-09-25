package store

import (
	"context"
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
	WatchRequests(ctx context.Context, orgID string) <-chan struct{}
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

// A level1 denial under real agent concurrency is a stampede on the same
// (grant, action) key — every retrying agent files at once. The partial
// unique index must collapse it to one open ask; callers that lose get the
// existing row back with Created=false. This is the write path the origin
// runs per denied Use, so contention here is the production shape.
func TestFileRequestStampede(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs agents/items/grants")
			}
			putTestGrant(t, full, "grant-a", nil)

			const racers = 32
			var wg sync.WaitGroup
			created := make(chan FileOutcome, racers)
			errs := make(chan error, racers)
			start := time.Now()
			for i := range racers {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					r := openRequest("a")
					r.ID = fmt.Sprintf("req-stampede-%d", i)
					out, err := s.FileRequest(r)
					if err != nil {
						errs <- err
						return
					}
					created <- out
				}(i)
			}
			wg.Wait()
			close(created)
			close(errs)
			for err := range errs {
				t.Fatalf("stampede file: %v", err)
			}
			var winners int
			var liveID string
			for out := range created {
				if out.Created {
					winners++
					liveID = out.Request.ID
				} else if liveID != "" && out.Request.ID != liveID {
					t.Fatalf("dedupe returned a different row: %q vs %q", out.Request.ID, liveID)
				}
			}
			if winners != 1 {
				t.Fatalf("%d creates, want exactly 1", winners)
			}
			opens, err := s.ListRequests("org", protocol.RequestOpen, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(opens) != 1 {
				t.Fatalf("%d open asks after stampede, want 1", len(opens))
			}
			t.Logf("stampede: %d racers -> 1 open ask in %s", racers, time.Since(start).Round(time.Microsecond))
		})
	}
}

// The sweep expires every stale open ask and writes one audit event per row
// in the same transaction. At alpha scale a backlog is hundreds of asks, not
// millions — this proves the loop stays correct and cheap at that size, and
// guards the expire+audit pairing against regressions that drop events.
func TestSweepExpiresAndAuditsAtScale(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs agents/items/grants")
			}
			owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "owner-1"}
			if err := full.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "a", OrgID: "org", Owner: owner}); err != nil {
				t.Fatal(err)
			}
			if err := full.PutItem(protocol.Item{ID: "github", OrgID: "org", Name: "github", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("x")); err != nil {
				t.Fatal(err)
			}
			const asks = 200
			for i := range asks {
				gid := fmt.Sprintf("grant-%d", i)
				if err := full.PutGrant(protocol.Grant{
					ID: gid, OrgID: "org", AgentID: "a", ItemID: "github",
					Level: protocol.Level1,
				}); err != nil {
					t.Fatal(err)
				}
				r := openRequest("a")
				r.ID = fmt.Sprintf("req-scale-%d", i)
				r.GrantID = gid
				r.ExpiresAt = time.Now().Add(-time.Minute) // already stale
				if _, err := s.FileRequest(r); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Now()
			rep, err := s.Sweep(time.Now().Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if rep.Requests != asks {
				t.Fatalf("sweep expired %d asks, want %d", rep.Requests, asks)
			}
			events, err := s.Audit()
			if err != nil {
				t.Fatal(err)
			}
			var expired int
			for _, e := range events {
				if e.Action == protocol.ActionRequestExpired {
					expired++
				}
			}
			if expired != asks {
				t.Fatalf("%d request_expired events, want %d", expired, asks)
			}
			opens, _ := s.ListRequests("org", protocol.RequestOpen, time.Now())
			if len(opens) != 0 {
				t.Fatalf("%d asks still open after sweep", len(opens))
			}
			t.Logf("sweep: %d expire+audit pairs in %s", asks, time.Since(start).Round(time.Millisecond))
		})
	}
}

// countAction tallies audit events of one action naming one request.
func countAction(events []protocol.AuditEvent, action protocol.ActionKind, reason string) int {
	n := 0
	for _, e := range events {
		if e.Action == action && e.Reason == reason {
			n++
		}
	}
	return n
}

// VEIL-50: every request lifecycle transition commits with its audit event —
// the store writes the event inside the state transaction, never after it.
// These tests assert the pairing at the observable level: state visible means
// the event is visible, exactly once.

func TestFileRequestAuditsFiledInCommit(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			events, err := s.Audit()
			if err != nil {
				t.Fatal(err)
			}
			if n := countAction(events, protocol.ActionRequestFiled, out.Request.ID); n != 1 {
				t.Fatalf("filed ask must carry exactly one request_filed event, got %d in %+v", n, events)
			}
			// A deduped refile writes no new row and no new event.
			if _, err := s.FileRequest(openRequest("a")); err != nil {
				t.Fatal(err)
			}
			events, _ = s.Audit()
			if n := countAction(events, protocol.ActionRequestFiled, out.Request.ID); n != 1 {
				t.Fatalf("a deduped refile must not re-audit, got %d events", n)
			}
		})
	}
}

func TestFileRequestRefireAuditsExpiredAndFiled(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			first := openRequest("a")
			first.ExpiresAt = time.Now().Add(time.Minute)
			if _, err := s.FileRequest(first); err != nil {
				t.Fatal(err)
			}
			second := openRequest("a")
			second.ID = "req-a-2"
			second.CreatedAt = time.Now().Add(2 * time.Minute)
			second.ExpiresAt = time.Now().Add(time.Hour)
			out, err := s.FileRequest(second)
			if err != nil {
				t.Fatal(err)
			}
			if !out.Created || out.ExpiredID != first.ID {
				t.Fatalf("refile past expiry must expire the predecessor: %+v", out)
			}
			events, _ := s.Audit()
			if n := countAction(events, protocol.ActionRequestExpired, first.ID); n != 1 {
				t.Fatalf("the expired predecessor must carry request_expired, got %d", n)
			}
			if n := countAction(events, protocol.ActionRequestFiled, second.ID); n != 1 {
				t.Fatalf("the fresh ask must carry request_filed, got %d", n)
			}
		})
	}
}

func TestResolveRequestAuditsDeniedInCommit(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			resolved, won, err := s.ResolveRequest(out.Request.ID, protocol.RequestDenied, "owner-1", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !won || resolved.Status != protocol.RequestDenied {
				t.Fatalf("deny must win: won=%v %+v", won, resolved)
			}
			events, _ := s.Audit()
			if n := countAction(events, protocol.ActionRequestDenied, out.Request.ID); n != 1 {
				t.Fatalf("denial must commit with exactly one request_denied, got %d in %+v", n, events)
			}
		})
	}
}

func TestApproveRequestAuditsTargetAndSiblings(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			full, ok := s.(Store)
			if !ok {
				t.Skip("needs PutGrant")
			}
			putTestGrant(t, full, "grant-a", nil)
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			sib := openRequest("a")
			sib.ID, sib.Action = "req-a-env", protocol.ActionEnv
			if _, err := s.FileRequest(sib); err != nil {
				t.Fatal(err)
			}
			appr := protocol.Approval{ID: "appr-1", HumanID: "owner-1", ExpiresAt: time.Now().Add(time.Hour)}
			resolved, won, err := s.ApproveRequest(out.Request.ID, appr, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !won || len(resolved) != 2 {
				t.Fatalf("one grant approval resolves every open ask: won=%v %+v", won, resolved)
			}
			events, _ := s.Audit()
			for _, r := range resolved {
				if n := countAction(events, protocol.ActionRequestApproved, r.ID); n != 1 {
					t.Fatalf("each resolved ask must carry request_approved, got %d for %s", n, r.ID)
				}
			}
		})
	}
}

func TestCancelRequestsForAgentAuditsCancelled(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			out, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CancelRequestsForAgent("a", time.Now()); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Request(out.Request.ID)
			if got.Status != protocol.RequestCancelled {
				t.Fatalf("agent revoke must cancel its ask, got %s", got.Status)
			}
			events, _ := s.Audit()
			if n := countAction(events, protocol.ActionRequestCancelled, out.Request.ID); n != 1 {
				t.Fatalf("a cancelled ask must carry request_cancelled, got %d in %+v", n, events)
			}
		})
	}
}

// poisonInsert fails every direct INSERT on a table — the failure-injection
// seam for the outbox-fallback tests. Runs against the test's own schema.
func poisonInsert(t *testing.T, pg *Postgres, table string) {
	t.Helper()
	fn := "reject_" + table
	_, err := pg.pool.Exec(context.Background(), fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $f$
		BEGIN RAISE EXCEPTION '%s write rejected'; END; $f$;
		CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s();`,
		fn, table, fn, table, fn))
	if err != nil {
		t.Fatal(err)
	}
}

func tableCount(t *testing.T, pg *Postgres, table string) int {
	t.Helper()
	var n int
	if err := pg.pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s`, table)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A failed audit insert must not kill the file — the event lands in
// audit_outbox inside the same commit, so the durable record exists either
// way and the relay delivers it later.
func TestFileRequestAuditOutboxFallback(t *testing.T) {
	if os.Getenv("PG_TEST_DSN") == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	pg := openTestPostgres(t)
	defer pg.Close()
	poisonInsert(t, pg, "audit")
	out, err := pg.FileRequest(openRequest("a"))
	if err != nil {
		t.Fatalf("audit failure must fall back to the outbox, not fail the file: %v", err)
	}
	if !out.Created {
		t.Fatal("the ask must be created even with audit broken")
	}
	if n := tableCount(t, pg, "audit"); n != 0 {
		t.Fatalf("the rejected insert must not land in audit, got %d rows", n)
	}
	if n := tableCount(t, pg, "audit_outbox"); n != 1 {
		t.Fatalf("the event must be durable in the outbox, got %d rows", n)
	}
	var action string
	if err := pg.pool.QueryRow(context.Background(),
		`SELECT action FROM audit_outbox`).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != string(protocol.ActionRequestFiled) {
		t.Fatalf("outbox must carry request_filed, got %q", action)
	}
	// The relay drains it — the event reaches audit once the table heals.
	pg.pool.Exec(context.Background(), `DROP TRIGGER reject_audit ON audit`)
	if n, err := pg.FlushAuditOutbox(10); err != nil || n != 1 {
		t.Fatalf("flush must deliver the queued event: n=%d err=%v", n, err)
	}
	if n := tableCount(t, pg, "audit"); n != 1 {
		t.Fatalf("flushed event must land in audit, got %d rows", n)
	}
}

// If audit AND outbox are both unwritable the file must fail closed — no
// request row commits without a durable record of it.
func TestFileRequestAuditTotalFailureRollsBack(t *testing.T) {
	if os.Getenv("PG_TEST_DSN") == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	pg := openTestPostgres(t)
	defer pg.Close()
	poisonInsert(t, pg, "audit")
	poisonInsert(t, pg, "audit_outbox")
	_, err := pg.FileRequest(openRequest("a"))
	if err == nil {
		t.Fatal("audit+outbox failure must fail the file — no silent record loss")
	}
	if n := tableCount(t, pg, "approval_requests"); n != 0 {
		t.Fatalf("the request row must roll back with its audit, got %d rows", n)
	}
}
