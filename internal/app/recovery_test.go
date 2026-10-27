package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// fakeRecoveryStore adds the VEIL-78 recovery plane over the sqlite store —
// real master + real device.Offer so proof_hmac and re-wrap semantics are
// honest; only the Postgres persistence is faked.
type fakeRecoveryStore struct {
	store.Store
	mu      sync.Mutex
	master  []byte
	pubkeys map[string]bool
	shares  map[string]protocol.OrgKeyShare
	reqs    map[string]protocol.RecoveryRequest
	open    map[string]string // org|human → open req id
	rotated []string
	recDel  [][2]string // DeleteHumanRecovery calls
}

func newFakeRecoveryStore(t *testing.T, s store.Store) *fakeRecoveryStore {
	t.Helper()
	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeRecoveryStore{
		Store:   s,
		master:  master,
		pubkeys: map[string]bool{},
		shares:  map[string]protocol.OrgKeyShare{},
		reqs:    map[string]protocol.RecoveryRequest{},
		open:    map[string]string{},
	}
}

func shareKey(orgID, humanID string) string { return orgID + "|" + humanID }

func (f *fakeRecoveryStore) EnrollOrgKeyShare(_ context.Context, orgID, humanID string, pubkey []byte, audit ...protocol.AuditEvent) (protocol.OrgKeyShare, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := shareKey(orgID, humanID)
	if share, ok := f.shares[k]; ok {
		return share, false, nil
	}
	if f.pubkeys[string(pubkey)] {
		return protocol.OrgKeyShare{}, false, store.ErrRecoveryPubkey
	}
	wrapped, err := device.Offer(f.master, pubkey)
	if err != nil {
		return protocol.OrgKeyShare{}, false, err
	}
	now := time.Now().UTC()
	share := protocol.OrgKeyShare{
		OrgID: orgID, HumanID: humanID, Pubkey: append([]byte(nil), pubkey...),
		WrappedMaster: wrapped, WrapPubkeyEpoch: 1, OrgKeyVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	f.shares[k] = share
	f.pubkeys[string(pubkey)] = true
	for _, e := range audit {
		if err := f.Store.AppendAudit(e); err != nil {
			return protocol.OrgKeyShare{}, false, err
		}
	}
	return share, true, nil
}

func (f *fakeRecoveryStore) OrgKeyShare(_ context.Context, orgID, humanID string) (protocol.OrgKeyShare, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	share, ok := f.shares[shareKey(orgID, humanID)]
	if !ok {
		return protocol.OrgKeyShare{}, store.ErrNotFound
	}
	return share, nil
}

func (f *fakeRecoveryStore) FileRecoveryRequest(_ context.Context, req protocol.RecoveryRequest, audit ...protocol.AuditEvent) (protocol.RecoveryRequest, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pubkeys[string(req.NewPubkey)] {
		return protocol.RecoveryRequest{}, false, store.ErrRecoveryPubkey
	}
	k := shareKey(req.OrgID, req.HumanID)
	if id, ok := f.open[k]; ok {
		return f.reqs[id], false, nil
	}
	f.reqs[req.ID] = req
	f.open[k] = req.ID
	for _, e := range audit {
		if err := f.Store.AppendAudit(e); err != nil {
			return protocol.RecoveryRequest{}, false, err
		}
	}
	return req, true, nil
}

func (f *fakeRecoveryStore) RecoveryRequest(_ context.Context, id string) (protocol.RecoveryRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	req, ok := f.reqs[id]
	if !ok {
		return protocol.RecoveryRequest{}, store.ErrNotFound
	}
	return req, nil
}

func (f *fakeRecoveryStore) ListRecoveryRequests(_ context.Context, orgID string, status protocol.RecoveryStatus) ([]protocol.RecoveryRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.RecoveryRequest
	for _, req := range f.reqs {
		if req.OrgID == orgID && req.Status == status {
			out = append(out, req)
		}
	}
	return out, nil
}

func (f *fakeRecoveryStore) CompleteRecovery(_ context.Context, orgID, id, confirmerID string, proofHMAC []byte) (protocol.RecoveryRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	req, ok := f.reqs[id]
	if !ok || req.OrgID != orgID {
		return protocol.RecoveryRequest{}, store.ErrNotFound
	}
	if req.Status != protocol.RecoveryOpen {
		return protocol.RecoveryRequest{}, store.ErrRequestResolved
	}
	now := time.Now().UTC()
	if !now.Before(req.ExpiresAt) {
		return protocol.RecoveryRequest{}, store.ErrRecoveryExpired
	}
	if now.Before(req.UnlockAt) {
		mac := hmac.New(sha256.New, f.master)
		mac.Write([]byte(req.ID))
		if len(proofHMAC) == 0 || !hmac.Equal(mac.Sum(nil), proofHMAC) {
			return protocol.RecoveryRequest{}, store.ErrRecoveryLocked
		}
	}
	// Rotate the master, re-wrap the target's share to new_pubkey, and
	// re-wrap every other share — the same contract Postgres enforces.
	f.master, _ = crypto.NewKey()
	f.rotated = append(f.rotated, orgID)
	oldEpoch := 0
	for k, share := range f.shares {
		if share.OrgID != orgID {
			continue
		}
		if share.HumanID == req.HumanID {
			oldEpoch = share.WrapPubkeyEpoch
			continue
		}
		reboxed, err := device.Offer(f.master, share.Pubkey)
		if err != nil {
			return protocol.RecoveryRequest{}, err
		}
		share.WrappedMaster = reboxed
		f.shares[k] = share
	}
	wrapped, err := device.Offer(f.master, req.NewPubkey)
	if err != nil {
		return protocol.RecoveryRequest{}, err
	}
	k := shareKey(orgID, req.HumanID)
	share := f.shares[k]
	share.OrgID, share.HumanID = orgID, req.HumanID
	share.Pubkey = append([]byte(nil), req.NewPubkey...)
	share.WrappedMaster = wrapped
	share.WrapPubkeyEpoch = oldEpoch + 1
	share.UpdatedAt = now
	if share.CreatedAt.IsZero() {
		share.CreatedAt = now
	}
	f.shares[k] = share
	f.pubkeys[string(req.NewPubkey)] = true
	req.Status = protocol.RecoveryCompleted
	req.ResolvedAt = &now
	req.ConfirmedBy = confirmerID
	epoch := share.WrapPubkeyEpoch
	req.ResultEpoch = &epoch
	f.reqs[id] = req
	delete(f.open, k)
	return req, nil
}

func (f *fakeRecoveryStore) DenyRecovery(_ context.Context, id, actorID string, status protocol.RecoveryStatus) (protocol.RecoveryRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	req, ok := f.reqs[id]
	if !ok {
		return protocol.RecoveryRequest{}, store.ErrNotFound
	}
	if req.Status != protocol.RecoveryOpen {
		return protocol.RecoveryRequest{}, store.ErrRequestResolved
	}
	now := time.Now().UTC()
	req.Status = status
	req.ResolvedAt = &now
	req.ConfirmedBy = actorID
	f.reqs[id] = req
	delete(f.open, shareKey(req.OrgID, req.HumanID))
	return req, nil
}

func (f *fakeRecoveryStore) RotateOrgKey(_ context.Context, orgID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.master, _ = crypto.NewKey()
	f.rotated = append(f.rotated, orgID)
	return nil
}

// OrgLifecycle plumbing — DeleteHuman/PurgeOrg delegate to the embedded
// sqlite store; DeleteHumanRecovery is the recovery-plane surgery.
func (f *fakeRecoveryStore) DeleteHuman(ctx context.Context, id string) error {
	return f.Store.(store.OrgLifecycle).DeleteHuman(ctx, id)
}

func (f *fakeRecoveryStore) PurgeOrg(ctx context.Context, orgID string) (store.PurgeReport, error) {
	return f.Store.(store.OrgLifecycle).PurgeOrg(ctx, orgID)
}

func (f *fakeRecoveryStore) DeleteHumanRecovery(_ context.Context, orgID, humanID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recDel = append(f.recDel, [2]string{orgID, humanID})
	delete(f.shares, shareKey(orgID, humanID))
	if id, ok := f.open[shareKey(orgID, humanID)]; ok {
		req := f.reqs[id]
		now := time.Now().UTC()
		req.Status = protocol.RecoveryCancelled
		req.ResolvedAt = &now
		f.reqs[id] = req
		delete(f.open, shareKey(orgID, humanID))
	}
	return nil
}

// fakeNotify counts recovery events — dedupe suppression is observable.
type fakeNotify struct {
	mu     sync.Mutex
	reqs   []protocol.ApprovalRequest
	events []string
}

func (f *fakeNotify) NotifyRequest(_ context.Context, req protocol.ApprovalRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return nil
}

func (f *fakeNotify) NotifyRecovery(_ context.Context, req protocol.RecoveryRequest, event string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event+":"+req.ID)
	return nil
}

