package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// AccessAnswer is Vortex's verdict on one customer's entitlement key —
// GET /v1/customers/{customerId}/access?key=<capability>. Allowed is the
// only field Veil acts on; State/ReasonCodes stay for debugging.
type AccessAnswer struct {
	Allowed     bool
	State       string
	ReasonCodes []string
}

// CheckAccess asks Vortex whether the customer's key capability is usable —
// "key=veil" is the paid-plan entitlement. Wire contract: billing package
// CheckCustomerAccessResponse — {data:{access:{allowed,state,reasonCodes}}}.
func (c *Client) CheckAccess(ctx context.Context, customerID, key string) (AccessAnswer, error) {
	path := "/v1/customers/" + url.PathEscape(customerID) + "/access?key=" + url.QueryEscape(key)
	status, raw, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return AccessAnswer{}, err
	}
	if status >= 300 {
		return AccessAnswer{}, fmt.Errorf("vortex access: %d", status)
	}
	var out struct {
		Data struct {
			Access struct {
				Allowed     bool     `json:"allowed"`
				State       string   `json:"state"`
				ReasonCodes []string `json:"reasonCodes"`
			} `json:"access"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return AccessAnswer{}, fmt.Errorf("vortex access: bad response: %w", err)
	}
	return AccessAnswer{
		Allowed:     out.Data.Access.Allowed,
		State:       out.Data.Access.State,
		ReasonCodes: out.Data.Access.ReasonCodes,
	}, nil
}

// AccessLookuper is what AccessChecker needs from the Vortex client — the
// seam tests fake.
type AccessLookuper interface {
	CheckAccess(ctx context.Context, customerID, key string) (AccessAnswer, error)
}

// AccessChecker is the deny-path backstop (VEIL-62): when local plan state
// caps an org, one definitive Vortex answer per TTL decides whether a missed
// webhook left the plan stale. The webhook is durable truth — this only ever
// *upgrades* a stale free→active; it never downgrades and never hard-fails:
// errors and non-answers return false and the local denial stands.
//
// Caching is mandatory, not an optimization: the check sits on a per-request
// path (denied Use), and a billing outage must not become an auth outage.
type AccessChecker struct {
	Lookuper AccessLookuper
	Key      string        // entitlement key — "veil"
	TTL      time.Duration // cache window; default 60s

	mu    sync.Mutex
	cache map[string]accessCacheEntry
	group singleflight.Group
}

type accessCacheEntry struct {
	allowed bool
	at      time.Time
}

// Allowed reports whether Vortex currently grants the org's entitlement.
// Only a definitive allowed=true is true — errors, timeouts, 404s, and
// explicit denials all return false (callers keep local state). Repeated
// calls inside TTL serve the cached answer; concurrent misses singleflight
// to one upstream call per org.
func (c *AccessChecker) Allowed(ctx context.Context, orgID, customerID string) bool {
	if c.Lookuper == nil {
		return false
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	c.mu.Lock()
	if e, ok := c.cache[orgID]; ok && time.Since(e.at) < ttl {
		c.mu.Unlock()
		return e.allowed
	}
	c.mu.Unlock()

	// Singleflight on the org id — a burst of capped denies makes one call.
	v, err, _ := c.group.Do(orgID, func() (any, error) {
		key := c.Key
		if key == "" {
			key = "veil"
		}
		ans, err := c.Lookuper.CheckAccess(ctx, customerID, key)
		if err != nil {
			return false, err
		}
		c.mu.Lock()
		if c.cache == nil {
			c.cache = map[string]accessCacheEntry{}
		}
		c.cache[orgID] = accessCacheEntry{allowed: ans.Allowed, at: time.Now()}
		c.mu.Unlock()
		return ans.Allowed, nil
	})
	if err != nil {
		slog.Warn("billing access check failed — local state stands", "org", orgID, "err", err)
		return false
	}
	allowed, _ := v.(bool)
	return allowed
}
