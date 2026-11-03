package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/dpop"
)

func TestOriginAttachAuthDPoP(t *testing.T) {
	t.Cleanup(dpop.ResetDefaultVerifierForTest)
	t.Setenv("VEIL_DPOP", "1")
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "cursor.jwt")
	if err := os.WriteFile(tokFile, []byte("jwt-agent-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_OIDC_TOKEN_FILE", tokFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "DPoP ")
		if raw == r.Header.Get("Authorization") {
			http.Error(w, "want DPoP scheme", http.StatusUnauthorized)
			return
		}
		if err := dpop.VerifyRequest(r, raw); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VEIL_ORIGIN", srv.URL)

	_, err := originDo(context.Background(), http.MethodGet, "/v1/items", "jwt-agent-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "cursor.dpop")
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected key file %s: %v", keyPath, err)
	}
}

func TestOriginAttachAuthDPoPDisabled(t *testing.T) {
	t.Setenv("VEIL_DPOP", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer plain" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("DPoP") != "" {
			http.Error(w, "unexpected dpop", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VEIL_ORIGIN", srv.URL)

	_, err := originDo(context.Background(), http.MethodGet, "/ping", "plain", nil)
	if err != nil {
		t.Fatal(err)
	}
}