func (f *fakeNotify) recoveryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// fakeDirectory resolves ids to emails — confirmer views carry human_email.
type fakeDirectory map[string]string

func (d fakeDirectory) Email(_ context.Context, id string) (string, error) {
	return d[id], nil
}

// jwtWithAMR mints an unsigned-shape JWT carrying amr — RequireMFA reads the
// payload only; the verifier fake never checks the signature.
func jwtWithAMR(amr ...string) string {
	payload := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf(`{"sub":"s","amr":[%q]}`, strings.Join(amr, `","`))))
	return "a." + payload + ".b"
}

// recoveryApp builds a vault with the recovery plane, an owner+member pair,
// and the notify/directory fakes the ceremony needs.
func recoveryApp(t *testing.T, owners ...string) (*App, *fakeRecoveryStore, *fakeOrgAdmin, orgRoles, *fakeNotify, protocol.Principal, protocol.Principal) {
	t.Helper()
	a, admin, roles, owner, member, _ := lifecycleApp(t)
	fs := newFakeRecoveryStore(t, a.Store)
	a.Store = fs
	notify := &fakeNotify{}
	a.Notify = notify
	a.Directory = fakeDirectory{owner.ID: "owner@x.test", member.ID: "member@x.test"}
	if len(owners) > 0 {
		admin.owners[owner.OrgID] = owners
	}
	return a, fs, admin, roles, notify, owner, member
}

