package app

import (
	"context"
	"errors"
	"testing"

	"github.com/VortexNYC/veil/internal/store"
)

func TestDeviceLinkCreateRateLimit(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	pub := make([]byte, 32)
	pub[0] = 1
	ctx := context.Background()
	ip := "203.0.113.50"
	for i := 0; i < 30; i++ {
		if _, err := a.MintDeviceLink(ctx, ip, "phone", pub); err != nil {
			t.Fatalf("mint %d: %v", i, err)
		}
	}
	if _, err := a.MintDeviceLink(ctx, ip, "phone", pub); !errors.Is(err, ErrDeviceLinkLimit) {
		t.Fatalf("want rate limit, got %v", err)
	}
}

func TestDeviceLinkClaimRateLimit(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	pub := make([]byte, 32)
	pub[0] = 2
	ctx := context.Background()
	l, err := a.MintDeviceLink(ctx, "198.51.100.1", "tab", pub)
	if err != nil {
		t.Fatal(err)
	}
	// Not sealed yet — claims fail as not found, but still burn claim budget.
	ip := "198.51.100.9"
	for i := 0; i < 10; i++ {
		if _, err := a.ClaimDeviceLink(ctx, ip, l.Token); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
	if _, err := a.ClaimDeviceLink(ctx, ip, l.Token); !errors.Is(err, ErrDeviceLinkLimit) {
		t.Fatalf("want rate limit, got %v", err)
	}
}
