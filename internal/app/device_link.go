package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// ErrDeviceLinkLimit means a public device-link endpoint hit the rate limiter.
var ErrDeviceLinkLimit = errors.New("app: device link rate limit")

var (
	// Pending link minting is unauthenticated — bound per caller IP.
	deviceLinkCreateIPLim = &slidingLimiter{max: 30, win: time.Minute, failOpenFull: true}
	// Claim uses the token as capability; per-token + per-IP like share redeem.
	deviceLinkClaimLim   = newSlidingLimiter(10, time.Minute)
	deviceLinkClaimIPLim = &slidingLimiter{max: 60, win: time.Minute, failOpenFull: true}
)

// MintDeviceLink creates a pending device link (QR handoff). Unauthenticated;
// org binding happens at approve time.
func (a *App) MintDeviceLink(ctx context.Context, clientIP string, label string, pubkey []byte) (protocol.DeviceLink, error) {
	if len(pubkey) != 32 {
		return protocol.DeviceLink{}, fmt.Errorf("app: invalid pubkey length")
	}
	if !deviceLinkCreateIPLim.allow(clientIP) {
		return protocol.DeviceLink{}, ErrDeviceLinkLimit
	}
	var tok [16]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return protocol.DeviceLink{}, err
	}
	now := time.Now()
	l := protocol.DeviceLink{
		Token:     hex.EncodeToString(tok[:]),
		Label:     label,
		Pubkey:    pubkey,
		CreatedAt: now,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := a.Store.PutDeviceLink(ctx, l); err != nil {
		return protocol.DeviceLink{}, err
	}
	return l, nil
}

// ClaimDeviceLink consumes a sealed link once for the new device. Unauthenticated;
// the token is the proof.
func (a *App) ClaimDeviceLink(ctx context.Context, clientIP, token string) (protocol.DeviceLink, error) {
	if !deviceLinkClaimIPLim.allow(clientIP) || !deviceLinkClaimLim.allow(token) {
		return protocol.DeviceLink{}, ErrDeviceLinkLimit
	}
	l, err := a.Store.DeviceLink(ctx, token)
	if err != nil || l.ConsumedAt != nil || len(l.Sealed) == 0 || time.Now().After(l.ExpiresAt) {
		return protocol.DeviceLink{}, store.ErrNotFound
	}
	if err := a.Store.ConsumeDeviceLink(ctx, l.Token); err != nil {
		return protocol.DeviceLink{}, err
	}
	return l, nil
}
