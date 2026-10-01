package grant

import (
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

func fixture(level protocol.GrantLevel) Input {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	return Input{
		Principal: protocol.Principal{Kind: protocol.PrincipalAgent, ID: "agent-1", OrgID: "org-1"},
		Item: protocol.Item{
			ID:    "item-1",
			OrgID: "org-1",
			Name:  "stripe-live",
			Kind:  protocol.ItemAPIKey,
			Owner: protocol.Owner{Kind: protocol.OwnerOrg, ID: "org-1"},
			URIs:  []string{"https://api.stripe.com"},
		},
		Grant: &protocol.Grant{
			ID:      "grant-1",
			OrgID:   "org-1",
			AgentID: "agent-1",
			ItemID:  "item-1",
			Level:   level,
			Actions: []protocol.ActionKind{protocol.ActionFetch},
		},
		Action:    protocol.ActionFetch,
		TargetURL: "https://api.stripe.com/v1/customers",
		Now:       now,
	}
}

func TestLevel2AllowsWithoutApproval(t *testing.T) {
	got := Evaluate(fixture(protocol.Level2))
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("decision=%s reason=%s", got.Decision, got.Reason)
	}
}

func TestLevel1NeedsApproval(t *testing.T) {
	got := Evaluate(fixture(protocol.Level1))
	if got.Decision != protocol.DecisionNeedApproval {
		t.Fatalf("decision=%s reason=%s", got.Decision, got.Reason)
	}
}

func TestLevel1AllowsWithLiveApproval(t *testing.T) {
	in := fixture(protocol.Level1)
	in.Approval = &protocol.Approval{
		ID:        "appr-1",
		GrantID:   "grant-1",
		HumanID:   "human-1",
		ExpiresAt: in.Now.Add(time.Minute),
	}
	got := Evaluate(in)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("decision=%s reason=%s", got.Decision, got.Reason)
	}
	if got.ApprovalID != "appr-1" {
		t.Fatalf("approval id=%s", got.ApprovalID)
	}
}

func TestExpiredApprovalNeedsAgain(t *testing.T) {
	in := fixture(protocol.Level1)
	in.Approval = &protocol.Approval{
		ID:        "appr-1",
		GrantID:   "grant-1",
		HumanID:   "human-1",
		ExpiresAt: in.Now,
	}
	got := Evaluate(in)
	if got.Decision != protocol.DecisionNeedApproval || got.Reason != "approval_expired" {
		t.Fatalf("got %+v", got)
	}
}

func TestHumanCannotUse(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Principal.Kind = protocol.PrincipalHuman
	in.Principal.ID = "human-1"
	got := Evaluate(in)
	if got.Decision != protocol.DecisionDeny || got.Reason != "human_cannot_use" {
		t.Fatalf("got %+v", got)
	}
}

func TestWrongAgentDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Principal.ID = "agent-other"
	got := Evaluate(in)
	if got.Reason != "wrong_agent" {
		t.Fatalf("got %+v", got)
	}
}

func TestHostNotOnItemDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	in.TargetURL = "https://evil.example/exfil"
	got := Evaluate(in)
	if got.Reason != "host_not_allowed" {
		t.Fatalf("got %+v", got)
	}
}

func TestArchivedItemDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Item.Archived = true
	got := Evaluate(in)
	if got.Reason != "item_archived" {
		t.Fatalf("got %+v", got)
	}
}

func TestExpiredGrantDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	exp := in.Now.Add(-time.Second)
	in.Grant.ExpiresAt = &exp
	got := Evaluate(in)
	if got.Reason != "grant_expired" {
		t.Fatalf("got %+v", got)
	}
}

func TestActionNotOnGrantDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Grant.Actions = nil
	got := Evaluate(in)
	if got.Reason != "action_not_allowed" {
		t.Fatalf("got %+v", got)
	}
}

func TestEnvAllowedWhenFetchIsOnGrant(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Action = protocol.ActionEnv
	in.TargetURL = ""
	got := Evaluate(in)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("got %+v", got)
	}
}

func TestEnvNeedsApprovalAtLevel1(t *testing.T) {
	in := fixture(protocol.Level1)
	in.Action = protocol.ActionEnv
	in.TargetURL = ""
	got := Evaluate(in)
	if got.Decision != protocol.DecisionNeedApproval {
		t.Fatalf("got %+v", got)
	}
}

func TestNoGrantDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Grant = nil
	got := Evaluate(in)
	if got.Reason != "no_grant" {
		t.Fatalf("got %+v", got)
	}
}

func TestRevokedAgentDenied(t *testing.T) {
	in := fixture(protocol.Level2)
	revoked := in.Now.Add(-time.Second)
	in.Principal.RevokedAt = &revoked
	got := Evaluate(in)
	if got.Decision != protocol.DecisionDeny || got.Reason != "agent_revoked" {
		t.Fatalf("got %+v", got)
	}
}

