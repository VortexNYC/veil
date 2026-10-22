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

func TestForHostAndProviderKeyID(t *testing.T) {
	if _, ok := ForHost("https://api.resend.com/emails"); !ok {
		t.Fatal("resend host unmapped")
	}
	if _, ok := ForHost("https://example.com"); ok {
		t.Fatal("unknown host mapped")
	}
	if got := ProviderKeyID([]string{"agents", "rot:key_9"}); got != "key_9" {
		t.Fatalf("provider key id %q", got)
	}
	if got := ProviderKeyID([]string{"agents"}); got != "" {
		t.Fatalf("phantom key id %q", got)
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
