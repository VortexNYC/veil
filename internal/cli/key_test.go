package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
	"github.com/openbao/openbao/sdk/v2/helper/shamir"
)

// The custody ceremony: a minted KEK splits 2-of-3, any two shares rebuild
// the identical bytes, and staging only yields share-*.hex.tmp (final names
// are the caller's post-commit rename — a share of a KEK that never rotated
// must never look final).
func TestStageSharesRoundTrip(t *testing.T) {
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir() + "/shares"

	staged, err := stageShares(key, 3, 2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 3 {
		t.Fatalf("%d staged files, want 3", len(staged))
	}
	for _, f := range staged {
		if filepath.Ext(f) != ".tmp" {
			t.Fatalf("staged share %q missing .tmp guard", f)
		}
		st, err := os.Stat(f)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("share %s mode = %v, %v — want 0600", f, st.Mode().Perm(), err)
		}
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("shares dir mode = %v, want 0700", st.Mode().Perm())
	}

	read := func(f string) []byte {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b, err := hex.DecodeString(string(bytes.TrimSpace(raw)))
		if err != nil {
			t.Fatalf("share %s not hex: %v", f, err)
		}
		return b
	}
	parts := [][]byte{read(staged[0]), read(staged[1]), read(staged[2])}

	// Any threshold-sufficient subset reconstructs the key.
	for _, idx := range [][2]int{{0, 1}, {1, 2}, {0, 2}} {
		got, err := shamir.Combine([][]byte{parts[idx[0]], parts[idx[1]]})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, key) {
			t.Fatalf("combine shares %d+%d: wrong key", idx[0], idx[1])
		}
	}
	// One share alone must not reconstruct.
	if got, err := shamir.Combine(parts[:1]); err == nil && bytes.Equal(got, key) {
		t.Fatal("a single share reconstructed the KEK — threshold ignored")
	}
}

func TestStageSharesRejectsBadParams(t *testing.T) {
	key := make([]byte, crypto.KeySize)
	for _, tc := range [][2]int{{1, 1}, {3, 1}, {2, 3}, {3, 4}} {
		if _, err := stageShares(key, tc[0], tc[1], t.TempDir()); err == nil {
			t.Fatalf("%d-of-%d accepted — want rejection", tc[1], tc[0])
		}
	}
}

// The full custody ceremony against a real schema: seed an org + item,
// `key rotate-kek --generate --shares 3 --threshold 2`, then `key combine`
// on two shares — the reconstructed KEK must open the rotated vault.
func TestRotateKEKCeremony(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`DROP SCHEMA IF EXISTS rot_ceremony CASCADE; CREATE SCHEMA rot_ceremony`); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", "rot_ceremony")
	u.RawQuery = q.Encode()
	dsnSchema := u.String()

	oldKEK, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenPostgres(dsnSchema, oldKEK)
	if err != nil {
		t.Fatal(err)
	}
	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, "org", master); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{
		ID: "it", OrgID: "org", Name: "it", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}, store.Secret("vault-secret")); err != nil {
		t.Fatal(err)
	}
	s.Close()

	dir := t.TempDir()
	kekFile := dir + "/kek.hex"
	if err := os.WriteFile(kekFile,
		[]byte(hex.EncodeToString(oldKEK)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sharesDir := dir + "/shares"

	c := keyCmd()
	c.SetArgs([]string{"rotate-kek", "--dsn", dsnSchema, "--kek-file", kekFile,
		"--generate", "--shares", "3", "--threshold", "2", "--shares-dir", sharesDir})
	var out bytes.Buffer
	c.SetOut(&out)
	if err := c.ExecuteContext(ctx); err != nil {
		t.Fatalf("rotate-kek ceremony: %v", err)
	}
	final, err := filepath.Glob(sharesDir + "/share-*.hex")
	if err != nil || len(final) != 3 {
		t.Fatalf("final shares = %v, %v — want 3 share-<i>-<fp>.hex", final, err)
	}
	if tmps, _ := filepath.Glob(sharesDir + "/*.tmp"); len(tmps) > 0 {
		t.Fatalf("staged .tmp left behind: %v", tmps)
	}

	// Reconstruct from a threshold subset (drop one share) via the command.
	os.Remove(final[len(final)-1])
	combined := dir + "/combined.hex"
	c2 := keyCmd()
	c2.SetArgs([]string{"combine", "--shares-dir", sharesDir, "--out", combined})
	c2.SetOut(&out)
	if err := c2.ExecuteContext(ctx); err != nil {
		t.Fatalf("combine: %v", err)
	}
	newKEK, err := loadHexKey("", combined)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := store.OpenPostgres(dsnSchema, newKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	sec, err := s2.Secret("it")
	if err != nil || string(sec) != "vault-secret" {
		t.Fatalf("vault under reconstructed KEK: %v %q", err, sec)
	}
}

// A minted KEK exists only in memory — --generate without --shares would
// rotate the vault under a key nobody can ever reproduce. It must refuse
// before touching the database (these run without PG_TEST_DSN).
func TestRotateKEKGenerateValidation(t *testing.T) {
	t.Setenv("VEIL_KEK_NEW", "")
	run := func(args ...string) error {
		c := keyCmd()
		c.SetArgs(args)
		return c.ExecuteContext(context.Background())
	}
	// --generate with no share output → refuse before touching the DB.
	if err := run("rotate-kek", "--generate"); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("requires --shares")) {
		t.Fatalf("--generate without --shares: %v", err)
	}
	// --generate plus any other new-key source → refuse.
	if err := run("rotate-kek", "--generate", "--new-kek-file", "x"); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("exclusive")) {
		t.Fatalf("--generate + --new-kek-file should fail closed, got %v", err)
	}
}