func TestHTTPSDefaultPortMatchesBareHost(t *testing.T) {
	in := fixture(protocol.Level2)
	in.Item.URIs = []string{"https://api.stripe.com"}
	in.TargetURL = "https://api.stripe.com:443/v1/customers"
	got := Evaluate(in)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("got %+v", got)
	}
}

func TestCanonicalHostCONNECT(t *testing.T) {
	u, err := ParseDest("api.stripe.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if CanonicalHost(u) != "api.stripe.com" {
		t.Fatalf("%q", CanonicalHost(u))
	}
}

func TestRegistrableIsETLDPlusOne(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://dash.cloudflare.com/login", "cloudflare.com"},
		{"https://github.com/login", "github.com"},
		{"https://www.amazon.com/checkout", "amazon.com"},
		{"https://www.amazon.co.uk/dp/1", "amazon.co.uk"},
		{"github.com", "github.com"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := Registrable(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	if Registrable("https://www.amazon.com/checkout") == Registrable("https://github.com") {
		t.Fatal("amazon and github share a scope")
	}
}

func FuzzRegistrable(f *testing.F) {
	f.Add("https://dash.cloudflare.com/login")
	f.Add("https://www.amazon.co.uk/dp/1")
	f.Add("github.com")
	f.Add("")
	f.Fuzz(func(t *testing.T, raw string) {
		got := Registrable(raw)
		if got != strings.ToLower(got) {
			t.Fatalf("not lower %q", got)
		}
		if strings.ContainsAny(got, "/?#") {
			t.Fatalf("path in scope %q from %q", got, raw)
		}
	})
}

func FuzzHostAllowed(f *testing.F) {
	f.Add("https://api.stripe.com", "https://api.stripe.com/v1/customers")
	f.Add("https://github.com", "https://amazon.com")
	f.Fuzz(func(t *testing.T, uri, target string) {
		_ = HostAllowed(protocol.Item{URIs: []string{uri}}, target)
	})
}

func groupFixture(level protocol.GrantLevel, groupID string, groups map[string]struct{}) Input {
	in := fixture(level)
	in.Grant.AgentID = groupID
	in.Grant.SubjectKind = protocol.SubjectGroup
	in.Groups = groups
	return in
}

func TestDenyGrantDenies(t *testing.T) {
	got := Evaluate(fixture(protocol.LevelDeny))
	if got.Decision != protocol.DecisionDeny || got.Reason != "grant_denied" {
		t.Fatalf("deny grant: %+v", got)
	}
}

func TestGroupGrantNeedsMembership(t *testing.T) {
	in := groupFixture(protocol.Level2, "g-eng", nil)
	got := Evaluate(in)
	if got.Decision != protocol.DecisionDeny || got.Reason != "wrong_group" {
		t.Fatalf("no membership: %+v", got)
	}
	in.Groups = map[string]struct{}{"g-eng": {}}
	got = Evaluate(in)
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("member: %+v", got)
	}
}

func TestSelect(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	mk := func(id string, kind protocol.SubjectKind, subject string, level protocol.GrantLevel) protocol.Grant {
		return protocol.Grant{ID: id, AgentID: subject, SubjectKind: kind, ItemID: "item-1", Level: level}
	}
	direct := mk("d1", protocol.SubjectAgent, "agent-1", protocol.Level1)
	grpDeny := mk("g1", protocol.SubjectGroup, "g-eng", protocol.LevelDeny)
	grpL2 := mk("g2", protocol.SubjectGroup, "g-ops", protocol.Level2)
	grpL1 := mk("g3", protocol.SubjectGroup, "g-eng", protocol.Level1)

	cases := []struct {
		name  string
		cands []protocol.Grant
		appr  map[string]bool
		want  string
	}{
		{"empty", nil, nil, ""},
		{"direct shadows group deny", []protocol.Grant{grpDeny, direct}, nil, "d1"},
		{"deny beats group allow", []protocol.Grant{grpL2, grpDeny}, nil, "g1"},
		{"level2 beats bare level1", []protocol.Grant{grpL1, grpL2}, nil, "g2"},
		{"approved level1 beats bare level1", []protocol.Grant{grpL1, grpL2}, nil, "g2"},
		{"approved beats unresolvable", []protocol.Grant{grpL1, {ID: "g4", AgentID: "g-x", SubjectKind: protocol.SubjectGroup, ItemID: "item-1", Level: protocol.Level1}}, map[string]bool{"g4": true}, "g4"},
		{"expired loses to live", []protocol.Grant{direct, grpL2}, nil, "d1"},
	}
	// expired variants
	expiredDirect := mk("d-exp", protocol.SubjectAgent, "agent-1", protocol.Level2)
	expiredDirect.ExpiresAt = &past
	cases = append(cases,
		struct {
			name  string
			cands []protocol.Grant
			appr  map[string]bool
			want  string
		}{"expired direct loses to live group", []protocol.Grant{expiredDirect, grpL2}, nil, "g2"},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Select(tc.cands, now, tc.appr)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("want nil got %v", got.ID)
				}
				return
			}
			if got == nil || got.ID != tc.want {
				t.Fatalf("want %q got %+v", tc.want, got)
			}
		})
	}
}
