package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
	"github.com/VortexNYC/veil/internal/store"
)

const secret = "sk_live_DO_NOT_LEAK_THIS"

func setup(t *testing.T, level protocol.GrantLevel) (*Broker, protocol.Principal, protocol.Principal, *httptest.Server, string) {
	t.Helper()
	var sawAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("X-Echo-Auth", sawAuth)
		_, _ = io.WriteString(w, `{"ok":true,"echo":"`+strings.TrimPrefix(sawAuth, "Bearer ")+`"}`)
	}))
	t.Cleanup(upstream.Close)

	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "human-1", OrgID: "org-1"}
	item := protocol.Item{
		ID:    "item-1",
		OrgID: "org-1",
		Name:  "stripe-live",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:  []string{upstream.URL},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutHuman(human); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(secret)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   level,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	b := New(mem)
	b.Now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }
	return b, agent, human, upstream, secret
}

func mustNoLeak(t *testing.T, v any) {
	t.Helper()
	if err := AssertNoSecret(v, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatalf("secret in json: %s", raw)
	}
}

func TestLevel2FetchInjectsAndScrubs(t *testing.T) {
	b, agent, _, upstream, _ := setup(t, protocol.Level2)
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("decision=%s reason=%s", got.Decision, got.Reason)
	}
	if got.Fetch == nil || got.Fetch.Status != 200 {
		t.Fatalf("fetch=%+v", got.Fetch)
	}
	if scrub.Contains(got.Fetch.Body, []byte(secret)) {
		t.Fatalf("secret in body: %s", got.Fetch.Body)
	}
	if strings.Contains(got.Fetch.Header.Get("X-Echo-Auth"), secret) {
		t.Fatalf("secret in header: %v", got.Fetch.Header)
	}
	mustNoLeak(t, got)
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Decision != protocol.DecisionAllow {
		t.Fatalf("audit=%+v", events)
	}
	mustNoLeak(t, events)
}

