package app

// VEIL-86 audit-completeness matrix: every state-changing or
// secret-revealing operation must land one durable audit row answering
// who (AgentID = acting principal), what (Action + ItemID/Reason target),
// when (Time). The credential-use plane is fail-closed audited in the
// broker; this matrix is the administration and human read planes, which
// had zero coverage before this ticket.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/rotate"
)

type stubRotator struct {
	n       int
	retired []string
}

func (s *stubRotator) Create(_ context.Context, _ string, _ []byte) ([]byte, string, error) {
	s.n++
	return []byte("re_next"), "key_" + string(rune('0'+s.n)), nil
}
func (*stubRotator) Verify(_ context.Context, _ []byte) error { return nil }
func (s *stubRotator) Retire(_ context.Context, keyID string, _ []byte) error {
	s.retired = append(s.retired, keyID)
	return nil
}
func (*stubRotator) ManagementPaths() []string { return []string{"/api-keys"} }

func auditRows(t *testing.T, a *App) []protocol.AuditEvent {
	t.Helper()
	rows, err := a.Store.Audit()
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return rows
}

// lastOf returns the newest event of kind, or fails naming what it found.
func lastOf(t *testing.T, rows []protocol.AuditEvent, kind protocol.ActionKind) protocol.AuditEvent {
	t.Helper()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Action == kind {
			return rows[i]
		}
	}
	seen := make(map[protocol.ActionKind]int)
	for _, r := range rows {
		seen[r.Action]++
	}
	t.Fatalf("no %s event; actions present: %v", kind, seen)
	return protocol.AuditEvent{}
}

// checkEvent asserts the who/what/when triplet on one row.
func checkEvent(t *testing.T, ev protocol.AuditEvent, actor, itemID, reasonHas string) {
	t.Helper()
	if ev.AgentID != actor {
		t.Fatalf("actor %q, want %q", ev.AgentID, actor)
	}
	if itemID != "" && ev.ItemID != itemID {
		t.Fatalf("item %q, want %q", ev.ItemID, itemID)
	}
	if reasonHas != "" && !strings.Contains(ev.Reason, reasonHas) {
		t.Fatalf("reason %q missing %q", ev.Reason, reasonHas)
	}
	if ev.Decision != protocol.DecisionAllow {
		t.Fatalf("decision %q, want allow", ev.Decision)
	}
	if ev.Time.IsZero() {
		t.Fatal("audit event missing timestamp")
	}
}

// fakeInviter satisfies Inviter without Kratos — the invite tuple/identity
// is Kratos's side; the matrix only needs the event the app emits.
type fakeInviter struct{}

func (fakeInviter) Invite(_ context.Context, email, actor, orgID string) (InviteResult, error) {
	return InviteResult{IdentityID: "id-" + email}, nil
}

// matrixApp is the app fixture: a provisioned owner on a real vault store.
func matrixApp(t *testing.T) (*App, *fakeProvision, *fakeOrgAdmin, orgRoles, protocol.Principal) {
	t.Helper()
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	prov := &fakeProvision{}
	roles := orgRoles{members: map[string]bool{}, owners: map[string]bool{}}
	admin := &fakeOrgAdmin{owners: map[string][]string{}}
	a.Provision = prov
	a.Members = roles
	a.OrgAdmin = admin
	a.Invites = fakeInviter{}

	owner := provisioned(t, a, prov, "owner")
	roles.members[owner.OrgID+"|"+owner.ID] = true
	roles.owners[owner.OrgID+"|"+owner.ID] = true
	admin.owners[owner.OrgID] = []string{owner.ID}
	return a, prov, admin, roles, owner
}

