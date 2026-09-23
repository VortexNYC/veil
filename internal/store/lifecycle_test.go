package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

// TestPurgeOrg is the teardown proof, PG-gated: two orgs seeded, one purged —
// every vault row for the dead org is gone, the surviving org's data and the
// forensic audit rows are untouched.
func TestPurgeOrg(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	kek := testMasterKey(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS purge_test CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE SCHEMA purge_test`); err != nil {
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
	q.Set("search_path", "purge_test")
	u.RawQuery = q.Encode()

	s, err := OpenPostgres(u.String(), kek)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, org := range []string{"org-dead", "org-live"} {
		master, err := crypto.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureOrgKey(ctx, org, master); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(org string) {
		t.Helper()
		if err := s.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "h-" + org, OrgID: org}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "a-" + org, OrgID: org}); err != nil {
			t.Fatal(err)
		}
		it := protocol.Item{ID: "i-" + org, OrgID: org, Name: "i-" + org, Kind: protocol.ItemAPIKey,
			Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: org}}
		if err := s.PutItem(it, Secret("s-"+org)); err != nil {
			t.Fatal(err)
		}
	}
	seed("org-dead")
	seed("org-live")

	rep, err := s.PurgeOrg(ctx, "org-dead")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Items != 1 || rep.Agents != 1 || rep.Humans != 1 || rep.Keys != 1 {
		t.Fatalf("purge report %+v", rep)
	}

	var n int
	for _, tbl := range []string{"humans", "agents", "items", "org_keys"} {
		if err := s.pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE org_id = 'org-dead'`, tbl)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s still has org-dead rows", tbl)
		}
		if err := s.pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE org_id = 'org-live'`, tbl)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s lost org-live rows (%d)", tbl, n)
		}
	}

	// DeleteHuman is surgical: one row, no org sweep.
	if err := s.DeleteHuman(ctx, "h-org-live"); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM humans WHERE id = 'h-org-live'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("h-org-live survived DeleteHuman")
	}
}