func TestLevel1BlocksUntilHumanApproves(t *testing.T) {
	b, agent, human, upstream, _ := setup(t, protocol.Level1)
	req := protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	}
	got, err := b.Use(context.Background(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionNeedApproval {
		t.Fatalf("decision=%s", got.Decision)
	}
	if got.Fetch != nil {
		t.Fatal("must not fetch before approval")
	}
	mustNoLeak(t, got)

	if _, err := b.Approve(human, "grant-1", time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err = b.Use(context.Background(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("after approve: %+v", got)
	}
	mustNoLeak(t, got)
}

func TestNeedApprovalFilesRequestOnce(t *testing.T) {
	b, agent, _, upstream, _ := setup(t, protocol.Level1)
	var filed []protocol.ApprovalRequest
	b.OnRequestFiled = func(_ context.Context, r protocol.ApprovalRequest) {
		filed = append(filed, r)
	}
	req := protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	}
	got, err := b.Use(context.Background(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionNeedApproval {
		t.Fatalf("decision=%s", got.Decision)
	}
	if got.RequestID == "" || got.RequestExpiresAt == nil {
		t.Fatalf("denial must carry the filed request: %+v", got)
	}
	mustNoLeak(t, got)

	again, err := b.Use(context.Background(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	if again.RequestID != got.RequestID {
		t.Fatalf("refile must dedupe: %s then %s", got.RequestID, again.RequestID)
	}
	if len(filed) != 1 {
		t.Fatalf("one notify per filed ask, got %d", len(filed))
	}
	stored, err := b.Store.Request(got.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != protocol.RequestOpen || stored.GrantID != "grant-1" || stored.AgentID != agent.ID {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestRequestExpiresAndRefilesWithAudit(t *testing.T) {
	b, agent, _, upstream, _ := setup(t, protocol.Level1)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	req := protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	}
	got, err := b.Use(context.Background(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	first := got.RequestID

	now = now.Add(requestTTL + time.Minute)
	again, err := b.Use(context.Background(), agent, req)
	if err != nil {
		t.Fatal(err)
	}
	if again.RequestID == "" || again.RequestID == first {
		t.Fatalf("stale ask must be replaced by a fresh one: %s then %s", first, again.RequestID)
	}
	old, err := b.Store.Request(first)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != protocol.RequestExpired {
		t.Fatalf("old ask must be expired, got %s", old.Status)
	}
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	var sawExpired, sawFiled int
	for _, e := range events {
		switch e.Action {
		case protocol.ActionRequestExpired:
			sawExpired++
			if e.Reason != first {
				t.Fatalf("expired audit must name the dead ask, got %q", e.Reason)
			}
		case protocol.ActionRequestFiled:
			sawFiled++
		}
	}
	if sawExpired != 1 || sawFiled != 2 {
		t.Fatalf("audit: expired=%d filed=%d", sawExpired, sawFiled)
	}
	mustNoLeak(t, again)
}

func TestAgentCannotApprove(t *testing.T) {
	b, agent, _, _, _ := setup(t, protocol.Level1)
	if _, err := b.Approve(agent, "grant-1", time.Minute); err == nil {
		t.Fatal("agent approved")
	}
}

func TestApproveDoesNotConsultSqliteHumans(t *testing.T) {
	b, _, _, _, _ := setup(t, protocol.Level1)
	outsider := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "kratos-id", OrgID: "org-1"}
	if _, err := b.Approve(outsider, "grant-1", time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestNoGrantDeniedAndNoFetch(t *testing.T) {
	b, _, _, upstream, _ := setup(t, protocol.Level2)
	other := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-2", OrgID: "org-1"}
	if err := b.Store.PutAgent(other); err != nil {
		t.Fatal(err)
	}
	got, err := b.Use(context.Background(), other, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionDeny {
		t.Fatalf("decision=%s", got.Decision)
	}
	mustNoLeak(t, got)
}

func TestFetchOtherHostDenied(t *testing.T) {
	b, agent, _, _, _ := setup(t, protocol.Level2)
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: "https://evil.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reason != "host_not_allowed" {
		t.Fatalf("got %+v", got)
	}
}

func TestTOTPMintedAtInjectNeverReturned(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	code, err := totp.GenerateCode(seed, now)
	if err != nil {
		t.Fatal(err)
	}

	var sawAuth, sawTOTP string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawTOTP = r.Header.Get("X-TOTP")
		w.Header().Set("X-Echo-TOTP", sawTOTP)
		_, _ = io.WriteString(w, `{"token":"`+strings.TrimPrefix(sawAuth, "Bearer ")+`","totp":"`+sawTOTP+`","seed":"`+seed+`"}`)
	}))
	t.Cleanup(upstream.Close)

	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	item := protocol.Item{
		ID:      "item-1",
		OrgID:   "org-1",
		Name:    "stripe-live",
		Kind:    protocol.ItemAPIKey,
		Owner:   protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:    []string{upstream.URL},
		HasTOTP: true,
	}
	blob, err := material.Pack([]byte(secret), []byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(blob)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	b := New(mem)
	b.Now = func() time.Time { return now }

	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("%+v", got)
	}
	if sawAuth != "Bearer "+secret {
		t.Fatalf("auth=%q", sawAuth)
	}
	if sawTOTP != code {
		t.Fatalf("upstream totp=%q want %q", sawTOTP, code)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{secret, seed, code} {
		if scrub.Contains(raw, []byte(leak)) {
			t.Fatalf("leaked %q in %s", leak, raw)
		}
	}
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	mustNoLeak(t, events)
	if scrub.Contains(mustJSON(t, events), []byte(seed)) || scrub.Contains(mustJSON(t, events), []byte(code)) {
		t.Fatal("audit leaked totp material")
	}
}

func TestOAuthRefreshInjectsAccessTokenNeverRefresh(t *testing.T) {
	const refresh = "refresh_DO_NOT_LEAK"
	const access = "access_DO_NOT_LEAK"
	var sawRefresh string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sawRefresh = r.Form.Get("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"`+access+`","token_type":"Bearer","expires_in":3600}`)
	}))
	t.Cleanup(tokenSrv.Close)

	var sawAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, sawAuth)
	}))
	t.Cleanup(upstream.Close)

	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	blob, err := material.PackOAuth([]byte(refresh), []byte(tokenSrv.URL), []byte("client"), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.Item{
		ID:    "item-1",
		OrgID: "org-1",
		Name:  "gmail",
		Kind:  protocol.ItemOAuth,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:  []string{upstream.URL},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(blob)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := New(mem).Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("%+v", got)
	}
	if sawRefresh != refresh {
		t.Fatalf("token endpoint saw %q", sawRefresh)
	}
	if sawAuth != "Bearer "+access {
		t.Fatalf("auth=%q", sawAuth)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{refresh, access, "secret"} {
		if scrub.Contains(raw, []byte(leak)) {
			t.Fatalf("leaked %q in %s", leak, raw)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestChildEnvInjectsLevel2AndScrubsAudit(t *testing.T) {
	b, agent, _, _, sec := setup(t, protocol.Level2)
	pairs, err := b.ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	want := EnvName("stripe-live") + "=" + sec
	if len(pairs) != 1 || pairs[0] != want {
		t.Fatalf("%q", pairs)
	}
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != protocol.ActionEnv || events[0].Decision != protocol.DecisionAllow {
		t.Fatalf("audit=%+v", events)
	}
	mustNoLeak(t, events)
}

func TestChildEnvSkipsLevel1WithoutApproval(t *testing.T) {
	b, agent, _, _, _ := setup(t, protocol.Level1)
	pairs, err := b.ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("prompted via env: %q", pairs)
	}
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 ||
		events[0].Action != protocol.ActionRequestFiled ||
		events[1].Action != protocol.ActionEnv || events[1].Decision != protocol.DecisionNeedApproval {
		t.Fatalf("audit=%+v", events)
	}
	at := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	if open, err := b.Store.ListRequests("org-1", protocol.RequestOpen, at); err != nil || len(open) != 1 {
		t.Fatalf("env denial must file the ask: %v %+v", err, open)
	}
	mustNoLeak(t, events)
}

func TestChildEnvSkipsSSH(t *testing.T) {
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	item := protocol.Item{
		ID:    "github",
		OrgID: "org-1",
		Name:  "github",
		Kind:  protocol.ItemSSH,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret([]byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n"))); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	pairs, err := New(mem).ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("ssh in env: %q", pairs)
	}
}

func TestChildEnvSkipsPasskey(t *testing.T) {
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	item := protocol.Item{
		ID:    "pk-github",
		OrgID: "org-1",
		Name:  "pk-github",
		Kind:  protocol.ItemPasskey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:  []string{"https://github.com"},
	}
	pem := []byte("-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----")
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(pem)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch, protocol.ActionEnv},
	}); err != nil {
		t.Fatal(err)
	}
	b := New(mem)
	pairs, err := b.ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("passkey in env: %q", pairs)
	}
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: item.ID,
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: "https://github.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionDeny || got.Reason != "not_injectable" {
		t.Fatalf("%+v", got)
	}
}

func TestCardUseIsNotInjectable(t *testing.T) {
	const pan = "4111111111111111"
	blob, err := material.PackCard(pan, "12", "2030", "123", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	item := protocol.Item{
		ID:    "amex",
		OrgID: "org-1",
		Name:  "amex",
		Kind:  protocol.ItemCard,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:  []string{"https://www.amazon.com"},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(blob)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch, protocol.ActionEnv},
	}); err != nil {
		t.Fatal(err)
	}
	b := New(mem)
	pairs, err := b.ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("card in env: %q", pairs)
	}
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: item.ID,
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: "https://www.amazon.com/checkout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionDeny || got.Reason != "not_injectable" {
		t.Fatalf("%+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(pan)) || scrub.Contains(raw, []byte("123")) {
		t.Fatalf("pan in use: %s", raw)
	}
}

func TestChildEnvSkipsFileAndArchived(t *testing.T) {
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	file := protocol.Item{
		ID:      "note",
		OrgID:   "org-1",
		Name:    "note",
		Kind:    protocol.ItemFile,
		Owner:   protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		HasFile: true,
	}
	archived := protocol.Item{
		ID:       "old",
		OrgID:    "org-1",
		Name:     "old",
		Kind:     protocol.ItemAPIKey,
		Owner:    protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		Archived: true,
	}
	if err := mem.PutItem(file, store.Secret([]byte("file-secret"))); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(archived, store.Secret([]byte(secret))); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"note", "old"} {
		if err := mem.PutGrant(protocol.Grant{
			ID:      "g-" + id,
			OrgID:   "org-1",
			AgentID: agent.ID,
			ItemID:  id,
			Level:   protocol.Level2,
			Actions: []protocol.ActionKind{protocol.ActionFetch},
		}); err != nil {
			t.Fatal(err)
		}
	}
	pairs, err := New(mem).ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("env=%q", pairs)
	}
}

