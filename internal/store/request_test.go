package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

type requestStore interface {
	FileRequest(protocol.ApprovalRequest) (protocol.ApprovalRequest, bool, error)
	Request(id string) (protocol.ApprovalRequest, error)
	ListRequests(orgID string, status protocol.RequestStatus, now time.Time) ([]protocol.ApprovalRequest, error)
	ResolveRequest(id string, status protocol.RequestStatus, humanID, approvalID string, at time.Time) (protocol.ApprovalRequest, bool, error)
	CancelRequestsForGrant(grantID string, at time.Time) error
	CancelRequestsForAgent(agentID string, at time.Time) error
	ExpireStaleRequests(now time.Time) error
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

func TestFileRequestDedupesOpen(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			req, created, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			if !created || req.Status != protocol.RequestOpen {
				t.Fatalf("first file: created=%v status=%s", created, req.Status)
			}
			again, created, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			if created || again.ID != req.ID {
				t.Fatalf("refile must reuse the open request: created=%v id=%s want %s", created, again.ID, req.ID)
			}
		})
	}
}

func TestFileRequestRefiresAfterExpiry(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			stale := openRequest("a")
			stale.ExpiresAt = time.Now().Add(-time.Minute)
			if _, _, err := s.FileRequest(stale); err != nil {
				t.Fatal(err)
			}
			fresh := openRequest("a")
			fresh.ID = "req-a-2"
			got, created, err := s.FileRequest(fresh)
			if err != nil {
				t.Fatal(err)
			}
			if !created || got.ID != "req-a-2" {
				t.Fatalf("expired open must not block a fresh file: created=%v id=%s", created, got.ID)
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
			req, _, err := s.FileRequest(openRequest("a"))
			if err != nil {
				t.Fatal(err)
			}
			won, ok, err := s.ResolveRequest(req.ID, protocol.RequestApproved, "owner-1", "appr-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !ok || won.Status != protocol.RequestApproved || won.ResolvedBy != "owner-1" || won.ApprovalID != "appr-1" {
				t.Fatalf("first resolve must win: %+v ok=%v", won, ok)
			}
			lost, ok, err := s.ResolveRequest(req.ID, protocol.RequestDenied, "owner-2", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Fatalf("second resolve must lose, got %+v", lost)
			}
			got, err := s.Request(req.ID)
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
			req, _, err := s.FileRequest(stale)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.ResolveRequest(req.ID, protocol.RequestApproved, "owner-1", "appr-1", time.Now()); err != nil {
				t.Fatal(err)
			} else if ok {
				t.Fatal("an expired request must not resolve")
			}
		})
	}
}

func TestCancelRequestsForGrantAndAgent(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.FileRequest(openRequest("a")); err != nil {
				t.Fatal(err)
			}
			b := openRequest("b")
			b.ID, b.GrantID = "req-b", "grant-b"
			if _, _, err := s.FileRequest(b); err != nil {
				t.Fatal(err)
			}
			if err := s.CancelRequestsForGrant("grant-a", time.Now()); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Request("req-a")
			if got.Status != protocol.RequestCancelled {
				t.Fatalf("grant revoke must cancel its ask, got %s", got.Status)
			}
			if err := s.CancelRequestsForAgent("b", time.Now()); err != nil {
				t.Fatal(err)
			}
			got, _ = s.Request("req-b")
			if got.Status != protocol.RequestCancelled {
				t.Fatalf("agent revoke must cancel its ask, got %s", got.Status)
			}
		})
	}
}

func TestListRequestsFiltersLiveOpens(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.FileRequest(openRequest("a")); err != nil {
				t.Fatal(err)
			}
			stale := openRequest("b")
			stale.ID, stale.GrantID, stale.ExpiresAt = "req-b", "grant-b", time.Now().Add(-time.Minute)
			if _, _, err := s.FileRequest(stale); err != nil {
				t.Fatal(err)
			}
			open, err := s.ListRequests("org", protocol.RequestOpen, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(open) != 1 || open[0].ID != "req-a" {
				t.Fatalf("live opens only, got %+v", open)
			}
			if err := s.ExpireStaleRequests(time.Now()); err != nil {
				t.Fatal(err)
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
