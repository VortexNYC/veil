package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// requestNotifyChannel is the Postgres LISTEN/NOTIFY channel the
// approval_requests trigger publishes org ids on.
const requestNotifyChannel = "approval_requests"

// WatchRequests ticks on approval-request changes for orgID. The first call
// starts a dedicated LISTEN connection so a write committed on ANY origin
// replica wakes the watchers here — the channel closes when ctx ends.
func (p *Postgres) WatchRequests(ctx context.Context, orgID string) <-chan struct{} {
	p.listenOnce.Do(func() { go p.listenLoop() })
	return p.reqBus.watch(ctx, orgID)
}

// listenLoop holds one LISTEN connection for the store's lifetime and fans
// each notification into the local request bus. Reconnects with backoff
// forever — LISTEN is best-effort fan-out, not the record of truth, so a
// dropped connection only delays UI refreshes until the next reconnect.
func (p *Postgres) listenLoop() {
	for {
		if p.listenCtx.Err() != nil {
			return
		}
		if err := p.listen(p.listenCtx); err != nil && p.listenCtx.Err() == nil {
			select {
			case <-p.listenCtx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (p *Postgres) listen(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, p.listenCfg.Copy())
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+requestNotifyChannel); err != nil {
		return err
	}
	// First attach only — reconnects must not re-close the channel.
	p.readyOnce.Do(func() { close(p.listenReady) })
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		p.reqBus.notify(n.Payload)
	}
}
