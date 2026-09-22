package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

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

// TestPostgresMintVsRotate: owner-DEK mints racing an org rotation must
// never strand a wrap under the retired master — FOR SHARE vs FOR UPDATE
// serializes them, so every minted item decrypts afterwards.
func TestPostgresMintVsRotate(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rot_mint", testMasterKey(t))
	defer s.Close()

	if err := s.EnsureOrgKey(ctx, "org", testMasterKey(t)); err != nil {
		t.Fatal(err)
	}
	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n+1)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.PutItem(protocol.Item{
				ID: fmt.Sprintf("i%d", i), OrgID: "org", Name: fmt.Sprintf("i%d", i), Kind: protocol.ItemAPIKey,
				Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: fmt.Sprintf("u%d", i)},
			}, Secret("s"))
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs[n] = s.RotateOrgKey(ctx, "org")
	}()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("op %d: %v", i, err)
		}
	}
	// Every owner_keys row — minted before or after the rotation — opens.
	for i := range n {
		if _, err := s.Secret(fmt.Sprintf("i%d", i)); err != nil {
			t.Fatalf("i%d stranded by the race: %v", i, err)
		}
	}
}

// TestPostgresWrapVsRotate: a recovery wrap racing an org rotation must open
// the committed master, never the dead one — the FOR SHARE read serializes
// against rotation's FOR UPDATE + delete.
func TestPostgresWrapVsRotate(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	s := rotateSchema(t, dsn, "rot_wrap", testMasterKey(t))
	defer s.Close()

	if err := s.EnsureOrgKey(ctx, "org", testMasterKey(t)); err != nil {
		t.Fatal(err)
	}
	const n = 8
	keys := make([][]byte, n)
	var wg sync.WaitGroup
	errs := make([]error, n+1)
	for i := range n {
		keys[i], _ = crypto.NewKey()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.StoreRecoveryWrap(ctx, "org",
				protocol.Owner{Kind: protocol.OwnerUser, ID: fmt.Sprintf("u%d", i)}, keys[i], time.Time{})
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs[n] = s.RotateOrgKey(ctx, "org")
	}()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("op %d: %v", i, err)
		}
	}
	// Every surviving wrap opens the CURRENT committed master — never a dead
	// one. Wraps that landed before rotation were deleted by it; their opens
	// fail closed on missing rows, which is correct.
	current, err := s.resolveOrgKey(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		master, err := s.OpenRecoveryWrap(ctx, "org",
			protocol.Owner{Kind: protocol.OwnerUser, ID: fmt.Sprintf("u%d", i)}, keys[i])
		if err == nil && string(master) != string(current) {
			t.Fatalf("wrap %d opened a dead master", i)
		}
	}
	// Positive check: a wrap minted post-rotation opens the committed
	// master — the race survivors above may all be deleted, so this also
	// guarantees at least one successful open path exists.
	post, _ := crypto.NewKey()
	if err := s.StoreRecoveryWrap(ctx, "org",
		protocol.Owner{Kind: protocol.OwnerUser, ID: "post"}, post, time.Time{}); err != nil {
		t.Fatal(err)
	}
	master, err := s.OpenRecoveryWrap(ctx, "org",
		protocol.Owner{Kind: protocol.OwnerUser, ID: "post"}, post)
	if err != nil {
		t.Fatal(err)
	}
	if string(master) != string(current) {
		t.Fatal("post-rotation wrap opened a non-current master")
	}
}

// TestPostgresProvisionVsKEK: orgs provisioned during a KEK rotation can
// never land sealed under the retired KEK — same-process inserts serialize
// on kekMu and seal under the adopted key.
func TestPostgresProvisionVsKEK(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	oldKEK := testMasterKey(t)
	s := rotateSchema(t, dsn, "rot_prov", oldKEK)
	defer s.Close()

	if err := s.EnsureOrgKey(ctx, "org-0", testMasterKey(t)); err != nil {
		t.Fatal(err)
	}
	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n+1)
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i-1] = s.EnsureOrgKey(ctx, fmt.Sprintf("org-%d", i), testMasterKey(t))
		}(i)
	}
	newKEK, _ := crypto.NewKey()
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs[n] = s.RotateKEK(ctx, newKEK)
	}()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("op %d: %v", i, err)
		}
	}
	// Every org — provisioned before, during, or after the rotation — opens
	// under the adopted KEK on this store and on a fresh store.
	for i := range n + 1 {
		org := fmt.Sprintf("org-%d", i)
		if _, err := s.resolveOrgKey(ctx, org); err != nil {
			t.Fatalf("%s stranded under retired KEK: %v", org, err)
		}
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rot_prov")
	u.RawQuery = q.Encode()
	fresh, err := OpenPostgres(u.String(), newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	for i := range n + 1 {
		org := fmt.Sprintf("org-%d", i)
		if _, err := fresh.resolveOrgKey(ctx, org); err != nil {
			t.Fatalf("%s unreachable under new KEK on fresh store: %v", org, err)
		}
	}
}

// TestPostgresKEKVsOrgRotateCrossStore: a KEK rotation on one store racing
// an org rotation on a second store (a different "replica") must serialize,
// not deadlock — SHARE ROW EXCLUSIVE conflicts with the ROW EXCLUSIVE table
// lock FOR UPDATE acquires, so one verb waits for the other. Whichever
// commits second sees the first's result; the org resolves under the final
// KEK either way.
func TestPostgresKEKVsOrgRotateCrossStore(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	oldKEK := testMasterKey(t)
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rot_xstore")
	u.RawQuery = q.Encode()

	a := rotateSchema(t, dsn, "rot_xstore", oldKEK)
	defer a.Close()
	b, err := OpenPostgres(u.String(), oldKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := a.EnsureOrgKey(ctx, "org", testMasterKey(t)); err != nil {
		t.Fatal(err)
	}
	if err := a.PutItem(protocol.Item{
		ID: "i1", OrgID: "org", Name: "i1", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}, Secret("xstore")); err != nil {
		t.Fatal(err)
	}

	newKEK, _ := crypto.NewKey()
	var wg sync.WaitGroup
	var kekErr, orgErr error
	wg.Add(2)
	go func() { defer wg.Done(); kekErr = a.RotateKEK(ctx, newKEK) }()
	go func() { defer wg.Done(); orgErr = b.RotateOrgKey(ctx, "org") }()
	wg.Wait() // deadlock here fails via test timeout

	// RotateKEK rewraps whatever committed last — it must succeed. The org
	// rotation either committed first (success — its new master then gets
	// rewrapped too) or opened the row after the KEK moved and failed
	// closed on the unwrap — both coherent.
	if kekErr != nil {
		t.Fatalf("RotateKEK: %v", kekErr)
	}
	if orgErr != nil {
		t.Logf("RotateOrgKey second: failed closed as expected: %v", orgErr)
	}
	fresh, err := OpenPostgres(u.String(), newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	sec, err := fresh.Secret("i1")
	if err != nil || string(sec) != "xstore" {
		t.Fatalf("post-race decrypt: %v %q", err, sec)
	}
}
