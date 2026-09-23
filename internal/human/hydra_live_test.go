//go:build live

package human_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/testutil"
)

// Real-Hydra end-to-end: production AuthCodeURL → real Hydra authorize →
// admin-accepted login + consent → production Exchange → production
// Subject (JWKS). The only thing faked is who clicked accept — the token
// is minted by real Hydra and verified by real JWKS. Run:
//
//	docker run -d --name veil-hydra-test -p 5555:4444 -p 5556:4445 \
//	  -e DSN=memory -e SECRETS_SYSTEM=test-system-secret-0123456789abcdef \
//	  -e OIDC_SUBJECT_IDENTIFIERS_PAIRWISE_SALT=test-salt-0123456789abcdef \
//	  -e URLS_SELF_ISSUER=http://127.0.0.1:5555 \
//	  oryd/hydra:v26.2.0 serve all --dev
//	HYDRA_TEST_PUBLIC=http://127.0.0.1:5555 HYDRA_TEST_ADMIN=http://127.0.0.1:5556 \
//	  go test -tags live ./internal/human -run HydraLive -v
func TestHydraLiveAuthorizeExchangeVerify(t *testing.T) {
	pub := strings.TrimRight(os.Getenv("HYDRA_TEST_PUBLIC"), "/")
	adm := strings.TrimRight(os.Getenv("HYDRA_TEST_ADMIN"), "/")
	if pub == "" || adm == "" {
		t.Skip("HYDRA_TEST_PUBLIC / HYDRA_TEST_ADMIN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		clientID = "test-human"
		subject  = "human-42"
	)
	v, err := human.New(human.Config{Issuer: pub, Audience: clientID})
	if err != nil {
		t.Fatal(err)
	}
	if err := testutil.HydraEnsureClient(ctx, adm, clientID, v.Redirect()); err != nil {
		t.Fatal(err)
	}
	verifier, state, err := human.PKCE()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := v.AuthCodeURL(ctx, state, verifier)
	if err != nil {
		t.Fatal(err)
	}
	code, err := testutil.HydraAuthorize(ctx, adm, authURL, subject)
	if err != nil {
		t.Fatal(err)
	}
	idToken, err := v.Exchange(ctx, code, verifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	sub, err := v.Subject(ctx, idToken)
	if err != nil {
		t.Fatalf("subject verify: %v", err)
	}
	if sub != subject {
		t.Fatalf("subject %q, want %q", sub, subject)
	}
	// A tampered token must fail closed.
	if _, err := v.Subject(ctx, idToken[:len(idToken)-2]+"xx"); err == nil {
		t.Fatal("tampered token verified")
	}
}
