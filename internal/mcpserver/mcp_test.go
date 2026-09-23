package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

const secret = "sk_live_MCP_SECRET"

func TestMCPFetchDoesNotReturnSecret(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("Authorization"))
	}))
	t.Cleanup(upstream.Close)
	const login = "stripe@example.com"
	if _, err := a.PutItem(app.ItemOpts{Name: "stripe", URI: upstream.URL, Token: []byte(secret), Login: login}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", "level2"); err != nil {
		t.Fatal(err)
	}

	out, err := Fetch(context.Background(), a, "claude", FetchIn{Item: "stripe", URL: upstream.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "allow" {
		t.Fatalf("%+v", out)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatalf("mcp output leaked secret: %s", raw)
	}

	items, err := a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	list, err := json.Marshal(ListOut{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(list, []byte(secret)) {
		t.Fatal("list_items leaked secret")
	}
	if !scrub.Contains(list, []byte(login)) {
		t.Fatal("list_items omitted login")
	}
}

func TestMCPListNeverHasPANOrCVV(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	const pan = "4111111111111111"
	const cvv = "999"
	blob, err := material.PackCard(pan, "01", "2031", cvv, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItem(app.ItemOpts{Name: "amex", Kind: protocol.ItemCard, Token: blob}); err != nil {
		t.Fatal(err)
	}
	items, err := a.ItemsForPrincipal(protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ListOut{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(pan)) || scrub.Contains(raw, []byte(cvv)) {
		t.Fatalf("list leaked card %s", raw)
	}
}

func TestCodingAgentsFetchOverRemoteMCP(t *testing.T) {
	agents := []string{"cursor", "devin", "pi", "opencode"}
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	iss := newTestIssuer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("Authorization"))
	}))
	t.Cleanup(upstream.Close)
	if _, err := a.AddItem("stripe", upstream.URL, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddItem("pi-mail", upstream.URL+"/mail", []byte(secret)); err != nil {
		t.Fatal(err)
	}
	for _, name := range agents {
		if _, err := a.AddAgent(name); err != nil {
			t.Fatal(err)
		}
		if _, err := a.AddGrant(name, "stripe", "level2"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.BindWorkload(name, iss.URL, "agent-"+name, "veil"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.AddGrant("pi", "pi-mail", "level2"); err != nil {
		t.Fatal(err)
	}

	public := "http://veil.test/mcp"
	ts := httptest.NewServer(Mux(a, public, iss.URL))
	t.Cleanup(ts.Close)
	endpoint := ts.URL + Path

	meta, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Body.Close()
	if meta.StatusCode != http.StatusOK {
		t.Fatalf("metadata %d", meta.StatusCode)
	}
	body, err := io.ReadAll(meta.Body)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(body, []byte(secret)) {
		t.Fatal("metadata leaked secret")
	}
	if !bytes.Contains(body, []byte(iss.URL)) {
		t.Fatalf("metadata missing issuer: %s", body)
	}

	unauth := httptest.NewRequest(http.MethodPost, endpoint, nil)
	rec := httptest.NewRecorder()
	Mux(a, public, iss.URL).ServeHTTP(rec, unauth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: %d", rec.Code)
	}

	health, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health %d", health.StatusCode)
	}
	healthBody, err := io.ReadAll(health.Body)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(healthBody, []byte(secret)) {
		t.Fatal("health leaked secret")
	}

	ready, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Body.Close()
	if ready.StatusCode != http.StatusOK {
		t.Fatalf("ready %d", ready.StatusCode)
	}

	specRes, err := http.Get(ts.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer specRes.Body.Close()
	if specRes.StatusCode != http.StatusOK {
		t.Fatalf("openapi %d", specRes.StatusCode)
	}
	specBody, err := io.ReadAll(specRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(specBody, []byte(secret)) {
		t.Fatal("openapi leaked secret")
	}
	if !bytes.Contains(specBody, []byte(`"useItem"`)) {
		t.Fatalf("spec missing useItem: %s", specBody)
	}

	ctx := context.Background()
	for _, name := range agents {
		tok := iss.token(t, "agent-"+name, "veil")
		cs := connect(t, ctx, endpoint, tok)
		list, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_items"})
		if err != nil {
			t.Fatal(err)
		}
		if list.IsError {
			t.Fatalf("%s list_items error: %+v", name, list)
		}
		listRaw, err := json.Marshal(list)
		if err != nil {
			t.Fatal(err)
		}
		if scrub.Contains(listRaw, []byte(secret)) {
			t.Fatalf("%s list_items leaked secret", name)
		}
		if !bytes.Contains(listRaw, []byte("stripe")) {
			t.Fatalf("%s missing stripe: %s", name, listRaw)
		}
		if name != "pi" && bytes.Contains(listRaw, []byte("pi-mail")) {
			t.Fatalf("%s saw pi-only item: %s", name, listRaw)
		}
		if name == "pi" && !bytes.Contains(listRaw, []byte("pi-mail")) {
			t.Fatalf("pi missing pi-mail: %s", listRaw)
		}

		fetch, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "fetch",
			Arguments: FetchIn{Item: "stripe", URL: upstream.URL + "/v1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if fetch.IsError {
			t.Fatalf("%s fetch error: %+v", name, fetch)
		}
		fetchRaw, err := json.Marshal(fetch)
		if err != nil {
			t.Fatal(err)
		}
		if scrub.Contains(fetchRaw, []byte(secret)) {
			t.Fatalf("%s fetch leaked secret: %s", name, fetchRaw)
		}
		_ = cs.Close()
	}
}

func TestRESTUsePOSTBodyDoesNotReturnSecret(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	iss := newTestIssuer(t)
	var sawBody, sawType string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		sawBody = string(raw)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)
	if _, err := a.AddItem("stripe", upstream.URL, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", "level2"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindWorkload("claude", iss.URL, "agent-claude", "veil"); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Mux(a, "http://veil.test/mcp", iss.URL))
	t.Cleanup(ts.Close)
	tok := iss.token(t, "agent-claude", "veil")
	reqBody := `{"item":"stripe","url":"` + upstream.URL + `/v1","method":"POST","headers":{"Content-Type":"application/json"},"body":"{\"email\":\"a@b.c\"}"}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/use", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, out)
	}
	if scrub.Contains(out, []byte(secret)) {
		t.Fatalf("rest leaked secret: %s", out)
	}
	if sawType != "application/json" || sawBody != `{"email":"a@b.c"}` {
		t.Fatalf("upstream type=%q body=%q", sawType, sawBody)
	}
	list, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	list.Header.Set("Authorization", "Bearer "+tok)
	listRes, err := http.DefaultClient.Do(list)
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	listOut, err := io.ReadAll(listRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(listOut, []byte(secret)) {
		t.Fatal("list leaked")
	}
	if !bytes.Contains(listOut, []byte("stripe")) {
		t.Fatalf("%s", listOut)
	}

	evReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	evReq.Header.Set("Authorization", "Bearer "+tok)
	evRes, err := http.DefaultClient.Do(evReq)
	if err != nil {
		t.Fatal(err)
	}
	defer evRes.Body.Close()
	evOut, err := io.ReadAll(evRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if evRes.StatusCode != http.StatusOK {
		t.Fatalf("events %d %s", evRes.StatusCode, evOut)
	}
	if scrub.Contains(evOut, []byte(secret)) {
		t.Fatal("events leaked secret")
	}
	if !bytes.Contains(evOut, []byte(`"agent_id":"claude"`)) {
		t.Fatalf("events missing agent: %s", evOut)
	}
	if !bytes.Contains(evOut, []byte(`"decision":"allow"`)) {
		t.Fatalf("events missing decision: %s", evOut)
	}
	noAuth, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer noAuth.Body.Close()
	if noAuth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("events no bearer %d", noAuth.StatusCode)
	}
}

func TestReadyFailsWhenIssuerDown(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ts := httptest.NewServer(Mux(a, "http://veil.test/mcp", "http://127.0.0.1:1"))
	t.Cleanup(ts.Close)
	health, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health is liveness, got %d", health.StatusCode)
	}
	ready, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Body.Close()
	if ready.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ready %d", ready.StatusCode)
	}
}

// The 5xx counter is the monitor's bleed signal: an origin can be "ready"
// while a route fails every call — the window count must show up in the
// /ready body.
func TestReadyReportsErrorWindow(t *testing.T) {
	e := &errWindow{}
	for i := 0; i < 3; i++ {
		e.add()
	}
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// Issuer up but store-less: /ready passes issuer discovery and has no
	// Ping to fail, so the body is the assertion surface.
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(issuer.Close)
	ts := httptest.NewServer(e.wrap(ready(a, issuer.URL, e)))
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("ready %d", res.StatusCode)
	}
	if !strings.Contains(string(b), "errors_5m=3") {
		t.Fatalf("body %q missing error window", string(b))
	}
}

func connect(t *testing.T, ctx context.Context, endpoint, tok string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		DisableStandaloneSSE: true,
		HTTPClient:           &http.Client{Transport: bearerTransport{tok: tok}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

type bearerTransport struct {
	tok string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

type testIssuer struct {
	URL    string
	key    *rsa.PrivateKey
	server *httptest.Server
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	iss := &testIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Issuer                           string   `json:"issuer"`
			JWKSURI                          string   `json:"jwks_uri"`
			AuthorizationEndpoint            string   `json:"authorization_endpoint"`
			ResponseTypesSupported           []string `json:"response_types_supported"`
			SubjectTypesSupported            []string `json:"subject_types_supported"`
			IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
		}{
			Issuer:                           iss.URL,
			JWKSURI:                          iss.URL + "/keys",
			AuthorizationEndpoint:            iss.URL + "/auth",
			ResponseTypesSupported:           []string{"id_token"},
			SubjectTypesSupported:            []string{"public"},
			IDTokenSigningAlgValuesSupported: []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &key.PublicKey,
			KeyID:     "test",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}}
		_ = json.NewEncoder(w).Encode(set)
	})
	iss.server = httptest.NewServer(mux)
	iss.URL = iss.server.URL
	t.Cleanup(iss.server.Close)
	return iss
}

func (i *testIssuer) token(t *testing.T, sub, aud string) string {
	t.Helper()
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: i.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(sig).Claims(jwt.Claims{
		Issuer:   i.URL,
		Subject:  sub,
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMCPRevokeAgent(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("Authorization"))
	}))
	t.Cleanup(upstream.Close)
	if _, err := a.AddItem("stripe", upstream.URL, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", protocol.Level2); err != nil {
		t.Fatal(err)
	}

	out, err := Fetch(context.Background(), a, "claude", FetchIn{Item: "stripe", URL: upstream.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "allow" {
		t.Fatalf("before: %+v", out)
	}

	owner := protocol.Principal{Kind: protocol.PrincipalHuman, ID: app.DefaultHuman, OrgID: a.OrgID}
	if err := a.RevokeAgent(owner, "claude"); err != nil {
		t.Fatal(err)
	}

	out, err = Fetch(context.Background(), a, "claude", FetchIn{Item: "stripe", URL: upstream.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "deny" || out.Reason != "agent_revoked" {
		t.Fatalf("after fetch: %+v", out)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatalf("mcp denied output leaked secret: %s", raw)
	}

	items, err := a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("mcp list after revoke: %+v", items)
	}
}
