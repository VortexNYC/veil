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
	"net/netip"
	"net/url"
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
	var audiences []string
	byAudience := map[string][]protocol.Workload{}
	for _, w := range bound {
		if _, ok := byAudience[w.Audience]; !ok {
			audiences = append(audiences, w.Audience)
		}
		byAudience[w.Audience] = append(byAudience[w.Audience], w)
	}
	for _, aud := range audiences {
		sub, err := c.subject(ctx, iss, aud, rawToken)
		if err != nil {
			last = err
			continue
		}
		hit := bestMatch(byAudience[aud], sub)
		if hit == nil {
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

// bestMatch picks the governing workload binding for sub among candidates
// sharing an (issuer, audience): exact subject match always wins; otherwise
// the longest trailing-* prefix match wins. Two prefixes of equal length can
// never both match sub because (issuer, subject) is the primary key.
func bestMatch(cands []protocol.Workload, sub string) *protocol.Workload {
	var best *protocol.Workload
	bestLen := -1
	for i := range cands {
		w := &cands[i]
		if w.Subject == sub {
			return w
		}
		if strings.HasSuffix(w.Subject, "*") && len(w.Subject) > 1 {
			prefix := w.Subject[:len(w.Subject)-1]
			if strings.HasPrefix(sub, prefix) && len(prefix) > bestLen {
				best = w
				bestLen = len(prefix)
			}
		}
	}
	return best
}

// ValidateBinding enforces Anthropic-style WIF safety rules at bind time:
//   - issuer must be https:// (or http:// on loopback for local dev/tests),
//     with no userinfo, query, or fragment, and must not target private,
//     link-local, multicast, or cloud-metadata (169.254.0.0/16) IP literals.
//   - subject may be an exact claim or a trailing-* prefix pattern whose
//     prefix is >= 4 chars and ends on a delimiter (':', '/', '-', '.').
//     On https://token.actions.githubusercontent.com, a wildcard subject must
//     include the owner/ substring (repo:<owner>/...) so 'repo:*' can never
//     trust every public repository on GitHub.
func ValidateBinding(issuer, subject string) error {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return fmt.Errorf("workload: issuer and subject are required")
	}
	u, err := url.Parse(issuer)
	if err != nil || u == nil || u.Host == "" {
		return fmt.Errorf("workload: invalid issuer URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("workload: issuer URL must not contain userinfo, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	scheme := strings.ToLower(u.Scheme)
	isLoopbackHost := host == "localhost"
	if ip, perr := netip.ParseAddr(host); perr == nil {
		ip = ip.Unmap()
		if ip.IsLoopback() {
			isLoopbackHost = true
		} else if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("workload: issuer IP literal %s is not a public address", host)
		}
	}
	switch scheme {
	case "https":
	case "http":
		if !isLoopbackHost {
			return fmt.Errorf("workload: issuer must use https")
		}
	default:
		return fmt.Errorf("workload: issuer must use https")
	}

	if strings.Contains(subject, "*") {
		if strings.Count(subject, "*") != 1 || !strings.HasSuffix(subject, "*") {
			return fmt.Errorf("workload: wildcard '*' is only allowed at the end of subject")
		}
		prefix := strings.TrimSuffix(subject, "*")
		if len(prefix) < 4 {
			return fmt.Errorf("workload: subject prefix before '*' is too short")
		}
		last := prefix[len(prefix)-1]
		if last != ':' && last != '/' && last != '-' && last != '.' {
			return fmt.Errorf("workload: subject prefix before '*' must end with ':', '/', '-', or '.'")
		}
		if host == "token.actions.githubusercontent.com" {
			if !strings.HasPrefix(prefix, "repo:") || !strings.Contains(prefix, "/") {
				return fmt.Errorf("workload: github-actions wildcard subject must scope at least repo:<owner>/*")
			}
		}
	}
	return nil
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
