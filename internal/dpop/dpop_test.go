package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDPoPVerifyReplayAndKeyPinning(t *testing.T) {
	v := NewVerifier()
	now := time.Now().UTC().Truncate(time.Second)
	v.Now = func() time.Time { return now }

	key1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tok := "ses_test_token_123"
	proof1, err := MintProof(key1, "POST", "https://veil.nyc/v1/use?foo=bar", tok, now)
	if err != nil {
		t.Fatal(err)
	}

	req1 := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	req1.Header.Set("DPoP", proof1)
	if err := v.VerifyRequest(req1, tok); err != nil {
		t.Fatalf("valid proof1 rejected: %v", err)
	}

	// Replaying the exact same proof (same jti) must fail.
	reqReplay := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	reqReplay.Header.Set("DPoP", proof1)
	if err := v.VerifyRequest(reqReplay, tok); err == nil {
		t.Fatal("replayed jti should be rejected")
	}

	// Once pinned to key1, stripping the DPoP header must fail (no downgrade to Bearer).
	reqDowngrade := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	if err := v.VerifyRequest(reqDowngrade, tok); err == nil {
		t.Fatal("downgrade to plain Bearer after DPoP pin should be rejected")
	}

	// Once pinned to key1, signing a new proof with key2 for the same token must fail.
	proofWrongKey, err := MintProof(key2, "POST", "https://veil.nyc/v1/use", tok, now)
	if err != nil {
		t.Fatal(err)
	}
	reqWrongKey := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	reqWrongKey.Header.Set("DPoP", proofWrongKey)
	if err := v.VerifyRequest(reqWrongKey, tok); err == nil {
		t.Fatal("proof signed by different key2 should be rejected")
	}

	// A fresh proof from key1 with a new jti succeeds.
	proof2, err := MintProof(key1, "POST", "https://veil.nyc/v1/use", tok, now)
	if err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	req2.Header.Set("DPoP", proof2)
	if err := v.VerifyRequest(req2, tok); err != nil {
		t.Fatalf("second proof from key1 rejected: %v", err)
	}
}
