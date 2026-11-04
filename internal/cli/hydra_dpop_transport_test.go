package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHydraTokenDPoPTransport(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var sawDPoP bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("DPoP") == "" {
			http.Error(w, "missing dpop", http.StatusBadRequest)
			return
		}
		sawDPoP = true
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client := hydraTokenDPoPHTTPClient(key, srv.URL+"/oauth2/token")
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/oauth2/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if !sawDPoP {
		t.Fatal("expected DPoP header on token request")
	}
}
