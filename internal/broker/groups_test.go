package broker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// groupSetup is setup() without the direct grant: the org-owned item, the
// org agent, an "eng" group, and (when grant is set) the group grant.
func groupSetup(t *testing.T, grantLevel protocol.GrantLevel, member bool) (*Broker, protocol.Principal, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Auth", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(upstream.Close)

	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org"}
	item := protocol.Item{
		ID: "item-1", OrgID: "org", Name: "stripe-live", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
		URIs:  []string{upstream.URL},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(secret)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGroup(protocol.Group{ID: "g-eng", OrgID: "org", Name: "eng"}); err != nil {
		t.Fatal(err)
	}
	if member {
		if err := mem.AddGroupMember("g-eng", protocol.GroupMember{
			GroupID: "g-eng", MemberKind: protocol.PrincipalAgent, MemberID: "claude",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if grantLevel != "" {
		if err := mem.PutGrant(protocol.Grant{
			ID: "grp-eng-item", OrgID: "org", AgentID: "g-eng",
			SubjectKind: protocol.SubjectGroup,
			ItemID:      "item-1", Level: grantLevel,
			Actions: []protocol.ActionKind{protocol.ActionFetch},
		}); err != nil {
			t.Fatal(err)
		}
	}
	b := New(mem)
	b.Now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }
	return b, agent, upstream
}

func useFetch(t *testing.T, b *Broker, agent protocol.Principal, upstream *httptest.Server) protocol.UseResult {
	t.Helper()
	got, err := b.Use(context.Background(), agent, protocol.UseRequest{
		ItemID: "item-1",
		Action: protocol.ActionFetch,
		Fetch:  &protocol.Fetch{URL: upstream.URL + "/v1/customers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestGroupGrantAllowsUse(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.Level2, true)
	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("decision=%s reason=%s", got.Decision, got.Reason)
	}
	mustNoLeak(t, got)
}

func TestGroupDenyBlocksUse(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.LevelDeny, true)
	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionDeny || got.Reason != "grant_denied" {
		t.Fatalf("group deny: %+v", got)
	}
	if got.Fetch != nil {
		t.Fatal("denied fetch must not run")
	}
	mustNoLeak(t, got)
}

func TestNoMembershipNoGrant(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.Level2, false)
	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionDeny {
		t.Fatalf("non-member: %+v", got)
	}
	mustNoLeak(t, got)
}

func TestMembershipExpandContract(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.Level2, false)
	mem := b.Store
	m := protocol.GroupMember{MemberKind: protocol.PrincipalAgent, MemberID: "claude"}

	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionDeny {
		t.Fatalf("before join: %+v", got)
	}
	if err := mem.AddGroupMember("g-eng", m); err != nil {
		t.Fatal(err)
	}
	got = useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("after join: %+v", got)
	}
	if err := mem.RemoveGroupMember("g-eng", m); err != nil {
		t.Fatal(err)
	}
	got = useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionDeny {
		t.Fatalf("after leave: %+v", got)
	}
}

// A user-owned agent inherits its human's membership — the team model:
// humans sit in groups, their agents work inside the shared vault.
func TestGroupGrantOwnerHumanUse(t *testing.T) {
	b, _, upstream := groupSetup(t, protocol.Level2, false)
	mem := b.Store
	if err := mem.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "ada", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	junior := protocol.Principal{
		Kind: protocol.PrincipalAgent, ID: "junior", OrgID: "org",
		Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: "ada"},
	}
	if err := mem.PutAgent(junior); err != nil {
		t.Fatal(err)
	}
	if err := mem.AddGroupMember("g-eng", protocol.GroupMember{
		MemberKind: protocol.PrincipalHuman, MemberID: "ada",
	}); err != nil {
		t.Fatal(err)
	}
	got := useFetch(t, b, junior, upstream)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("owner-inherited group grant: %+v", got)
	}
	mustNoLeak(t, got)
}

func TestDirectDenyShadowsGroupAllow(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.Level2, true)
	if err := b.Store.PutGrant(protocol.Grant{
		ID: "claude:item-1", OrgID: "org", AgentID: "claude",
		ItemID: "item-1", Level: protocol.LevelDeny,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionDeny || got.Reason != "grant_denied" {
		t.Fatalf("direct deny should shadow group allow: %+v", got)
	}
}

func TestDirectAllowShadowsGroupDeny(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.LevelDeny, true)
	if err := b.Store.PutGrant(protocol.Grant{
		ID: "claude:item-1", OrgID: "org", AgentID: "claude",
		ItemID: "item-1", Level: protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("direct allow should shadow group deny: %+v", got)
	}
}

// need_approval flows through a group grant untouched — the inherited edge
// is a level1 grant like any other.
func TestGroupLevel1NeedsApproval(t *testing.T) {
	b, agent, upstream := groupSetup(t, protocol.Level1, true)
	got := useFetch(t, b, agent, upstream)
	if got.Decision != protocol.DecisionNeedApproval {
		t.Fatalf("group level1: %+v", got)
	}
	if got.RequestID == "" {
		t.Fatal("need_approval must file the ask against the group grant")
	}
	mustNoLeak(t, got)
}

// ChildEnv injects group-granted items exactly like direct grants — the
// shared vault shows up in `run --inject`.
func TestChildEnvGroupGrant(t *testing.T) {
	mem := store.NewMemory()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org"}
	item := protocol.Item{
		ID: "item-1", OrgID: "org", Name: "api_token", Kind: protocol.ItemAPIKey,
		Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"},
	}
	if err := mem.PutAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutItem(item, store.Secret(secret)); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGroup(protocol.Group{ID: "g-eng", OrgID: "org", Name: "eng"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.AddGroupMember("g-eng", protocol.GroupMember{
		MemberKind: protocol.PrincipalAgent, MemberID: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.PutGrant(protocol.Grant{
		ID: "grp-eng-item", OrgID: "org", AgentID: "g-eng",
		SubjectKind: protocol.SubjectGroup,
		ItemID:      "item-1", Level: protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
	b := New(mem)
	b.Now = func() time.Time { return time.Now().UTC() }
	env, err := b.ChildEnv(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, kv := range env {
		if kv == "API_TOKEN="+secret {
			found = true
		}
	}
	if !found {
		t.Fatalf("group-granted env not injected: %v", env)
	}
}
