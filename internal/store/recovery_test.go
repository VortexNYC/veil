package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/id"
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

// --- VEIL-78: per-member wrapped org-key shares + recovery ceremony ---

// TestPostgresOrgKeyShareEnroll proves enrollment seals the committed org
// master under the member's X25519 pubkey, is idempotent (a refile returns
// the committed row, never overwriting pubkey), and rejects bad key sizes.
func TestPostgresOrgKeyShareEnroll(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_share", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	share, created, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pub)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first enroll reported not-created")
	}
	if share.WrapPubkeyEpoch != 1 || share.OrgKeyVersion != 1 {
		t.Fatalf("share epoch/version = %d/%d", share.WrapPubkeyEpoch, share.OrgKeyVersion)
	}
	got, err := device.Accept(share.WrappedMaster, priv)
	if err != nil {
		t.Fatalf("share did not open: %v", err)
	}
	if string(got) != string(master) {
		t.Fatal("share opened to a different master")
	}
	// Idempotent: same member re-enrolls — committed row is returned.
	again, created, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pub)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("re-enroll reported created")
	}
	if !hmac.Equal(again.Pubkey, share.Pubkey) || !hmac.Equal(again.WrappedMaster, share.WrappedMaster) {
		t.Fatal("re-enroll mutated the committed share")
	}
	// A different pubkey must NOT overwrite the committed share.
	pub2, _, _ := device.Generate()
	other, created, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pub2)
	if err != nil {
		t.Fatal(err)
	}
	if created || !hmac.Equal(other.Pubkey, pub) {
		t.Fatal("enroll overwrote an enrolled member's pubkey")
	}
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-2", []byte("short")); err == nil {
		t.Fatal("bad pubkey size enrolled")
	}
}

func TestPostgresRealignOrgKeyShareCustodian(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_realign", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	pub1, priv1, _ := device.Generate()
	pub2, priv2, _ := device.Generate()
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pub1); err != nil {
		t.Fatal(err)
	}
	share, err := s.RealignOrgKeyShareCustodian(ctx, "org", "human-1", pub2)
	if err != nil {
		t.Fatal(err)
	}
	if share.WrapPubkeyEpoch != 2 {
		t.Fatalf("epoch %d", share.WrapPubkeyEpoch)
	}
	got, err := device.Accept(share.WrappedMaster, priv2)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(master) {
		t.Fatal("realign opened wrong master")
	}
	if _, err := device.Accept(share.WrappedMaster, priv1); err == nil {
		t.Fatal("old custodian still opens")
	}
	again, err := s.RealignOrgKeyShareCustodian(ctx, "org", "human-1", pub2)
	if err != nil || again.WrapPubkeyEpoch != 2 {
		t.Fatalf("idempotent realign: %v epoch=%d", err, again.WrapPubkeyEpoch)
	}
}

// TestPostgresRecoveryRequestPubkeyUnique: a new_pubkey already enrolled on
// any share is rejected — the global-unique pubkey invariant (VEIL-78).
func TestPostgresRecoveryRequestPubkeyUnique(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_pubuniq", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	pub, _, _ := device.Generate()
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pub); err != nil {
		t.Fatal(err)
	}
	reqID, _ := id.NewRecovery()
	now := time.Now().UTC()
	_, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: reqID, OrgID: "org", HumanID: "human-2", InitiatedBy: "human-2",
		NewPubkey: pub, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if !errors.Is(err, ErrRecoveryPubkey) {
		t.Fatalf("reused pubkey filed: %v", err)
	}
}