func TestRecoveryEnrollRequiresMFA(t *testing.T) {
	a, _, _, _, _, owner, _ := recoveryApp(t)
	a.Human = fakeVerifier{sub: owner.ID}
	pub, _, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	// aal1 token — JWT with only a password factor — is refused.
	if _, _, err := a.EnrollRecoveryShare(context.Background(), jwtWithAMR("pwd"), pub); !errors.Is(err, ErrForbidden) {
		t.Fatalf("aal1 enroll: %v", err)
	}
	share, created, err := a.EnrollRecoveryShare(context.Background(), "tok", pub)
	if err != nil || !created {
		t.Fatalf("enroll: %v created=%v", err, created)
	}
	// Re-enroll never overwrites — same row back, created=false → 409.
	again, created, err := a.EnrollRecoveryShare(context.Background(), "tok", pub)
	if err != nil || created {
		t.Fatalf("re-enroll: %v created=%v", err, created)
	}
	if string(again.Pubkey) != string(share.Pubkey) {
		t.Fatal("re-enroll overwrote the share")
	}
}

func TestRecoveryTwoOwnerConfirm(t *testing.T) {
	a, _, admin, roles, notify, owner, member := recoveryApp(t)
	// Two owners — strict two-person integrity applies.
	roles.owners[owner.OrgID+"|co"] = true
	roles.members[owner.OrgID+"|co"] = true
	if err := a.Store.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "co", OrgID: owner.OrgID}); err != nil {
		t.Fatal(err)
	}
	admin.owners[owner.OrgID] = []string{owner.ID, "co"}
	ctx := context.Background()

	a.Human = fakeVerifier{sub: member.ID}
	pub, _, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	req, created, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub, ResetMFA: true})
	if err != nil || !created {
		t.Fatalf("start: %v created=%v", err, created)
	}
	if !req.UnlockAt.Equal(req.CreatedAt) {
		t.Fatal("member recovery should unlock immediately")
	}

	// The confirmer is the second owner; view carries member email + epoch.
	a.Human = fakeVerifier{sub: "co"}
	v, err := a.GetRecovery(ctx, "tok", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.HumanEmail != "member@x.test" {
		t.Fatalf("human_email %q", v.HumanEmail)
	}
	done, err := a.CompleteRecovery(ctx, "tok", req.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != protocol.RecoveryCompleted || done.ConfirmedBy != "co" {
		t.Fatalf("complete: %+v", done.RecoveryRequest)
	}
	// reset_mfa fired through the identity plane — to the member, post-commit.
	want := "mfa:" + owner.OrgID + ":" + member.ID
	found := false
	for _, c := range admin.calls {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("ResetMemberMFA missing: %v", admin.calls)
	}
	// Notifications fan out on a detached goroutine — poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for notify.recoveryCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if notify.recoveryCount() != 2 {
		t.Fatalf("notify events %v, want started+completed", notify.events)
	}
}

