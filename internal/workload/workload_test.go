package workload

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

type testIssuer struct {
	URL         string
	key         *rsa.PrivateKey
	server      *httptest.Server
	discoveries atomic.Int64
	gate        chan struct{}
}

func newTestIssuer(tb testing.TB) *testIssuer {
	tb.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		tb.Fatal(err)
	}
	iss := &testIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		iss.discoveries.Add(1)
		if iss.gate != nil {
			<-iss.gate
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.URL,
			"jwks_uri":                              iss.URL + "/keys",
			"authorization_endpoint":                iss.URL + "/auth",
			"response_types_supported":              []string{"id_token"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &key.PublicKey,
			KeyID:     "test",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}}
		_ = json.NewEncoder(w).Encode(set)
	})
	iss.server = httptest.NewServer(mux)
	iss.URL = iss.server.URL
	tb.Cleanup(iss.server.Close)
	return iss
}

func (i *testIssuer) token(tb testing.TB, sub, aud string, exp time.Time) string {
	tb.Helper()
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: i.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		tb.Fatal(err)
	}
	raw, err := jwt.Signed(sig).Claims(jwt.Claims{
		Issuer:   i.URL,
		Subject:  sub,
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(exp),
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).Serialize()
	if err != nil {
		tb.Fatal(err)
	}
	return raw
}