// TestPostgresRotateOrgKeyRewrapsShares proves member shares survive org
// master rotation — reboxed to the same pubkey under the new master —
// while symmetric recovery_wraps still die.
func TestPostgresRotateOrgKeyRewrapsShares(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_share_rot", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner,
	}, Secret("rot-secret")); err != nil {
		t.Fatal(err)
	}
	pubA, privA, _ := device.Generate()
	pubB, privB, _ := device.Generate()
	shareA, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pubA)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-2", pubB); err != nil {
		t.Fatal(err)
	}
	oldBlob := append([]byte(nil), shareA.WrappedMaster...)

	if err := s.RotateOrgKey(ctx, "org"); err != nil {
		t.Fatal(err)
	}
	newMaster, err := s.resolveOrgKey(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	for humanID, priv := range map[string][]byte{"human-1": privA, "human-2": privB} {
		share, err := s.OrgKeyShare(ctx, "org", humanID)
		if err != nil {
			t.Fatalf("share for %s lost on rotation: %v", humanID, err)
		}
		if share.OrgKeyVersion != 2 {
			t.Fatalf("share org_key_version = %d, want 2", share.OrgKeyVersion)
		}
		got, err := device.Accept(share.WrappedMaster, priv)
		if err != nil {
			t.Fatalf("rewrapped share for %s did not open: %v", humanID, err)
		}
		if string(got) != string(newMaster) {
			t.Fatalf("share for %s opens a dead master", humanID)
		}
	}
	if share, _ := s.OrgKeyShare(ctx, "org", "human-1"); hmac.Equal(share.WrappedMaster, oldBlob) {
		t.Fatal("rotation left the old wrapped blob in place")
	}
	// Items still decrypt — DEKs were rewrapped, not resealed.
	sec, err := s.Secret("i1")
	if err != nil || string(sec) != "rot-secret" {
		t.Fatalf("post-rotation decrypt: %v %q", err, sec)
	}
}

// TestPostgresCompleteRecovery is the ceremony commit: open request +
// confirmer → org master rotates inline, the recovering member's share
// moves to the staged pubkey at epoch+1, every other share rewraps, the
// request resolves completed, and one recovery_completed audit row lands.
func TestPostgresCompleteRecovery(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_complete", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner,
	}, Secret("ceremony-secret")); err != nil {
		t.Fatal(err)
	}
	pubA, _, _ := device.Generate()
	pubB, privB, _ := device.Generate()
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pubA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-2", pubB); err != nil {
		t.Fatal(err)
	}
	// Member files a request staging their NEW pubkey.
	newPub, newPriv, _ := device.Generate()
	reqID, _ := id.NewRecovery()
	now := time.Now().UTC()
	req, created, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: reqID, OrgID: "org", HumanID: "human-1", InitiatedBy: "human-1",
		NewPubkey: newPub, ResetMFA: true, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil || !created {
		t.Fatalf("file: created=%v err=%v", created, err)
	}
	// Dedupe: a second file returns the live row, not a new one.
	dupe, created, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: "rec-dupe", OrgID: "org", HumanID: "human-1", InitiatedBy: "human-1",
		NewPubkey: newPub, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || dupe.ID != req.ID {
		t.Fatalf("refile created=%v id=%q", created, dupe.ID)
	}

	done, err := s.CompleteRecovery(ctx, "org", reqID, "owner-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != protocol.RecoveryCompleted {
		t.Fatalf("status = %q", done.Status)
	}
	if done.ConfirmedBy != "owner-1" || done.ResultEpoch == nil || *done.ResultEpoch != 2 {
		t.Fatalf("confirmed_by=%q epoch=%v", done.ConfirmedBy, done.ResultEpoch)
	}
	newMaster, err := s.resolveOrgKey(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if string(newMaster) == string(master) {
		t.Fatal("org master did not rotate")
	}
	// The recovering member's share is on the staged pubkey at epoch 2.
	share, err := s.OrgKeyShare(ctx, "org", "human-1")
	if err != nil {
		t.Fatal(err)
	}
	if !hmac.Equal(share.Pubkey, newPub) || share.WrapPubkeyEpoch != 2 || share.OrgKeyVersion != 2 {
		t.Fatalf("share = epoch %d v%d", share.WrapPubkeyEpoch, share.OrgKeyVersion)
	}
	got, err := device.Accept(share.WrappedMaster, newPriv)
	if err != nil || string(got) != string(newMaster) {
		t.Fatalf("new share did not open the rotated master: %v", err)
	}
	// Every other member rewrapped under their own pubkey.
	shareB, err := s.OrgKeyShare(ctx, "org", "human-2")
	if err != nil {
		t.Fatal(err)
	}
	got, err = device.Accept(shareB.WrappedMaster, privB)
	if err != nil || string(got) != string(newMaster) {
		t.Fatalf("other member's share did not rewrap: %v", err)
	}
	// Items decrypt — owner DEKs rewrapped inline.
	sec, err := s.Secret("i1")
	if err != nil || string(sec) != "ceremony-secret" {
		t.Fatalf("post-recovery decrypt: %v %q", err, sec)
	}
	// One recovery_completed audit row.
	events, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Action == protocol.ActionRecoveryCompleted && e.ItemID == reqID && e.AgentID == "owner-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("recovery_completed audit row missing")
	}
	// Terminal: a second complete fails resolved.
	if _, err := s.CompleteRecovery(ctx, "org", reqID, "owner-1", nil); !errors.Is(err, ErrRequestResolved) {
		t.Fatalf("double complete: %v", err)
	}
}

