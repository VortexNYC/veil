package publicapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/VortexNYC/veil/identity/glue"
	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/jackc/pgx/v5"
)

// e2eOry fakes exactly the Ory boundary — Kratos subject verification, Keto
// owner/member tuples, the Kratos organization_id stamp. Everything below it
// (Postgres, grants, crypto, audit, the broker's outbound fetch) is real.
type e2eOry struct {
	mu     sync.Mutex
	subs   map[string]string
	owners map[string]map[string]bool
	stamps map[string]string
}

func (e *e2eOry) Subject(_ context.Context, tok string) (string, error) {
	if sub, ok := e.subs[tok]; ok {
		return sub, nil
	}
	return "", errors.New("ory: bad token")
}

func (e *e2eOry) ProvisionMember(_ context.Context, orgID, sub string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.owners[orgID] == nil {
		e.owners[orgID] = map[string]bool{}
	}
	e.owners[orgID][sub] = true
	return nil
}

func (e *e2eOry) SetIdentityOrg(_ context.Context, sub, orgID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stamps[sub] = orgID
	return nil
}

func (e *e2eOry) IsMember(_ context.Context, orgID, sub string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.owners[orgID][sub], nil
}

func (e *e2eOry) IsOwner(_ context.Context, orgID, sub string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.owners[orgID][sub], nil
}