func TestChildEnvMintsTOTPNotSeed(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	blob, err := material.Pack([]byte(secret), []byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.Item{
		ID:      "stripe",
		OrgID:   "org-1",
		Name:    "stripe",
		Kind:    protocol.ItemAPIKey,
		Owner:   protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		HasTOTP: true,
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(blob)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	b := New(mem)
	b.Now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }
	pairs, err := b.ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(pairs, "\n")
	if !strings.HasPrefix(joined, "STRIPE="+secret+"\nSTRIPE_TOTP=") {
		t.Fatalf("%q", pairs)
	}
	code := strings.TrimPrefix(pairs[1], "STRIPE_TOTP=")
	if len(code) != 6 {
		t.Fatalf("totp %q", code)
	}
	if strings.Contains(joined, seed) {
		t.Fatal("seed in env")
	}
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	mustNoLeak(t, events)
	if scrub.Contains(mustJSON(t, events), []byte(seed)) {
		t.Fatal("seed in audit")
	}
}

func TestUseLogHasNoSecret(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	b, agent, _, upstream, _ := setup(t, protocol.Level2)
	// Secret in path AND query — webhook-style URLs carry the credential in
	// the path segment, so the log line must show host only.
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/services/" + secret + "?token=" + secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("decision=%s", got.Decision)
	}
	line := buf.String()
	if strings.Contains(line, secret) {
		t.Fatal("secret in slog")
	}
	if !strings.Contains(line, `"item":"stripe-live"`) || !strings.Contains(line, `"decision":"allow"`) {
		t.Fatalf("log=%s", line)
	}
	if strings.Contains(line, "token=") {
		t.Fatal("query in slog")
	}
}

