package store

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
)

func ownerCacheKey(orgID string, o protocol.Owner) string {
	return orgID + "\x00" + string(o.Kind) + "\x00" + o.ID
}

// ownerSource is the persistence surface a keyManager uses to load and mint
// wrapped per-owner DEKs. SQLite and Postgres implement it.
type ownerSource interface {
	loadOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner) ([]byte, error)
	// mintOwnerWrapped seals dek under the org's committed master and
	// inserts the owner_keys row — atomically, under the org_keys row lock
	// where the backend has one, so a wrap can never land under a master
	// rotation just retired.
	mintOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner, dek []byte) error
}

// keyManager resolves per-org master keys and caches unwrapped DEKs. It does
// not depend on a particular database driver: Postgres resolves org masters by
// unwrapping org_keys rows under the deployment KEK; the sqlite vault resolves
// every org to its single vault key (a local vault is one tenant).
//
// gens is a per-org generation counter bumped by InvalidateOrg. Cache writes
// that were resolved under an older generation are dropped, so a rotation
// mid-resolve cannot resurrect a stale master or persist a DEK wrap under a
// key that is no longer the org's committed master.
type keyManager struct {
	resolve func(ctx context.Context, orgID string) ([]byte, error)
	mu      sync.Mutex
	masters map[string][]byte
	deks    map[string][]byte
	gens    map[string]uint64
}

func newKeyManager(resolve func(ctx context.Context, orgID string) ([]byte, error)) *keyManager {
	return &keyManager{
		resolve: resolve,
		masters: map[string][]byte{},
		deks:    map[string][]byte{},
		gens:    map[string]uint64{},
	}
}

// master returns the unwrapped org master, cached per org. A missing org_keys
// row fails closed: callers get ErrOrgKeyMissing, not a wrong-key decrypt
// attempt.
func (km *keyManager) master(ctx context.Context, orgID string) ([]byte, error) {
	if orgID == "" {
		return nil, fmt.Errorf("store: missing org")
	}
	km.mu.Lock()
	gen := km.gens[orgID]
	if k, ok := km.masters[orgID]; ok {
		km.mu.Unlock()
		return k, nil
	}
	km.mu.Unlock()

	k, err := km.resolve(ctx, orgID)
	if err != nil {
		return nil, err
	}
	km.mu.Lock()
	if km.gens[orgID] == gen {
		km.masters[orgID] = k
	}
	km.mu.Unlock()
	return k, nil
}

// InvalidateOrg drops a master's cached copy and every DEK derived under it.
// Rotation bumps org_keys.key_version and calls this; the next request
// re-resolves. Cross-replica invalidation is a rotation-runbook concern (the
// rotate verb redeploys); the cache itself is per-process.
func (km *keyManager) InvalidateOrg(orgID string) {
	prefix := orgID + "\x00"
	km.mu.Lock()
	km.gens[orgID]++
	delete(km.masters, orgID)
	for k := range km.deks {
		if strings.HasPrefix(k, prefix) {
			delete(km.deks, k)
		}
	}
	km.mu.Unlock()
}

// ownerDEK is the per-owner data key, scoped to an org. The org master unwraps
// it. Grants never see it.
func (km *keyManager) ownerDEK(ctx context.Context, s ownerSource, orgID string, o protocol.Owner) ([]byte, error) {
	if o.Kind == "" || o.ID == "" {
		return nil, fmt.Errorf("store: missing owner")
	}
	k := ownerCacheKey(orgID, o)
	for attempt := 0; ; attempt++ {
		if attempt >= 3 {
			return nil, fmt.Errorf("store: owner DEK for %s/%s did not stabilize", orgID, o.ID)
		}
		km.mu.Lock()
		gen := km.gens[orgID]
		if dek, ok := km.deks[k]; ok {
			km.mu.Unlock()
			return dek, nil
		}
		km.mu.Unlock()

		master, err := km.master(ctx, orgID)
		if err != nil {
			return nil, err
		}

		wrapped, err := s.loadOwnerWrapped(ctx, orgID, o)
		if err == ErrNotFound {
			dek, err := crypto.NewKey()
			if err != nil {
				return nil, err
			}
			// The mint seals under the committed org master inside the
			// store's own lock scope — a rotation cannot strand it. A
			// concurrent mint may still commit first (ON CONFLICT DO
			// NOTHING): the stored row is authoritative — reload it.
			if err := s.mintOwnerWrapped(ctx, orgID, o, dek); err != nil {
				return nil, err
			}
			wrapped, err = s.loadOwnerWrapped(ctx, orgID, o)
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		plain, err := crypto.Open(master, wrapped)
		if err != nil {
			// The loaded wrap was rewrapped by a rotation committed on
			// another replica while this master was cached — or the row is
			// genuinely corrupt. Drop every cached key for the org and
			// re-resolve; bounded by the attempt cap so corruption errors
			// instead of spinning.
			km.InvalidateOrg(orgID)
			continue
		}
		km.mu.Lock()
		if km.gens[orgID] == gen {
			km.deks[k] = plain
		}
		km.mu.Unlock()
		return plain, nil
	}
}