func TestRecoverySelfConfirmForbiddenMultiOwner(t *testing.T) {
	a, _, admin, roles, _, owner, _ := recoveryApp(t)
	roles.owners[owner.OrgID+"|co"] = true
	roles.members[owner.OrgID+"|co"] = true
	if err := a.Store.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: "co", OrgID: owner.OrgID}); err != nil {
		t.Fatal(err)
	}
	admin.owners[owner.OrgID] = []string{owner.ID, "co"}
	ctx := context.Background()

	a.Human = fakeVerifier{sub: owner.ID}
	pub, _, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	req, _, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub})
	if err != nil {
		t.Fatal(err)
	}
	// Two owners exist — the initiator may not self-confirm.
	if _, err := a.CompleteRecovery(ctx, "tok", req.ID, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self-confirm: %v", err)
	}
	// The second owner confirms fine.
	a.Human = fakeVerifier{sub: "co"}
	if _, err := a.CompleteRecovery(ctx, "tok", req.ID, nil); err != nil {
		t.Fatalf("co-owner confirm: %v", err)
	}
}

// TestRecoverySoleOwnerProofVsTimelock: a sole owner recovering themselves
// gets the 24h/48h windows — locked until unlock_at unless they prove the
// committed master with proof_hmac (offline recovery.key fast path).
func TestRecoverySoleOwnerProofVsTimelock(t *testing.T) {
	a, fs, _, _, _, owner, _ := recoveryApp(t) // single owner: owner
	ctx := context.Background()
	a.Human = fakeVerifier{sub: owner.ID}
	pub, _, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	req, _, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub})
	if err != nil {
		t.Fatal(err)
	}
	if d := req.UnlockAt.Sub(req.CreatedAt); d != 24*time.Hour {
		t.Fatalf("unlock_at window %v, want 24h", d)
	}
	if d := req.ExpiresAt.Sub(req.CreatedAt); d != 48*time.Hour {
		t.Fatalf("expires_at window %v, want 48h", d)
	}
	// Timelock: no proof → locked.
	if _, err := a.CompleteRecovery(ctx, "tok", req.ID, nil); !errors.Is(err, store.ErrRecoveryLocked) {
		t.Fatalf("timelock complete: %v", err)
	}
	// Wrong proof → still locked.
	if _, err := a.CompleteRecovery(ctx, "tok", req.ID, []byte("not-the-mac")); !errors.Is(err, store.ErrRecoveryLocked) {
		t.Fatalf("bad proof: %v", err)
	}
	// Valid proof = HMAC-SHA256(committed master, req.ID) → immediate.
	mac := hmac.New(sha256.New, fs.master)
	mac.Write([]byte(req.ID))
	done, err := a.CompleteRecovery(ctx, "tok", req.ID, mac.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != protocol.RecoveryCompleted {
		t.Fatalf("status %q", done.Status)
	}
}

