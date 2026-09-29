package fill

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// loginIssuer is a fake Hydra: discovery, JWKS, and a token endpoint that
// mints an aal2 ID token for any code+verifier.
type loginIssuer struct {
	URL    string
	key    *rsa.PrivateKey
	server *httptest.Server
}

func newLoginIssuer(t *testing.T) *loginIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	iss := &loginIssuer{key: key}
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
		sig, serr := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: iss.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
		if serr != nil {
			http.Error(w, serr.Error(), http.StatusInternalServerError)
			return
		}
		raw, serr := jwt.Signed(sig).Claims(jwt.Claims{
			Issuer:   iss.URL,
			Subject:  "id-human-1",
			Audience: jwt.Audience{"veil"},
			Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		}).Claims(map[string]any{"amr": []string{"password", "totp"}}).Serialize()
		if serr != nil {
			http.Error(w, serr.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "not-an-id-token",
			"token_type":   "bearer",
			"id_token":     raw,
		})
	})
	iss.server = httptest.NewServer(mux)
	iss.URL = iss.server.URL
	t.Cleanup(iss.server.Close)
	return iss
}

func freeRedirect(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr + "/oidc/callback"
}

func TestLoginActionMintsTokenFile(t *testing.T) {
	iss := newLoginIssuer(t)
	tokenPath := filepath.Join(t.TempDir(), "human.jwt")
	h := &Host{
		Issuer:    iss.URL,
		ClientID:  "veil",
		Redirect:  freeRedirect(t),
		TokenPath: tokenPath,
	}
	h.setNeedLogin(true)

	raw := jsonHandle(t, h, map[string]string{"action": "login"})
	var got struct {
		AuthURL string `json:"auth_url"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "" || got.AuthURL == "" {
		t.Fatalf("login: %+v", got)
	}
	u, err := url.Parse(got.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" || !strings.HasPrefix(got.AuthURL, iss.URL+"/auth") {
		t.Fatalf("auth url: %s", got.AuthURL)
	}

	// Second call while in-flight returns the same URL, not a second listener.
	again := jsonHandle(t, h, map[string]string{"action": "login"})
	var got2 struct {
		AuthURL string `json:"auth_url"`
	}
	if err := json.Unmarshal(again, &got2); err != nil {
		t.Fatal(err)
	}
	if got2.AuthURL != got.AuthURL {
		t.Fatal("second login spawned a second flow")
	}

	// The browser lands on the localhost redirect.
	ru, _ := url.Parse(h.Redirect)
	q := ru.Query()
	q.Set("code", "code-1")
	q.Set("state", state)
	ru.RawQuery = q.Encode()
	res, err := http.Get(ru.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, err := os.ReadFile(tokenPath); err == nil && strings.TrimSpace(string(b)) != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("token file never written")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if h.loginNeeded() {
		t.Fatal("needLogin still set after exchange")
	}
}

func TestLoginActionNeedsIssuer(t *testing.T) {
	h := &Host{}
	raw := jsonHandle(t, h, map[string]string{"action": "login"})
	var got struct {
		AuthURL string `json:"auth_url"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.AuthURL != "" || got.Error == "" {
		t.Fatalf("login without issuer must fail closed: %+v", got)
	}
}
