package store

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

// seedGroupStore plants an org-owned item, an org-owned agent, and a user
// owner whose agent "junior" inherits her memberships.
func seedGroupStore(t *testing.T, s Store) {
	t.Helper()
	org := protocol.Owner{Kind: protocol.OwnerOrg, ID: "org"}
	if err := s.PutItem(protocol.Item{
		ID: "stripe", OrgID: "org", Name: "stripe", Kind: protocol.ItemAPIKey,
		Owner: org, URIs: []string{"https://api.stripe.com"},
	}, Secret("sk_live_secret")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgent(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: "org", Owner: org}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "ada", OrgID: "org"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgent(protocol.Principal{
		Kind: protocol.PrincipalAgent, ID: "junior", OrgID: "org",
		Owner: protocol.Owner{Kind: protocol.OwnerUser, ID: "ada"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGroup(protocol.Group{ID: "g-eng", OrgID: "org", Name: "eng"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGroup(protocol.Group{ID: "g-ops", OrgID: "org", Name: "ops"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGrant(protocol.Grant{
		ID: "grp-eng-stripe", OrgID: "org", AgentID: "g-eng",
		SubjectKind: protocol.SubjectGroup,
		ItemID:      "stripe", Level: protocol.Level2,
		Actions: []protocol.ActionKind{protocol.ActionFetch},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGroupRoundTrip(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			if err := s.PutGroup(protocol.Group{ID: "g-eng", OrgID: "org", Name: "eng"}); err != nil {
				t.Fatal(err)
			}
			g, err := s.Group("g-eng")
			if err != nil {
				t.Fatal(err)
			}
			if g.Name != "eng" || g.OrgID != "org" {
				t.Fatalf("group %+v", g)
			}
			if _, err := s.Group("nope"); err != ErrNotFound {
				t.Fatalf("missing group err=%v", err)
			}
			if err := s.PutGroup(protocol.Group{ID: "g-eng2", OrgID: "org", Name: "eng"}); err == nil {
				t.Fatal("duplicate org/name should fail")
			}
			groups, err := s.ListGroups()
			if err != nil {
				t.Fatal(err)
			}
			if len(groups) != 1 {
				t.Fatalf("groups %+v", groups)
			}
			m := protocol.GroupMember{MemberKind: protocol.PrincipalAgent, MemberID: "claude"}
			if err := s.AddGroupMember("g-eng", m); err != nil {
				t.Fatal(err)
			}
			// Idempotent re-add.
			if err := s.AddGroupMember("g-eng", m); err != nil {
				t.Fatal(err)
			}
			members, err := s.GroupMembers("g-eng")
			if err != nil {
				t.Fatal(err)
			}
			if len(members) != 1 || members[0].MemberID != "claude" {
				t.Fatalf("members %+v", members)
			}
			if err := s.RemoveGroupMember("g-eng", m); err != nil {
				t.Fatal(err)
			}
			// Idempotent re-remove.
			if err := s.RemoveGroupMember("g-eng", m); err != nil {
				t.Fatal(err)
			}
			members, err = s.GroupMembers("g-eng")
			if err != nil {
				t.Fatal(err)
			}
			if len(members) != 0 {
				t.Fatalf("members %+v", members)
			}
			if err := s.AddGroupMember("nope", m); err != ErrNotFound {
				t.Fatalf("member add to missing group err=%v", err)
			}
			if _, err := s.GroupMembers("nope"); err != ErrNotFound {
				t.Fatalf("members of missing group err=%v", err)
			}
		})
	}
}

func TestGroupGrantExpandsOnUseAuth(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			now := time.Now().UTC()

			// No membership: no grant.
			auth, err := s.UseAuth("claude", "stripe", now)
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant != nil {
				t.Fatalf("grant without membership: %+v", auth.Grant)
			}
			if len(auth.Groups) != 0 {
				t.Fatalf("groups without membership: %v", auth.Groups)
			}

			// Add the agent — access expands.
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			auth, err = s.UseAuth("claude", "stripe", now)
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.ID != "grp-eng-stripe" {
				t.Fatalf("grant after member add: %+v", auth.Grant)
			}
			if auth.Grant.Subject() != protocol.SubjectGroup {
				t.Fatalf("subject kind %q", auth.Grant.SubjectKind)
			}
			if len(auth.Groups) != 1 || auth.Groups[0] != "g-eng" {
				t.Fatalf("groups %v", auth.Groups)
			}

			// Remove the member — access contracts.
			if err := s.RemoveGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			auth, err = s.UseAuth("claude", "stripe", now)
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant != nil {
				t.Fatalf("grant after member remove: %+v", auth.Grant)
			}
		})
	}
}

// TestGroupGrantOwnerHuman: a user-owned agent inherits its human's group
// memberships — the team model where humans join groups and their agents
// Use the shared vault.
func TestGroupGrantOwnerHuman(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalHuman, MemberID: "ada",
			}); err != nil {
				t.Fatal(err)
			}
			auth, err := s.UseAuth("junior", "stripe", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.ID != "grp-eng-stripe" {
				t.Fatalf("owner-human grant: %+v", auth.Grant)
			}
			if len(auth.Groups) != 1 || auth.Groups[0] != "g-eng" {
				t.Fatalf("groups %v", auth.Groups)
			}
		})
	}
}

// Direct grant beats group grant — specificity wins even over a group deny.
func TestUseAuthDirectBeatsGroup(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			// Group DENY + direct level2 → direct wins.
			deny := protocol.Grant{
				ID: "grp-eng-deny", OrgID: "org", AgentID: "g-eng",
				SubjectKind: protocol.SubjectGroup,
				ItemID:      "stripe", Level: protocol.LevelDeny,
				Actions: []protocol.ActionKind{protocol.ActionFetch},
			}
			if err := s.PutGrant(deny); err != nil {
				t.Fatal(err)
			}
			direct := protocol.Grant{
				ID: "claude:stripe", OrgID: "org", AgentID: "claude",
				SubjectKind: protocol.SubjectAgent,
				ItemID:      "stripe", Level: protocol.Level2,
				Actions: []protocol.ActionKind{protocol.ActionFetch},
			}
			if err := s.PutGrant(direct); err != nil {
				t.Fatal(err)
			}
			auth, err := s.UseAuth("claude", "stripe", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.ID != "claude:stripe" {
				t.Fatalf("direct should shadow group deny: %+v", auth.Grant)
			}
		})
	}
}

