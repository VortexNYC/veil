package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
	"github.com/VortexNYC/veil/internal/store"
)

// mustNoAppLeak fails if marshaled agent-visible output carries secret
// material — item metadata in list results, never vault bytes.
func mustNoAppLeak(t *testing.T, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatalf("secret in json: %s", raw)
	}
}

func groupApp(t *testing.T) *App {
	t.Helper()
	a, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func self(a *App) protocol.Principal {
	return protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
}

func seedGroupApp(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.AddItem("stripe", "https://api.stripe.com", []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGroup(self(a), "eng"); err != nil {
		t.Fatal(err)
	}
}

func TestGroupAddListAudit(t *testing.T) {
	a := groupApp(t)
	g, err := a.AddGroup(self(a), "eng")
	if err != nil {
		t.Fatal(err)
	}
	if g.OrgID != a.OrgID || g.Name != "eng" || g.ID == "" {
		t.Fatalf("group %+v", g)
	}
	if _, err := a.AddGroup(self(a), "eng"); err == nil {
		t.Fatal("duplicate group name must fail")
	}
	groups, err := a.Groups(self(a))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "eng" {
		t.Fatalf("groups %+v", groups)
	}
	ev := lastOf(t, mustAudit(t, a), protocol.ActionGroupCreated)
	if ev.Reason != "group=eng" || ev.AgentID != a.HumanID {
		t.Fatalf("group_created audit %+v", ev)
	}
}

func TestGroupMemberOpsAudit(t *testing.T) {
	a := groupApp(t)
	seedGroupApp(t, a)
	s := self(a)
	if err := a.GroupAddMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	members, err := a.GroupMemberList(s, "eng")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].MemberID != "claude" {
		t.Fatalf("members %+v", members)
	}
	if err := a.GroupAddMember(s, "eng", protocol.PrincipalAgent, "ghost"); err == nil {
		t.Fatal("member must exist")
	}
	if err := a.GroupAddMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal("re-add is idempotent")
	}
	if err := a.GroupRemoveMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := a.GroupRemoveMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal("re-remove is idempotent")
	}
	rows := mustAudit(t, a)
	added := lastOf(t, rows, protocol.ActionGroupMemberAdded)
	if added.Reason != "group=eng member=agent:claude" {
		t.Fatalf("member_added reason %q", added.Reason)
	}
	removed := lastOf(t, rows, protocol.ActionGroupMemberRemoved)
	if removed.Reason != "group=eng member=agent:claude" {
		t.Fatalf("member_removed reason %q", removed.Reason)
	}
}

func mustAudit(t *testing.T, a *App) []protocol.AuditEvent {
	t.Helper()
	rows, err := a.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestGroupGrantAndExpansion(t *testing.T) {
	a := groupApp(t)
	seedGroupApp(t, a)
	s := self(a)

	// No membership: not in the agent's list.
	items, err := a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("visible without membership: %+v", items)
	}

	g, err := a.GrantSubject(s, protocol.SubjectGroup, "eng", "stripe", protocol.Level2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.Subject() != protocol.SubjectGroup || g.AgentID == "" {
		t.Fatalf("group grant %+v", g)
	}
	ev := lastOf(t, mustAudit(t, a), protocol.ActionGrantGranted)
	if !strings.Contains(ev.Reason, "group=eng") {
		t.Fatalf("grant audit reason %q", ev.Reason)
	}

	// Member add expands access.
	if err := a.GroupAddMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	items, err = a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "stripe" {
		t.Fatalf("group grant list: %+v", items)
	}
	mustNoAppLeak(t, items)

	// Member remove contracts it.
	if err := a.GroupRemoveMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	items, err = a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("post-remove list: %+v", items)
	}
}

// Direct beats group in list/fill — a deny on the direct edge hides the
// group-granted item.
func TestItemsForAgentDirectBeatsGroup(t *testing.T) {
	a := groupApp(t)
	seedGroupApp(t, a)
	s := self(a)
	if err := a.GroupAddMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantSubject(s, protocol.SubjectGroup, "eng", "stripe", protocol.Level2, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantSubject(s, protocol.SubjectAgent, "claude", "stripe", protocol.LevelDeny, nil); err != nil {
		t.Fatal(err)
	}
	items, err := a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("direct deny should hide group-granted item: %+v", items)
	}
}

// Group deny hides a group allow from another group — deny beats allow in
// the inherited tier.
func TestItemsForAgentGroupDenyBeatsAllow(t *testing.T) {
	a := groupApp(t)
	seedGroupApp(t, a)
	s := self(a)
	if _, err := a.AddGroup(s, "ops"); err != nil {
		t.Fatal(err)
	}
	if err := a.GroupAddMember(s, "eng", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := a.GroupAddMember(s, "ops", protocol.PrincipalAgent, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantSubject(s, protocol.SubjectGroup, "eng", "stripe", protocol.Level2, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantSubject(s, protocol.SubjectGroup, "ops", "stripe", protocol.LevelDeny, nil); err != nil {
		t.Fatal(err)
	}
	items, err := a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("group deny should beat group allow: %+v", items)
	}
}

func TestGrantSubjectValidation(t *testing.T) {
	a := groupApp(t)
	seedGroupApp(t, a)
	s := self(a)
	if _, err := a.GrantSubject(s, protocol.SubjectGroup, "ghost", "stripe", protocol.Level2, nil); err != store.ErrNotFound {
		t.Fatalf("unknown group: %v", err)
	}
	if _, err := a.GrantSubject(s, protocol.SubjectGroup, "eng", "ghost", protocol.Level2, nil); err == nil {
		t.Fatal("unknown item must fail")
	}
	if _, err := a.GrantSubject(s, protocol.SubjectGroup, "eng", "stripe", "bogus", nil); err == nil {
		t.Fatal("bad level must fail")
	}
	// deny on a direct grant through the subject path.
	if _, err := a.GrantSubject(s, protocol.SubjectAgent, "claude", "stripe", protocol.LevelDeny, nil); err != nil {
		t.Fatal(err)
	}
}