func TestAuditMatrix_ItemLifecycle(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "mx-github", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemCreated), owner.ID, item.ID, "")

	if _, err := a.UpdateItemFor(owner, item.ID, nil, nil, []string{"tag1"}, "login", nil); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemUpdated), owner.ID, item.ID, "")

	sr := &stubRotator{}
	rotate.Register("rot.test", sr)
	rotItem, err := a.PutItemFor(owner, ItemOpts{Name: "mx-rot", URI: "https://rot.test", Tags: []string{"user-tag"}, Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	// Cycle 1: key_1 minted, no prior key; Tags untouched.
	after1, err := a.RotateItem(owner, rotItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after1.Tags) != 1 || after1.Tags[0] != "user-tag" {
		t.Fatalf("rotate polluted item.Tags: %v", after1.Tags)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemRotated), owner.ID, rotItem.ID, "new_key_id=key_1 old_key_id= retired_key_id=")

	// Cycle 2: key_2 minted, key_1 moves to pending, still nothing to retire.
	if _, err := a.RotateItem(owner, rotItem.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemRotated), owner.ID, rotItem.ID, "new_key_id=key_2 old_key_id=key_1 retired_key_id=")

	// Cycle 3: key_3 minted, key_2 moves to pending, key_1 retired after PutItem commits.
	if _, err := a.RotateItem(owner, rotItem.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemRotated), owner.ID, rotItem.ID, "new_key_id=key_3 old_key_id=key_2 retired_key_id=key_1")
	if len(sr.retired) != 1 || sr.retired[0] != "key_1" {
		t.Fatalf("want [key_1] retired on cycle 3, got %v", sr.retired)
	}

	if err := a.ArchiveItemFor(owner, item.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemArchived), owner.ID, item.ID, "")

	if err := a.DeleteItemFor(owner, item.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionItemDeleted), owner.ID, item.ID, "")
}

func TestAuditMatrix_AgentAndGrant(t *testing.T) {
	a, _, _, roles, owner := matrixApp(t)

	agent, err := a.AddAgentFor(owner, "mx-agent")
	if err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionAgentCreated), owner.ID, "", "agent="+agent.ID)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "mx-grantitem", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantUntil(owner, agent.ID, item.ID, protocol.Level1, nil); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionGrantGranted), owner.ID, item.ID, "agent="+agent.ID)

	// Agent revocation was already audited — the actor is who matters now.
	if err := a.RevokeAgent(owner, agent.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionRevoke), owner.ID, "", "agent="+agent.ID)
	_ = roles
}

func TestAuditMatrix_SessionLifecycle(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)

	agent, err := a.AddAgentFor(owner, "mx-session-agent")
	if err != nil {
		t.Fatal(err)
	}
	sess, _, err := a.CreateSession(owner, agent.ID, time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionSessionCreated), owner.ID, "", "agent="+agent.ID)
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionSessionCreated), owner.ID, "", "session="+sess.ID)

	if _, err := a.RenewSession(owner, sess.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionSessionRenewed), owner.ID, "", "session="+sess.ID)

	if _, err := a.RevokeSession(owner, sess.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionSessionRevoked), owner.ID, "", "session="+sess.ID)
}

func TestAuditMatrix_WorkloadBind(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	agent, err := a.AddAgentFor(owner, "mx-wl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindWorkloadFor(owner, agent.ID, "https://issuer.test", "sub", "aud"); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionWorkloadBound), owner.ID, "", "agent="+agent.ID)
}

func TestAuditMatrix_HumanProvisioned(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	// matrixApp already ran ProvisionHuman for "owner" — the row exists.
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionHumanProvisioned), owner.ID, "", "")
}

func TestAuditMatrix_HumanReadPlane(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)

	item, err := a.PutItemFor(owner, ItemOpts{Name: "mx-fill", Login: "u@x", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.FillLogin(owner, item.ID, false); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionFill), owner.ID, item.ID, "")

	// TOTP mint is a separate disclosure — its own event.
	totpItem, err := a.PutItemFor(owner, ItemOpts{Name: "mx-totp", Token: []byte(secret), TOTPSeed: []byte("JBSWY3DPEHPK3PXP")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.FillTOTP(owner, totpItem.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionTOTPMint), owner.ID, totpItem.ID, "")

	// The full-vault replica pull.
	if _, _, err := a.FillSync(owner, ""); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionFillSync), owner.ID, "", "")
}

