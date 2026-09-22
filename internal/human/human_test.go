package human

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/VortexNYC/veil/internal/protocol"
)

type testIssuer struct {
	URL    string
	mux    *http.ServeMux
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.URL,
			"jwks_uri":                              iss.URL + "/keys",
			"authorization_endpoint":                iss.URL + "/auth",
			"token_endpoint":                        iss.URL + "/token",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
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
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "missing pkce", http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_secret") != "" {
			http.Error(w, "public client", http.StatusBadRequest)
			return
		}
		id := iss.token(t, "id-human-1", DefaultAudience, time.Now().Add(time.Hour))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "not-an-id-token",
			"token_type":   "bearer",
			"id_token":     id,
		})
	})
	iss.mux = mux
	iss.server = httptest.NewServer(mux)
	iss.URL = iss.server.URL
	t.Cleanup(iss.server.Close)
	return iss
}

func (i *testIssuer) token(t *testing.T, sub, aud string, exp time.Time) string {
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
	}).Claims(map[string]any{"amr": []string{"password", "totp"}}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHydraTokenIsTheHuman(t *testing.T) {
	iss := newTestIssuer(t)
	v, err := New(Config{Issuer: iss.URL, Audience: DefaultAudience})
	if err != nil {
		t.Fatal(err)
	}
	raw := iss.token(t, "id-human-1", DefaultAudience, time.Now().Add(time.Hour))
	h, err := v.Human(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != protocol.PrincipalHuman || h.ID != "id-human-1" || h.OrgID != "" {
		t.Fatalf("%+v", h)
	}
}

func TestUnknownIssuerRejected(t *testing.T) {
	iss := newTestIssuer(t)
	v, err := New(Config{Issuer: iss.URL})
	if err != nil {
		t.Fatal(err)
	}
	other := newTestIssuer(t)
	raw := other.token(t, "id-human-1", DefaultAudience, time.Now().Add(time.Hour))
	if _, err := v.Human(context.Background(), raw); err == nil {
		t.Fatal("trusted a foreign issuer")
	}
}

func TestWrongAudienceRejected(t *testing.T) {
	iss := newTestIssuer(t)
	v, err := New(Config{Issuer: iss.URL, Audience: DefaultAudience})
	if err != nil {
		t.Fatal(err)
	}
	raw := iss.token(t, "id-human-1", "someone-else", time.Now().Add(time.Hour))
	if _, err := v.Human(context.Background(), raw); err == nil {
		t.Fatal("accepted the wrong audience")
	}
}

func TestAuthCodeURLForcesLogin(t *testing.T) {
	iss := newTestIssuer(t)
	v, err := New(Config{Issuer: iss.URL, RedirectURL: DefaultRedirect})
	if err != nil {
		t.Fatal(err)
	}
	u, err := v.AuthCodeURL(context.Background(), "state-1", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "prompt=login") || !strings.Contains(u, "max_age=0") {
		t.Fatalf("remint can skip TOTP: %s", u)
	}
}

func TestExchangeReturnsIDTokenNotAccess(t *testing.T) {
	iss := newTestIssuer(t)
	v, err := New(Config{Issuer: iss.URL, RedirectURL: DefaultRedirect})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v.Exchange(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if raw == "not-an-id-token" {
		t.Fatal("returned the access token")
	}
	h, err := v.Human(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "id-human-1" {
		t.Fatalf("%+v", h)
	}
}

func TestRequireTOTP(t *testing.T) {
	b64 := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	with := "e30." + b64(`{"amr":["password","totp"]}`) + ".x"
	without := "e30." + b64(`{"amr":["password"]}`) + ".x"
	if err := RequireTOTP(with); err != nil {
		t.Fatal(err)
	}
	if err := RequireTOTP(without); err == nil {
		t.Fatal("accepted aal1")
	}
	if err := RequireTOTP("jwt-human-id-token"); err != nil {
		t.Fatal("opaque test token")
	}
}
