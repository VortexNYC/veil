package publicapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// fakeRecovery adds the VEIL-78 recovery plane over the test vault's sqlite
// store — real master + real box wrapping so the proof path is honest.
type fakeRecovery struct {
	store.Store
	mu      sync.Mutex
	master  []byte
	pubkeys map[string]bool
	shares  map[string]protocol.OrgKeyShare
	reqs    map[string]protocol.RecoveryRequest
	open    map[string]string
}

func newFakeRecovery(t *testing.T, s store.Store) *fakeRecovery {
	t.Helper()
	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeRecovery{
		Store:   s,
		master:  master,
		pubkeys: map[string]bool{},
		shares:  map[string]protocol.OrgKeyShare{},
		reqs:    map[string]protocol.RecoveryRequest{},
		open:    map[string]string{},
	}
}

func (f *fakeRecovery) EnrollOrgKeyShare(_ context.Context, orgID, humanID string, pubkey []byte, audit ...protocol.AuditEvent) (protocol.OrgKeyShare, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := orgID + "|" + humanID
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

func (f *fakeRecovery) OrgKeyShare(_ context.Context, orgID, humanID string) (protocol.OrgKeyShare, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	share, ok := f.shares[orgID+"|"+humanID]
	if !ok {
		return protocol.OrgKeyShare{}, store.ErrNotFound
	}
	return share, nil
}

func (f *fakeRecovery) FileRecoveryRequest(_ context.Context, req protocol.RecoveryRequest, audit ...protocol.AuditEvent) (protocol.RecoveryRequest, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pubkeys[string(req.NewPubkey)] {
		return protocol.RecoveryRequest{}, false, store.ErrRecoveryPubkey
	}
	k := req.OrgID + "|" + req.HumanID
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

func (f *fakeRecovery) RecoveryRequest(_ context.Context, id string) (protocol.RecoveryRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	req, ok := f.reqs[id]
	if !ok {
		return protocol.RecoveryRequest{}, store.ErrNotFound
	}
	return req, nil
}

func (f *fakeRecovery) ListRecoveryRequests(_ context.Context, orgID string, status protocol.RecoveryStatus) ([]protocol.RecoveryRequest, error) {
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

func (f *fakeRecovery) CompleteRecovery(_ context.Context, orgID, id, confirmerID string, proofHMAC []byte) (protocol.RecoveryRequest, error) {
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
	f.master, _ = crypto.NewKey()
	k := orgID + "|" + req.HumanID
	oldEpoch := 0
	if share, ok := f.shares[k]; ok {
		oldEpoch = share.WrapPubkeyEpoch
	}
	wrapped, err := device.Offer(f.master, req.NewPubkey)
	if err != nil {
		return protocol.RecoveryRequest{}, err
	}
	f.shares[k] = protocol.OrgKeyShare{
		OrgID: orgID, HumanID: req.HumanID, Pubkey: append([]byte(nil), req.NewPubkey...),
		WrappedMaster: wrapped, WrapPubkeyEpoch: oldEpoch + 1, OrgKeyVersion: 2,
		CreatedAt: now, UpdatedAt: now,
	}
	f.pubkeys[string(req.NewPubkey)] = true
	req.Status = protocol.RecoveryCompleted
	req.ResolvedAt = &now
	req.ConfirmedBy = confirmerID
	epoch := oldEpoch + 1
	req.ResultEpoch = &epoch
	f.reqs[id] = req
	delete(f.open, k)
	return req, nil
}

func (f *fakeRecovery) DenyRecovery(_ context.Context, id, actorID string, status protocol.RecoveryStatus) (protocol.RecoveryRequest, error) {
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
	delete(f.open, req.OrgID+"|"+req.HumanID)
	return req, nil
}

// recoveryServer builds a test server wired like the lifecycle tests: an
// owner, a member, an outsider; the recovery plane on a fake store.
func recoveryServer(t *testing.T) (*httptest.Server, *app.App, *fakeRecovery) {
	t.Helper()
	a := testApp(t)
	f := newFakeRecovery(t, a.Store)
	a.Store = f
	a.Human = mapVerifier{"tok-owner": "sub-owner", "tok-member": "sub-member", "tok-out": "sub-out"}
	a.Provision = fakeProvisioner{}
	a.Members = fakeMembers{
		owners:  map[string]bool{"sub-owner": true},
		members: map[string]bool{"sub-owner": true, "sub-member": true},
	}
	admin := &fakeOrgAdmin{}
	a.OrgAdmin = admin
	mux := http.NewServeMux()
	(&Server{App: a}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	var org string
	for _, tok := range []string{"tok-owner", "tok-member", "tok-out"} {
		code, raw := doJSON(t, srv, http.MethodPost, "/v1/provision", tok, nil)
		if code != http.StatusOK {
			t.Fatalf("provision %s: %d %s", tok, code, raw)
		}
		var out ProvisionResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if tok == "tok-owner" {
			org = out.OrgID
		}
	}
	// Provisioning self-signs each token into its own org; the ceremony needs
	// one shared org — re-home the member and outsider rows.
	for _, sub := range []string{"sub-member", "sub-out"} {
		if err := f.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: sub, OrgID: org}); err != nil {
			t.Fatal(err)
		}
	}
	return srv, a, f
}

func TestRecoveryShareEnrollAndConflict(t *testing.T) {
	srv, _, _ := recoveryServer(t)
	pub, _, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	body := EnrollRecoveryRequest{Pubkey: base64.StdEncoding.EncodeToString(pub)}

	// Anonymous and outsiders are refused before the store is touched.
	if code, _ := doJSON(t, srv, http.MethodGet, "/v1/recovery/share", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("anon share %d", code)
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/enroll", "tok-out", body); code != http.StatusUnauthorized {
		t.Fatalf("outsider enroll %d", code)
	}

	// Member enrolls — share reads back enrolled.
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/recovery/enroll", "tok-member", body)
	if code != http.StatusOK {
		t.Fatalf("enroll %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/recovery/share", "tok-member", nil)
	if code != http.StatusOK {
		t.Fatalf("share %d", code)
	}
	var v app.RecoveryShareView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Enrolled || len(v.WrappedMaster) != 80 {
		t.Fatalf("share %+v", v)
	}
	// Re-enroll is a 409 — the pubkey never rotates outside the ceremony.
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/enroll", "tok-member", body); code != http.StatusConflict {
		t.Fatalf("re-enroll %d", code)
	}
}

func TestRecoveryRouteAuthzMatrix(t *testing.T) {
	srv, _, _ := recoveryServer(t)
	pub, _, _ := device.Generate()
	start := StartRecoveryRequest{NewPubkey: base64.StdEncoding.EncodeToString(pub), ResetMFA: true}

	// The member files for themselves.
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/recovery/start", "tok-member", start)
	if code != http.StatusOK {
		t.Fatalf("member start %d %s", code, raw)
	}
	var filed StartRecoveryResponse
	if err := json.Unmarshal(raw, &filed); err != nil {
		t.Fatal(err)
	}
	if !filed.Created || filed.Request.Status != protocol.RecoveryOpen {
		t.Fatalf("filed %+v", filed)
	}
	reqID := filed.Request.ID

	// A non-member's token resolves nothing: 401 (not provisioned), not 403.
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/start", "tok-out", StartRecoveryRequest{
		NewPubkey: base64.StdEncoding.EncodeToString(pub),
	}); code != http.StatusUnauthorized {
		t.Fatalf("outsider start %d", code)
	}
	// A member cannot file for someone else — owner leg required.
	other, _, _ := device.Generate()
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/start", "tok-member", StartRecoveryRequest{
		HumanID: "sub-owner", NewPubkey: base64.StdEncoding.EncodeToString(other),
	}); code != http.StatusForbidden {
		t.Fatalf("member owner-start %d", code)
	}
	// Members can't list or complete — those are owner verbs.
	if code, _ := doJSON(t, srv, http.MethodGet, "/v1/recovery", "tok-member", nil); code != http.StatusForbidden {
		t.Fatalf("member list %d", code)
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/"+reqID+"/complete", "tok-member", nil); code != http.StatusForbidden {
		t.Fatalf("member complete %d", code)
	}
	// But the initiator sees and cancels their own ask.
	if code, _ := doJSON(t, srv, http.MethodGet, "/v1/recovery/"+reqID, "tok-member", nil); code != http.StatusOK {
		t.Fatalf("initiator show %d", code)
	}
	// The outsider can't see someone else's request at all.
	if code, _ := doJSON(t, srv, http.MethodGet, "/v1/recovery/"+reqID, "tok-out", nil); code == http.StatusOK {
		t.Fatalf("outsider show %d", code)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/recovery/"+reqID+"/deny", "tok-member", nil)
	if code != http.StatusOK {
		t.Fatalf("initiator deny %d %s", code, raw)
	}
	var denied app.RecoveryView
	if err := json.Unmarshal(raw, &denied); err != nil {
		t.Fatal(err)
	}
	if denied.Status != protocol.RecoveryCancelled {
		t.Fatalf("initiator deny status %q, want cancelled", denied.Status)
	}
	// Second resolve is a 409.
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/"+reqID+"/deny", "tok-owner", nil); code != http.StatusConflict {
		t.Fatalf("re-deny %d", code)
	}
}

