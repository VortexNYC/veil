package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"

	_ "modernc.org/sqlite"
)

func TestSQLiteRoundTripAndNoPlaintextOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	secret := Secret("sk_live_PLAINTEXT_MUST_NOT_HIT_DISK")
	item := protocol.Item{
		ID:    "stripe",
		OrgID: "org",
		Name:  "stripe",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
		URIs:  []string{"https://api.stripe.com"},
	}
	if err := s.PutItem(item, secret); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGrant(protocol.Grant{
		ID:      "claude:stripe",
		OrgID:   "org",
		AgentID: "claude",
		ItemID:  "stripe",
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Item("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "stripe" {
		t.Fatalf("%+v", got)
	}
	plain, err := s.Secret("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(secret) {
		t.Fatalf("secret=%q", plain)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, secret) {
		t.Fatal("plaintext secret written to sqlite file")
	}

	// reopen
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	plain, err = s2.Secret("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(secret) {
		t.Fatalf("reopen secret=%q", plain)
	}
	g, err := s2.GrantFor("claude", "stripe")
	if err != nil || g == nil || g.Level != protocol.Level2 {
		t.Fatalf("grant=%+v err=%v", g, err)
	}
	var sealed []byte
	if err := s2.db.QueryRow(`SELECT secret FROM items WHERE id=?`, "stripe").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Open(key, sealed); err == nil {
		t.Fatal("item still sealed with master; owner DEK unused")
	}
}

func TestSQLiteWrongKeyCannotReadSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{ID: "x", OrgID: "o", Name: "x", Kind: protocol.ItemAPIKey, Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "o"}}, Secret("secret-value")); err != nil {
		t.Fatal(err)
	}
	s.Close()

	other, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := OpenSQLite(path, other)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.Secret("x"); err == nil {
		t.Fatal("wrong key opened secret")
	}
}

func TestSQLiteAuditHasNoSecret(t *testing.T) {
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
	secret := []byte("sk_audit_secret")
	if err := s.AppendAudit(protocol.AuditEvent{
		Time:     time.Now().UTC(),
		OrgID:    "org",
		AgentID:  "claude",
		ItemID:   "stripe",
		Action:   protocol.ActionFetch,
		Decision: protocol.DecisionAllow,
	}); err != nil {
		t.Fatal(err)
	}
	events, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("%d", len(events))
	}
	if scrub.Contains([]byte(events[0].Reason), secret) {
		t.Fatal("secret in audit")
	}
}