// Within the group tier, deny beats allow across different groups.
func TestUseAuthGroupDenyBeatsAllow(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGroupMember("g-ops", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.PutGrant(protocol.Grant{
				ID: "grp-ops-deny", OrgID: "org", AgentID: "g-ops",
				SubjectKind: protocol.SubjectGroup,
				ItemID:      "stripe", Level: protocol.LevelDeny,
				Actions: []protocol.ActionKind{protocol.ActionFetch},
			}); err != nil {
				t.Fatal(err)
			}
			auth, err := s.UseAuth("claude", "stripe", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.Level != protocol.LevelDeny {
				t.Fatalf("deny should win the group tier: %+v", auth.Grant)
			}
		})
	}
}

// An expired direct edge is dead — it must not shadow a live group grant,
// but alone it still surfaces grant_expired.
func TestUseAuthExpiredEdgeDoesNotShadow(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			past := time.Now().UTC().Add(-time.Hour)
			if err := s.PutGrant(protocol.Grant{
				ID: "claude:stripe", OrgID: "org", AgentID: "claude",
				ItemID: "stripe", Level: protocol.Level2, ExpiresAt: &past,
				Actions: []protocol.ActionKind{protocol.ActionFetch},
			}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()

			// Expired alone → the grant row, so the broker reports
			// grant_expired rather than no_grant.
			auth, err := s.UseAuth("claude", "stripe", now)
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.ID != "claude:stripe" {
				t.Fatalf("expired alone should surface: %+v", auth.Grant)
			}

			// Live group grant → it beats the dead edge.
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			auth, err = s.UseAuth("claude", "stripe", now)
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.ID != "grp-eng-stripe" {
				t.Fatalf("live group grant should beat expired direct: %+v", auth.Grant)
			}
		})
	}
}

