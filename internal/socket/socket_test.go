package socket

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

const secret = "sk_live_SOCKET_SECRET"

type testIssuer struct {
	URL    string
	key    *rsa.PrivateKey
	server *http.Server
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
			TokenEndpoint                    string   `json:"token_endpoint"`
			ResponseTypesSupported           []string `json:"response_types_supported"`
			SubjectTypesSupported            []string `json:"subject_types_supported"`
			IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
		}{
			Issuer:                           iss.URL,
			JWKSURI:                          iss.URL + "/keys",
			AuthorizationEndpoint:            iss.URL + "/auth",
			TokenEndpoint:                    iss.URL + "/token",
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
	ln := httptestListener(t)
	iss.URL = "http://" + ln.Addr().String()
	iss.server = &http.Server{Handler: mux}
	go func() { _ = iss.server.Serve(ln) }()
	t.Cleanup(func() { _ = iss.server.Close() })
	return iss
}

func httptestListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
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

func setup(t *testing.T) (*app.App, *testIssuer, *http.Client, string) {
	t.Helper()
	iss := newTestIssuer(t)
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	uln := httptestListener(t)
	us := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok:"+r.Header.Get("Authorization"))
	})}
	go func() { _ = us.Serve(uln) }()
	t.Cleanup(func() { _ = us.Close() })
	up := "http://" + uln.Addr().String()
	if _, err := a.AddItem("stripe", up, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindWorkload("claude", iss.URL, "agent-claude", "veil"); err != nil {
		t.Fatal(err)
	}
	sockDir, err := os.MkdirTemp("/tmp", "veil")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	srv, err := Listen(a, filepath.Join(sockDir, Name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return a, iss, unixClient(srv.Path), up
}

func unixClient(path string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
		},
	}
}

func TestSocketUseWithBearerIsTheBoundAgent(t *testing.T) {
	_, iss, c, up := setup(t)
	tok := iss.token(t, "agent-claude", "veil")
	req, err := http.NewRequest(http.MethodPost, "http://veil/use", bytes.NewBufferString(`{"item":"stripe","url":"`+up+`","method":"GET"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
	if scrub.Contains(body, []byte(secret)) {
		t.Fatalf("secret leaked: %s", body)
	}
	var got useOut
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("%+v", got)
	}
}

func TestSocketRejectsMissingBearer(t *testing.T) {
	_, _, c, up := setup(t)
	req, err := http.NewRequest(http.MethodPost, "http://veil/use", bytes.NewBufferString(`{"item":"stripe","url":"`+up+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSocketRejectsUnknownIssuer(t *testing.T) {
	_, _, c, up := setup(t)
	other := newTestIssuer(t)
	tok := other.token(t, "agent-claude", "veil")
	req, err := http.NewRequest(http.MethodPost, "http://veil/use", bytes.NewBufferString(`{"item":"stripe","url":"`+up+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSocketNameIsNotIdentity(t *testing.T) {
	a, iss, c, _ := setup(t)
	if _, err := a.AddAgent("other"); err != nil {
		t.Fatal(err)
	}
	tok := iss.token(t, "agent-claude", "veil")
	req, err := http.NewRequest(http.MethodGet, "http://veil/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Agent", "other")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", resp.StatusCode, body)
	}
	var got itemsOut
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "stripe" {
		t.Fatalf("%+v", got)
	}
}

func TestListenRefusesRegularFile(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	path := filepath.Join(a.Dir, Name)
	if err := os.WriteFile(path, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(a, path); err == nil {
		t.Fatal("overwrote a regular file")
	}
}