// e2eServer stands up the real public API over a real Postgres schema. Bearer
// tokens map to subjects at the Ory boundary; everything else — org
// resolution, grants, crypto, audit — resolves through real store rows.
func e2eServer(t *testing.T, ory *e2eOry) (*httptest.Server, *app.App) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	ctx := context.Background()
	schema := "test_" + strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())
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
	kek, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_KEK", hex.EncodeToString(kek))
	a, err := app.OpenPostgres(u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.Human = ory
	a.Provision = ory
	a.Members = ory

	// The token→identity mapping is the faked boundary; the principal's org
	// resolves through real humans/agents rows exactly like PrincipalFromOIDC.
	identity := func(_ context.Context, tok string) (protocol.Principal, error) {
		switch {
		case strings.HasPrefix(tok, "h:"):
			sub := strings.TrimPrefix(tok, "h:")
			if mapped, ok := ory.subs[tok]; ok {
				sub = mapped
			}
			h, err := a.Store.Human(sub)
			if err != nil {
				return protocol.Principal{}, errors.New("unauthorized")
			}
			return protocol.Principal{Kind: protocol.PrincipalHuman, ID: h.ID, OrgID: h.OrgID}, nil
		case strings.HasPrefix(tok, "a:"):
			ag, err := a.Store.Agent(strings.TrimPrefix(tok, "a:"))
			if err != nil {
				return protocol.Principal{}, errors.New("unauthorized")
			}
			return protocol.Principal{Kind: protocol.PrincipalAgent, ID: ag.ID, OrgID: ag.OrgID}, nil
		}
		return protocol.Principal{}, errors.New("unauthorized")
	}
	mux := http.NewServeMux()
	(&Server{App: a, Identity: identity}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, a
}

func e2eProvision(t *testing.T, srv *httptest.Server, token string) (sub, org string) {
	t.Helper()
	code, body := doJSON(t, srv, http.MethodPost, "/v1/provision", token, nil)
	if code != http.StatusOK {
		t.Fatalf("provision %s: %d %s", token, code, body)
	}
	var out ProvisionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Subject == "" || out.OrgID == "" {
		t.Fatalf("provision returned %s", body)
	}
	return out.Subject, out.OrgID
}

// Full signup→secret round-trip over HTTP on real Postgres: two humans get
// two orgs, an agent uses a granted item against a live upstream that echoes
// the injected Authorization, cross-org access is denied at every layer, and
// no response body ever contains the secret.
func TestE2EProvisionGrantUsePostgres(t *testing.T) {
	ory := &e2eOry{
		subs:   map[string]string{"h:alice": "alice", "h:bob": "bob"},
		owners: map[string]map[string]bool{},
		stamps: map[string]string{},
	}
	srv, a := e2eServer(t, ory)
	const secretValue = "sk-live-e2e-9f8e7d6c"

	// A real upstream: echoes the Authorization header the broker injected,
	// and on ?leak embeds it in the body to prove response scrubbing.
	var sawAuth string
	var sawMu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawMu.Lock()
		sawAuth = r.Header.Get("Authorization")
		sawMu.Unlock()
		if r.URL.Query().Get("leak") == "1" {
			fmt.Fprintf(w, `{"ok":true,"dbg":%q}`, r.Header.Get("Authorization"))
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	// Signup: two humans, two orgs; re-provisioning converges.
	_, orgA := e2eProvision(t, srv, "h:alice")
	_, orgA2 := e2eProvision(t, srv, "h:alice")
	if orgA != orgA2 {
		t.Fatalf("re-provision diverged: %q vs %q", orgA, orgA2)
	}
	_, orgB := e2eProvision(t, srv, "h:bob")
	if orgA == orgB {
		t.Fatal("two humans got the same org")
	}
	// Kratos stamp + Keto tuple landed for both.
	if ory.stamps["alice"] != orgA || ory.stamps["bob"] != orgB {
		t.Fatalf("identity stamps: %v", ory.stamps)
	}

	// Alice creates the item; Bob cannot take the name — the org-scoped
	// upsert gate rejects it atomically.
	code, body := doJSON(t, srv, http.MethodPost, "/v1/items", "h:alice",
		CreateItemRequest{Name: "github", Secret: secretValue, URI: upstream.URL})
	if code != http.StatusOK {
		t.Fatalf("create item: %d %s", code, body)
	}
	if code, body := doJSON(t, srv, http.MethodPost, "/v1/items", "h:bob",
		CreateItemRequest{Name: "github", Secret: "stolen"}); code == http.StatusOK {
		t.Fatalf("cross-org item takeover succeeded: %s", body)
	}
	code, body = doJSON(t, srv, http.MethodPost, "/v1/items", "h:bob",
		CreateItemRequest{Name: "bob-item", Secret: "bob-secret"})
	if code != http.StatusOK {
		t.Fatalf("bob create: %d %s", code, body)
	}

	// Listings carry no secret material.
	code, body = doJSON(t, srv, http.MethodGet, "/v1/items", "h:alice", nil)
	if code != http.StatusOK || !strings.Contains(string(body), "github") {
		t.Fatalf("list items: %d %s", code, body)
	}
	if strings.Contains(string(body), secretValue) {
		t.Fatal("secret leaked in /v1/items")
	}
	code, body = doJSON(t, srv, http.MethodGet, "/v1/items", "h:bob", nil)
	if code != http.StatusOK || strings.Contains(string(body), `"github"`) {
		t.Fatalf("bob sees alice's item: %d %s", code, body)
	}

	// Agents and grants are owner-administered inside the item's org.
	code, body = doJSON(t, srv, http.MethodPost, "/v1/agents", "h:alice", CreateAgentRequest{Name: "bot"})
	if code != http.StatusOK {
		t.Fatalf("create agent: %d %s", code, body)
	}
	code, body = doJSON(t, srv, http.MethodPost, "/v1/agents", "h:bob", CreateAgentRequest{Name: "bbot"})
	if code != http.StatusOK {
		t.Fatalf("bob agent: %d %s", code, body)
	}
	// Bob cannot grant Alice's item, and cannot grant it to his own agent.
	if code, body := doJSON(t, srv, http.MethodPost, "/v1/grants", "h:bob",
		CreateGrantRequest{Agent: "bbot", Item: "github", Level: string(protocol.Level2)}); code == http.StatusOK {
		t.Fatalf("cross-org grant succeeded: %s", body)
	}
	// Bob cannot even reach Alice's agent.
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/grants", "h:bob",
		CreateGrantRequest{Agent: "bot", Item: "bob-item", Level: string(protocol.Level2)}); code == http.StatusOK {
		t.Fatal("bob granted alice's agent")
	}
	code, body = doJSON(t, srv, http.MethodPost, "/v1/grants", "h:alice",
		CreateGrantRequest{Agent: "bot", Item: "github", Level: string(protocol.Level2)})
	if code != http.StatusOK {
		t.Fatalf("grant: %d %s", code, body)
	}
	// Bob's grant list must not contain Alice's grant.
	code, body = doJSON(t, srv, http.MethodGet, "/v1/grants", "h:bob", nil)
	if code != http.StatusOK || strings.Contains(string(body), "github") {
		t.Fatalf("bob sees cross-org grant: %d %s", code, body)
	}

	// The use: bot fetches through the origin; the upstream must see the
	// injected bearer and the response must not carry the secret back.
	code, body = doJSON(t, srv, http.MethodPost, "/v1/use", "a:bot",
		UseRequest{Item: "github", URL: upstream.URL + "/user"})
	if code != http.StatusOK {
		t.Fatalf("use: %d %s", code, body)
	}
	var use UseResponse
	if err := json.Unmarshal(body, &use); err != nil {
		t.Fatal(err)
	}
	if use.Decision != protocol.DecisionAllow {
		t.Fatalf("use denied: %s", body)
	}
	sawMu.Lock()
	gotAuth := sawAuth
	sawMu.Unlock()
	if gotAuth != "Bearer "+secretValue {
		t.Fatalf("upstream auth %q", gotAuth)
	}
	if strings.Contains(string(body), secretValue) {
		t.Fatal("secret leaked in /v1/use response")
	}

	// Upstream echoing the secret must be scrubbed out of the response body.
	code, body = doJSON(t, srv, http.MethodPost, "/v1/use", "a:bot",
		UseRequest{Item: "github", URL: upstream.URL + "/user?leak=1"})
	if code != http.StatusOK {
		t.Fatalf("use leak: %d %s", code, body)
	}
	if strings.Contains(string(body), secretValue) {
		t.Fatal("upstream-echoed secret survived scrubbing")
	}

	// Denials: Bob's agent has no grant on github; an agent cannot reach
	// another org's item even when granted nothing.
	code, body = doJSON(t, srv, http.MethodPost, "/v1/use", "a:bbot",
		UseRequest{Item: "github", URL: upstream.URL + "/user"})
	if code != http.StatusOK {
		t.Fatalf("bbot use: %d %s", code, body)
	}
	if err := json.Unmarshal(body, &use); err != nil {
		t.Fatal(err)
	}
	if use.Decision == protocol.DecisionAllow {
		t.Fatal("bbot used alice's item")
	}

	// Sessions consume atomically: one use at max_uses=1, then denied.
	code, body = doJSON(t, srv, http.MethodPost, "/v1/sessions", "h:alice",
		CreateSessionRequest{Agent: "bot", MaxUses: 1})
	if code != http.StatusOK {
		t.Fatalf("session: %d %s", code, body)
	}
	var sess CreateSessionResponse
	if err := json.Unmarshal(body, &sess); err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, srv, http.MethodPost, "/v1/use", sess.Token,
		UseRequest{Item: "github", URL: upstream.URL + "/user"})
	if code != http.StatusOK {
		t.Fatalf("session use: %d %s", code, body)
	}
	if err := json.Unmarshal(body, &use); err != nil {
		t.Fatal(err)
	}
	if use.Decision != protocol.DecisionAllow {
		t.Fatalf("session denied: %s", body)
	}
	code, body = doJSON(t, srv, http.MethodPost, "/v1/use", sess.Token,
		UseRequest{Item: "github", URL: upstream.URL + "/user"})
	if code == http.StatusOK {
		if err := json.Unmarshal(body, &use); err != nil {
			t.Fatal(err)
		}
		if use.Decision == protocol.DecisionAllow {
			t.Fatal("consumed session allowed a second use")
		}
	} else if code != http.StatusUnauthorized {
		t.Fatalf("session reuse: %d %s", code, body)
	}

	// Audit is durable: alice's owner-scoped event list contains the allow.
	code, body = doJSON(t, srv, http.MethodGet, "/v1/events", "h:alice", nil)
	if code != http.StatusOK {
		t.Fatalf("events: %d %s", code, body)
	}
	if !strings.Contains(string(body), `"allow"`) {
		t.Fatalf("no allow event: %s", body)
	}
	if strings.Contains(string(body), secretValue) {
		t.Fatal("secret leaked in /v1/events")
	}

	// Revoked agent is denied even with a live grant.
	code, body = doJSON(t, srv, http.MethodPost, "/v1/agents/bot/revoke", "h:alice", nil)
	if code != http.StatusOK {
		t.Fatalf("revoke: %d %s", code, body)
	}
	code, body = doJSON(t, srv, http.MethodPost, "/v1/use", "a:bot",
		UseRequest{Item: "github", URL: upstream.URL + "/user"})
	if err := json.Unmarshal(body, &use); err != nil {
		t.Fatal(err)
	}
	if use.Decision == protocol.DecisionAllow {
		t.Fatal("revoked agent allowed")
	}
	_ = a
}