func TestRecoveryLifecycleHappyPath(t *testing.T) {
	srv, _, _ := recoveryServer(t)
	// Enroll the member's share first — their epoch starts at 1.
	pub, _, _ := device.Generate()
	code, _ := doJSON(t, srv, http.MethodPost, "/v1/recovery/enroll", "tok-member",
		EnrollRecoveryRequest{Pubkey: base64.StdEncoding.EncodeToString(pub)})
	if code != http.StatusOK {
		t.Fatalf("enroll %d", code)
	}
	// Member files with a fresh staged pubkey.
	newPub, _, _ := device.Generate()
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/recovery/start", "tok-member",
		StartRecoveryRequest{NewPubkey: base64.StdEncoding.EncodeToString(newPub), ResetMFA: true})
	if code != http.StatusOK {
		t.Fatalf("start %d", code)
	}
	var filed StartRecoveryResponse
	if err := json.Unmarshal(raw, &filed); err != nil {
		t.Fatal(err)
	}
	// Owner lists + shows the confirmer view.
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/recovery?status=open", "tok-owner", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d", code)
	}
	var list RecoveriesResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Requests) != 1 || list.Requests[0].ID != filed.Request.ID {
		t.Fatalf("list %+v", list)
	}
	if list.Requests[0].HumanEmail != "" {
		t.Fatal("no directory in tests — human_email stays empty")
	}
	// Owner completes — epoch bumps, share re-wraps to the new pubkey.
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/recovery/"+filed.Request.ID+"/complete", "tok-owner", nil)
	if code != http.StatusOK {
		t.Fatalf("complete %d %s", code, raw)
	}
	var done app.RecoveryView
	if err := json.Unmarshal(raw, &done); err != nil {
		t.Fatal(err)
	}
	if done.Status != protocol.RecoveryCompleted || done.ConfirmedBy != "sub-owner" {
		t.Fatalf("complete %+v", done)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/recovery/share", "tok-member", nil)
	if code != http.StatusOK {
		t.Fatal(code)
	}
	var v app.RecoveryShareView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.WrapPubkeyEpoch != 2 || string(v.Pubkey) != string(newPub) {
		t.Fatalf("share after recovery %+v", v)
	}
}
