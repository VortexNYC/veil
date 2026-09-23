//go:build live

package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/testutil"
	"github.com/jackc/pgx/v5"
)

// The signup boundary with the synthetic JWT removed: a real Hydra-minted
// id_token flows through the production verifier (env-wired by
// OpenPostgres/attachHydra, as in prod) into ProvisionHuman, then the
// provisioned human runs item → agent → grant → use on real Postgres.
// Kratos/Keto stay faked — they are the identity-plane seam proven
// separately by prove-identity; what this kills is the fake JWT.
//
// Needs the dev Hydra from internal/human/hydra_live_test.go plus
// PG_TEST_DSN. Run:
//
//	PG_TEST_DSN=... HYDRA_TEST_PUBLIC=... HYDRA_TEST_ADMIN=... \
//	  go test -tags live ./internal/app -run HydraLive -v
func TestHydraLiveProvisionChain(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	pub := strings.TrimRight(os.Getenv("HYDRA_TEST_PUBLIC"), "/")
	adm := strings.TrimRight(os.Getenv("HYDRA_TEST_ADMIN"), "/")
	if dsn == "" || pub == "" || adm == "" {
		t.Skip("PG_TEST_DSN / HYDRA_TEST_PUBLIC / HYDRA_TEST_ADMIN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	schema := "test_app_hydra"
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE; CREATE SCHEMA %s`, schema, schema)); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)

	const clientID = "test-human"
	t.Setenv("VEIL_KEK", strings.Repeat("ab", 32))
	t.Setenv("VEIL_HYDRA_ISSUER", pub)
	t.Setenv("VEIL_HYDRA_CLIENT_ID", clientID)
	searchDSN := strings.TrimSpace(dsn)
	if u, perr := url.Parse(searchDSN); perr == nil && u.Scheme != "" {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		searchDSN = u.String()
	} else {
		searchDSN += " search_path=" + schema
	}
	a, err := OpenPostgres(searchDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Human == nil {
		t.Fatal("attachHydra did not wire the real verifier")
	}
	prov := &fakeProvision{}
	a.Provision = prov
	a.Members = orgMembers{}

	// Mint a real id_token: production AuthCodeURL → Hydra → Exchange.
	v, ok := a.Human.(*human.Verifier)
	if !ok {
		t.Fatal("a.Human is not the real *human.Verifier")
	}
	if err := testutil.HydraEnsureClient(ctx, adm, clientID, v.Redirect()); err != nil {
		t.Fatal(err)
	}
	pkce, state, err := human.PKCE()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := v.AuthCodeURL(ctx, state, pkce)
	if err != nil {
		t.Fatal(err)
	}
	code, err := testutil.HydraAuthorize(ctx, adm, authURL, "alice@corp.example")
	if err != nil {
		t.Fatal(err)
	}
	idToken, err := v.Exchange(ctx, code, pkce)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	// Signup with the real token, then the same chain the fake-verifier
	// test proves — but every identity decision is now cryptographic.
	p, err := a.ProvisionHuman(ctx, idToken)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if p.ID != "alice@corp.example" || p.OrgID == "" || p.OrgID == a.OrgID {
		t.Fatalf("principal %+v", p)
	}
	if len(prov.tuples) != 1 {
		t.Fatalf("keto tuples %+v", prov.tuples)
	}
	a.Members = orgMembers{p.OrgID + "|" + p.ID: true}

	item, err := a.PutItemFor(p, ItemOpts{
		Name: "stripe", URI: "https://api.stripe.com", Token: []byte("sk-live-test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := a.AddAgentFor(p, "bot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantUntil(p, agent.ID, item.ID, protocol.Level2, nil); err != nil {
		t.Fatal(err)
	}
	res, err := a.Use(ctx, agent.ID, item.ID, "GET", "https://api.stripe.com/v1/charges")
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if res.Decision != protocol.DecisionAllow {
		t.Fatalf("use decision %q (%s)", res.Decision, res.Reason)
	}

	// A forged token must not provision.
	if _, err := a.ProvisionHuman(ctx, idToken[:len(idToken)-2]+"xx"); err == nil {
		t.Fatal("tampered token provisioned")
	}
}