// Group ops carry their audit events atomically.
func TestGroupOpsAudit(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			ev := func(action protocol.ActionKind, reason string) protocol.AuditEvent {
				return protocol.AuditEvent{
					Time: time.Now().UTC(), OrgID: "org", AgentID: "self",
					Action: action, Decision: protocol.DecisionAllow, Reason: reason,
				}
			}
			if err := s.PutGroup(protocol.Group{ID: "g-eng", OrgID: "org", Name: "eng"},
				ev(protocol.ActionGroupCreated, "group=eng")); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{MemberKind: protocol.PrincipalAgent, MemberID: "claude"},
				ev(protocol.ActionGroupMemberAdded, "group=eng member=agent:claude")); err != nil {
				t.Fatal(err)
			}
			if err := s.RemoveGroupMember("g-eng", protocol.GroupMember{MemberKind: protocol.PrincipalAgent, MemberID: "claude"},
				ev(protocol.ActionGroupMemberRemoved, "group=eng member=agent:claude")); err != nil {
				t.Fatal(err)
			}
			rows, err := s.Audit()
			if err != nil {
				t.Fatal(err)
			}
			seen := map[protocol.ActionKind]string{}
			for _, r := range rows {
				seen[r.Action] = r.Reason
			}
			for _, want := range []protocol.ActionKind{
				protocol.ActionGroupCreated, protocol.ActionGroupMemberAdded, protocol.ActionGroupMemberRemoved,
			} {
				if _, ok := seen[want]; !ok {
					t.Fatalf("missing audit %s in %v", want, seen)
				}
			}
			if seen[protocol.ActionGroupCreated] != "group=eng" {
				t.Fatalf("group_created reason %q", seen[protocol.ActionGroupCreated])
			}
		})
	}
}

// A grant to a group that does not exist in the org is a dead edge — the
// approve path must refuse it (it could never mint a usable approval).
func TestGroupGrantApproveLiveness(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			// A live group grant approves.
			ok, err := s.ApproveGrant("grp-eng-stripe", protocol.Approval{
				ID: "ap-1", GrantID: "grp-eng-stripe", HumanID: "ada",
				ExpiresAt: time.Now().UTC().Add(time.Hour),
			}, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if len(ok) != 0 {
				t.Fatalf("unexpected sibling resolutions %v", ok)
			}
			appr, err := s.LiveApproval("grp-eng-stripe", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if appr == nil {
				t.Fatal("approval not live")
			}
		})
	}
}

// Agent tokens under a session get the same expansion — no second store.
func TestSessionGroupGrant(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			seedGroupStore(t, s)
			sess := protocol.Session{
				ID: "ses_1", OrgID: "org", AgentID: "claude",
				CreatedAt: time.Now().UTC().Add(-time.Minute),
				ExpiresAt: time.Now().UTC().Add(time.Hour),
				TTL:       int64(time.Hour.Seconds()),
			}
			h := sha256.Sum256([]byte("token"))
			sum := h[:]
			if err := s.PutSession(sess, sum); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGroupMember("g-eng", protocol.GroupMember{
				MemberKind: protocol.PrincipalAgent, MemberID: "claude",
			}); err != nil {
				t.Fatal(err)
			}
			auth, err := s.UseAuthSession(sum, "stripe", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if auth.Grant == nil || auth.Grant.ID != "grp-eng-stripe" {
				t.Fatalf("session group grant: %+v", auth.Grant)
			}
		})
	}
}

// GroupIDsFor pairs kind+id so a human whose id equals an agent id does not
// inherit the agent's groups.
func TestGroupIDsForPairsKindAndID(t *testing.T) {
	for _, s := range testStores(t) {
		name := fmt.Sprintf("%T", s)
		t.Run(name, func(t *testing.T) {
			defer s.Close()
			if err := s.PutGroup(protocol.Group{ID: "g1", OrgID: "org", Name: "g1"}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddGroupMember("g1", protocol.GroupMember{MemberKind: protocol.PrincipalAgent, MemberID: "same"}); err != nil {
				t.Fatal(err)
			}
			ids, err := s.GroupIDsFor([]protocol.GroupMember{{MemberKind: protocol.PrincipalHuman, MemberID: "same"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(ids) != 0 {
				t.Fatalf("kind-paired lookup leaked: %v", ids)
			}
			ids, err = s.GroupIDsFor([]protocol.GroupMember{{MemberKind: protocol.PrincipalAgent, MemberID: "same"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(ids) != 1 || ids[0] != "g1" {
				t.Fatalf("agent membership: %v", ids)
			}
		})
	}
}
