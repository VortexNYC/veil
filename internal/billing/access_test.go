package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckAccessParsesEnvelope(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"data":{"access":{"customerExternalId":"org-1","key":"veil","allowed":true,"state":"ready","billingStatus":"active","reasonCodes":[],"activeGrants":[]},"entitlements":{"active":[],"revoked":[]}}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Key: "k", MerchantID: "m", Environment: "sandbox"}
	ans, err := c.CheckAccess(context.Background(), "cus_1", "veil")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/customers/cus_1/access?key=veil" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if !ans.Allowed || ans.State != "ready" {
		t.Fatalf("answer = %+v", ans)
	}
}

type fakeLookuper struct {
	calls *atomic.Int32
	ans   AccessAnswer
	err   error
}

func (f fakeLookuper) CheckAccess(context.Context, string, string) (AccessAnswer, error) {
	f.calls.Add(1)
	return f.ans, f.err
}

func TestAccessCheckerCachesPerOrg(t *testing.T) {
	calls := &atomic.Int32{}
	c := &AccessChecker{Lookuper: fakeLookuper{calls: calls, ans: AccessAnswer{Allowed: true}}, TTL: time.Minute}
	for i := 0; i < 5; i++ {
		if !c.Allowed(context.Background(), "org-1", "cus_1") {
			t.Fatal("expected allowed")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (cache)", calls.Load())
	}
	// Other orgs check independently.
	if !c.Allowed(context.Background(), "org-2", "cus_2") {
		t.Fatal("expected allowed")
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2", calls.Load())
	}
}

func TestAccessCheckerExpiredEntryRechecks(t *testing.T) {
	calls := &atomic.Int32{}
	c := &AccessChecker{Lookuper: fakeLookuper{calls: calls, ans: AccessAnswer{Allowed: true}}, TTL: time.Nanosecond}
	c.Allowed(context.Background(), "org-1", "cus_1")
	time.Sleep(time.Millisecond)
	c.Allowed(context.Background(), "org-1", "cus_1")
	if calls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want 2 (TTL expired)", calls.Load())
	}
}

func TestAccessCheckerFailsOpen(t *testing.T) {
	calls := &atomic.Int32{}
	c := &AccessChecker{Lookuper: fakeLookuper{calls: calls, err: errors.New("vortex down")}, TTL: time.Minute}
	// Error is swallowed into false — and not cached (a healed org shouldn't
	// wait out the TTL on a transient failure).
	if c.Allowed(context.Background(), "org-1", "cus_1") {
		t.Fatal("error must resolve to not-allowed")
	}
	c.Allowed(context.Background(), "org-1", "cus_1")
	if calls.Load() != 2 {
		t.Fatalf("errors must not cache: calls = %d", calls.Load())
	}
	if c.Allowed(context.Background(), "org-1", "cus_1") {
		// still failing — deny stands
		t.Fatal("persistent error still false")
	}
}

func TestAccessCheckerConcurrentSingleflight(t *testing.T) {
	calls := &atomic.Int32{}
	slow := singleflightLookuper{calls: calls, delay: 50 * time.Millisecond}
	c := &AccessChecker{Lookuper: slow, TTL: time.Minute}
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() { done <- c.Allowed(context.Background(), "org-1", "cus_1") }()
	}
	for i := 0; i < 10; i++ {
		if !<-done {
			t.Fatal("expected allowed")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (singleflight)", calls.Load())
	}
}

type singleflightLookuper struct {
	calls *atomic.Int32
	delay time.Duration
}

func (s singleflightLookuper) CheckAccess(context.Context, string, string) (AccessAnswer, error) {
	s.calls.Add(1)
	time.Sleep(s.delay)
	return AccessAnswer{Allowed: true}, nil
}