func TestSQLiteTOTPSeedNotOnDiskAndHasTOTPPersists(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	blob, err := material.Pack([]byte("sk_live_token"), []byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.Item{
		ID:      "gmail",
		OrgID:   "org",
		Name:    "gmail",
		Kind:    protocol.ItemAPIKey,
		Owner:   protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
		HasTOTP: true,
	}
	if err := s.PutItem(item, Secret(blob)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Item("gmail")
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasTOTP {
		t.Fatal("has_totp dropped")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(seed)) || bytes.Contains(raw, []byte("sk_live_token")) {
		t.Fatal("totp seed or token written in plaintext")
	}
}

func TestSQLiteLoginPersistsAsMetadata(t *testing.T) {
	const login = "stripe@example.com"
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	item := protocol.Item{
		ID:    "stripe",
		OrgID: "org",
		Name:  "stripe",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
		Login: login,
	}
	if err := s.PutItem(item, Secret("sk_live_token")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Item("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if got.Login != login {
		t.Fatalf("login dropped: %+v", got)
	}
	listed, err := s.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Login != login {
		t.Fatalf("list login %+v", listed)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("sk_live_token")) {
		t.Fatal("token written in plaintext")
	}
}

func TestSQLiteOwnerKeysDifferAndGrantHasNoDEK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	org := protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"}
	user := protocol.Owner{Kind: protocol.OwnerUser, ID: "self"}
	if err := s.PutItem(protocol.Item{ID: "stripe", OrgID: "org", Name: "stripe", Kind: protocol.ItemAPIKey, Owner: org}, Secret("sk_org")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(protocol.Item{ID: "gmail", OrgID: "org", Name: "gmail", Kind: protocol.ItemAPIKey, Owner: user}, Secret("sk_user")); err != nil {
		t.Fatal(err)
	}
	dekOrg, err := s.ownerDEK("org", org)
	if err != nil {
		t.Fatal(err)
	}
	dekUser, err := s.ownerDEK("org", user)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(dekOrg, dekUser) {
		t.Fatal("owners share a DEK")
	}
	var userBlob []byte
	if err := s.db.QueryRow(`SELECT secret FROM items WHERE id=?`, "gmail").Scan(&userBlob); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Open(dekOrg, userBlob); err == nil {
		t.Fatal("org DEK opened another owner's item")
	}
	got, err := s.Secret("gmail")
	if err != nil || string(got) != "sk_user" {
		t.Fatalf("gmail=%q err=%v", got, err)
	}

	if err := s.PutGrant(protocol.Grant{
		ID: "claude:stripe", OrgID: "org", AgentID: "claude", ItemID: "stripe",
		Level: protocol.Level2, Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	grants, err := s.ListGrants()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(grants)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, dekOrg) || scrub.Contains(raw, dekUser) || scrub.Contains(raw, key) {
		t.Fatalf("grant JSON has a key: %s", raw)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, dekOrg) || bytes.Contains(disk, dekUser) {
		t.Fatal("owner DEK plaintext on disk")
	}
}

func TestSQLiteLegacyMasterSealedSecretsRewrap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	secret := Secret("sk_legacy_must_survive")
	blob, err := crypto.Seal(key, secret)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"stripe", "org", "stripe", protocol.ItemAPIKey, protocol.OwnerOrg, "org", "[]", blob, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.Secret("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(secret) {
		t.Fatalf("got %q", got)
	}
	var sealed []byte
	if err := s2.db.QueryRow(`SELECT secret FROM items WHERE id=?`, "stripe").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Open(key, sealed); err == nil {
		t.Fatal("legacy secret still sealed with master")
	}
}

func TestSQLiteLegacyVersionRewrapAndRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.Item{
		ID:    "stripe",
		OrgID: "org",
		Name:  "stripe",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}
	if err := s.PutItem(item, Secret("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(item, Secret("v2")); err != nil {
		t.Fatal(err)
	}
	vers, err := s.Versions("stripe")
	if err != nil || len(vers) != 1 {
		t.Fatalf("versions=%+v err=%v", vers, err)
	}
	// Reset the migration marker so the next open re-scans. Then inject a
	// master-sealed historical version behind the store's back.
	if _, err := s.db.Exec(`DELETE FROM schema_version WHERE name='rewrap_legacy'`); err != nil {
		t.Fatal(err)
	}
	v0 := Secret("v0")
	blob, err := crypto.Seal(key, v0)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec(`INSERT INTO item_versions(item_id, at, secret) VALUES(?,?,?)`, "stripe", time.Now().UTC().Format(time.RFC3339Nano), blob)
	if err != nil {
		t.Fatal(err)
	}
	v0ID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// The legacy version should have been re-encrypted with the owner DEK and
	// must be restorable.
	if err := s2.RestoreVersion("stripe", v0ID); err != nil {
		t.Fatal(err)
	}
	got, err := s2.Secret("stripe")
	if err != nil || string(got) != string(v0) {
		t.Fatalf("secret=%q err=%v", got, err)
	}
	// After a successful rewrap the migration should be marked complete.
	var version int
	if err := s2.db.QueryRow(`SELECT version FROM schema_version WHERE name='rewrap_legacy'`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("schema_version=%d err=%v", version, err)
	}
}

func TestSQLiteLegacyMixedMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	modern := protocol.Item{
		ID:    "modern",
		OrgID: "org",
		Name:  "modern",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}
	if err := s.PutItem(modern, Secret("sk_modern")); err != nil {
		t.Fatal(err)
	}
	// Reset marker and inject a legacy master-sealed item for a different owner.
	if _, err := s.db.Exec(`DELETE FROM schema_version WHERE name='rewrap_legacy'`); err != nil {
		t.Fatal(err)
	}
	legacy := Secret("sk_legacy")
	blob, err := crypto.Seal(key, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"legacy", "org2", "legacy", protocol.ItemAPIKey, protocol.OwnerUser, "self2", "[]", blob, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.Secret("modern")
	if err != nil || string(got) != "sk_modern" {
		t.Fatalf("modern=%q err=%v", got, err)
	}
	got, err = s2.Secret("legacy")
	if err != nil || string(got) != string(legacy) {
		t.Fatalf("legacy=%q err=%v", got, err)
	}
	// Both owner keys should now exist.
	var n int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM owner_keys`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("owner_keys=%d err=%v", n, err)
	}
}

func TestSQLiteWrongKeyDoesNotCrashWithLegacyItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	legacy := Secret("sk_legacy")
	blob, err := crypto.Seal(key, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"legacy", "org", "legacy", protocol.ItemAPIKey, protocol.OwnerOrg, "org", "[]", blob, 0); err != nil {
		t.Fatal(err)
	}
	// Reset the marker so the next open attempts the legacy rewrap with the
	// wrong key and proves it does not crash.
	if _, err := s.db.Exec(`DELETE FROM schema_version WHERE name='rewrap_legacy'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	other, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := OpenSQLite(path, other)
	if err != nil {
		t.Fatalf("open with wrong key failed: %v", err)
	}
	defer s2.Close()
	if _, err := s2.Secret("legacy"); err == nil {
		t.Fatal("wrong key opened legacy secret")
	}
}

func TestSQLiteArchiveHistoryFileNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	body := []byte("FILE_PLAINTEXT_MUST_NOT_HIT_DISK")
	blob, err := material.PackFile("note.txt", "text/plain", body)
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.Item{
		ID:      "note",
		OrgID:   "org",
		Name:    "note",
		Kind:    protocol.ItemFile,
		Owner:   protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
		HasFile: true,
		Tags:    []string{"docs"},
	}
	if err := s.PutItem(item, Secret(blob)); err != nil {
		t.Fatal(err)
	}
	first := Secret("sk_live_VERSION_ONE")
	api := protocol.Item{
		ID:    "stripe",
		OrgID: "org",
		Name:  "stripe",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}
	if err := s.PutItem(api, first); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(api, Secret("sk_live_VERSION_TWO")); err != nil {
		t.Fatal(err)
	}
	vs, err := s.Versions("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("versions=%d", len(vs))
	}
	if err := s.RestoreVersion("stripe", vs[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Secret("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(first) {
		t.Fatalf("restored %q", got)
	}
	if err := s.ArchiveItem("stripe"); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "note" {
		t.Fatalf("%+v", listed)
	}
	archived, err := s.Item("stripe")
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived {
		t.Fatal("expected archived")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, body) || bytes.Contains(raw, first) {
		t.Fatal("plaintext on disk")
	}
	sec, err := s.Secret("note")
	if err != nil {
		t.Fatal(err)
	}
	file, err := material.FileBytes(material.Unpack(sec))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, body) {
		t.Fatalf("%q", file)
	}
	if err := s.DeleteItem("stripe"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Item("stripe"); err != ErrNotFound {
		t.Fatalf("deleted: %v", err)
	}
}

func TestSQLiteAllowsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE items (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			uris TEXT NOT NULL,
			secret BLOB NOT NULL,
			has_totp INTEGER NOT NULL DEFAULT 0,
			tags TEXT NOT NULL DEFAULT '[]',
			archived INTEGER NOT NULL DEFAULT 0,
			has_file INTEGER NOT NULL DEFAULT 0,
			login TEXT NOT NULL DEFAULT '',
			UNIQUE(org_id, name)
		)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	secret := Secret("x")
	base := protocol.Item{
		OrgID: "org",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}
	a := base
	a.ID, a.Name = "i1", "Microsoft"
	b := base
	b.ID, b.Name = "i2", "Microsoft"
	if err := s.PutItem(a, secret); err != nil {
		t.Fatal(err)
	}
	if err := s.PutItem(b, secret); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("n=%d", len(items))
	}
}

func TestSQLiteSessionHashNotPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	token := "ses_" + strings.Repeat("ab", 32)
	sum := sha256.Sum256([]byte(token))
	sess := protocol.Session{
		ID:        "s1",
		OrgID:     "org",
		AgentID:   "claude",
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	if err := s.PutSession(sess, sum[:]); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("session token on disk")
	}
	got, err := s.SessionByHash(sum[:])
	if err != nil || got.AgentID != "claude" || got.ID != "s1" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSQLiteRevokeAgentIsIdempotentAndSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	s1, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	first := time.Unix(1700000000, 0).UTC()
	if err := s1.RevokeAgent("flue", first); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	p, err := s2.Agent("flue")
	if err != nil {
		t.Fatal(err)
	}
	if p.RevokedAt == nil || !p.RevokedAt.Equal(first) {
		t.Fatalf("revoked_at not preserved: %+v", p.RevokedAt)
	}
	later := time.Unix(1800000000, 0).UTC()
	if err := s2.RevokeAgent("flue", later); err != nil {
		t.Fatal(err)
	}
	p, err = s2.Agent("flue")
	if err != nil {
		t.Fatal(err)
	}
	if p.RevokedAt == nil || !p.RevokedAt.Equal(first) {
		t.Fatalf("revoked_at was overwritten to %+v", p.RevokedAt)
	}
}

func TestSQLiteAppendAudits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSQLite(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	events := []protocol.AuditEvent{
		{Time: time.Unix(1, 0), OrgID: "o", AgentID: "a1", Action: protocol.ActionFetch, Decision: protocol.DecisionAllow},
		{Time: time.Unix(2, 0), OrgID: "o", AgentID: "a2", Action: protocol.ActionFetch, Decision: protocol.DecisionAllow},
	}
	if err := s.AppendAudits(events); err != nil {
		t.Fatal(err)
	}
	got, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(events) {
		t.Fatalf("got %d events", len(got))
	}
	if got[0].AgentID != "a1" || got[1].AgentID != "a2" {
		t.Fatalf("order wrong: %+v", got)
	}
}
