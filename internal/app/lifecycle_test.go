package app

import (
	"context"
	"errors"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// fakeOrgAdmin records the Keto-side calls the app makes — order matters:
// tuples die before vault data does. owners mirrors the owner roster the
// app expects ListOwners to return.
type fakeOrgAdmin struct {
	calls  []string
	owners map[string][]string // orgID → owner ids
}

func (f *fakeOrgAdmin) RemoveMember(_ context.Context, orgID, id string) error {
	f.calls = append(f.calls, "rm:"+orgID+":"+id)
	return nil
}

func (f *fakeOrgAdmin) RemoveOwner(_ context.Context, orgID, id string) error {
	f.calls = append(f.calls, "ro:"+orgID+":"+id)
	return nil
}

func (f *fakeOrgAdmin) PromoteOwner(_ context.Context, orgID, id string) error {
	f.calls = append(f.calls, "po:"+orgID+":"+id)
	return nil
}

func (f *fakeOrgAdmin) RemoveOrgTuples(_ context.Context, orgID string) error {
	f.calls = append(f.calls, "purge:"+orgID)
	return nil
}

func (f *fakeOrgAdmin) ListOwners(_ context.Context, orgID string) ([]string, error) {
	return append([]string(nil), f.owners[orgID]...), nil
}

// orgRoles is the org-scoped MemberCheck with split member/owner legs — the
// orgMembers fake in provision_test keys both legs on one map entry, which
// cannot represent "member but not owner".
type orgRoles struct {
	members map[string]bool
	owners  map[string]bool
}

func (r orgRoles) IsMember(_ context.Context, orgID, id string) (bool, error) {
	return r.members[orgID+"|"+id], nil
}

func (r orgRoles) IsOwner(_ context.Context, orgID, id string) (bool, error) {
	return r.owners[orgID+"|"+id], nil
}

// lifecycleApp builds a vault with an owner, a member who joined the owner's
// org, and an outsider in a different org — the three actors every verb must
// distinguish.
func lifecycleApp(t *testing.T) (*App, *fakeOrgAdmin, orgRoles, protocol.Principal, protocol.Principal, protocol.Principal) {
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

	owner := provisioned(t, a, prov, "owner")
	roles.members[owner.OrgID+"|"+owner.ID] = true
	roles.owners[owner.OrgID+"|"+owner.ID] = true
	admin.owners[owner.OrgID] = []string{owner.ID}

	// Invite-time order: the tuple lands before the invitee ever provisions.
	prov.stamps = append(prov.stamps, [2]string{"member", owner.OrgID})
	roles.members[owner.OrgID+"|member"] = true
	member := provisioned(t, a, prov, "member")
	if member.OrgID != owner.OrgID {
		t.Fatalf("member joined %s, want %s", member.OrgID, owner.OrgID)
	}

	outside := provisioned(t, a, prov, "outside")
	if outside.OrgID == owner.OrgID {
		t.Fatal("outsider landed in owner's org")
	}
	roles.members[outside.OrgID+"|"+outside.ID] = true
	roles.owners[outside.OrgID+"|"+outside.ID] = true
	return a, admin, roles, owner, member, outside
}

func TestRemoveMemberRequiresOwner(t *testing.T) {
	a, admin, _, _, member, _ := lifecycleApp(t)
	a.Human = fakeVerifier{sub: "member"}
	if err := a.RemoveMember(context.Background(), "tok", member.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member removing member: %v", err)
	}
	if len(admin.calls) != 0 {
		t.Fatalf("forbidden call reached OrgAdmin: %v", admin.calls)
	}
}

func TestRemoveMemberDropsTupleAndRow(t *testing.T) {
	a, admin, roles, owner, member, _ := lifecycleApp(t)
	a.Human = fakeVerifier{sub: "owner"}
	if err := a.RemoveMember(context.Background(), "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	want := "rm:" + owner.OrgID + ":" + member.ID
	if len(admin.calls) != 1 || admin.calls[0] != want {
		t.Fatalf("admin calls %v, want [%s]", admin.calls, want)
	}
	// The humans row is gone — the removed member's token resolves nothing.
	delete(roles.members, owner.OrgID+"|"+member.ID)
	a.Human = fakeVerifier{sub: "member"}
	if _, err := a.PrincipalFromOIDC(context.Background(), "tok"); err == nil {
		t.Fatal("removed member still resolves")
	}
}

func TestRemoveMemberRefusals(t *testing.T) {
	a, admin, _, owner, _, outside := lifecycleApp(t)
	a.Human = fakeVerifier{sub: "owner"}
	ctx := context.Background()
	if err := a.RemoveMember(ctx, "tok", owner.ID); err == nil {
		t.Fatal("self-remove allowed")
	}
	if err := a.RemoveMember(ctx, "tok", outside.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-org member: %v", err)
	}
	if err := a.RemoveMember(ctx, "tok", "no-such-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
	if len(admin.calls) != 0 {
		t.Fatalf("refused calls still hit OrgAdmin: %v", admin.calls)
	}
}

func TestRemoveMemberRefusesOwnerTarget(t *testing.T) {
	a, admin, roles, owner, member, _ := lifecycleApp(t)
	roles.owners[owner.OrgID+"|"+member.ID] = true // member is also an owner
	a.Human = fakeVerifier{sub: "owner"}
	if err := a.RemoveMember(context.Background(), "tok", member.ID); err == nil {
		t.Fatal("owner target removable")
	}
	if len(admin.calls) != 0 {
		t.Fatalf("refused call still hit OrgAdmin: %v", admin.calls)
	}
}

func TestPromoteOwner(t *testing.T) {
	a, admin, _, _, member, outside := lifecycleApp(t)
	ctx := context.Background()

	a.Human = fakeVerifier{sub: "member"}
	if err := a.PromoteOwner(ctx, "tok", member.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member promoting: %v", err)
	}

	a.Human = fakeVerifier{sub: "owner"}
	if err := a.PromoteOwner(ctx, "tok", outside.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-org promote: %v", err)
	}
	ownerOrg := member.OrgID
	if err := a.PromoteOwner(ctx, "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	want := "po:" + ownerOrg + ":" + member.ID
	if len(admin.calls) != 1 || admin.calls[0] != want {
		t.Fatalf("admin calls %v, want [%s]", admin.calls, want)
	}
}

func TestDeleteOrgPurgesEverythingButAudit(t *testing.T) {
	a, admin, _, owner, _, _ := lifecycleApp(t)
	ctx := context.Background()

	a.Human = fakeVerifier{sub: "member"}
	if _, err := a.DeleteOrg(ctx, "tok"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member deleting org: %v", err)
	}

	a.Human = fakeVerifier{sub: "owner"}
	rep, err := a.DeleteOrg(ctx, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Humans != 2 {
		t.Fatalf("humans purged %d, want 2", rep.Humans)
	}
	want := "purge:" + owner.OrgID
	if len(admin.calls) != 1 || admin.calls[0] != want {
		t.Fatalf("admin calls %v, want [%s]", admin.calls, want)
	}
	// Teardown killed access: the owner's own token resolves nothing now.
	if _, err := a.PrincipalFromOIDC(ctx, "tok"); err == nil {
		t.Fatal("owner still resolves after teardown")
	}
}

func TestDemoteOwnerLastOwnerRefused(t *testing.T) {
	a, admin, roles, owner, member, _ := lifecycleApp(t)
	ctx := context.Background()
	a.Human = fakeVerifier{sub: "owner"}

	// Member isn't an owner — demote is a 404, not a no-op.
	if err := a.DemoteOwner(ctx, "tok", member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("demote non-owner: %v", err)
	}
	// Sole owner cannot be demoted.
	if err := a.DemoteOwner(ctx, "tok", owner.ID); err == nil {
		t.Fatal("last-owner demote allowed")
	}
	// With a co-owner, demotion goes through.
	roles.owners[owner.OrgID+"|"+member.ID] = true
	admin.owners[owner.OrgID] = []string{owner.ID, member.ID}
	if err := a.DemoteOwner(ctx, "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	want := "ro:" + owner.OrgID + ":" + member.ID
	if len(admin.calls) != 1 || admin.calls[0] != want {
		t.Fatalf("admin calls %v, want [%s]", admin.calls, want)
	}
}

func TestDeleteMe(t *testing.T) {
	a, admin, roles, owner, member, _ := lifecycleApp(t)
	ctx := context.Background()

	// Sole owner cannot leave — the org would have no administrator.
	a.Human = fakeVerifier{sub: "owner"}
	if err := a.DeleteMe(ctx, "tok"); err == nil {
		t.Fatal("sole owner left")
	}

	// A member can leave: both legs' tuples drop, humans row dies.
	a.Human = fakeVerifier{sub: "member"}
	if err := a.DeleteMe(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	want := "rm:" + owner.OrgID + ":" + member.ID
	if len(admin.calls) != 1 || admin.calls[0] != want {
		t.Fatalf("admin calls %v, want [%s]", admin.calls, want)
	}
	delete(roles.members, owner.OrgID+"|"+member.ID)
	if _, err := a.PrincipalFromOIDC(ctx, "tok"); err == nil {
		t.Fatal("departed member still resolves")
	}

	// A co-owner can leave — owner leg drops first, then member leg.
	roles.owners[owner.OrgID+"|co"] = true
	roles.members[owner.OrgID+"|co"] = true
	admin.owners[owner.OrgID] = []string{owner.ID, "co"}
	if err := a.Store.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "co", OrgID: owner.OrgID}); err != nil {
		t.Fatal(err)
	}
	a.Human = fakeVerifier{sub: "co"}
	if err := a.DeleteMe(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	ro := "ro:" + owner.OrgID + ":co"
	rm := "rm:" + owner.OrgID + ":co"
	if len(admin.calls) != 3 || admin.calls[1] != ro || admin.calls[2] != rm {
		t.Fatalf("co-owner leave calls %v", admin.calls)
	}
}
