package app

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

func shareOwner(a *App) protocol.Principal {
	return protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
}

func TestShareCreateRedeemOnce(t *testing.T) {
	a, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	sh, err := a.CreateShareFor(shareOwner(a), "stripe", []byte("envelope"), []byte(`{"v":1}`), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.RedeemShare(sh.ID, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Blob) != "envelope" || got.ConsumedAt == nil {
		t.Fatalf("redeem %+v", got)
	}
	if _, err := a.RedeemShare(sh.ID, "203.0.113.9"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second redeem: %v", err)
	}

	events, err := a.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	var created, redeemed bool
	for _, e := range events {
		if e.Action == protocol.ActionShareCreated {
			created = true
		}
		if e.Action == protocol.ActionShareRedeemed {
			redeemed = true
		}
	}
	if !created || !redeemed {
		t.Fatalf("audit missing share rows: %+v", events)
	}
}

func TestShareCreateOwnerOnly(t *testing.T) {
	a, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	member := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "member-2", OrgID: a.OrgID}
	if _, err := a.CreateShareFor(member, "x", []byte("b"), nil, time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member create: %v", err)
	}
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: a.OrgID}
	if _, err := a.CreateShareFor(agent, "x", []byte("b"), nil, time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent create: %v", err)
	}
	if _, err := a.CreateShareFor(shareOwner(a), "x", nil, nil, time.Hour); err == nil {
		t.Fatal("empty blob accepted")
	}
	if _, err := a.CreateShareFor(shareOwner(a), "x", []byte("b"), nil, 31*24*time.Hour); err == nil {
		t.Fatal("ttl over max accepted")
	}
}

func TestShareRevokeAndList(t *testing.T) {
	a, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	owner := shareOwner(a)

	sh, err := a.CreateShareFor(owner, "stripe", []byte("envelope"), nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.ListSharesFor(owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || len(list[0].Blob) != 0 {
		t.Fatalf("list leaked blob or missed share: %+v", list)
	}
	if err := a.RevokeShareFor(owner, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RedeemShare(sh.ID, "203.0.113.9"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("redeem after revoke: %v", err)
	}
	member := protocol.Principal{Kind: protocol.PrincipalHuman, ID: "member-2", OrgID: a.OrgID}
	if _, err := a.ListSharesFor(member); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member list: %v", err)
	}
}

func TestShareRedeemLimited(t *testing.T) {
	a, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// Ten probes a minute per id — the 11th is the limiter, not a miss.
	for i := 0; i < 10; i++ {
		if _, err := a.RedeemShare("shr_probe", "203.0.113.9"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("probe %d: %v", i, err)
		}
	}
	if _, err := a.RedeemShare("shr_probe", "203.0.113.9"); !errors.Is(err, ErrShareLimit) {
		t.Fatalf("11th probe: %v", err)
	}
}

func TestShareRedeemIPLimited(t *testing.T) {
	a, err := OpenOrInit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// Sixty attempts a minute per caller IP across all shares — distinct ids,
	// so no single share's cap fires first (VTX-790).
	for i := 0; i < 60; i++ {
		id := "shr_spray_" + strconv.Itoa(i)
		if _, err := a.RedeemShare(id, "198.51.100.7"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("probe %d: %v", i, err)
		}
	}
	if _, err := a.RedeemShare("shr_spray_60", "198.51.100.7"); !errors.Is(err, ErrShareLimit) {
		t.Fatalf("61st probe from one IP: %v", err)
	}
	// The bound is per-IP — other callers are unaffected.
	if _, err := a.RedeemShare("shr_spray_60", "203.0.113.9"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fresh IP, same id: %v", err)
	}
}
