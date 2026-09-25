// Package billing is the Vortex webhook receiver — the only inbound edge of
// the billing plane. It verifies Vortex-Signature (HMAC-SHA256 over
// "{t}.{body}", ±5min tolerance, constant-time compare — the scheme in
// @vortex/billing/webhooks/signatures.ts) and maps subscription/entitlement
// lifecycle events onto the org's local plan state. The Use gate reads that
// state; nothing in the request path talks to Vortex directly.
package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/VortexNYC/veil/internal/protocol"
	"time"

	"github.com/VortexNYC/veil/internal/store"
)

var (
	ErrMissingHeader  = errors.New("missing Vortex-Signature header")
	ErrInvalidHeader  = errors.New("malformed Vortex-Signature header")
	ErrStaleTimestamp = errors.New("signature timestamp outside tolerance")
	ErrBadSignature   = errors.New("signature mismatch")
)

const tolerance = 5 * time.Minute

// VerifySignature checks the Vortex-Signature header against the raw payload.
// Header form: "t=<ms-epoch>,v1=<hex>" — multiple v1 entries allowed so the
// platform can rotate secrets without breaking in-flight deliveries.
func VerifySignature(secret, header string, payload []byte, now time.Time) error {
	if header == "" {
		return ErrMissingHeader
	}
	var ts int64
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return ErrInvalidHeader
		}
		switch kv[0] {
		case "t":
			v, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil || v <= 0 {
				return ErrInvalidHeader
			}
			ts = v
		case "v1":
			b, err := hex.DecodeString(kv[1])
			if err != nil {
				return ErrInvalidHeader
			}
			sigs = append(sigs, b)
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return ErrInvalidHeader
	}
	if d := now.UnixMilli() - ts; d > int64(tolerance/time.Millisecond) || d < -int64(tolerance/time.Millisecond) {
		return ErrStaleTimestamp
	}
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "%d.", ts)
	m.Write(payload)
	expected := m.Sum(nil)
	for _, sig := range sigs {
		if subtle.ConstantTimeCompare(expected, sig) == 1 {
			return nil
		}
	}
	return ErrBadSignature
}

// Event is the Vortex webhook envelope. Data stays raw — each aggregate type
// decodes its own shape.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt int64           `json:"createdAt"` // ms epoch
	Data      json.RawMessage `json:"data"`
}

// Store is the sliver of the vault store the receiver needs.
type Store interface {
	Billing(orgID string) (store.OrgBilling, error)
	SetBilling(store.OrgBilling) error
	AppendAudit(protocol.AuditEvent) error
}

type subscriptionData struct {
	Subscription struct {
		CustomerExternalID string `json:"customerExternalId"`
		CustomerID         string `json:"customerId"`
		Status             string `json:"status"`
	} `json:"subscription"`
}

type entitlementData struct {
	Entitlement struct {
		CustomerExternalID string `json:"customerExternalId"`
		CustomerID         string `json:"customerId"`
		Key                string `json:"entitlementKey"`
		Status             string `json:"status"`
	} `json:"entitlement"`
}

// Apply maps a verified event to org billing state. Unknown types, missing
// customers, and malformed payloads are durable no-ops — the endpoint must
// survive events from Vortex versions newer than this build. Ordering is
// guarded by the event's createdAt: a replayed or stale event never
// overwrites a newer plan state.
func Apply(s Store, ev Event) error {
	orgID, plan, ok := planFor(ev)
	if !ok {
		return nil
	}
	cur, err := s.Billing(orgID)
	if err != nil {
		return fmt.Errorf("billing read: %w", err)
	}
	at := time.UnixMilli(ev.CreatedAt).UTC()
	if !cur.UpdatedAt.IsZero() && !at.After(cur.UpdatedAt) {
		return nil // replayed or stale — newer state already landed
	}
	if cur.Plan == plan {
		return s.SetBilling(store.OrgBilling{
			OrgID:     orgID,
			Plan:      plan,
			UpdatedAt: at,
		})
	}
	if err := s.SetBilling(store.OrgBilling{
		OrgID:     orgID,
		Plan:      plan,
		UpdatedAt: at,
	}); err != nil {
		return err
	}
	// A plan flip changes what every agent in the org may do — that is an
	// authorization decision and belongs in the ledger.
	return s.AppendAudit(protocol.AuditEvent{
		Time:     at,
		OrgID:    orgID,
		AgentID:  "vortex-webhook",
		ItemID:   "",
		Action:   protocol.ActionBillingPlanChanged,
		Decision: protocol.DecisionAllow,
		Reason:   ev.Type + ":" + plan,
	})
}

// planFor extracts (orgID, plan) from the event data. Subscription-active
// statuses map to "active" — past_due keeps access through the dunning grace;
// paused/canceled/draft return to "free". The veil entitlement grant/revoke
// is the operator override.
func planFor(ev Event) (orgID, plan string, ok bool) {
	switch {
	case strings.HasPrefix(ev.Type, "subscription."):
		var d subscriptionData
		if json.Unmarshal(ev.Data, &d) != nil {
			return "", "", false
		}
		orgID = d.Subscription.CustomerExternalID
		if orgID == "" {
			orgID = d.Subscription.CustomerID
		}
		if orgID == "" {
			return "", "", false
		}
		switch d.Subscription.Status {
		case "active", "trialing", "past_due":
			return orgID, "active", true
		default: // draft, paused, canceled
			return orgID, "free", true
		}
	case ev.Type == "entitlement.granted" || ev.Type == "entitlement.revoked":
		var d entitlementData
		if json.Unmarshal(ev.Data, &d) != nil {
			return "", "", false
		}
		if d.Entitlement.Key != "veil" {
			return "", "", false
		}
		orgID = d.Entitlement.CustomerExternalID
		if orgID == "" {
			orgID = d.Entitlement.CustomerID
		}
		if orgID == "" {
			return "", "", false
		}
		if ev.Type == "entitlement.granted" && d.Entitlement.Status != "revoked" {
			return orgID, "active", true
		}
		return orgID, "free", true
	}
	return "", "", false
}
