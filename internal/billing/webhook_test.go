package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

const testSecret = "whsec_test_key_9f8e7d6c5b4a"

// Cross-implementation vector: generated with Node's crypto — the same
// algorithm as packages/billing/src/webhooks/signatures.ts.
const (
	vecPayload = `{"id":"evt_1","type":"subscription.updated","createdAt":1730000000000,"data":{"subscription":{"customerExternalId":"org-1","status":"active"}}}`
	vecHeader  = "t=1730000000123,v1=61ea40a027f21e6143e84bc5e102b38f5f00cf98aa74e9385ae7cc057df82dfb"
	vecNow     = 1730000000123
)

func sign(secret, payload string, t int64) string {
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "%d.", t)
	m.Write([]byte(payload))
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(m.Sum(nil)))
}

func TestVerifySignatureVector(t *testing.T) {
	if err := VerifySignature(testSecret, vecHeader, []byte(vecPayload), time.UnixMilli(vecNow)); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifySignatureFailures(t *testing.T) {
	now := time.UnixMilli(vecNow)
	for name, tc := range map[string]struct {
		header  string
		payload string
		secret  string
		now     time.Time
		want    error
	}{
		"missing header":   {"", vecPayload, testSecret, now, ErrMissingHeader},
		"malformed header": {"garbage", vecPayload, testSecret, now, ErrInvalidHeader},
		"no v1":            {"t=1730000000123", vecPayload, testSecret, now, ErrInvalidHeader},
		"bad signature":    {"t=1730000000123,v1=deadbeef", vecPayload, testSecret, now, ErrBadSignature},
		"wrong secret":     {sign("other-secret", vecPayload, vecNow), vecPayload, testSecret, now, ErrBadSignature},
		"tampered payload": {sign(testSecret, vecPayload, vecNow), vecPayload + " ", testSecret, now, ErrBadSignature},
		"stale timestamp":  {sign(testSecret, vecPayload, vecNow-600_000), vecPayload, testSecret, now, ErrStaleTimestamp},
		"future timestamp": {sign(testSecret, vecPayload, vecNow+600_000), vecPayload, testSecret, now, ErrStaleTimestamp},
		"boundary ok":      {sign(testSecret, vecPayload, vecNow-300_000), vecPayload, testSecret, now, nil},
	} {
		t.Run(name, func(t *testing.T) {
			err := VerifySignature(tc.secret, tc.header, []byte(tc.payload), tc.now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

// Rotation: multiple v1 entries — any matching signature passes.
func TestVerifySignatureRotation(t *testing.T) {
	header := sign("old-secret", vecPayload, vecNow) + "," + sign(testSecret, vecPayload, vecNow)[len("t=1730000000123,"):]
	header = "t=1730000000123," + header[len("t=1730000000123,"):]
	// header is now t=...,v1=<old>,v1=<new>
	if err := VerifySignature(testSecret, header, []byte(vecPayload), time.UnixMilli(vecNow)); err != nil {
		t.Fatalf("rotation signature rejected: %v", err)
	}
}

type fakeStore struct {
	billing map[string]store.OrgBilling
	audit   []protocol.AuditEvent
}

func (f *fakeStore) Billing(orgID string) (store.OrgBilling, error) {
	if ob, ok := f.billing[orgID]; ok {
		return ob, nil
	}
	return store.OrgBilling{OrgID: orgID, Plan: "free"}, nil
}
func (f *fakeStore) SetBilling(ob store.OrgBilling) error {
	f.billing[ob.OrgID] = ob
	return nil
}
func (f *fakeStore) AppendAudit(e protocol.AuditEvent) error {
	f.audit = append(f.audit, e)
	return nil
}

func newFake() *fakeStore {
	return &fakeStore{billing: map[string]store.OrgBilling{}}
}

func event(t *testing.T, id, typ string, createdAt int64, data string) Event {
	return Event{ID: id, Type: typ, CreatedAt: createdAt, Data: []byte(data)}
}

func TestApplySubscriptionLifecycle(t *testing.T) {
	for name, tc := range map[string]struct {
		typ  string
		data string
		want string
	}{
		"created active": {"subscription.created", `{"subscription":{"customerExternalId":"org-1","status":"active"}}`, "active"},
		"trialing":       {"subscription.updated", `{"subscription":{"customerExternalId":"org-1","status":"trialing"}}`, "active"},
		"past_due grace": {"subscription.updated", `{"subscription":{"customerExternalId":"org-1","status":"past_due"}}`, "active"},
		"paused":         {"subscription.updated", `{"subscription":{"customerExternalId":"org-1","status":"paused"}}`, "free"},
		"canceled":       {"subscription.canceled", `{"subscription":{"customerExternalId":"org-1","status":"canceled"}}`, "free"},
		"draft":          {"subscription.updated", `{"subscription":{"customerExternalId":"org-1","status":"draft"}}`, "free"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newFake()
			if err := Apply(s, event(t, "evt_x", tc.typ, 1730000000000, tc.data)); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Billing("org-1")
			if got.Plan != tc.want {
				t.Fatalf("plan=%q want %q", got.Plan, tc.want)
			}
		})
	}
}

func TestApplyEntitlement(t *testing.T) {
	s := newFake()
	if err := Apply(s, event(t, "e1", "entitlement.granted", 1730000000000, `{"entitlement":{"customerExternalId":"org-1","entitlementKey":"veil"}}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Billing("org-1"); got.Plan != "active" {
		t.Fatalf("granted plan=%q", got.Plan)
	}
	// A plan flip is an authorization decision — it lands in the audit ledger.
	var audited bool
	for _, e := range s.audit {
		if e.Action == protocol.ActionBillingPlanChanged && e.OrgID == "org-1" {
			audited = true
		}
	}
	if !audited {
		t.Fatal("plan flip not audited")
	}
	if err := Apply(s, event(t, "e2", "entitlement.revoked", 1730000001000, `{"entitlement":{"customerExternalId":"org-1","entitlementKey":"veil"}}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Billing("org-1"); got.Plan != "free" {
		t.Fatalf("revoked plan=%q", got.Plan)
	}
}

func TestApplyIgnoresUnknown(t *testing.T) {
	s := newFake()
	// Unknown type, missing customer, malformed data — all durable no-ops.
	for _, ev := range []Event{
		event(t, "e1", "invoice.paid", 1, `{"invoice":{}}`),
		event(t, "e2", "subscription.updated", 2, `{"subscription":{"status":"active"}}`),
		event(t, "e3", "subscription.updated", 3, `not json`),
	} {
		if err := Apply(s, ev); err != nil {
			t.Fatalf("event %s: %v", ev.ID, err)
		}
	}
	if got, _ := s.Billing("org-1"); got.Plan != "free" {
		t.Fatalf("unknown events must not flip state: %q", got.Plan)
	}
}

// Replay + reorder: the same event twice is a no-op; a stale event never
// overwrites a newer plan state.
func TestApplyReplayAndStale(t *testing.T) {
	s := newFake()
	active := event(t, "e1", "subscription.updated", 2000, `{"subscription":{"customerExternalId":"org-1","status":"active"}}`)
	if err := Apply(s, active); err != nil {
		t.Fatal(err)
	}
	if err := Apply(s, active); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Billing("org-1"); got.Plan != "active" {
		t.Fatalf("replay broke state: %q", got.Plan)
	}
	stale := event(t, "e0", "subscription.canceled", 1000, `{"subscription":{"customerExternalId":"org-1","status":"canceled"}}`)
	if err := Apply(s, stale); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Billing("org-1"); got.Plan != "active" {
		t.Fatalf("stale event overwrote newer state: %q", got.Plan)
	}
}