func TestAuditMatrix_FileWrite(t *testing.T) {
	a, _, _, _, _ := matrixApp(t)

	if _, err := a.PutItem(ItemOpts{Name: "mx-file", Kind: protocol.ItemFile, FileName: "note.txt", File: []byte("body")}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := a.WriteFile("mx-file", dest); err != nil {
		t.Fatal(err)
	}
	// WriteFile is the local-vault path — the vault's human is the actor.
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionFileWrite), a.HumanID, "mx-file", "")
}

func TestAuditMatrix_MembershipOps(t *testing.T) {
	a, prov, admin, roles, owner := matrixApp(t)
	ctx := context.Background()

	// Invite a member: the tuple stamps before the invitee provisions.
	a.Human = fakeVerifier{sub: "owner"}
	if _, err := a.InviteHuman(ctx, "tok", "m@x.test"); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionMemberInvited), owner.ID, "", "m@x.test")

	// Provision the member so Promote/Remove have a real target.
	prov.stamps = append(prov.stamps, [2]string{"member", owner.OrgID})
	roles.members[owner.OrgID+"|member"] = true
	member := provisioned(t, a, prov, "member")
	a.Human = fakeVerifier{sub: "owner"}

	if err := a.PromoteOwner(ctx, "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionOwnerPromoted), owner.ID, "", "member="+member.ID)
	roles.owners[owner.OrgID+"|"+member.ID] = true
	admin.owners[owner.OrgID] = append(admin.owners[owner.OrgID], member.ID)

	if err := a.DemoteOwner(ctx, "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionOwnerDemoted), owner.ID, "", "member="+member.ID)
	delete(roles.owners, owner.OrgID+"|"+member.ID)

	if err := a.RemoveMember(ctx, "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	checkEvent(t, lastOf(t, auditRows(t, a), protocol.ActionMemberRemoved), owner.ID, "", "member="+member.ID)
}

// The one row compliance.md flagged as untested: a registered passkey
// asserting must land passkey_assert with the item id attributed.
func TestAuditMatrix_PasskeyAssert(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: DefaultHuman, OrgID: a.OrgID}

	create, err := json.Marshal(map[string]any{
		"challenge": "dGVzdGNoYWxsZW5nZQ",
		"rp":        map[string]string{"id": "localhost", "name": "Veil fixture"},
		"user":      map[string]string{"id": "AQIDBA", "name": "ada", "displayName": "Ada"},
		"pubKeyCredParams": []map[string]any{
			{"type": "public-key", "alg": -7},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := a.FillPasskeyRegister(human, "http://localhost:8899", create, nil)
	if err != nil {
		t.Fatal(err)
	}
	var cred struct {
		ID        string `json:"id"`
		ErrorCode int    `json:"errorCode"`
	}
	if err := json.Unmarshal(resp, &cred); err != nil || cred.ErrorCode != 0 || cred.ID == "" {
		t.Fatalf("register: %s", resp)
	}

	var itemID string
	items, err := a.ItemsForPrincipal(human)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == protocol.ItemPasskey {
			itemID = it.ID
		}
	}
	if itemID == "" {
		t.Fatal("no passkey item after register")
	}

	get, err := json.Marshal(map[string]any{
		"challenge": "b3RoZXJjaGFsbGVuZ2U",
		"rpId":      "localhost",
		"allowCredentials": []map[string]any{
			{"type": "public-key", "id": cred.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := a.FillPasskeyGet(human, "http://localhost:8899", get)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ErrorCode int `json:"errorCode"`
	}
	if json.Unmarshal(assertion, &got) == nil && got.ErrorCode != 0 {
		t.Fatalf("assert failed: %s", assertion)
	}

	ev := lastOf(t, auditRows(t, a), protocol.ActionPasskeyAssert)
	checkEvent(t, ev, human.ID, itemID, "")
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "BEGIN") || strings.Contains(string(raw), cred.ID) {
		t.Fatal("passkey_assert event leaked credential material")
	}
}

// veil:never-fill strips the item from every replica pull — the strictest
// per-item posture means no device ever holds its material.
func TestFillSyncNeverFillTag(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)

	live, err := a.PutItemFor(owner, ItemOpts{Name: "sync-live", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItemFor(owner, ItemOpts{Name: "sync-nope", Token: []byte(secret), Tags: []string{protocol.TagNeverFill}}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := a.FillSync(owner, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Item.Name == "sync-nope" {
			t.Fatal("never-fill item reached the replica")
		}
	}
	seen := false
	for _, row := range rows {
		if row.Item.ID == live.ID {
			seen = true
		}
	}
	if !seen {
		t.Fatal("untagged item missing from sync")
	}
}

// Regression for 75b9cdc: an OIDC owner (actor.ID != a.HumanID, proven via
// Members.IsOwner) may delegate an agent created locally with Owner.ID=a.HumanID
// ("self"), while a non-owner member in the same org is still rejected.
func TestOwnerDelegatesSelfOwnedAgent(t *testing.T) {
	a, prov, _, roles, owner := matrixApp(t)
	a.OrgID = owner.OrgID
	agent, err := a.AddAgent("local-self-agent")
	if err != nil {
		t.Fatal(err)
	}
	item, err := a.PutItemFor(owner, ItemOpts{Name: "self-agent-item", Token: []byte(secret)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GrantUntil(owner, agent.ID, item.ID, protocol.Level2, nil); err != nil {
		t.Fatalf("OIDC owner rejected on self-owned agent: %v", err)
	}
	prov.stamps = append(prov.stamps, [2]string{"member", owner.OrgID})
	roles.members[owner.OrgID+"|member"] = true
	member := provisioned(t, a, prov, "member")
	if _, err := a.GrantUntil(member, agent.ID, item.ID, protocol.Level2, nil); err == nil {
		t.Fatal("non-owner member minted a grant on self-owned agent")
	}
}

type authCheckRotator struct {
	sawKey string
	n      int
}

func (r *authCheckRotator) Create(_ context.Context, _ string, liveKey []byte) ([]byte, string, error) {
	r.sawKey = string(liveKey)
	r.n++
	return []byte("rotated_worker_secret"), "kid_1", nil
}
func (*authCheckRotator) Verify(_ context.Context, _ []byte) error           { return nil }
func (*authCheckRotator) Retire(_ context.Context, _ string, _ []byte) error { return nil }
func (*authCheckRotator) ManagementPaths() []string                          { return []string{"/keys"} }

func TestRotateWithManagementItemAndSchedule(t *testing.T) {
	a, _, _, _, owner := matrixApp(t)
	ar := &authCheckRotator{}
	rotate.Register("mgmt.test", ar)

	mgmt, err := a.PutItemFor(owner, ItemOpts{Name: "aws-iam-admin", URI: "https://iam.test", Token: []byte("ADMIN_ROOT_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := a.PutItemFor(owner, ItemOpts{Name: "aws-worker", URI: "https://mgmt.test", Token: []byte("WORKER_SCOPED_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RotateItemWith(owner, worker.ID, RotateOpts{
		ManagementItem: mgmt.ID,
		Interval:       time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	if ar.sawKey != "ADMIN_ROOT_KEY" {
		t.Fatalf("rotator signed with %q, want management item secret", ar.sawKey)
	}
	// Not due yet -> RotateDue is a zero-write no-op.
	if n, err := a.RotateDue(time.Now(), 10); err != nil || n != 0 {
		t.Fatalf("RotateDue before deadline: n=%d err=%v", n, err)
	}
	// Past deadline -> RotateDue rotates it using the persisted ManagementItemID.
	ar.sawKey = ""
	if n, err := a.RotateDue(time.Now().Add(2*time.Hour), 10); err != nil || n != 1 {
		t.Fatalf("RotateDue after deadline: n=%d err=%v", n, err)
	}
	if ar.sawKey != "ADMIN_ROOT_KEY" {
		t.Fatalf("scheduled rotation lost management item: saw %q", ar.sawKey)
	}
}