func TestOIDCTokenResolvesBoundAgent(t *testing.T) {
	iss := newTestIssuer(t)
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutWorkload(protocol.Workload{
		AgentID:  agent.ID,
		Issuer:   iss.URL,
		Subject:  "repo:VortexNYC/veil:ref:refs/heads/main",
		Audience: "veil",
	}); err != nil {
		t.Fatal(err)
	}

	c := New(mem)
	tok := iss.token(t, "repo:VortexNYC/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour))
	got, err := c.Agent(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != agent.ID {
		t.Fatalf("%+v", got)
	}
}

func TestUnknownIssuerNeverTrusted(t *testing.T) {
	mem := store.NewMemory()
	c := New(mem)
	// Well-formed token, issuer we have no binding for. Must fail before discovery.
	raw := "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJodHRwczovL2V2aWwuZXhhbXBsZSIsInN1YiI6IngiLCJhdWQiOiJwYXNzd29yZC1tYW5hZ2VyIn0.sig"
	if _, err := c.Agent(context.Background(), raw); err == nil {
		t.Fatal("untrusted issuer accepted")
	}
}

func TestWrongSubjectDenied(t *testing.T) {
	iss := newTestIssuer(t)
	mem := store.NewMemory()
	if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutWorkload(protocol.Workload{
		AgentID:  "flue",
		Issuer:   iss.URL,
		Subject:  "the-real-worker",
		Audience: "veil",
	}); err != nil {
		t.Fatal(err)
	}
	c := New(mem)
	tok := iss.token(t, "someone-else", "veil", time.Now().Add(time.Hour))
	if _, err := c.Agent(context.Background(), tok); err == nil {
		t.Fatal("wrong subject accepted")
	}
}

func TestProviderDiscoveryCachedAcrossCalls(t *testing.T) {
	iss := newTestIssuer(t)
	mem := store.NewMemory()
	if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutWorkload(protocol.Workload{
		AgentID:  "flue",
		Issuer:   iss.URL,
		Subject:  "repo:VortexNYC/veil:ref:refs/heads/main",
		Audience: "veil",
	}); err != nil {
		t.Fatal(err)
	}

	c := New(mem)
	tok := iss.token(t, "repo:VortexNYC/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour))
	if _, err := c.Agent(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Agent(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if iss.discoveries.Load() != 1 {
		t.Fatalf("provider discovery called %d times, want 1", iss.discoveries.Load())
	}
}

// TestColdDiscoverySingleflight: N concurrent first-auths on one cold
// issuer share exactly one discovery call.
func TestColdDiscoverySingleflight(t *testing.T) {
	iss := newTestIssuer(t)
	mem := store.NewMemory()
	if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutWorkload(protocol.Workload{
		AgentID:  "flue",
		Issuer:   iss.URL,
		Subject:  "repo:VortexNYC/veil:ref:refs/heads/main",
		Audience: "veil",
	}); err != nil {
		t.Fatal(err)
	}
	c := New(mem)
	tok := iss.token(t, "repo:VortexNYC/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour))
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Agent(context.Background(), tok)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("auth %d: %v", i, err)
		}
	}
	if iss.discoveries.Load() != 1 {
		t.Fatalf("provider discovery called %d times, want 1", iss.discoveries.Load())
	}
}

// TestColdDiscoveryPerIssuer: a slow discovery on one issuer must not
// serialize auths bound to a different issuer — the pre-singleflight global
// mutex parked every workload auth behind one HTTP call.
func TestColdDiscoveryPerIssuer(t *testing.T) {
	slow := newTestIssuer(t)
	slow.gate = make(chan struct{})
	fast := newTestIssuer(t)
	mem := store.NewMemory()
	for _, tc := range []struct {
		agentID string
		iss     *testIssuer
		sub     string
	}{
		{"slow-agent", slow, "sub-slow"},
		{"fast-agent", fast, "sub-fast"},
	} {
		if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: tc.agentID, OrgID: "org"}); err != nil {
			t.Fatal(err)
		}
		if err := mem.PutWorkload(protocol.Workload{
			AgentID:  tc.agentID,
			Issuer:   tc.iss.URL,
			Subject:  tc.sub,
			Audience: "veil",
		}); err != nil {
			t.Fatal(err)
		}
	}
	c := New(mem)
	slowTok := slow.token(t, "sub-slow", "veil", time.Now().Add(time.Hour))
	slowDone := make(chan error, 1)
	go func() {
		_, err := c.Agent(context.Background(), slowTok)
		slowDone <- err
	}()
	// Wait until the slow discovery is actually in flight (it parks on the
	// gate after incrementing).
	deadline := time.Now().Add(5 * time.Second)
	for slow.discoveries.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("slow discovery never started")
		}
		time.Sleep(time.Millisecond)
	}
	// The fast issuer's auth must complete while the slow one is parked.
	if _, err := c.Agent(context.Background(), fast.token(t, "sub-fast", "veil", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("fast issuer blocked behind slow discovery: %v", err)
	}
	close(slow.gate)
	if err := <-slowDone; err != nil {
		t.Fatalf("slow auth: %v", err)
	}
}

// Steady-state cost of one workload auth: RS256 verify + three store reads,
// provider and JWKS already cached. Reported per-op so the number is directly
// comparable against the measured session-token path.
func BenchmarkAgentVerify(b *testing.B) {
	iss := newTestIssuer(b)
	mem := store.NewMemory()
	if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}); err != nil {
		b.Fatal(err)
	}
	if err := mem.PutWorkload(protocol.Workload{
		AgentID:  "flue",
		Issuer:   iss.URL,
		Subject:  "repo:VortexNYC/veil:ref:refs/heads/main",
		Audience: "veil",
	}); err != nil {
		b.Fatal(err)
	}
	c := New(mem)
	tok := iss.token(b, "repo:VortexNYC/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour))
	// Warm the provider/JWKS cache so the loop measures steady state.
	if _, err := c.Agent(context.Background(), tok); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Agent(context.Background(), tok); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAgentVerifyParallel(b *testing.B) {
	iss := newTestIssuer(b)
	mem := store.NewMemory()
	if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "flue", OrgID: "org"}); err != nil {
		b.Fatal(err)
	}
	if err := mem.PutWorkload(protocol.Workload{
		AgentID:  "flue",
		Issuer:   iss.URL,
		Subject:  "repo:VortexNYC/veil:ref:refs/heads/main",
		Audience: "veil",
	}); err != nil {
		b.Fatal(err)
	}
	c := New(mem)
	tok := iss.token(b, "repo:VortexNYC/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour))
	if _, err := c.Agent(context.Background(), tok); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := c.Agent(context.Background(), tok); err != nil {
				b.Fatal(err)
			}
		}
	})
	if iss.discoveries.Load() != 1 {
		b.Fatalf("provider discovery called %d times, want 1", iss.discoveries.Load())
	}
}

