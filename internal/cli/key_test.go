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
	"github.com/hashicorp/vault/shamir"
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
	for i := 1; i <= 3; i++ {
		f := fmt.Sprintf("%s/share-%d.hex", sharesDir, i)
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("final share missing: %s", f)
		}
	}
	if _, err := os.Stat(sharesDir + "/share-1.hex.tmp"); !os.IsNotExist(err) {
		t.Fatal("staged .tmp left behind — shares must finalize post-commit")
	}

	// Reconstruct from a threshold subset (drop share-3) via the command.
	os.Remove(sharesDir + "/share-3.hex")
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