func TestRecoveryStartDedupeSuppressesNotify(t *testing.T) {
	a, _, _, _, notify, _, member := recoveryApp(t)
	ctx := context.Background()
	a.Human = fakeVerifier{sub: member.ID}
	pub, _, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub})
	if err != nil || !created {
		t.Fatalf("first start: %v created=%v", err, created)
	}
	// Wait for the first (detached) notify so the dedupe check below is
	// comparing against a settled count, not racing the goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for notify.recoveryCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// A second start returns the live request and does not re-notify.
	pub2, _, _ := device.Generate()
	second, created, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub2})
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("dedupe: created=%v id=%s want %s", created, second.ID, first.ID)
	}
	time.Sleep(50 * time.Millisecond)
	if notify.recoveryCount() != 1 {
		t.Fatalf("duplicate start notified: %v", notify.events)
	}
}

func TestRecoveryRateLimit(t *testing.T) {
	a, _, _, _, _, _, member := recoveryApp(t)
	ctx := context.Background()
	a.Human = fakeVerifier{sub: member.ID}
	deny := func(id string) {
		if _, err := a.DenyRecovery(ctx, "tok", id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < recoveryPerHumanPerDay; i++ {
		pub, _, _ := device.Generate()
		req, _, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		deny(req.ID)
	}
	pub, _, _ := device.Generate()
	if _, _, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub}); !errors.Is(err, ErrRecoveryLimit) {
		t.Fatalf("fourth start: %v", err)
	}
}

func TestRecoveryInitiatorCanDenyOwn(t *testing.T) {
	a, _, _, _, _, _, member := recoveryApp(t)
	ctx := context.Background()
	a.Human = fakeVerifier{sub: member.ID}
	pub, _, _ := device.Generate()
	req, _, err := a.StartRecovery(ctx, "tok", StartRecoveryInput{NewPubkey: pub})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := a.DenyRecovery(ctx, "tok", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if denied.Status != protocol.RecoveryCancelled {
		t.Fatalf("initiator deny status %q, want cancelled", denied.Status)
	}
	// An unrelated member cannot deny someone else's ask.
	if _, err := a.DenyRecovery(ctx, "tok", req.ID); !errors.Is(err, store.ErrRequestResolved) {
		t.Fatalf("re-deny: %v", err)
	}
}

func TestRemoveMemberRotatesAndDropsShare(t *testing.T) {
	a, fs, _, _, _, owner, member := recoveryApp(t)
	ctx := context.Background()
	// Enroll the member first so there is a share to drop.
	a.Human = fakeVerifier{sub: member.ID}
	pub, _, _ := device.Generate()
	if _, _, err := a.EnrollRecoveryShare(ctx, "tok", pub); err != nil {
		t.Fatal(err)
	}
	a.Human = fakeVerifier{sub: owner.ID}
	if err := a.RemoveMember(ctx, "tok", member.ID); err != nil {
		t.Fatal(err)
	}
	want := [2]string{owner.OrgID, member.ID}
	found := false
	for _, c := range fs.recDel {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("DeleteHumanRecovery missing: %v", fs.recDel)
	}
	if len(fs.rotated) == 0 || fs.rotated[len(fs.rotated)-1] != owner.OrgID {
		t.Fatalf("RotateOrgKey missing: %v", fs.rotated)
	}
	if _, err := fs.OrgKeyShare(ctx, owner.OrgID, member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed member's share still enrolled: %v", err)
	}
}