// TestPostgresCompleteRecoveryTimelock: unlock_at gates completion — only
// a proof_hmac computed over the committed master bypasses it.
func TestPostgresCompleteRecoveryTimelock(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_timelock", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	newPub, _, _ := device.Generate()
	reqID, _ := id.NewRecovery()
	now := time.Now().UTC()
	if _, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: reqID, OrgID: "org", HumanID: "human-1", InitiatedBy: "human-1",
		NewPubkey: newPub, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now.Add(24 * time.Hour), ExpiresAt: now.Add(48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Inside the timelock, no proof → locked.
	if _, err := s.CompleteRecovery(ctx, "org", reqID, "owner-1", nil); !errors.Is(err, ErrRecoveryLocked) {
		t.Fatalf("timelocked complete: %v", err)
	}
	// A wrong proof does not bypass.
	bad := make([]byte, 32)
	if _, err := s.CompleteRecovery(ctx, "org", reqID, "owner-1", bad); !errors.Is(err, ErrRecoveryLocked) {
		t.Fatalf("bad proof completed: %v", err)
	}
	// The real proof — HMAC-SHA256(committed master, req.ID) — completes.
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(reqID))
	done, err := s.CompleteRecovery(ctx, "org", reqID, "owner-1", mac.Sum(nil))
	if err != nil {
		t.Fatalf("proof_hmac did not bypass the timelock: %v", err)
	}
	if done.Status != protocol.RecoveryCompleted {
		t.Fatalf("status = %q", done.Status)
	}
}

// TestPostgresRecoverOrgKeyViaShare: the lost-KEK path falls back to the
// member's org_key_shares row when no symmetric wrap exists — the X25519
// recovery private key opens the share and re-anchors under a new KEK.
func TestPostgresRecoverOrgKeyViaShare(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_share_kek", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner,
	}, Secret("share-kek-secret")); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := device.Generate()
	if _, _, err := s.EnrollOrgKeyShare(ctx, "org", "human-1", pub); err != nil {
		t.Fatal(err)
	}

	// Lost KEK: a store under a new KEK cannot resolve the org.
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rec_share_kek")
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
	// recovery.key opens the member share; the org re-anchors.
	if err := fresh.RecoverOrgKey(ctx, "org", owner, priv); err != nil {
		t.Fatalf("share-keyed recovery: %v", err)
	}
	sec, err := fresh.Secret("i1")
	if err != nil || string(sec) != "share-kek-secret" {
		t.Fatalf("post-recovery decrypt: %v %q", err, sec)
	}
	// Shares are not single-use: a second recover is a clean no-op.
	if err := fresh.RecoverOrgKey(ctx, "org", owner, priv); err != nil {
		t.Fatalf("share recovery replayed as an error: %v", err)
	}
}