func TestUseSpanHasNoSecret(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	b, agent, _, upstream, _ := setup(t, protocol.Level2)
	// Secret in path AND query — webhook-style URLs carry the credential in
	// the path segment, so veil.host must be scheme://host and nothing more.
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/services/" + secret + "?token=" + secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("decision=%s", got.Decision)
	}
	ended := sr.Ended()
	if len(ended) < 2 {
		t.Fatalf("spans=%d", len(ended))
	}
	var sawUse bool
	for _, sp := range ended {
		for _, a := range sp.Attributes() {
			if strings.Contains(a.Value.AsString(), secret) || strings.Contains(a.Value.AsString(), "token=") {
				t.Fatalf("secret in span %s %s=%s", sp.Name(), a.Key, a.Value.AsString())
			}
		}
		if sp.Name() == "use" {
			sawUse = true
		}
	}
	if !sawUse {
		t.Fatal("missing use span")
	}
}

func TestUseReloadsAgentAndDeniesRevoked(t *testing.T) {
	b, agent, _, upstream, _ := setup(t, protocol.Level2)
	mem := b.Store.(*store.Memory)
	now := time.Now().UTC()
	if err := mem.RevokeAgent(agent.ID, now); err != nil {
		t.Fatal(err)
	}
	// Pass a stale, non-revoked copy of the principal. Broker should reload and deny.
	stale := agent
	got, err := b.Use(context.Background(), stale, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionDeny || got.Reason != "agent_revoked" {
		t.Fatalf("got %+v", got)
	}
}

func TestUseInFlightLimit(t *testing.T) {
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}

	blocker := make(chan struct{})
	started := make(chan struct{}, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-blocker
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	item := protocol.Item{
		ID:    "item-1",
		OrgID: "org-1",
		Name:  "stripe-live",
		Kind:  protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:  []string{upstream.URL},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(secret)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}

	b := NewWithInFlight(mem, 2)
	b.Now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }

	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 3)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.Use(ctx, agent, protocol.UseRequest{
				ItemID: item.ID,
				Action: protocol.ActionFetch,
				Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1"},
			})
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		<-started
	}

	_, err := b.Use(ctx, agent, protocol.UseRequest{
		ItemID: item.ID,
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1"},
	})
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("third Use got %v, want ErrOverloaded", err)
	}

	close(blocker)
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatalf("in-flight Use returned error: %v", e)
		}
	}
}