// combine must only accept one fingerprinted share set — a stray share from
// another ceremony, or a non-share .hex, interpolates to a wrong key with no
// error and must be rejected up front.
func TestCombineRejectsMixedSets(t *testing.T) {
	mkShares := func(key []byte) string {
		dir := t.TempDir()
		parts, err := shamir.Split(key, 3, 2)
		if err != nil {
			t.Fatal(err)
		}
		fp := shareFingerprint(key)
		for i, p := range parts {
			f := filepath.Join(dir, fmt.Sprintf("share-%d-%s.hex", i+1, fp))
			if err := os.WriteFile(f, []byte(hex.EncodeToString(p)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	k1 := make([]byte, crypto.KeySize)
	k2 := make([]byte, crypto.KeySize)
	rand.Read(k1)
	rand.Read(k2)
	d1, d2 := mkShares(k1), mkShares(k2)

	// Two ceremonies in one dir → refuse.
	mixed := t.TempDir()
	for _, src := range []string{d1, d2} {
		ents, _ := os.ReadDir(src)
		for _, e := range ents {
			raw, _ := os.ReadFile(filepath.Join(src, e.Name()))
			os.WriteFile(filepath.Join(mixed, e.Name()), raw, 0o600)
		}
	}
	c := keyCmd()
	c.SetArgs([]string{"combine", "--shares-dir", mixed, "--out", mixed + "/o.hex"})
	if err := c.ExecuteContext(context.Background()); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("multiple ceremonies")) {
		t.Fatalf("mixed ceremonies should fail closed, got %v", err)
	}

	// A foreign .hex (not share-<i>-<fp>.hex shape) → refuse, don't skip.
	foreign := t.TempDir()
	ents, _ := os.ReadDir(d1)
	for _, e := range ents {
		raw, _ := os.ReadFile(filepath.Join(d1, e.Name()))
		os.WriteFile(filepath.Join(foreign, e.Name()), raw, 0o600)
	}
	os.WriteFile(filepath.Join(foreign, "old-backup.hex"), []byte("deadbeef\n"), 0o600)
	c2 := keyCmd()
	c2.SetArgs([]string{"combine", "--shares-dir", foreign, "--out", foreign + "/o.hex"})
	if err := c2.ExecuteContext(context.Background()); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("not a share")) {
		t.Fatalf("foreign .hex should fail closed, got %v", err)
	}

	// A pre-existing world-readable --out must be tightened, not left open.
	out := filepath.Join(t.TempDir(), "combined.hex")
	os.WriteFile(out, []byte("junk"), 0o644)
	c3 := keyCmd()
	c3.SetArgs([]string{"combine", "--shares-dir", d1, "--out", out})
	if err := c3.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(out)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("out file mode = %v, want 0600", st.Mode().Perm())
	}
	got, _ := loadHexKey("", out)
	if !bytes.Equal(got, k1) {
		t.Fatal("combined key mismatch")
	}
}
