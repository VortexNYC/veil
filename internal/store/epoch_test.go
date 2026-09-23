package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store/sqlc"
)

var epochMarkTest = []byte("VEIL1")

// TestPostgresEpochMixedCorpus: a vault holding legacy nil-AAD rows next to
// epoch-1 rows must read both, write only epoch-1, and refuse a marked blob
// transplanted across item or owner contexts.
func TestPostgresEpochMixedCorpus(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "epoch_mix", testMasterKey(t))
	defer s.Close()

	org := "org"
	ownerA := protocol.Owner{Kind: protocol.OwnerUser, ID: "h1"}
	ownerB := protocol.Owner{Kind: protocol.OwnerUser, ID: "h2"}
	if err := s.EnsureOrgKey(ctx, org, testMasterKey(t)); err != nil {
		t.Fatal(err)
	}
	for _, it := range []protocol.Item{
		{ID: "i1", OrgID: org, Name: "i1", Kind: protocol.ItemAPIKey, Owner: ownerA},
		{ID: "i2", OrgID: org, Name: "i2", Kind: protocol.ItemAPIKey, Owner: ownerA},
	} {
		if err := s.PutItem(it, Secret("secret-"+it.ID)); err != nil {
			t.Fatal(err)
		}
	}

	// New writes carry the epoch marker.
	i1Row, err := s.sqlc.ItemSecretOwner(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(i1Row.Secret, epochMarkTest) {
		t.Fatal("new item write missing epoch marker")
	}
	wrapRow, err := s.sqlc.OwnerWrapped(ctx, sqlc.OwnerWrappedParams{
		OrgID: org, OwnerKind: string(ownerA.Kind), OwnerID: ownerA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(wrapRow, epochMarkTest) {
		t.Fatal("new owner wrap missing epoch marker")
	}

	// Downgrade i1's blob and owner A's wrap to legacy nil-AAD, then prove the
	// store still resolves both formats from cold cache.
	dek, err := s.ownerDEK(org, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	master, err := s.resolveOrgKey(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	legacyItem, err := crypto.Seal(dek, []byte("secret-i1"))
	if err != nil {
		t.Fatal(err)
	}
	legacyWrap, err := crypto.Seal(master, dek)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sqlc.RestoreItemSecret(ctx, sqlc.RestoreItemSecretParams{Secret: legacyItem, ID: "i1"}); err != nil {
		t.Fatal(err)
	}
	// Raw UPDATE — PutOwnerWrapped is ON CONFLICT DO NOTHING and would no-op
	// over the existing v1 row, leaving the "legacy" claim unproven.
	if _, err := s.pool.Exec(ctx,
		`UPDATE owner_keys SET wrapped=$1 WHERE org_id=$2 AND owner_kind=$3 AND owner_id=$4`,
		legacyWrap, org, string(ownerA.Kind), ownerA.ID); err != nil {
		t.Fatal(err)
	}
	s.km.InvalidateOrg(org)

	for _, id := range []string{"i1", "i2"} {
		sec, err := s.Secret(id)
		if err != nil || string(sec) != "secret-"+id {
			t.Fatalf("%s mixed-format read: %v %q", id, err, sec)
		}
	}

	// Transplant i2's marked blob into i1's row: AAD binds to the row id and
	// must fail closed.
	i2Row, err := s.sqlc.ItemSecretOwner(ctx, "i2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sqlc.RestoreItemSecret(ctx, sqlc.RestoreItemSecretParams{Secret: i2Row.Secret, ID: "i1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Secret("i1"); !errors.Is(err, crypto.ErrAuth) {
		t.Fatalf("transplanted v1 blob: %v", err)
	}
	if err := s.sqlc.RestoreItemSecret(ctx, sqlc.RestoreItemSecretParams{Secret: legacyItem, ID: "i1"}); err != nil {
		t.Fatal(err)
	}

	// A marked owner wrap under the wrong owner row fails closed: hand owner B
	// a copy of owner A's wrap and require ownerDEK to refuse it.
	if err := s.sqlc.PutOwnerWrapped(ctx, sqlc.PutOwnerWrappedParams{
		OrgID: org, OwnerKind: string(ownerB.Kind), OwnerID: ownerB.ID, Wrapped: wrapRow,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ownerDEK(org, ownerB); err == nil {
		t.Fatal("transplanted owner wrap opened")
	}
}

// TestPostgresEpochRotationConverges: org-master rotation re-seals owner wraps
// at epoch 1 — a corpus holding legacy wraps converges without touching items.
func TestPostgresEpochRotationConverges(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "epoch_rot", testMasterKey(t))
	defer s.Close()

	org := "org"
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "h1"}
	if err := s.EnsureOrgKey(ctx, org, testMasterKey(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{ID: "i1", OrgID: org, Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("secret-i1")); err != nil {
		t.Fatal(err)
	}

	// Plant a legacy wrap and force it authoritative.
	dek, err := s.ownerDEK(org, owner)
	if err != nil {
		t.Fatal(err)
	}
	master, err := s.resolveOrgKey(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	legacyWrap, err := crypto.Seal(master, dek)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE owner_keys SET wrapped=$1 WHERE org_id=$2 AND owner_kind=$3 AND owner_id=$4`,
		legacyWrap, org, string(owner.Kind), owner.ID); err != nil {
		t.Fatal(err)
	}
	s.km.InvalidateOrg(org)

	if err := s.RotateOrgKey(ctx, org); err != nil {
		t.Fatal(err)
	}
	wrap, err := s.sqlc.OwnerWrapped(ctx, sqlc.OwnerWrappedParams{
		OrgID: org, OwnerKind: string(owner.Kind), OwnerID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(wrap, epochMarkTest) {
		t.Fatal("rotation left a legacy wrap")
	}
	sec, err := s.Secret("i1")
	if err != nil || string(sec) != "secret-i1" {
		t.Fatalf("post-rotation read: %v %q", err, sec)
	}
}

// TestSQLiteEpochMixedCorpus: the sqlite vault's epoch dispatch — CI-safe
// coverage for environments without PG_TEST_DSN.
func TestSQLiteEpochMixedCorpus(t *testing.T) {
	dir := t.TempDir()
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(filepath.Join(dir, "vault.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	owner := protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"}
	for _, it := range []protocol.Item{
		{ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: owner},
		{ID: "i2", OrgID: "org", Name: "i2", Kind: protocol.ItemAPIKey, Owner: owner},
	} {
		if err := s.PutItem(it, Secret("secret-"+it.ID)); err != nil {
			t.Fatal(err)
		}
	}

	var i1blob, i2blob []byte
	if err := s.db.QueryRow(`SELECT secret FROM items WHERE id='i1'`).Scan(&i1blob); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT secret FROM items WHERE id='i2'`).Scan(&i2blob); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(i1blob, epochMarkTest) || !bytes.HasPrefix(i2blob, epochMarkTest) {
		t.Fatal("sqlite writes missing epoch marker")
	}

	// Downgrade i1 to a legacy nil-AAD blob — still reads.
	dek, err := s.ownerDEK("org", owner)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := crypto.Seal(dek, []byte("secret-i1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE items SET secret=? WHERE id='i1'`, legacy); err != nil {
		t.Fatal(err)
	}
	sec, err := s.Secret("i1")
	if err != nil || string(sec) != "secret-i1" {
		t.Fatalf("legacy read: %v %q", err, sec)
	}

	// Transplant i2's marked blob onto i1 — fails closed.
	if _, err := s.db.Exec(`UPDATE items SET secret=? WHERE id='i1'`, i2blob); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Secret("i1"); !errors.Is(err, crypto.ErrAuth) {
		t.Fatalf("transplanted v1 blob: %v", err)
	}
}