func TestSubjectPrefixWildcardAndExactPrecedence(t *testing.T) {
	iss := newTestIssuer(t)
	mem := store.NewMemory()
	for _, id := range []string{"org-wide", "repo-wide", "main-exact"} {
		if err := mem.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: id, OrgID: "org"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []protocol.Workload{
		{AgentID: "org-wide", Issuer: iss.URL, Subject: "repo:VortexNYC/*", Audience: "veil"},
		{AgentID: "repo-wide", Issuer: iss.URL, Subject: "repo:VortexNYC/veil:*", Audience: "veil"},
		{AgentID: "main-exact", Issuer: iss.URL, Subject: "repo:VortexNYC/veil:ref:refs/heads/main", Audience: "veil"},
	} {
		if err := mem.PutWorkload(w); err != nil {
			t.Fatal(err)
		}
	}
	c := New(mem)
	ctx := context.Background()

	// Exact match beats both prefixes.
	got, err := c.Agent(ctx, iss.token(t, "repo:VortexNYC/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour)))
	if err != nil || got.ID != "main-exact" {
		t.Fatalf("exact match: got %+v err=%v, want main-exact", got, err)
	}
	// Longer prefix (repo:VortexNYC/veil:*) beats shorter prefix (repo:VortexNYC/*).
	got, err = c.Agent(ctx, iss.token(t, "repo:VortexNYC/veil:pull_request", "veil", time.Now().Add(time.Hour)))
	if err != nil || got.ID != "repo-wide" {
		t.Fatalf("longer prefix: got %+v err=%v, want repo-wide", got, err)
	}
	// Shorter prefix matches another repo in the same org.
	got, err = c.Agent(ctx, iss.token(t, "repo:VortexNYC/pile:ref:refs/heads/main", "veil", time.Now().Add(time.Hour)))
	if err != nil || got.ID != "org-wide" {
		t.Fatalf("shorter prefix: got %+v err=%v, want org-wide", got, err)
	}
	// Unrelated org is denied.
	if _, err := c.Agent(ctx, iss.token(t, "repo:OtherOrg/veil:ref:refs/heads/main", "veil", time.Now().Add(time.Hour))); err == nil {
		t.Fatal("unrelated org matched")
	}
}

func TestValidateBindingBlocksGlobalImpersonationAndSSRF(t *testing.T) {
	bad := [][2]string{
		{"https://token.actions.githubusercontent.com", "*"},
		{"https://token.actions.githubusercontent.com", "repo:*"},
		{"https://token.actions.githubusercontent.com", "repo:VortexNYC*"},
		{"http://token.actions.githubusercontent.com", "repo:VortexNYC/veil:ref:refs/heads/main"},
		{"https://169.254.169.254", "sub"},
		{"https://10.0.0.1", "sub"},
		{"https://192.168.1.1", "sub"},
		{"https://user:pass@issuer.example.com", "sub"},
		{"https://issuer.example.com?foo=bar", "sub"},
		{"file:///etc/passwd", "sub"},
	}
	for _, tc := range bad {
		if err := ValidateBinding(tc[0], tc[1]); err == nil {
			t.Fatalf("ValidateBinding(%q, %q) accepted unsafe binding", tc[0], tc[1])
		}
	}
	good := [][2]string{
		{"https://token.actions.githubusercontent.com", "repo:VortexNYC/veil:*"},
		{"https://token.actions.githubusercontent.com", "repo:VortexNYC/*"},
		{"https://oidc.eks.us-west-2.amazonaws.com/id/EX", "system:serviceaccount:prod:*"},
		{"http://127.0.0.1:4444", "agent-cursor"},
	}
	for _, tc := range good {
		if err := ValidateBinding(tc[0], tc[1]); err != nil {
			t.Fatalf("ValidateBinding(%q, %q) rejected valid binding: %v", tc[0], tc[1], err)
		}
	}
}
