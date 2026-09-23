// Package workload verifies an external OIDC ID token and maps it to an
// existing agent. Same idea as Entra workload federation: trust an issuer,
// do not issue tokens, do not become an IdP.
//
// Verification is github.com/coreos/go-oidc/v3. We do not parse signatures.
package workload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/sync/singleflight"

	"github.com/VortexNYC/veil/internal/oidchttp"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

type Checker struct {
	Store store.Store

	mu        sync.Mutex
	providers map[string]*oidc.Provider
	group     singleflight.Group
}

func New(s store.Store) *Checker {
	return &Checker{Store: s, providers: map[string]*oidc.Provider{}}
}

// Agent verifies rawToken and returns the bound agent. Unknown issuers are
// rejected before any discovery request.
func (c *Checker) Agent(ctx context.Context, rawToken string) (protocol.Principal, error) {
	rawToken = strings.TrimSpace(rawToken)
	iss, err := unverifiedIssuer(rawToken)
	if err != nil {
		return protocol.Principal{}, err
	}
	bound, err := c.Store.WorkloadsForIssuer(iss)
	if err != nil {
		return protocol.Principal{}, err
	}
	if len(bound) == 0 {
		return protocol.Principal{}, fmt.Errorf("workload: issuer not trusted")
	}

	var last error
	seen := map[string]struct{}{}
	for _, w := range bound {
		if _, ok := seen[w.Audience]; ok {
			continue
		}
		seen[w.Audience] = struct{}{}
		sub, err := c.subject(ctx, iss, w.Audience, rawToken)
		if err != nil {
			last = err
			continue
		}
		hit, err := c.Store.Workload(iss, sub)
		if err != nil {
			return protocol.Principal{}, err
		}
		if hit == nil || hit.Audience != w.Audience {
			continue
		}
		agent, err := c.Store.Agent(hit.AgentID)
		if err != nil {
			return protocol.Principal{}, err
		}
		return agent, nil
	}
	if last != nil {
		return protocol.Principal{}, fmt.Errorf("workload: %w", last)
	}
	return protocol.Principal{}, fmt.Errorf("workload: subject not bound")
}

func (c *Checker) subject(ctx context.Context, issuer, audience, raw string) (string, error) {
	p, err := c.provider(ctx, issuer)
	if err != nil {
		return "", err
	}
	tok, err := p.Verifier(&oidc.Config{ClientID: audience}).Verify(ctx, raw)
	if err != nil {
		return "", err
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := tok.Claims(&claims); err != nil {
		return "", err
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("workload: empty subject")
	}
	return claims.Sub, nil
}

// provider resolves the issuer's OIDC provider, discovering on first use.
// Per-issuer singleflight: concurrent cold auths on the same issuer share
// one discovery call, and a cold issuer never serializes auths bound to a
// different one — the map mutex is only ever held for lookups.
func (c *Checker) provider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	c.mu.Lock()
	p, ok := c.providers[issuer]
	c.mu.Unlock()
	if ok {
		return p, nil
	}
	v, err, _ := c.group.Do(issuer, func() (any, error) {
		c.mu.Lock()
		p, ok := c.providers[issuer]
		c.mu.Unlock()
		if ok {
			return p, nil
		}
		p, err := oidc.NewProvider(oidchttp.Context(ctx), issuer)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.providers[issuer] = p
		c.mu.Unlock()
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*oidc.Provider), nil
}

// unverifiedIssuer reads iss so we can refuse unknown issuers before discovery.
// The signature is checked by go-oidc after that.
func unverifiedIssuer(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("workload: malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("workload: malformed token")
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Iss == "" {
		return "", fmt.Errorf("workload: missing issuer")
	}
	return claims.Iss, nil
}
