package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/jackc/pgx/v5"
)

func openTestPostgres(t *testing.T) *Postgres {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	key, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(t.Name(), "/", "_")

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
		t.Fatalf("close setup conn: %v", err)
	}

	searchDSN := dsn
	if parsed, err := url.Parse(dsn); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		q := parsed.Query()
		q.Set("search_path", schema)
		parsed.RawQuery = q.Encode()
		searchDSN = parsed.String()
	} else {
		searchDSN = strings.TrimSpace(dsn) + " search_path=" + schema
	}

	s, err := OpenPostgres(searchDSN, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// key is the KEK; every test org gets its own randomly generated master,
	// which is also the cross-org isolation proof.
	for _, org := range []string{"org", "org-1", "o", "org-test", protocol.LocalOrgID} {
		master, err := crypto.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureOrgKey(ctx, org, master); err != nil {
			t.Fatalf("seed org key %s: %v", org, err)
		}
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPostgresRoundTrip(t *testing.T) {
	s := openTestPostgres(t)

	org := protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"}
	item := protocol.Item{
		ID:    "stripe",
		OrgID: "org",
		Name:  "stripe",
		Kind:  protocol.ItemAPIKey,
		Owner: org,
		URIs:  []string{"https://api.stripe.com"},
	}
	secret := Secret("sk_live_secret")
	if err := s.PutItem(item, secret); err != nil {
		t.Fatalf("put item: %v", err)
	}
	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org"}); err != nil {
		t.Fatalf("put agent: %v", err)
	}
	if err := s.PutGrant(protocol.Grant{
		ID:      "claude:stripe",
		OrgID:   "org",
		AgentID: "claude",
		ItemID:  "stripe",
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatalf("put grant: %v", err)
	}

	got, err := s.Item("stripe")
	if err != nil {
		t.Fatalf("item: %v", err)
	}
	if got.Name != "stripe" {
		t.Fatalf("name %q", got.Name)
	}

	g, err := s.GrantFor("claude", "stripe")
	if err != nil {
		t.Fatalf("grant for: %v", err)
	}
	if g == nil {
		t.Fatal("grant not found")
	}
	if g.Level != protocol.Level2 {
		t.Fatalf("level %q", g.Level)
	}

	plain, err := s.Secret("stripe")
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if string(plain) != "sk_live_secret" {
		t.Fatalf("secret mismatch")
	}

	byName, err := s.ItemByName("org", "stripe")
	if err != nil {
		t.Fatalf("item by name: %v", err)
	}
	if byName.ID != "stripe" {
		t.Fatalf("by name %q", byName.ID)
	}

	items, err := s.ListItems()
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items %d", len(items))
	}

	agents, err := s.ListAgents()
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("agents %d", len(agents))
	}

	grants, err := s.ListGrants()
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("grants %d", len(grants))
	}
}

func TestPostgresPerOwnerDEK(t *testing.T) {
	s := openTestPostgres(t)

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
	if string(dekOrg) == string(dekUser) {
		t.Fatal("owners share a DEK")
	}

	var userBlob []byte
	if err := s.pool.QueryRow(context.Background(), `SELECT secret FROM items WHERE id=$1`, "gmail").Scan(&userBlob); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Open(dekOrg, userBlob); err == nil {
		t.Fatal("org DEK opened another owner's item")
	}

	got, err := s.Secret("gmail")
	if err != nil || string(got) != "sk_user" {
		t.Fatalf("gmail=%q err=%v", got, err)
	}
}

func TestPostgresRevokeAgentAndAudit(t *testing.T) {
	s := openTestPostgres(t)

	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := s.RevokeAgent("claude", at, protocol.AuditEvent{
		Time:     at,
		OrgID:    "org",
		AgentID:  "claude",
		ItemID:   "",
		Action:   protocol.ActionRevoke,
		Decision: protocol.DecisionAllow,
		Reason:   "incident",
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	ag, err := s.Agent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if ag.RevokedAt == nil {
		t.Fatal("revoked_at not set")
	}

	events, err := s.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events %d", len(events))
	}
	if events[0].Reason != "incident" {
		t.Fatalf("reason %q", events[0].Reason)
	}
}

func TestPostgresAppendAudits(t *testing.T) {
	s := openTestPostgres(t)

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
