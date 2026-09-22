package human

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginHTTPPasswordThenTOTP(t *testing.T) {
	iss := newTestIssuer(t)
	var state string
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/self-service/login/browser":
			if r.URL.Query().Get("flow") == "aal2" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "aal2",
					"ui": map[string]any{
						"action": "/self-service/login?flow=aal2",
						"nodes": []map[string]any{
							{"attributes": map[string]any{"name": "csrf_token", "value": "csrf"}},
							{"attributes": map[string]any{"name": "totp_code", "type": "text"}},
						},
					},
				})
				return
			}
			if r.URL.Query().Get("login_challenge") == "" {
				http.Error(w, "challenge", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "pw",
				"ui": map[string]any{
					"action": "/self-service/login?flow=pw",
					"nodes": []map[string]any{
						{"attributes": map[string]any{"name": "csrf_token", "value": "csrf"}},
						{"attributes": map[string]any{"name": "password", "type": "password"}},
						{"attributes": map[string]any{"name": "identifier", "type": "email"}},
					},
				},
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/self-service/login"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(raw, &body)
			if body["method"] == "password" {
				if body["identifier"] != "a@b.c" || body["password"] != "secret-pass" {
					http.Error(w, "password", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"redirect_browser_to": "/self-service/login/browser?flow=aal2",
				})
				return
			}
			if body["method"] == "totp" {
				if body["totp_code"] == "" {
					http.Error(w, "totp", http.StatusBadRequest)
					return
				}
				http.Redirect(w, r, iss.URL+"/callback?code=ok&state="+url.QueryEscape(state), http.StatusFound)
				return
			}
			http.Error(w, "method", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(kratos.Close)
	iss.mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		state = r.URL.Query().Get("state")
		http.Redirect(w, r, kratos.URL+"/self-service/login/browser?login_challenge=ch", http.StatusFound)
	})
	v, err := New(Config{Issuer: iss.URL, Audience: DefaultAudience, RedirectURL: iss.URL + "/callback"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v.LoginHTTP(context.Background(), HTTPLogin{
		KratosPublic: kratos.URL,
		Email:        "a@b.c",
		Password:     "secret-pass",
		TOTPSeed:     "JBSWY3DPEHPK3PXP",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireTOTP(raw); err != nil {
		t.Fatal(err)
	}
	h, err := v.Human(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "id-human-1" {
		t.Fatalf("%+v", h)
	}
}
