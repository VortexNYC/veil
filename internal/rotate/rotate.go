// Package rotate owns broker-side credential rotation. A rotator knows one
// provider's key-management API: create a fresh credential, verify it, retire
// the old one. Agents never see this — the item id and grants are unchanged,
// only the sealed material is replaced.
//
// Coverage is a matrix, not a claim: providers with issuance APIs get
// zero-touch rotation; dashboard-only providers get nothing here yet.
package rotate

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Rotator is one provider's key-management API. Create and Retire take the
// live (old) secret as an explicit parameter — it signs the provider's
// management calls. It is never stored in a context or logged.
type Rotator interface {
	// Create mints a fresh credential for the item's name, returning the new
	// secret and the provider-side key id (stored on the item so the old
	// credential can be retired later). liveKey is the current secret.
	Create(ctx context.Context, itemName string, liveKey []byte) (secret []byte, keyID string, err error)
	// Verify proves the new credential authenticates — cheap authed call,
	// no side effects.
	Verify(ctx context.Context, secret []byte) error
	// Retire deletes the provider-side credential by id, signed with
	// liveKey. Best-effort — an orphan key still costs the provider
	// nothing, but it must be logged.
	Retire(ctx context.Context, keyID string, liveKey []byte) error
}

// ForHost maps an item's URI host to a registered provider rotator.
func ForHost(rawURL string) (Rotator, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return nil, false
	}
	r, ok := byHost[u.Hostname()]
	return r, ok
}

var byHost = map[string]Rotator{}

func register(hosts []string, r Rotator) {
	for _, h := range hosts {
		byHost[strings.ToLower(h)] = r
	}
}

// Coverage is the provider matrix for docs/status — which hosts rotate
// zero-touch.
func Coverage() []string {
	out := make([]string, 0, len(byHost))
	for h := range byHost {
		out = append(out, h)
	}
	return out
}

// ProviderIDTag is the item tag carrying the provider-side key id of the
// live credential — rot:<key-id>. Not secret; lets Retire find the old key.
// It must be stripped from agent-facing item lists — see ScrubAgentItem.
const ProviderIDTagPrefix = "rot:"

func ProviderKeyID(tags []string) string {
	for _, t := range tags {
		if s, ok := strings.CutPrefix(t, ProviderIDTagPrefix); ok {
			return s
		}
	}
	return ""
}

// StripRotationTags removes provider bookkeeping tags from tags an agent is
// about to see — rotation state is owner-facing, not agent-facing.
func StripRotationTags(tags []string) []string {
	out := tags[:0:0]
	for _, t := range tags {
		if !strings.HasPrefix(t, ProviderIDTagPrefix) {
			out = append(out, t)
		}
	}
	return out
}

var errNoRotator = fmt.Errorf("rotate: no provider rotator for host")

func ErrNoRotator() error { return errNoRotator }
