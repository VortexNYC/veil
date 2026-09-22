package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

// Two orgs, same owner id, distinct masters: each org's DEK set is independent
// and a secret sealed under one org does not open under the other org's master.
func TestPostgresOrgKeyIsolation(t *testing.T) {
	s := openTestPostgres(t)
	ctx := context.Background()

	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "shared-owner"}
	if err := s.PutItem(protocol.Item{ID: "a-item", OrgID: "org", Name: "a", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("sk-org-a")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{ID: "b-item", OrgID: "org-1", Name: "b", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("sk-org-b")); err != nil {
		t.Fatal(err)
	}

	// Same owner id in two orgs produced two owner_keys rows.
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM owner_keys WHERE owner_id='shared-owner'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 owner_keys rows for shared-owner, got %d", n)
	}

	sec, err := s.Secret("a-item")
	if err != nil || string(sec) != "sk-org-a" {
		t.Fatalf("org secret: %v %q", err, sec)
	}
	sec, err = s.Secret("b-item")
	if err != nil || string(sec) != "sk-org-b" {
		t.Fatalf("org-1 secret: %v %q", err, sec)
	}

	// The blob sealed under org's DEK does not open under org-1's master chain.
	var blob []byte
	if err := s.pool.QueryRow(ctx, `SELECT secret FROM items WHERE id='a-item'`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	dekB, err := s.ownerDEK("org-1", owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Open(dekB, blob); err == nil {
		t.Fatal("org-1 owner DEK opened org's ciphertext")
	}
}

// An item write into an org with no org_keys row fails closed — no DEK is
// minted under a wrong key and nothing is persisted.
func TestPostgresOrgKeyMissingFailClosed(t *testing.T) {
	s := openTestPostgres(t)

	err := s.PutItem(protocol.Item{
		ID: "x", OrgID: "org-nokey", Name: "x", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: "self"},
	}, Secret("sk"))
	if err == nil {
		t.Fatal("PutItem into org with no key row succeeded")
	}
	if _, err := s.Item("x"); err == nil {
		t.Fatal("item row persisted despite key failure")
	}
}

// EnsureOrgKey never overwrites an existing row: re-seeding with the same
// master is a no-op, but a different master fails with ErrOrgKeyMismatch —
// a boot asserting the wrong key must be loud, not silently lock the vault.
func TestPostgresEnsureOrgKeyIdempotent(t *testing.T) {
	s := openTestPostgres(t)
	ctx := context.Background()

	org := "org-seed"
	first, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, org, first); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, org, first); err != nil {
		t.Fatalf("same-master re-seed must be a no-op: %v", err)
	}
	other, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureOrgKey(ctx, org, other); !errors.Is(err, ErrOrgKeyMismatch) {
		t.Fatalf("different-master re-seed must fail, got %v", err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "self"}
	if err := s.PutItem(protocol.Item{ID: "keep", OrgID: org, Name: "keep", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("sk")); err != nil {
		t.Fatal(err)
	}
	sec, err := s.Secret("keep")
	if err != nil || string(sec) != "sk" {
		t.Fatalf("secret unreadable after duplicate EnsureOrgKey: %v", err)
	}
}

// A database written before owner_keys had org_id still opens: the migration
// rebuilds the table and backfills rows to the one pre-multi-tenant org
// (LocalOrgID — the org every legacy row belonged to), so wrapped DEKs keep
// unwrapping.
func TestPostgresOwnerKeysMigration(t *testing.T) {
	s := openTestPostgres(t)
	ctx := context.Background()

	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "self"}
	if err := s.PutItem(protocol.Item{ID: "pre", OrgID: protocol.LocalOrgID, Name: "pre", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("sk-pre")); err != nil {
		t.Fatal(err)
	}

	// Revert owner_keys to the pre-org shape in place, keeping the wrapped blob.
	for _, q := range []string{
		`ALTER TABLE owner_keys DROP CONSTRAINT owner_keys_pkey`,
		`ALTER TABLE owner_keys DROP COLUMN org_id`,
		`ALTER TABLE owner_keys ADD PRIMARY KEY (owner_kind, owner_id)`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatalf("revert owner_keys: %v", err)
		}
	}

	if err := s.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s.km.InvalidateOrg(protocol.LocalOrgID)
	sec, err := s.Secret("pre")
	if err != nil || string(sec) != "sk-pre" {
		t.Fatalf("secret unreadable after owner_keys migration: %v", err)
	}
	var org string
	if err := s.pool.QueryRow(ctx, `SELECT org_id FROM owner_keys WHERE owner_id='self'`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if org != protocol.LocalOrgID {
		t.Fatalf("owner_keys row backfilled to %q, want %q", org, protocol.LocalOrgID)
	}
}

// A crash mid-migration must not strand the table: the rebuild gate is PK
// membership, not column presence, so a boot that finds a partial state
// completes it instead of skipping forever.
func TestPostgresOwnerKeysMidMigration(t *testing.T) {
	// Full revert to the pre-org shape, then the partial prefix the crash left.
	crashes := map[string][]string{
		"after add column": {
			`ALTER TABLE owner_keys DROP CONSTRAINT owner_keys_pkey`,
			`ALTER TABLE owner_keys DROP COLUMN org_id`,
			`ALTER TABLE owner_keys ADD PRIMARY KEY (owner_kind, owner_id)`,
			`ALTER TABLE owner_keys ADD COLUMN org_id TEXT`,
		},
		"after drop constraint": {
			`ALTER TABLE owner_keys DROP CONSTRAINT owner_keys_pkey`,
			`ALTER TABLE owner_keys DROP COLUMN org_id`,
			`ALTER TABLE owner_keys ADD PRIMARY KEY (owner_kind, owner_id)`,
			`ALTER TABLE owner_keys ADD COLUMN org_id TEXT`,
			`UPDATE owner_keys SET org_id = '` + protocol.LocalOrgID + `'`,
			`ALTER TABLE owner_keys DROP CONSTRAINT owner_keys_pkey`,
		},
	}
	for name, stmts := range crashes {
		t.Run(name, func(t *testing.T) {
			s := openTestPostgres(t)
			ctx := context.Background()

			owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "self"}
			if err := s.PutItem(protocol.Item{ID: "pre", OrgID: protocol.LocalOrgID, Name: "pre", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("sk-pre")); err != nil {
				t.Fatal(err)
			}
			for _, q := range stmts {
				if _, err := s.pool.Exec(ctx, q); err != nil {
					t.Fatalf("simulate mid-migration crash %q: %v", q, err)
				}
			}
			if err := s.migrate(); err != nil {
				t.Fatalf("migrate after partial state: %v", err)
			}
			s.km.InvalidateOrg(protocol.LocalOrgID)
			sec, err := s.Secret("pre")
			if err != nil || string(sec) != "sk-pre" {
				t.Fatalf("secret unreadable after mid-migration recovery: %v", err)
			}
			var n int
			if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=ANY(c.conkey) WHERE t.relname='owner_keys' AND c.contype='p' AND a.attname='org_id'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				t.Fatal("owner_keys PK does not include org_id after recovery")
			}
		})
	}
}

// Same migration on the sqlite side: drop the table back to its pre-org shape
// under a raw handle, reopen, and the secret must still read.
func TestSQLiteOwnerKeysMigration(t *testing.T) {
	path := t.TempDir() + "/vault.db"
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	owner := protocol.Owner{Kind: protocol.OwnerUser, ID: "self"}
	if err := s.PutItem(protocol.Item{ID: "pre", OrgID: protocol.LocalOrgID, Name: "pre", Kind: protocol.ItemAPIKey, Owner: owner}, Secret("sk-pre")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE owner_keys RENAME TO owner_keys_old`,
		`CREATE TABLE owner_keys (
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			wrapped BLOB NOT NULL,
			PRIMARY KEY (owner_kind, owner_id)
		)`,
		`INSERT INTO owner_keys(owner_kind, owner_id, wrapped) SELECT owner_kind, owner_id, wrapped FROM owner_keys_old`,
		`DROP TABLE owner_keys_old`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("revert owner_keys: %v", err)
		}
	}
	_ = db.Close()

	s2, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	sec, err := s2.Secret("pre")
	if err != nil || string(sec) != "sk-pre" {
		t.Fatalf("secret unreadable after sqlite owner_keys migration: %v", err)
	}
}
