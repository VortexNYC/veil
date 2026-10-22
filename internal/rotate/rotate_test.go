package rotate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResendRotateLifecycle(t *testing.T) {
	var created, deleted atomic.Int32
	keys := map[string]bool{"re_live": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !keys[auth] {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api-keys":
			created.Add(1)
			keys["re_new"] = true
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "key_new", "token": "re_new"})
		case r.Method == http.MethodGet && r.URL.Path == "/api-keys":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api-keys/"):
			deleted.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	rot := &resendRotator{base: srv.URL}
	ctx := context.Background()

	next, keyID, err := rot.Create(ctx, "resend", []byte("re_live"))
	if err != nil {
		t.Fatal(err)
	}
	if string(next) != "re_new" || keyID != "key_new" {
		t.Fatalf("create: token=%s id=%s", next, keyID)
	}
	if err := rot.Verify(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := rot.Verify(ctx, []byte("re_bogus")); err == nil {
		t.Fatal("verify accepted a dead key")
	}
	if err := rot.Retire(ctx, "key_old", []byte("re_live")); err != nil {
		t.Fatal(err)
	}
	if created.Load() != 1 || deleted.Load() != 1 {
		t.Fatalf("created=%d deleted=%d", created.Load(), deleted.Load())
	}
}

func TestForHostAndManagementEndpoints(t *testing.T) {
	if _, ok := ForHost("https://api.resend.com/emails"); !ok {
		t.Fatal("resend host unmapped")
	}
	if _, ok := ForHost("https://example.com"); ok {
		t.Fatal("unknown host mapped")
	}
	if !ManagementEndpoint("https://api.resend.com/api-keys/123") {
		t.Fatal("resend management path missed")
	}
	if !ManagementEndpoint("https://api.sendgrid.com/v3/api_keys") {
		t.Fatal("sendgrid management path missed")
	}
	if ManagementEndpoint("https://api.resend.com/emails") {
		t.Fatal("ordinary path flagged as management")
	}
}

func TestSendgridRotateLifecycle(t *testing.T) {
	var created, deleted atomic.Int32
	keys := map[string]bool{"sg_live": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !keys[auth] {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v3/api_keys":
			created.Add(1)
			keys["sg_new"] = true
			_ = json.NewEncoder(w).Encode(map[string]string{"api_key_id": "k_new", "api_key": "sg_new"})
		case r.Method == http.MethodGet && r.URL.Path == "/v3/scopes":
			_ = json.NewEncoder(w).Encode(map[string]any{"scopes": []string{}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v3/api_keys/"):
			deleted.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	rot := &sendgridRotator{base: srv.URL}
	ctx := context.Background()
	next, keyID, err := rot.Create(ctx, "sendgrid", []byte("sg_live"))
	if err != nil {
		t.Fatal(err)
	}
	if string(next) != "sg_new" || keyID != "k_new" {
		t.Fatalf("create: token=%s id=%s", next, keyID)
	}
	if err := rot.Verify(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := rot.Retire(ctx, "k_old", []byte("sg_live")); err != nil {
		t.Fatal(err)
	}
	if created.Load() != 1 || deleted.Load() != 1 {
		t.Fatalf("created=%d deleted=%d", created.Load(), deleted.Load())
	}
	if _, ok := ForHost("https://api.sendgrid.com/v3/mail/send"); !ok {
		t.Fatal("sendgrid host unmapped")
	}
}

func TestSpecRotatorAndRedirectRefusal(t *testing.T) {
	exfilHit := atomic.Bool{}
	exfil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exfilHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(exfil.Close)

	redirectMode := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirectMode.Load() {
			http.Redirect(w, r, exfil.URL+"/steal", http.StatusFound)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": "tok_42", "secret": "sk_next_42"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/me":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/tokens/tok_old":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")
	if err := RegisterSpec(Spec{
		Host:            host,
		Scheme:          "http",
		CreatePath:      "/v1/tokens",
		TokenField:      "data.secret",
		KeyIDField:      "data.id",
		VerifyPath:      "/v1/me",
		RetirePath:      "/v1/tokens/{id}",
		ManagementPaths: []string{"/v1/tokens"},
	}); err != nil {
		t.Fatal(err)
	}
	if !ManagementEndpoint(srv.URL + "/v1/tokens/tok_42") {
		t.Fatal("spec management path not guarded")
	}
	rot, ok := ForHost(srv.URL + "/v1/chat")
	if !ok {
		t.Fatal("spec rotator not registered")
	}
	ctx := context.Background()
	tok, keyID, err := rot.Create(ctx, "acme", []byte("sk_live"))
	if err != nil || string(tok) != "sk_next_42" || keyID != "tok_42" {
		t.Fatalf("spec create: %s %s %v", tok, keyID, err)
	}
	if err := rot.Verify(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := rot.Retire(ctx, "tok_old", []byte("sk_live")); err != nil {
		t.Fatal(err)
	}

	// Redirect refusal: a 302 must error out without ever hitting exfil.
	redirectMode.Store(true)
	if _, _, err := rot.Create(ctx, "acme", []byte("sk_live")); err == nil {
		t.Fatal("redirect should be refused")
	}
	if exfilHit.Load() {
		t.Fatal("redirect followed to exfil server")
	}

	// Spec with create_path/retire_path not covered by management_paths is rejected.
	if _, err := NewSpecRotator(Spec{
		Host:            host,
		Scheme:          "http",
		CreatePath:      "/v1/tokens",
		TokenField:      "data.secret",
		KeyIDField:      "data.id",
		VerifyPath:      "/v1/me",
		RetirePath:      "/v1/tokens/{id}",
		ManagementPaths: []string{"/v1/other"},
	}); err == nil {
		t.Fatal("spec with uncovered create/retire path should be rejected")
	}
}
