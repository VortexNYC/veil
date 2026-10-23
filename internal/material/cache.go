package material

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const tokenCacheHeadroom = 60 * time.Second

type cachedToken struct {
	access     string
	extraScrub string
	expiresAt  time.Time
}

// TokenCache caches short-lived access tokens minted from OAuth refresh or
// RFC 7523 WIF exchanges until expiresAt - 60s headroom, keyed by a SHA-256
// digest of the envelope's token-minting inputs. Rotating or editing the
// item's secret changes the digest and invalidates the entry automatically.
type TokenCache struct {
	mu      sync.Mutex
	entries map[string]cachedToken
	group   singleflight.Group
}

var defaultTokenCache = NewTokenCache()

func NewTokenCache() *TokenCache {
	return &TokenCache{entries: map[string]cachedToken{}}
}

func tokenCacheKey(env Envelope) string {
	h := sha256.New()
	write := func(fields ...string) {
		for _, f := range fields {
			_, _ = fmt.Fprintf(h, "%d:%s\x00", len(f), f)
		}
	}
	if env.FederationRuleID != "" {
		write("wif", env.TokenURL, env.AssertionTokenURL, env.AssertionClientID,
			env.AssertionClientSec, env.AssertionAudience, env.FederationRuleID,
			env.FederationOrgID, env.ServiceAccountID, env.WorkspaceID)
	} else {
		write("oauth", env.TokenURL, env.ClientID, env.ClientSec, env.Refresh)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *TokenCache) get(key string, now time.Time) (cachedToken, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return cachedToken{}, false
	}
	if !now.Add(tokenCacheHeadroom).Before(e.expiresAt) {
		delete(c.entries, key)
		return cachedToken{}, false
	}
	return e, true
}

func (c *TokenCache) put(key, access, extraScrub string, expiresAt time.Time, now time.Time) {
	if access == "" || !now.Add(tokenCacheHeadroom).Before(expiresAt) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cachedToken{
		access:     access,
		extraScrub: extraScrub,
		expiresAt:  expiresAt,
	}
}
