package store

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

// TestPostgresRecoveryWrap proves the owner-recovery path (VEIL-13): the org
// master is escrowed under owner-held material — KEK-independent. Wrong
// material, expiry, and replay all fail closed; a successful open restores
// the master and can re-seed the org under a new KEK.
func TestPostgresRecoveryWrap(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	kek := testMasterKey(t)
	s := rotateSchema(t, dsn, "rec_wrap", kek)
	defer s.Close()

	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner,
	}, Secret("recoverable-secret")); err != nil {
		t.Fatal(err)
	}

	recoveryKey, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreRecoveryWrap(ctx, "org", owner, recoveryKey, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// Wrong material fails and does NOT consume the wrap.
	wrong, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenRecoveryWrap(ctx, "org", owner, wrong); err == nil {
		t.Fatal("wrong recovery key opened the wrap")
	}
	// Wrong owner AAD fails too — a wrap copied to another owner doesn't open.
	if _, err := s.OpenRecoveryWrap(ctx, "org", protocol.Owner{Kind: protocol.OwnerUser, ID: "human-2"}, recoveryKey); err == nil {
		t.Fatal("wrap opened under a different owner")
	}
	// Legit open returns the committed master.
	got, err := s.OpenRecoveryWrap(ctx, "org", owner, recoveryKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(master) {
		t.Fatal("recovered master mismatch")
	}
	// Replay: single-use — used_at is stamped.
	if _, err := s.OpenRecoveryWrap(ctx, "org", owner, recoveryKey); err == nil {
		t.Fatal("used recovery wrap opened twice")
	}

	// Re-mint replaces and clears used_at.
	recoveryKey2, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreRecoveryWrap(ctx, "org", owner, recoveryKey2, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenRecoveryWrap(ctx, "org", owner, recoveryKey2); err != nil {
		t.Fatalf("re-minted wrap did not open: %v", err)
	}
}

// TestPostgresRecoveryWrapExpiry: an expired wrap fails closed even with the
// right material.
func TestPostgresRecoveryWrapExpiry(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_exp", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	recoveryKey, _ := crypto.NewKey()
	if err := s.StoreRecoveryWrap(ctx, "org", owner, recoveryKey, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenRecoveryWrap(ctx, "org", owner, recoveryKey); err == nil {
		t.Fatal("expired wrap opened")
	}
}

// TestPostgresRecoveryAfterKEKLoss is the incident path: the deployment KEK
// is gone, but an owner's recovery wrap still opens the org master — re-seed
// org_keys under a fresh KEK and the vault is live again. This is the escape
// hatch that makes lost-KEK recoverable instead of catastrophic.
func TestPostgresRecoveryAfterKEKLoss(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	oldKEK := testMasterKey(t)
	s := rotateSchema(t, dsn, "rec_kek", oldKEK)
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner,
	}, Secret("vault-secret")); err != nil {
		t.Fatal(err)
	}
	recoveryKey, _ := crypto.NewKey()
	if err := s.StoreRecoveryWrap(ctx, "org", owner, recoveryKey, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// Disaster: the deployment KEK is lost. The owner presents recovery
	// material to a store under a NEW KEK.
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rec_kek")
	u.RawQuery = q.Encode()
	newKEK, _ := crypto.NewKey()
	fresh, err := OpenPostgres(u.String(), newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	// The new-KEK store cannot resolve the org — its wrap is under the lost KEK.
	if _, err := fresh.Secret("i1"); err == nil {
		t.Fatal("org resolved under the wrong KEK")
	}
	// Recovery: open the wrap AND re-seed org_keys under the new KEK in one
	// transaction — the wrap only spends if the reseed commits.
	if err := fresh.RecoverOrgKey(ctx, "org", owner, recoveryKey); err != nil {
		t.Fatal(err)
	}
	// The vault is live: the same item ciphertext decrypts again.
	sec, err := fresh.Secret("i1")
	if err != nil || string(sec) != "vault-secret" {
		t.Fatalf("post-recovery decrypt: %v %q", err, sec)
	}
	// The wrap is spent — replay fails closed.
	if err := fresh.RecoverOrgKey(ctx, "org", owner, recoveryKey); err == nil {
		t.Fatal("spent recovery wrap replayed")
	}
}

// TestPostgresRecoverOrgKeyAtomic: a failed recovery must not burn the
// wrap — open+consume+reseed is one transaction, so a wrong-key attempt
// leaves the wrap intact for the real lost-KEK recovery that follows.
func TestPostgresRecoverOrgKeyAtomic(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_atomic", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}, Secret("atomic-secret")); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	recoveryKey, _ := crypto.NewKey()
	if err := s.StoreRecoveryWrap(ctx, "org", owner, recoveryKey, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// Lost KEK: a store under a new KEK cannot resolve the org.
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rec_atomic")
	u.RawQuery = q.Encode()
	newKEK, _ := crypto.NewKey()
	fresh, err := OpenPostgres(u.String(), newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err := fresh.Secret("i1"); err == nil {
		t.Fatal("org resolved under the wrong KEK")
	}

	// Wrong recovery material: open fails inside the tx, nothing commits —
	// the wrap must still be usable for the real recovery.
	wrongKey, _ := crypto.NewKey()
	if err := fresh.RecoverOrgKey(ctx, "org", owner, wrongKey); err == nil {
		t.Fatal("wrong recovery key recovered")
	}
	// The real path: consume+reseed commits together — item decrypts under
	// the new KEK, and the wrap is spent (replay fails).
	if err := fresh.RecoverOrgKey(ctx, "org", owner, recoveryKey); err != nil {
		t.Fatalf("wrap burned by the failed attempt: %v", err)
	}
	sec, err := fresh.Secret("i1")
	if err != nil || string(sec) != "atomic-secret" {
		t.Fatalf("post-recovery decrypt: %v %q", err, sec)
	}
	if err := fresh.RecoverOrgKey(ctx, "org", owner, recoveryKey); err == nil {
		t.Fatal("spent recovery wrap replayed")
	}
}

// TestPostgresRotateOrgKeyDropsRecoveryWraps: rotation invalidates wraps that
// seal the dead master — the wrap row is deleted, and the owner re-mints.
func TestPostgresRotateOrgKeyDropsRecoveryWraps(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_rot", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	recoveryKey, _ := crypto.NewKey()
	if err := s.StoreRecoveryWrap(ctx, "org", owner, recoveryKey, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RotateOrgKey(ctx, "org"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenRecoveryWrap(ctx, "org", owner, recoveryKey); err == nil {
		t.Fatal("wrap for the dead master still opens")
	}
}
