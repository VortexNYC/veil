package store

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/dpop"
)

func TestPostgresDPoPSharedPinning(t *testing.T) {
	pg := openTestPostgres(t)
	dpop.SetCoordinator(dpop.NewPostgresCoordinator(pg.Pool()))
	t.Cleanup(func() {
		dpop.SetCoordinator(nil)
		dpop.ResetDefaultVerifierForTest()
	})

	now := time.Now().UTC().Truncate(time.Second)
	key1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tok := "ses_shared_pin_test"
	proof1, err := dpop.MintProof(key1, "POST", "https://veil.nyc/v1/use", tok, now)
	if err != nil {
		t.Fatal(err)
	}
	req1 := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	req1.Header.Set("DPoP", proof1)
	if err := dpop.VerifyRequest(req1, tok); err != nil {
		t.Fatalf("first proof: %v", err)
	}
	reqDowngrade := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	if err := dpop.VerifyRequest(reqDowngrade, tok); err == nil {
		t.Fatal("downgrade after postgres pin should fail")
	}
}