func TestUseInFlightLimitReleasesOnDenial(t *testing.T) {
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"}
	item := protocol.Item{
		ID:    "item-1",
		OrgID: "org-1",
		Name:  "file",
		Kind:  protocol.ItemFile,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
		URIs:  []string{"http://localhost"},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(secret)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID:      "grant-1",
		OrgID:   "org-1",
		AgentID: agent.ID,
		ItemID:  item.ID,
		Level:   protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}

	b := NewWithInFlight(mem, 1)
	b.Now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }

	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: item.ID,
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: "http://localhost/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionDeny || got.Reason != "not_injectable" {
		t.Fatalf("first use got %+v", got)
	}

	got2, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: item.ID,
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: "http://localhost/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got2.Decision != protocol.DecisionDeny || got2.Reason != "not_injectable" {
		t.Fatalf("second use got %+v", got2)
	}
}

// failAuditStore makes every audit write fail — ConsumeSessionAudited before
// its commit boundary — to prove allows fail closed with nothing disclosed.
type failAuditStore struct {
	store.Store
}

func (failAuditStore) AppendAudit(protocol.AuditEvent) error {
	return errors.New("audit store unavailable")
}

func (f failAuditStore) ConsumeSessionAudited([]byte, time.Time, protocol.AuditEvent) (protocol.Principal, error) {
	return protocol.Principal{}, errors.New("audit store unavailable")
}

func TestUseAuditFailureFailsClosed(t *testing.T) {
	b, agent, _, upstream, _ := setup(t, protocol.Level2)
	fb := New(failAuditStore{Store: b.Store})
	fb.Now = b.Now

	req := protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL},
	}
	if _, err := fb.Use(context.Background(), agent, req); err == nil {
		t.Fatal("agent-token use must fail closed when the audit write fails")
	}
	events, err := b.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("phantom audit rows=%d", len(events))
	}

	// Session-token path: the audit failure must roll the consume back —
	// uses stays 0 and no secret is released.
	sess := protocol.Session{
		ID: "ses_fail", OrgID: agent.OrgID, AgentID: agent.ID,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	hash := sha256.Sum256([]byte("ses_fail_token"))
	if err := b.Store.PutSession(sess, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := fb.UseSession(context.Background(), hash[:], req); err == nil {
		t.Fatal("session-token use must fail closed when the audited consume fails")
	}
	got, err := b.Store.SessionByHash(hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if got.Uses != 0 {
		t.Fatalf("consume not rolled back: uses=%d", got.Uses)
	}
}
