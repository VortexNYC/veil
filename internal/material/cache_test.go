package material

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTokenCacheAvoidsRepeatedWIFExchangeUntilInvalidated(t *testing.T) {
	var idpHits, wifHits atomic.Int32
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idpHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"assertion_jwt_1","token_type":"Bearer","expires_in":300}`)
	}))
	t.Cleanup(idp.Close)

	wif := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wifHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"oat_cached_1","token_type":"Bearer","expires_in":600}`)
	}))
	t.Cleanup(wif.Close)

	raw, err := PackWIF(WIFOpts{
		TokenURL:          wif.URL,
		AssertionTokenURL: idp.URL,
		AssertionClientID: "agent-claude",
		AssertionSecret:   "sec_1",
		FederationRuleID:  "fdrl_cache_test",
	})
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	ctx := context.Background()

	for range 3 {
		tok, _, scrub, err := AccessTokenWithMetadata(ctx, env, http.DefaultClient)
		if err != nil || tok != "oat_cached_1" || scrub != "assertion_jwt_1" {
			t.Fatalf("got tok=%q scrub=%q err=%v", tok, scrub, err)
		}
	}
	if idpHits.Load() != 1 || wifHits.Load() != 1 {
		t.Fatalf("expected 1 exchange across 3 calls, got idp=%d wif=%d", idpHits.Load(), wifHits.Load())
	}

	// Rotating the secret invalidates the cache key immediately.
	env.AssertionClientSec = "sec_2"
	if _, _, _, err := AccessTokenWithMetadata(ctx, env, http.DefaultClient); err != nil {
		t.Fatal(err)
	}
	if idpHits.Load() != 2 || wifHits.Load() != 2 {
		t.Fatalf("expected cache miss on secret rotation, got idp=%d wif=%d", idpHits.Load(), wifHits.Load())
	}
}
