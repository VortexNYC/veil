package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/jackc/pgx/v5"
)

// pgTool resolves pg_dump/pg_restore style binaries: explicit env override
// (e.g. PG_DUMP="docker exec pg pg_dump" when the server only exists inside a
// container), then PATH. Skips when neither exists — the drill is opt-in like
// every Postgres test.
func pgTool(t *testing.T, name, envName string) []string {
	t.Helper()
	if cmd := os.Getenv(envName); cmd != "" {
		return strings.Fields(cmd)
	}
	if path, err := exec.LookPath(name); err == nil {
		return []string{path}
	}
	t.Skipf("%s not found (set %s)", name, envName)
	return nil
}

// TestPostgresBackupRestoreDrill is the recovery proof for VEIL-7: a logical
// pg_dump of a seeded schema restored into a fresh schema yields a working
// vault — org_keys unwrap under the same deployment KEK, items decrypt,
// grants/agents/humans/sessions/audit all come back. The second half is the
// operational invariant: the same restored bytes under a different KEK do not
// decrypt — ciphertext is only as durable as the KEK escrow (VEIL-8).
func TestPostgresBackupRestoreDrill(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	kek := testMasterKey(t)

	openSchema := func(schema string) *Postgres {
		t.Helper()
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

	src := openSchema("drill_src")
	defer src.Close()
	for _, org := range []string{"org", "org-1"} {
		master, err := crypto.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := src.EnsureOrgKey(ctx, org, master); err != nil {
			t.Fatal(err)
		}
	}
	if err := src.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "human-1", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := src.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "bot", OrgID: "org", Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: "human-1"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	item := protocol.Item{
		ID: "drill-item", OrgID: "org", Name: "drill-item", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
		URIs:  []string{"api.example.com"},
	}
	if err := src.PutItem(item, Secret("drill-secret-value")); err != nil {
		t.Fatal(err)
	}
	item2 := protocol.Item{
		ID: "drill-item2", OrgID: "org-1", Name: "drill-item2", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
	}
	if err := src.PutItem(item2, Secret("org1-secret")); err != nil {
		t.Fatal(err)
	}
	if err := src.PutGrant(protocol.Grant{
		ID: "drill-grant", OrgID: "org", AgentID: "bot", ItemID: "drill-item",
		Level: protocol.Level2, Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	sess := protocol.Session{
		ID: "drill-sess", OrgID: "org", AgentID: "bot",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		TTL: 3600, MaxTTL: 3600, MaxUses: 3,
	}
	if err := src.PutSession(sess, []byte("drill-session-hash")); err != nil {
		t.Fatal(err)
	}
	if err := src.AppendAudit(protocol.AuditEvent{
		Time: now, OrgID: "org", AgentID: "bot", ItemID: "drill-item",
		Action: protocol.ActionFetch, Decision: protocol.DecisionAllow,
	}); err != nil {
		t.Fatal(err)
	}

	// The dump: real pg_dump of the whole schema. PG_DUMP_CMD lets the test
	// drive the binary inside the server container when the host lacks it.
	dumpArgs := append(pgTool(t, "pg_dump", "PG_DUMP_CMD"),
		"--schema=drill_src", "--no-owner", "--no-privileges", "--no-comments",
		"--inserts", dsn)
	dump, err := exec.Command(dumpArgs[0], dumpArgs[1:]...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("pg_dump: %v\n%s", err, ee.Stderr)
		}
		t.Fatal(err)
	}
	if !strings.Contains(string(dump), "drill_src.") {
		t.Fatal("pg_dump produced no drill_src objects")
	}

	// The restore: same bytes, new schema — equivalent to restoring a backup
	// onto a fresh instance. pgx runs the rewritten script verbatim.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS drill_dst CASCADE`); err != nil {
		t.Fatal(err)
	}
	// Rename the schema token wholesale — the dump carries both qualified
	// refs (drill_src.items) and CREATE SCHEMA drill_src itself.
	restored := strings.ReplaceAll(string(dump), "drill_src", "drill_dst")
	// pg_dump 16 emits \restrict/\unrestrict psql meta-commands that only
	// psql understands; strip backslash lines before replaying via pgx.
	var sql strings.Builder
	for _, line := range strings.Split(restored, "\n") {
		if strings.HasPrefix(line, "\\") {
			continue
		}
		sql.WriteString(line)
		sql.WriteByte('\n')
	}
	if _, err := conn.Exec(ctx, sql.String()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// The recovered vault under the same deployment KEK must be fully live.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", "drill_dst")
	u.RawQuery = q.Encode()
	dst, err := OpenPostgres(u.String(), kek)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()

	sec, err := dst.Secret("drill-item")
	if err != nil {
		t.Fatalf("restored secret unreadable: %v", err)
	}
	if string(sec) != "drill-secret-value" {
		t.Fatalf("restored secret %q", sec)
	}
	sec2, err := dst.Secret("drill-item2")
	if err != nil || string(sec2) != "org1-secret" {
		t.Fatalf("org-1 restored secret: %v %q", err, sec2)
	}
	got, err := dst.Item("drill-item")
	if err != nil || got.OrgID != "org" || got.Name != "drill-item" {
		t.Fatalf("restored item: %v %+v", err, got)
	}
	if _, err := dst.Agent("bot"); err != nil {
		t.Fatalf("restored agent: %v", err)
	}
	if _, err := dst.Human("human-1"); err != nil {
		t.Fatalf("restored human: %v", err)
	}
	g, err := dst.Grant("drill-grant")
	if err != nil || g.AgentID != "bot" || g.ItemID != "drill-item" {
		t.Fatalf("restored grant: %v %+v", err, g)
	}
	sessions, err := dst.ListSessions()
	if err != nil || len(sessions) != 1 || sessions[0].ID != "drill-sess" {
		t.Fatalf("restored sessions: %v %+v", err, sessions)
	}
	events, err := dst.Audit()
	if err != nil || len(events) != 1 || events[0].Decision != protocol.DecisionAllow {
		t.Fatalf("restored audit: %v %+v", err, events)
	}

	// The invariant the runbook exists for: identical bytes under a different
	// KEK yield nothing. org_keys unwrap fails, so Secret fails closed.
	bad, err := OpenPostgres(u.String(), testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	if _, err := bad.Secret("drill-item"); err == nil {
		t.Fatal("restored vault decrypted under the wrong KEK")
	}
}
