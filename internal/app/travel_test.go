package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// VTX-788: Travel Mode is a server-side filter. While the org flag is on,
// items without travel_safe drop out of list/match/fill/Use on every
// surface; the toggle itself is owner-only and audited.

func travelApp(t *testing.T) (*App, protocol.Principal) {
	t.Helper()
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	owner := protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
	return a, owner
}

func TestTravelModeFiltersListMatchFillUse(t *testing.T) {
	a, owner := travelApp(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{}")
	}))
	t.Cleanup(upstream.Close)

	if _, err := a.PutItemFor(owner, ItemOpts{Name: "stripe", URI: upstream.URL, Token: []byte(secret)}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItemFor(owner, ItemOpts{Name: "gmail", URI: upstream.URL, Token: []byte("gm_secret"), TravelSafe: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "gmail", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: a.OrgID}

	names := func(items []protocol.Item) map[string]bool {
		out := map[string]bool{}
		for _, it := range items {
			out[it.Name] = true
		}
		return out
	}

	// Flag off: everything lists — the no-op case.
	listed, err := a.ItemsForPrincipal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if !names(listed)["stripe"] || !names(listed)["gmail"] {
		t.Fatalf("pre-toggle list %+v", listed)
	}

	if err := a.SetTravelModeFor(owner, true); err != nil {
		t.Fatal(err)
	}
	on, err := a.TravelMode(a.OrgID)
	if err != nil || !on {
		t.Fatalf("travel mode read %v %v", on, err)
	}

	// Human list, agent list, match, and fill all drop the non-safe item.
	listed, err = a.ItemsForPrincipal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if names(listed)["stripe"] || !names(listed)["gmail"] {
		t.Fatalf("owner list under travel mode %+v", listed)
	}
	listed, err = a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	if names(listed)["stripe"] || !names(listed)["gmail"] {
		t.Fatalf("agent list under travel mode %+v", listed)
	}
	// Both items bind the same host: only the safe one may surface.
	matched, err := a.Match(owner, upstream.URL+"/login")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].Name != "gmail" {
		t.Fatalf("hidden item still matched %+v", matched)
	}
	fills, err := a.FillLogins(owner, upstream.URL+"/login")
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].Name != "gmail" {
		t.Fatalf("hidden item still filled %+v", fills)
	}

	// A live grant on a hidden item still refuses Use — auditable deny.
	dec, err := a.Use(context.Background(), agent.ID, "stripe", http.MethodGet, upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision != protocol.DecisionDeny || dec.Reason != "travel_mode" {
		t.Fatalf("use on hidden item %+v", dec)
	}
	dec, err = a.Use(context.Background(), agent.ID, "gmail", http.MethodGet, upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision != protocol.DecisionAllow {
		t.Fatalf("safe item must still use %+v", dec)
	}

	// The denies and the toggle are audit rows.
	evs, err := a.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	var sawToggle, sawDeny bool
	for _, e := range evs {
		if e.Action == protocol.ActionTravelModeToggled {
			sawToggle = true
			if e.Reason != "enabled=true" {
				t.Fatalf("toggle reason %q", e.Reason)
			}
		}
		if e.Reason == "travel_mode" && e.Decision == protocol.DecisionDeny {
			sawDeny = true
		}
	}
	if !sawToggle || !sawDeny {
		t.Fatalf("audit missing toggle=%v deny=%v", sawToggle, sawDeny)
	}

	// Flag back off: the item resurfaces everywhere.
	if err := a.SetTravelModeFor(owner, false); err != nil {
		t.Fatal(err)
	}
	listed, err = a.ItemsForPrincipal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if !names(listed)["stripe"] {
		t.Fatalf("post-toggle list %+v", listed)
	}
	dec, err = a.Use(context.Background(), agent.ID, "stripe", http.MethodGet, upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision != protocol.DecisionAllow {
		t.Fatalf("post-toggle use %+v", dec)
	}
}

func TestTravelModeToggleIsOwnerOnly(t *testing.T) {
	a, owner := travelApp(t)
	member := protocol.Principal{Kind: protocol.PrincipalHuman, ID: familyHuman, OrgID: a.OrgID}
	a.Members = fakeMembers{members: map[string]bool{familyHuman: true}}

	if err := a.SetTravelModeFor(member, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member toggle: %v", err)
	}
	on, err := a.TravelMode(a.OrgID)
	if err != nil || on {
		t.Fatalf("flag flipped by member: %v", on)
	}
	// An agent principal is not a vault owner either.
	if err := a.SetTravelModeFor(protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: a.OrgID}, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent toggle: %v", err)
	}
	if err := a.SetTravelModeFor(owner, true); err != nil {
		t.Fatal(err)
	}
}

func TestTravelSafeFlagRoundTrips(t *testing.T) {
	a, owner := travelApp(t)
	item, err := a.PutItemFor(owner, ItemOpts{Name: "stripe", Token: []byte(secret), TravelSafe: true})
	if err != nil {
		t.Fatal(err)
	}
	if !item.TravelSafe {
		t.Fatal("create did not carry travel_safe")
	}
	got, err := a.Store.Item(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TravelSafe {
		t.Fatal("stored item lost travel_safe")
	}
	off := false
	got, err = a.UpdateItemFor(owner, "stripe", nil, nil, nil, "", nil, &off)
	if err != nil {
		t.Fatal(err)
	}
	if got.TravelSafe {
		t.Fatal("update did not clear travel_safe")
	}
	on := true
	got, err = a.UpdateItemFor(owner, "stripe", nil, nil, nil, "", nil, &on)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TravelSafe {
		t.Fatal("update did not set travel_safe")
	}
	// Absent flag leaves it untouched.
	got, err = a.UpdateItemFor(owner, "stripe", nil, nil, []string{"x"}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TravelSafe {
		t.Fatal("untouched travel_safe cleared")
	}
}

// The sqlite store must apply the column migration on existing vaults.
func TestTravelModeStoreRoundTrip(t *testing.T) {
	m := store.NewMemory()
	if on, err := m.TravelMode("org1"); err != nil || on {
		t.Fatalf("absent row reads off: %v %v", on, err)
	}
	if err := m.SetTravelMode("org1", true); err != nil {
		t.Fatal(err)
	}
	if on, err := m.TravelMode("org1"); err != nil || !on {
		t.Fatalf("set: %v %v", on, err)
	}
	if on, err := m.TravelMode("org2"); err != nil || on {
		t.Fatalf("other org reads off: %v", on)
	}
}
