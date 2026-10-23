package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/VortexNYC/veil/internal/protocol"
)

// credTestIssuer fakes both halves of Hydra: OIDC discovery + JWKS for the
// workload verifier, and /oauth2/token for the client_credentials exchange.
type credTestIssuer struct {
	URL       string
	key       *rsa.PrivateKey
	server    *httptest.Server
	tokenHits atomic.Int64
	lastForm  atomic.Value
	failToken atomic.Bool
}

func newCredTestIssuer(t *testing.T) *credTestIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	iss := &credTestIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.URL,
			"jwks_uri":                              iss.URL + "/keys",
			"token_endpoint":                        iss.URL + "/oauth2/token",
			"response_types_supported":              []string{"id_token"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &key.PublicKey,
			KeyID:     "test",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}})
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		iss.tokenHits.Add(1)
		if iss.failToken.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		iss.lastForm.Store(r.Form)
		if r.Form.Get("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unsupported_grant_type"})
			return
		}
		cid, secret, ok := r.BasicAuth()
		if !ok {
			cid, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
		}
		if cid != "agent-grok" || secret != "topsecret" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		raw := iss.token(t, cid, r.Form.Get("audience"), time.Now().Add(time.Hour))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": raw,
			"token_type":   "bearer",
			"expires_in":   3600,
		})
	})
	iss.server = httptest.NewServer(mux)
	iss.URL = iss.server.URL
	t.Cleanup(iss.server.Close)
	return iss
}

func (i *credTestIssuer) token(t *testing.T, sub, aud string, exp time.Time) string {
	t.Helper()
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: i.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(sig).Claims(jwt.Claims{
		Issuer:   i.URL,
		Subject:  sub,
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(exp),
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newCredTestApp(t *testing.T, issuer string) *App {
	t.Helper()
	t.Setenv("VEIL_HYDRA_ISSUER", issuer)
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestCredentialBearerResolvesBoundAgent(t *testing.T) {
	iss := newCredTestIssuer(t)
	a := newCredTestApp(t, iss.URL)
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "grok", OrgID: a.OrgID}
	if err := a.Store.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.PutWorkload(protocol.Workload{
		AgentID:  "grok",
		OrgID:    a.OrgID,
		Issuer:   iss.URL,
		Subject:  "agent-grok",
		Audience: "veil",
	}); err != nil {
		t.Fatal(err)
	}

	p, err := a.AgentFromOIDC(context.Background(), "agent-grok:topsecret")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "grok" || p.Kind != protocol.PrincipalAgent {
		t.Fatalf("%+v", p)
	}
	if iss.tokenHits.Load() != 1 {
		t.Fatalf("token hits %d", iss.tokenHits.Load())
	}
	form, _ := iss.lastForm.Load().(url.Values)
	if form.Get("audience") != "veil" {
		t.Fatalf("audience %q", form.Get("audience"))
	}

	// Second call: cached token source, no new exchange.
	if _, err := a.AgentFromOIDC(context.Background(), "agent-grok:topsecret"); err != nil {
		t.Fatal(err)
	}
	if iss.tokenHits.Load() != 1 {
		t.Fatalf("token hits %d, expected cache reuse", iss.tokenHits.Load())
	}
}

func TestCredentialBearerRejectsBadSecret(t *testing.T) {
	iss := newCredTestIssuer(t)
	a := newCredTestApp(t, iss.URL)
	if err := a.Store.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "grok", OrgID: a.OrgID}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.PutWorkload(protocol.Workload{AgentID: "grok", OrgID: a.OrgID, Issuer: iss.URL, Subject: "agent-grok", Audience: "veil"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AgentFromOIDC(context.Background(), "agent-grok:wrong"); err == nil {
		t.Fatal("bad secret accepted")
	}
	// x/oauth2 retries once with the alternate client-auth style.
	if hits := iss.tokenHits.Load(); hits < 1 || hits > 2 {
		t.Fatalf("expected a rejected exchange, hits=%d", hits)
	}
}

func TestCredentialBearerUnboundNeverExchanges(t *testing.T) {
	iss := newCredTestIssuer(t)
	a := newCredTestApp(t, iss.URL)
	if _, err := a.AgentFromOIDC(context.Background(), "agent-ghost:topsecret"); err == nil {
		t.Fatal("unbound client id accepted")
	}
	if iss.tokenHits.Load() != 0 {
		t.Fatalf("exchange attempted for unbound client, hits=%d", iss.tokenHits.Load())
	}
}

func TestCredentialBearerRequiresIssuer(t *testing.T) {
	iss := newCredTestIssuer(t)
	t.Setenv("VEIL_HYDRA_ISSUER", "")
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.AgentFromOIDC(context.Background(), "agent-grok:topsecret"); err == nil {
		t.Fatal("credential bearer accepted with no issuer configured")
	}
	_ = iss
}

func TestCredentialBearerShape(t *testing.T) {
	cases := []struct {
		raw string
		ok  bool
	}{
		{"agent-grok:secret123", true},
		{"agent-cursor:secret:with:colons", true},
		{"vs1_" + strings.Repeat("a", 64), false}, // session token
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.sig", false},
		{"agent-:secret", false},
		{"agent-UPPER:secret", false},
		{"notagent-grok:secret", false},
		{"agent-grok:", false},
		{":secret", false},
		{"", false},
	}
	for _, c := range cases {
		cid, _, ok := splitCredentialBearer(c.raw)
		if ok != c.ok {
			t.Fatalf("%q: ok=%v want %v", c.raw, ok, c.ok)
		}
		if ok && !strings.HasPrefix(cid, agentClientIDPrefix) {
			t.Fatalf("%q: bad cid %q", c.raw, cid)
		}
	}
}
