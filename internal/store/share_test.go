package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

func putTestShare(t *testing.T, s Store, id string, expiresAt time.Time) protocol.Share {
	t.Helper()
	now := time.Now().UTC()
	sh := protocol.Share{
		ID: id, OrgID: "org", Name: "stripe-key",
		Blob: []byte("sealed-envelope"), Meta: []byte(`{"v":1,"alg":"nacl.secretbox"}`),
		CreatedBy: "self", ExpiresAt: expiresAt.UTC(), CreatedAt: now,
	}
	if err := s.PutShare(sh); err != nil {
		t.Fatal(err)
	}
	return sh
}

func TestShareConsumeOnce(t *testing.T) {
	for _, s := range testStores(t) {
		t.Run(fmt.Sprintf("%T", s), func(t *testing.T) {
			defer s.Close()
			sh := putTestShare(t, s, "shr_1", time.Now().Add(time.Hour))

			got, err := s.ConsumeShare(sh.ID, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Blob) != "sealed-envelope" {
				t.Fatalf("blob %q", got.Blob)
			}
			if got.ConsumedAt == nil {
				t.Fatal("consumed_at not stamped")
			}
			// One-time: the second read is the generic miss.
			if _, err := s.ConsumeShare(sh.ID, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
				t.Fatalf("second consume: %v", err)
			}
		})
	}
}

func TestShareConsumeMissingExpiredRevoked(t *testing.T) {
	for _, s := range testStores(t) {
		t.Run(fmt.Sprintf("%T", s), func(t *testing.T) {
			defer s.Close()
			now := time.Now().UTC()
			if _, err := s.ConsumeShare("shr_missing", now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing: %v", err)
			}
			exp := putTestShare(t, s, "shr_exp", now.Add(-time.Minute))
			if _, err := s.ConsumeShare(exp.ID, now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("expired: %v", err)
			}
			rev := putTestShare(t, s, "shr_rev", now.Add(time.Hour))
			if err := s.RevokeShare(rev.ID, "org", now); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ConsumeShare(rev.ID, now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("revoked: %v", err)
			}
		})
	}
}

func TestShareRevokeOrgScoped(t *testing.T) {
	for _, s := range testStores(t) {
		t.Run(fmt.Sprintf("%T", s), func(t *testing.T) {
			defer s.Close()
			now := time.Now().UTC()
			sh := putTestShare(t, s, "shr_1", now.Add(time.Hour))
			if err := s.RevokeShare(sh.ID, "org-other", now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-org revoke: %v", err)
			}
			if err := s.RevokeShare("shr_missing", "org", now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing revoke: %v", err)
			}
			if err := s.RevokeShare(sh.ID, "org", now); err != nil {
				t.Fatal(err)
			}
			if err := s.RevokeShare(sh.ID, "org", now.Add(time.Minute)); err != nil {
				t.Fatalf("second revoke should be a no-op: %v", err)
			}
		})
	}
}

// The blob is redeem-once material — owner list must never carry it.
func TestShareListNeverBlob(t *testing.T) {
	for _, s := range testStores(t) {
		t.Run(fmt.Sprintf("%T", s), func(t *testing.T) {
			defer s.Close()
			putTestShare(t, s, "shr_1", time.Now().Add(time.Hour))
			putTestShare(t, s, "shr_2", time.Now().Add(time.Hour))
			got, err := s.ListShares("org")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 {
				t.Fatalf("list %+v", got)
			}
			for _, sh := range got {
				if len(sh.Blob) != 0 {
					t.Fatal("list leaked the sealed blob")
				}
				if sh.Name != "stripe-key" || sh.OrgID != "org" {
					t.Fatalf("share %+v", sh)
				}
			}
			other, err := s.ListShares("org-other")
			if err != nil {
				t.Fatal(err)
			}
			if len(other) != 0 {
				t.Fatalf("cross-org list: %+v", other)
			}
		})
	}
}

func TestShareSweep(t *testing.T) {
	for _, s := range testStores(t) {
		t.Run(fmt.Sprintf("%T", s), func(t *testing.T) {
			defer s.Close()
			now := time.Now().UTC()
			old := now.Add(-48 * time.Hour)

			putTestShare(t, s, "shr_expired", old)
			live := putTestShare(t, s, "shr_live", now.Add(time.Hour))
			cons := putTestShare(t, s, "shr_consumed", now.Add(time.Hour))
			if _, err := s.ConsumeShare(cons.ID, old); err != nil {
				t.Fatal(err)
			}
			rev := putTestShare(t, s, "shr_revoked", now.Add(time.Hour))
			if err := s.RevokeShare(rev.ID, "org", old); err != nil {
				t.Fatal(err)
			}
			fresh := putTestShare(t, s, "shr_fresh_consumed", now.Add(time.Hour))
			if _, err := s.ConsumeShare(fresh.ID, now.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}

			if _, err := s.Sweep(now.Add(-24 * time.Hour)); err != nil {
				t.Fatal(err)
			}
			got, err := s.ListShares("org")
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]bool{}
			for _, sh := range got {
				ids[sh.ID] = true
			}
			for _, dead := range []string{"shr_expired", "shr_consumed", "shr_revoked"} {
				if ids[dead] {
					t.Fatalf("sweep kept %s", dead)
				}
			}
			if !ids[live.ID] || !ids[fresh.ID] {
				t.Fatalf("sweep dropped live shares: %v", ids)
			}
		})
	}
}