// TestPostgresRecoverySweepExpires: a request past expires_at is marked
// expired by the sweep with its recovery_expired audit row.
func TestPostgresRecoverySweepExpires(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_sweep", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	pub, _, _ := device.Generate()
	reqID, _ := id.NewRecovery()
	past := time.Now().Add(-time.Hour).UTC()
	if _, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: reqID, OrgID: "org", HumanID: "human-1", InitiatedBy: "human-1",
		NewPubkey: pub, Status: protocol.RecoveryOpen,
		CreatedAt: past, UnlockAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Sweep(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Recoveries != 1 {
		t.Fatalf("sweep recoveries = %d", rep.Recoveries)
	}
	req, err := s.RecoveryRequest(ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != protocol.RecoveryExpired {
		t.Fatalf("status = %q", req.Status)
	}
	events, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Action == protocol.ActionRecoveryExpired && e.ItemID == reqID {
			found = true
		}
	}
	if !found {
		t.Fatal("recovery_expired audit row missing")
	}
}

// TestPostgresDenyRecovery: an owner deny or initiator cancel flips the
// open ask terminally and audits it; a second resolution fails closed.
func TestPostgresDenyRecovery(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rec_deny", testMasterKey(t))
	defer s.Close()

	master, _ := crypto.NewKey()
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	pub, _, _ := device.Generate()
	reqID, _ := id.NewRecovery()
	now := time.Now().UTC()
	if _, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: reqID, OrgID: "org", HumanID: "human-1", InitiatedBy: "human-1",
		NewPubkey: pub, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	done, err := s.DenyRecovery(ctx, reqID, "owner-1", protocol.RecoveryDenied)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != protocol.RecoveryDenied || done.ConfirmedBy != "owner-1" {
		t.Fatalf("deny = %q by %q", done.Status, done.ConfirmedBy)
	}
	if _, err := s.DenyRecovery(ctx, reqID, "owner-1", protocol.RecoveryDenied); !errors.Is(err, ErrRequestResolved) {
		t.Fatalf("second deny: %v", err)
	}
	events, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Action == protocol.ActionRecoveryDenied && e.ItemID == reqID {
			found = true
		}
	}
	if !found {
		t.Fatal("recovery_denied audit row missing")
	}
}

// TestRecoveryMonitorFindings: the ops tripwire sees live ceremonies,
// timelocked sole-owner asks, and refile floods (VEIL-78).
func TestRecoveryMonitorFindings(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	kek := testMasterKey(t)
	s := rotateSchema(t, dsn, "rec_mon", kek)
	defer s.Close()

	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, protocol.LocalOrgID, master); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rec_mon")
	u.RawQuery = q.Encode()
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// Quiet baseline: no findings on a clean table.
	findings, err := RecoveryMonitorFindings(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("clean table findings: %v", findings)
	}

	// An open ask on the local org is a finding — including its timelock.
	pub, _, _ := device.Generate()
	reqID, _ := id.NewRecovery()
	now := time.Now().UTC()
	if _, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: reqID, OrgID: protocol.LocalOrgID, HumanID: "h1", InitiatedBy: "h1",
		NewPubkey: pub, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now.Add(24 * time.Hour), ExpiresAt: now.Add(48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	findings, err = RecoveryMonitorFindings(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) < 2 {
		t.Fatalf("open+timelock findings: %v", findings)
	}

	// Resolve it denied; >=3 denied in an hour trips the flood finding.
	if _, err := s.DenyRecovery(ctx, reqID, "h1", protocol.RecoveryCancelled); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		pub2, _, _ := device.Generate()
		rid, _ := id.NewRecovery()
		if _, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
			ID: rid, OrgID: protocol.LocalOrgID, HumanID: "h" + fmt.Sprint(i+2), InitiatedBy: "h" + fmt.Sprint(i+2),
			NewPubkey: pub2, Status: protocol.RecoveryOpen,
			CreatedAt: now, UnlockAt: now, ExpiresAt: now.Add(24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DenyRecovery(ctx, rid, "o", protocol.RecoveryDenied); err != nil {
			t.Fatal(err)
		}
	}
	// Third denied: same human re-files after a cancel.
	pub3, _, _ := device.Generate()
	rid3, _ := id.NewRecovery()
	if _, _, err := s.FileRecoveryRequest(ctx, protocol.RecoveryRequest{
		ID: rid3, OrgID: protocol.LocalOrgID, HumanID: "h1", InitiatedBy: "h1",
		NewPubkey: pub3, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DenyRecovery(ctx, rid3, "o", protocol.RecoveryDenied); err != nil {
		t.Fatal(err)
	}
	findings, err = RecoveryMonitorFindings(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	flood := false
	for _, f := range findings {
		if strings.Contains(f, "denied/expired") {
			flood = true
		}
	}
	if !flood {
		t.Fatalf("refile flood finding missing: %v", findings)
	}
}
