package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
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

// Handlers verify, then resolve() verifies the same request again. The second
// pass is the same request, not a replay.
func TestDPoPVerifySameRequestTwice(t *testing.T) {
	v := NewVerifier()
	now := time.Now().UTC().Truncate(time.Second)
	v.Now = func() time.Time { return now }

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tok := "ses_test_token_double"
	proof, err := MintProof(key, "POST", "https://veil.nyc/v1/use", tok, now)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	req.Header.Set("DPoP", proof)
	if err := v.VerifyRequest(req, tok); err != nil {
		t.Fatalf("first verify rejected: %v", err)
	}
	if err := v.VerifyRequest(req, tok); err != nil {
		t.Fatalf("second verify on same request rejected: %v", err)
	}

	// The same proof on a different request is still a replay.
	other := httptest.NewRequest("POST", "https://veil.nyc/v1/use", nil)
	other.Header.Set("DPoP", proof)
	if err := v.VerifyRequest(other, tok); err == nil {
		t.Fatal("replayed jti on a different request should be rejected")
	}
}

func TestMatchHTUIgnoresForgedForwardedHostWithoutEdge(t *testing.T) {
	v := NewVerifier()
	now := time.Now().UTC().Truncate(time.Second)
	v.Now = func() time.Time { return now }

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tok := "plain_bearer_token"
	proof, err := MintProof(key, "POST", "https://veil.nyc/v1/provision", tok, now)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "https://veil.nyc/v1/provision", nil)
	req.Host = "veil.nyc"
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("DPoP", proof)
	if err := v.VerifyRequest(req, tok); err != nil {
		t.Fatalf("forged X-Forwarded-Host without edge must not break htu: %v", err)
	}

	v2 := NewVerifier()
	v2.Now = v.Now
	tok2 := "plain_bearer_token_2"
	proofEvil, err := MintProof(key, "POST", "https://attacker.example/v1/provision", tok2, now)
	if err != nil {
		t.Fatal(err)
	}
	reqEvil := httptest.NewRequest("POST", "https://veil.nyc/v1/provision", nil)
	reqEvil.Host = "veil.nyc"
	reqEvil.Header.Set("X-Forwarded-Host", "attacker.example")
	reqEvil.Header.Set("DPoP", proofEvil)
	if err := v2.VerifyRequest(reqEvil, tok2); err == nil {
		t.Fatal("htu bound to attacker host must not verify against veil.nyc request")
	}
}

func TestMatchHTURejectsSchemeMismatch(t *testing.T) {
	v := NewVerifier()
	now := time.Now().UTC().Truncate(time.Second)
	v.Now = func() time.Time { return now }

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tok := "scheme_test"
	proof, err := MintProof(key, "POST", "http://127.0.0.1:8080/v1/provision", tok, now)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "https://127.0.0.1:8080/v1/provision", nil)
	req.Host = "127.0.0.1:8080"
	req.TLS = &tls.ConnectionState{} // https
	req.Header.Set("DPoP", proof)
	if err := v.VerifyRequest(req, tok); err == nil {
		t.Fatal("http htu must not match https request")
	}
}
