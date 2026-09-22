package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testMasterKey(t *testing.T) []byte {
	t.Helper()
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func seedSourceVault(t *testing.T) (sqlitePath string, key []byte, want map[string]string) {
	t.Helper()
	key = testMasterKey(t)
	dir := t.TempDir()
	sqlitePath = filepath.Join(dir, "vault.db")
	src, err := OpenSQLite(sqlitePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	org := "org-test"
	human := protocol.Principal{ID: "human-1", OrgID: org}
	if err := src.PutHuman(human); err != nil {
		t.Fatal(err)
	}
	agent := protocol.Principal{ID: "agent-1", OrgID: org}
	if err := src.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	revoked := protocol.Principal{ID: "agent-dead", OrgID: org}
	if err := src.PutAgent(revoked); err != nil {
		t.Fatal(err)
	}
	if err := src.RevokeAgent("agent-dead", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	item := protocol.Item{
		ID: "item-1", OrgID: org, Name: "github", Kind: "api",
		Owner: protocol.Owner{Kind: "org", ID: org},
		URIs:  []string{"api.github.com"},
		Tags:  []string{"ci"},
	}
	if err := src.PutItem(item, Secret("first-secret")); err != nil {
		t.Fatal(err)
	}
	if err := src.PutItem(item, Secret("live-secret")); err != nil {
		t.Fatal(err)
	}
	old := item
	old.ID = "item-archived"
	old.Name = "old-key"
	if err := src.PutItem(old, Secret("archived-secret")); err != nil {
		t.Fatal(err)
	}
	if err := src.ArchiveItem("item-archived"); err != nil {
		t.Fatal(err)
	}

	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err := src.PutGrant(protocol.Grant{
		ID: "grant-1", OrgID: org, AgentID: "agent-1", ItemID: "item-1",
		Level: "use", Actions: []protocol.ActionKind{protocol.ActionFetch}, ExpiresAt: &expiry,
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.PutApproval(protocol.Approval{
		ID: "ap-1", GrantID: "grant-1", HumanID: "human-1",
		ExpiresAt: time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.PutWorkload(protocol.Workload{
		Issuer: "https://id.veil.nyc", Subject: "agent-1", AgentID: "agent-1", Audience: "password-manager",
	}); err != nil {
		t.Fatal(err)
	}

	sess := protocol.Session{
		ID: "sess-1", OrgID: org, AgentID: "agent-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC(),
		TTL: 900, MaxTTL: 3600, MaxUses: 3, Uses: 1,
	}
	if err := src.PutSession(sess, []byte("hash-live")); err != nil {
		t.Fatal(err)
	}
	dead := sess
	dead.ID = "sess-dead"
	if err := src.PutSession(dead, []byte("hash-dead")); err != nil {
		t.Fatal(err)
	}
	if err := src.RevokeSession("sess-dead", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if err := src.AppendAudit(protocol.AuditEvent{
		Time: time.Now().UTC(), OrgID: org, AgentID: "agent-1", ItemID: "item-1",
		Action: "use", Decision: "allow", Reason: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	return sqlitePath, key, map[string]string{"item-1": "live-secret", "item-archived": "archived-secret"}
}

// migrateDSN rewrites the test DSN so the migrate target lives in a
// per-test schema, the same isolation openTestPostgres uses.
func migrateDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	schema := "mig_" + strings.ReplaceAll(t.Name(), "/", "_")
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema)); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func tableCounts(t *testing.T, dsn string) map[string]int64 {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	out := map[string]int64{}
	for _, table := range []string{"humans", "agents", "items", "grants", "approvals", "audit", "workloads", "owner_keys", "item_versions", "sessions"} {
		var n int64
		if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

func TestMigrateSQLiteToPostgres(t *testing.T) {
	sqlitePath, key, wantSecrets := seedSourceVault(t)
	dsn := migrateDSN(t)

	report, err := MigrateSQLiteToPostgres(context.Background(), sqlitePath, dsn)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := tableCounts(t, dsn)
	for _, tr := range report.Tables {
		if tr.Inserted != tr.Read {
			t.Errorf("%s: inserted %d != read %d", tr.Table, tr.Inserted, tr.Read)
		}
		if got[tr.Table] != int64(tr.Read) {
			t.Errorf("%s: pg count %d != sqlite read %d", tr.Table, got[tr.Table], tr.Read)
		}
	}
	want := map[string]int64{
		"humans": 1, "agents": 2, "items": 2, "grants": 1, "approvals": 1,
		"audit": 1, "workloads": 1, "owner_keys": 1, "item_versions": 1, "sessions": 2,
	}
	for table, n := range want {
		if got[table] != n {
			t.Errorf("%s: got %d rows want %d", table, got[table], n)
		}
	}

	// Domain-level proof: the same master key opens the migrated store and
	// decrypts secrets, which only works if ciphertext and owner keys moved
	// byte-for-byte.
	dst, err := OpenPostgres(dsn, key)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer dst.Close()
	// key doubles as the KEK here; the migrated vault's master is the same
	// bytes, so org-test's wrapped DEKs still unwrap.
	if err := dst.EnsureOrgKey(context.Background(), "org-test", key); err != nil {
		t.Fatalf("seed org key: %v", err)
	}
	for id, want := range wantSecrets {
		sec, err := dst.Secret(id)
		if err != nil {
			t.Fatalf("secret %s: %v", id, err)
		}
		if string(sec) != want {
			t.Fatalf("secret %s: got %q want %q", id, sec, want)
		}
	}
	if _, err := dst.ItemByName("org-test", "old-key"); err != nil {
		t.Fatalf("archived item missing: %v", err)
	}
	vers, err := dst.Versions("item-1")
	if err != nil || len(vers) != 1 {
		t.Fatalf("versions: %d %v", len(vers), err)
	}
	ag, err := dst.Agent("agent-dead")
	if err != nil || ag.RevokedAt == nil {
		t.Fatalf("revoked agent lost revocation: %+v %v", ag, err)
	}
	sess, err := dst.SessionByHash([]byte("hash-live"))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if sess.Uses != 1 || sess.MaxUses != 3 {
		t.Fatalf("session counters lost: %+v", sess)
	}
	// Atomic consume still works on the migrated session.
	if _, err := dst.ConsumeSession([]byte("hash-live"), time.Now().UTC()); err != nil {
		t.Fatalf("consume: %v", err)
	}
	// Revoked session stays revoked.
	if _, err := dst.SessionByHash([]byte("hash-dead")); err == nil {
		// SessionByHash may return the row; ConsumeSession must refuse.
		if _, err := dst.ConsumeSession([]byte("hash-dead"), time.Now().UTC()); err == nil {
			t.Fatal("consumed revoked session")
		}
	}
	// Grant expiry survived the epoch→timestamptz hop.
	g, err := dst.GrantFor("agent-1", "item-1")
	if err != nil || g.ExpiresAt == nil {
		t.Fatalf("grant: %+v %v", g, err)
	}
	if time.Until(*g.ExpiresAt) <= 0 {
		t.Fatalf("grant expiry in the past: %v", g.ExpiresAt)
	}

	// Live writes continue: a fresh audit insert must not collide with the
	// copied identity sequence.
	if err := dst.AppendAudit(protocol.AuditEvent{
		Time: time.Now().UTC(), OrgID: "org-test", AgentID: "agent-1",
		ItemID: "item-1", Action: "use", Decision: "allow", Reason: "post-migrate",
	}); err != nil {
		t.Fatalf("post-migrate audit: %v", err)
	}

	// Rerun is idempotent: every row skips, nothing double-writes.
	second, err := MigrateSQLiteToPostgres(context.Background(), sqlitePath, dsn)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	for _, tr := range second.Tables {
		if tr.Inserted != 0 {
			t.Errorf("rerun %s: inserted %d, want 0", tr.Table, tr.Inserted)
		}
	}
	if n := tableCounts(t, dsn)["audit"]; n != 2 {
		t.Fatalf("audit after rerun: %d, want 2", n)
	}
}

// Legacy single-tenant rows carry org_id ”. They decrypt only under the
// master wrapped as LocalOrgID's org_keys row, so migration assigns them to
// that org — after which strict org equality is fail-closed everywhere.
func TestEmptyOrgBackfill(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		s, err := OpenSQLite(filepath.Join(t.TempDir(), "v.db"), testMasterKey(t))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if _, err := s.db.Exec(`INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret)
			VALUES('legacy', '', 'legacy', 'api_key', 'org', '', '[]', X'00')`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO humans(id, org_id) VALUES('legacy-h', '')`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO agents(id, org_id) VALUES('legacy-a', '')`); err != nil {
			t.Fatal(err)
		}
		if err := s.migrate(); err != nil {
			t.Fatal(err)
		}
		it, err := s.Item("legacy")
		if err != nil {
			t.Fatal(err)
		}
		if it.OrgID != protocol.LocalOrgID {
			t.Fatalf("item org %q", it.OrgID)
		}
		h, err := s.Human("legacy-h")
		if err != nil {
			t.Fatal(err)
		}
		if h.OrgID != protocol.LocalOrgID {
			t.Fatalf("human org %q", h.OrgID)
		}
		ag, err := s.Agent("legacy-a")
		if err != nil {
			t.Fatal(err)
		}
		if ag.OrgID != protocol.LocalOrgID {
			t.Fatalf("agent org %q", ag.OrgID)
		}
	})
	t.Run("postgres", func(t *testing.T) {
		p := openTestPostgres(t)
		ctx := context.Background()
		if _, err := p.pool.Exec(ctx, `INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret)
			VALUES('legacy', '', 'legacy', 'api_key', 'org', '', '[]', '\x00')`); err != nil {
			t.Fatal(err)
		}
		if _, err := p.pool.Exec(ctx, `INSERT INTO humans(id, org_id) VALUES('legacy-h', '')`); err != nil {
			t.Fatal(err)
		}
		if _, err := p.pool.Exec(ctx, `INSERT INTO agents(id, org_id) VALUES('legacy-a', '')`); err != nil {
			t.Fatal(err)
		}
		if err := EnsurePostgresSchema(ctx, p.pool); err != nil {
			t.Fatal(err)
		}
		it, err := p.Item("legacy")
		if err != nil {
			t.Fatal(err)
		}
		if it.OrgID != protocol.LocalOrgID {
			t.Fatalf("item org %q", it.OrgID)
		}
		h, err := p.Human("legacy-h")
		if err != nil {
			t.Fatal(err)
		}
		if h.OrgID != protocol.LocalOrgID {
			t.Fatalf("human org %q", h.OrgID)
		}
		ag, err := p.Agent("legacy-a")
		if err != nil {
			t.Fatal(err)
		}
		if ag.OrgID != protocol.LocalOrgID {
			t.Fatalf("agent org %q", ag.OrgID)
		}
	})
}
