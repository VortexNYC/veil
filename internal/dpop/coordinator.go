package dpop

import (
	"fmt"
	"sync"
	"time"
)

// Coordinator holds cross-request DPoP replay and bind-on-first-use state.
// A Postgres implementation shares pins across origin replicas; nil uses the
// verifier's in-process maps (single-process / tests).
type Coordinator interface {
	PinnedJKT(tokHash string, now time.Time) string
	RecordProof(tokHash, jti, jkt string, now time.Time) error
}

type memoryCoordinator struct {
	mu   sync.Mutex
	jtis map[string]time.Time
	pins map[string]pinEntry
}

func newMemoryCoordinator() *memoryCoordinator {
	return &memoryCoordinator{
		jtis: map[string]time.Time{},
		pins: map[string]pinEntry{},
	}
}

func (m *memoryCoordinator) PinnedJKT(tokHash string, now time.Time) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pins[tokHash]
	if !ok {
		return ""
	}
	if !now.Before(p.expiresAt) {
		delete(m.pins, tokHash)
		return ""
	}
	return p.jkt
}

func (m *memoryCoordinator) RecordProof(tokHash, jti, jkt string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	jtiKey := tokHash + "\x00" + jti
	if exp, seen := m.jtis[jtiKey]; seen && now.Before(exp) {
		return fmt.Errorf("dpop: replayed jti")
	}
	if p, ok := m.pins[tokHash]; ok && now.Before(p.expiresAt) && p.jkt != jkt {
		return fmt.Errorf("dpop: jkt mismatch with pinned token key")
	}

	trimExpired(m.jtis, now, maxCacheEntries)
	m.jtis[jtiKey] = now.Add(2 * clockSkewWindow)

	trimPinExpired(m.pins, now, maxCacheEntries)
	m.pins[tokHash] = pinEntry{
		jkt:       jkt,
		expiresAt: now.Add(pinTTL),
	}
	return nil
}

func trimExpired(m map[string]time.Time, now time.Time, max int) {
	for k, exp := range m {
		if !now.Before(exp) {
			delete(m, k)
		}
	}
	if len(m) < max {
		return
	}
	for k := range m {
		delete(m, k)
		break
	}
}

func trimPinExpired(m map[string]pinEntry, now time.Time, max int) {
	for k, p := range m {
		if !now.Before(p.expiresAt) {
			delete(m, k)
		}
	}
	if len(m) < max {
		return
	}
	for k := range m {
		delete(m, k)
		break
	}
}

var (
	coordMu sync.RWMutex
	coord   Coordinator
)

// SetCoordinator installs shared DPoP state for the process-wide verifier.
// Pass nil to revert to in-process memory inside each Verifier.
func SetCoordinator(c Coordinator) {
	coordMu.Lock()
	coord = c
	coordMu.Unlock()
	ResetDefaultVerifierForTest()
}

func activeCoordinator() Coordinator {
	coordMu.RLock()
	defer coordMu.RUnlock()
	return coord
}
