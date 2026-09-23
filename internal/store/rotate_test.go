package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/jackc/pgx/v5"
)

// rotateSchema opens a fresh schema-isolated Postgres under kek.
func rotateSchema(t *testing.T, dsn, schema string, kek []byte) *Postgres {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schema)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := OpenPostgres(u.String(), kek)
	if err != nil {
		t.Fatalf("open %s: %v", schema, err)
	}
	return s
}

// TestPostgresRotateOrgKey proves the rotation invariant: org masters change,
// owner DEKs do not — so item ciphertexts sealed before the rotation still
// open after it, key_version bumps, and the running store's caches drop the
// old master immediately.
func TestPostgresRotateOrgKey(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	kek := testMasterKey(t)
	s := rotateSchema(t, dsn, "rot_org", kek)
	defer s.Close()

	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	// Two owners → two owner_keys rows to rewrap.
	for _, it := range []protocol.Item{
		{ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey, Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: "h1"}},
		{ID: "i2", OrgID: "org", Name: "i2", Kind: protocol.ItemAPIKey, Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"}},
	} {
		if err := s.PutItem(it, Secret("secret-"+it.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RotateOrgKey(ctx, "org"); err != nil {
		t.Fatal(err)
	}
	row, err := s.sqlc.OrgKey(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if row.KeyVersion != 2 {
		t.Fatalf("key_version %d, want 2", row.KeyVersion)
	}
	if !row.RotatedAt.Valid {
		t.Fatal("rotated_at not stamped")
	}
	// Pre-rotation ciphertexts open under the same store — the generation
	// guard dropped the cached old master and owner DEKs.
	for _, it := range []string{"i1", "i2"} {
		sec, err := s.Secret(it)
		if err != nil || string(sec) != "secret-"+it {
			t.Fatalf("%s after rotation: %v %q", it, err, sec)
		}
	}
	// The committed master is genuinely new.
	newMaster, err := s.resolveOrgKey(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if string(newMaster) == string(master) {
		t.Fatal("master did not change")
	}
	// A second rotation bumps to 3 and stays live.
	if err := s.RotateOrgKey(ctx, "org"); err != nil {
		t.Fatal(err)
	}
	row, _ = s.sqlc.OrgKey(ctx, "org")
	if row.KeyVersion != 3 {
		t.Fatalf("key_version %d, want 3", row.KeyVersion)
	}
	sec, err := s.Secret("i1")
	if err != nil || string(sec) != "secret-i1" {
		t.Fatalf("i1 after second rotation: %v %q", err, sec)
	}
	// Rotating an org with no row fails closed.
	if err := s.RotateOrgKey(ctx, "ghost"); !errors.Is(err, ErrOrgKeyMissing) {
		t.Fatalf("ghost org rotation: %v", err)
	}
}

// TestPostgresRotateOrgKeyConcurrent: two racing rotations on the same org
// must not interleave DEK rewraps under different masters. The version guard
// fails one side; the committed state stays coherent and decryptable.
func TestPostgresRotateOrgKeyConcurrent(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	kek := testMasterKey(t)
	s := rotateSchema(t, dsn, "rot_race", kek)
	defer s.Close()

	if err := s.EnsureOrgKey(ctx, "org", nil); err == nil {
		t.Fatal("EnsureOrgKey accepted nil master")
	}
	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}, Secret("race-secret")); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.RotateOrgKey(ctx, "org")
		}(i)
	}
	wg.Wait()
	conflicts := 0
	for _, err := range errs {
		if errors.Is(err, ErrRotationConflict) {
			conflicts++
		} else if err != nil {
			t.Fatalf("rotation error: %v", err)
		}
	}
	if conflicts > 1 {
		t.Fatalf("both rotations conflicted — none committed")
	}
	sec, err := s.Secret("i1")
	if err != nil || string(sec) != "race-secret" {
		t.Fatalf("post-race decrypt: %v %q", err, sec)
	}
}

// TestPostgresRotateKEK proves deployment-KEK rotation: every org_keys row
// rewraps in one transaction, org masters are unchanged (owner DEKs and item
// ciphertexts untouched), the live store adopts the new KEK, and a store
// still holding the old KEK fails closed.
func TestPostgresRotateKEK(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	oldKEK := testMasterKey(t)
	s := rotateSchema(t, dsn, "rot_kek", oldKEK)
	defer s.Close()

	for _, org := range []string{"org-a", "org-b"} {
		master, err := crypto.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureOrgKey(ctx, org, master); err != nil {
			t.Fatal(err)
		}
		if err := s.PutItem(protocol.Item{
			ID: "i-" + org, OrgID: org, Name: "i-" + org, Kind: protocol.ItemAPIKey,
			Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: org},
		}, Secret("secret-"+org)); err != nil {
			t.Fatal(err)
		}
	}
	newKEK, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RotateKEK(ctx, newKEK); err != nil {
		t.Fatal(err)
	}
	// Live store adopted newKEK and invalidated every cached master.
	for _, org := range []string{"org-a", "org-b"} {
		sec, err := s.Secret("i-" + org)
		if err != nil || string(sec) != "secret-"+org {
			t.Fatalf("%s post-KEK-rotation: %v %q", org, err, sec)
		}
	}
	// A fresh store on the old KEK fails closed.
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rot_kek")
	u.RawQuery = q.Encode()
	stale, err := OpenPostgres(u.String(), oldKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	if _, err := stale.Secret("i-org-a"); err == nil {
		t.Fatal("old KEK still opens the vault after rotation")
	}
	// Rotating to a wrong-size KEK refuses before touching the database.
	if err := s.RotateKEK(ctx, []byte("short")); err == nil {
		t.Fatal("short KEK accepted")
	}
}
