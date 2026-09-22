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

// ownerSource is the persistence surface a keyManager uses to load and store
// wrapped per-owner DEKs. SQLite and Postgres implement it.
type ownerSource interface {
	loadOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner) ([]byte, error)
	storeOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner, wrapped []byte) error
}

// keyManager resolves per-org master keys and caches unwrapped DEKs. It does
// not depend on a particular database driver: Postgres resolves org masters by
// unwrapping org_keys rows under the deployment KEK; the sqlite vault resolves
// every org to its single vault key (a local vault is one tenant).
type keyManager struct {
	resolve func(ctx context.Context, orgID string) ([]byte, error)
	mu      sync.Mutex
	masters map[string][]byte
	deks    map[string][]byte
}

func newKeyManager(resolve func(ctx context.Context, orgID string) ([]byte, error)) *keyManager {
	return &keyManager{
		resolve: resolve,
		masters: map[string][]byte{},
		deks:    map[string][]byte{},
	}
}

// master returns the unwrapped org master, cached per org. A missing org_keys
// row fails closed: callers get ErrNotFound, not a wrong-key decrypt attempt.
func (km *keyManager) master(ctx context.Context, orgID string) ([]byte, error) {
	if orgID == "" {
		return nil, fmt.Errorf("store: missing org")
	}
	km.mu.Lock()
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
	km.masters[orgID] = k
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
	km.mu.Lock()
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
		sealed, err := crypto.Seal(master, dek)
		if err != nil {
			return nil, err
		}
		if err := s.storeOwnerWrapped(ctx, orgID, o, sealed); err != nil {
			return nil, err
		}
		wrapped, err = s.loadOwnerWrapped(ctx, orgID, o)
		if err != nil {
			return nil, err
		}
		plain, err := crypto.Open(master, wrapped)
		if err != nil {
			return nil, err
		}
		km.mu.Lock()
		km.deks[k] = plain
		km.mu.Unlock()
		return plain, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := crypto.Open(master, wrapped)
	if err != nil {
		return nil, err
	}
	km.mu.Lock()
	km.deks[k] = plain
	km.mu.Unlock()
	return plain, nil
}