// True signup against the live Ory stack: real Kratos identities, real Keto
// tuples, real Kratos organization_id stamp. Only the OIDC token→subject hop
// is faked — a real Hydra-issued JWT needs the browser consent flow, which is
// the one piece not automatable here. Everything the broker writes is real.
func TestE2EProvisionLiveOry(t *testing.T) {
	ctx := context.Background()
	if _, err := http.Get("http://127.0.0.1:4434/admin/identities?pagesize=1"); err != nil {
		t.Skip("local Ory stack not running (identity/compose.yml)")
	}
	g, err := glue.New(glue.Config{
		KratosPublic: "http://127.0.0.1:4433",
		KratosAdmin:  "http://127.0.0.1:4434",
		HydraAdmin:   "http://127.0.0.1:4445",
		KetoRead:     "http://127.0.0.1:4466",
		KetoWrite:    "http://127.0.0.1:4467",
		OrgID:        protocol.LocalOrgID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Real Kratos identities — signup's directory step.
	newIdentity := func(email string) string {
		body := fmt.Sprintf(`{"schema_id":"default","traits":{"email":%q}}`, email)
		resp, err := http.Post("http://127.0.0.1:4434/admin/identities", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ID == "" {
			t.Fatalf("create identity %s: %v", email, err)
		}
		return out.ID
	}
	carolID := newIdentity("carol@e2e.test")
	daveID := newIdentity("dave@e2e.test")
	t.Cleanup(func() {
		for _, id := range []string{carolID, daveID} {
			req, _ := http.NewRequest(http.MethodDelete, "http://127.0.0.1:4434/admin/identities/"+id, nil)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	})

	// The verifier fakes only token→subject; Provision/Members are real glue.
	ory := &e2eOry{subs: map[string]string{"h:carol": carolID, "h:dave": daveID}}
	srv, a := e2eServer(t, ory)
	a.Provision = g
	a.Members = g

	_, orgC := e2eProvision(t, srv, "h:carol")
	_, orgD := e2eProvision(t, srv, "h:dave")
	if orgC == orgD {
		t.Fatal("two signups got the same org")
	}

	// Verify against the real Ory plane, not the app's word.
	if ok, err := g.IsOwner(ctx, orgC, carolID); err != nil || !ok {
		t.Fatalf("keto: carol not owner of %s: %v", orgC, err)
	}
	if ok, err := g.IsMember(ctx, orgD, daveID); err != nil || !ok {
		t.Fatalf("keto: dave not member of %s: %v", orgD, err)
	}
	if ok, err := g.IsMember(ctx, orgC, daveID); err != nil || ok {
		t.Fatal("keto: dave is a member of carol's org")
	}
	if got, err := g.IdentityOrg(ctx, carolID); err != nil || got != orgC {
		t.Fatalf("kratos: carol org %q want %q: %v", got, orgC, err)
	}

	// Owner checks now answer through real Keto: carol can administer her
	// org's vault, dave cannot touch it.
	code, body := doJSON(t, srv, http.MethodPost, "/v1/agents", "h:carol", CreateAgentRequest{Name: "cbot"})
	if code != http.StatusOK {
		t.Fatalf("carol agent: %d %s", code, body)
	}
	if code, body := doJSON(t, srv, http.MethodPost, "/v1/agents", "h:dave", CreateAgentRequest{Name: "cbot2"}); code != http.StatusOK {
		t.Fatalf("dave agent: %d %s", code, body)
	}
	if code, body := doJSON(t, srv, http.MethodPost, "/v1/agents", "h:dave", CreateAgentRequest{Name: "cbot"}); code == http.StatusOK {
		t.Fatalf("dave took carol's agent name: %s", body)
	}
}
