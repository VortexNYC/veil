package store

import (
	"context"
	"sync"
)

// requestBus fans out approval-request change ticks to watchers in this
// process. Memory and sqlite drive it directly from their mutations;
// Postgres feeds it from LISTEN/NOTIFY so ticks arrive for writes made by
// any origin replica, not just this process.
type requestBus struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func newRequestBus() *requestBus {
	return &requestBus{subs: map[string]map[chan struct{}]struct{}{}}
}

// notify pings every watcher of orgID. Non-blocking: a slow watcher keeps
// its pending tick (the cap-1 channel coalesces bursts into one refetch).
func (b *requestBus) notify(orgID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[orgID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// notifyOrgs is notify for a set of orgs — one tick per org that changed.
func (b *requestBus) notifyOrgs(orgIDs map[string]struct{}) {
	for org := range orgIDs {
		b.notify(org)
	}
}

// watch returns a channel that ticks on orgID request changes and closes
// when ctx ends.
func (b *requestBus) watch(ctx context.Context, orgID string) <-chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	set := b.subs[orgID]
	if set == nil {
		set = map[chan struct{}]struct{}{}
		b.subs[orgID] = set
	}
	set[ch] = struct{}{}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if set, ok := b.subs[orgID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, orgID)
			}
		}
		b.mu.Unlock()
		close(ch)
	}()
	return ch
}
