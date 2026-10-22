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
	"path"
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
	// ManagementPaths lists URL path prefixes on this provider's host that
	// mint, list, or revoke API keys. Agent fetch calls to these paths are
	// denied by the grant engine so a self-rotating credential cannot be
	// used by an agent to mint its own keys.
	ManagementPaths() []string
}

// ForHost maps an item's URI host to a registered provider rotator.
func ForHost(rawURL string) (Rotator, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return nil, false
	}
	if r, ok := byHost[strings.ToLower(u.Host)]; ok {
		return r, true
	}
	r, ok := byHost[strings.ToLower(u.Hostname())]
	return r, ok
}

// ManagementEndpoint reports whether rawURL targets a registered provider's
// key-management endpoint — paths only the broker's RotateItem may call.
func ManagementEndpoint(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return false
	}
	r, ok := ForHost(rawURL)
	if !ok {
		return false
	}
	decoded := u.Path
	for range 4 {
		next, err := url.PathUnescape(decoded)
		if err != nil {
			// Malformed escape on a provider with management endpoints fails closed.
			return true
		}
		if next == decoded {
			break
		}
		decoded = next
	}
	clean := path.Clean("/" + strings.ToLower(decoded))
	for _, prefix := range r.ManagementPaths() {
		p := path.Clean("/" + strings.ToLower(prefix))
		if clean == p || strings.HasPrefix(clean, p+"/") {
			return true
		}
	}
	return false
}

var byHost = map[string]Rotator{}

func register(hosts []string, r Rotator) {
	for _, h := range hosts {
		byHost[strings.ToLower(h)] = r
	}
}

// Register binds a rotator to a host or host:port.
func Register(host string, r Rotator) {
	byHost[strings.ToLower(host)] = r
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

var errNoRotator = fmt.Errorf("rotate: no provider rotator for host")

func ErrNoRotator() error { return errNoRotator }
