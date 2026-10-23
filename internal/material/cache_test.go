package material

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenCacheAvoidsRepeatedWIFExchangeUntilInvalidated(t *testing.T) {
	var idpHits, wifHits atomic.Int32
	const fakeAssertion = "a1"
	const fakeOAT = "t1"
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idpHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"`+fakeAssertion+`","token_type":"Bearer","expires_in":300}`)
	}))
	t.Cleanup(idp.Close)

	wif := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wifHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"`+fakeOAT+`","token_type":"Bearer","expires_in":600}`)
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
		if err != nil || tok != fakeOAT || scrub != fakeAssertion {
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

func TestTokenCacheBoundedEviction(t *testing.T) {
	c := NewTokenCache()
	now := time.Now().UTC()
	for i := 0; i < maxTokenCacheEntries+50; i++ {
		c.put(fmt.Sprintf("k-%d", i), "tok", "", now.Add(time.Duration(i+120)*time.Second), now)
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != maxTokenCacheEntries {
		t.Fatalf("expected capped len %d, got %d", maxTokenCacheEntries, n)
	}
	if _, ok := c.get("k-0", now); ok {
		t.Fatal("oldest entry k-0 should have been evicted")
	}
}
