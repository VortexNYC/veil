package store

import (
	"context"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

// awaitTick fails unless ch ticks within the window — Postgres delivers via
// LISTEN/NOTIFY so the bound is generous; memory/sqlite tick instantly.
func awaitTick(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("no watch tick for %s", what)
	}
}

func assertNoTick(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("unexpected watch tick for %s", what)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatchRequests(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch := s.WatchRequests(ctx, "org")
			other := s.WatchRequests(ctx, "other-org")
			// On Postgres the tick rides LISTEN/NOTIFY — the dedicated
			// connection must be attached before the write commits or the
			// notification is lost (real fan-out is allowed to miss; the
			// test is not).
			if pg, ok := s.(*Postgres); ok {
				select {
				case <-pg.listenReady:
				case <-time.After(10 * time.Second):
					t.Fatal("LISTEN never attached")
				}
			}

			// File → tick on the org, silence on a foreign org.
			if _, err := s.FileRequest(openRequest("watch-file")); err != nil {
				t.Fatal(err)
			}
			awaitTick(t, ch, "file")
			assertNoTick(t, other, "file on another org")

			// Resolve (deny) → tick.
			req := openRequest("watch-resolve")
			if _, err := s.FileRequest(req); err != nil {
				t.Fatal(err)
			}
			awaitTick(t, ch, "second file")
			if _, won, err := s.ResolveRequest(req.ID, protocol.RequestDenied, "self", "", time.Now()); err != nil || !won {
				t.Fatalf("resolve won=%v err=%v", won, err)
			}
			awaitTick(t, ch, "resolve")

			// Expiry via the sweep path → tick.
			stale := openRequest("watch-expire")
			stale.ExpiresAt = time.Now().Add(-time.Minute)
			if _, err := s.FileRequest(stale); err != nil {
				t.Fatal(err)
			}
			awaitTick(t, ch, "stale file")
			if _, err := s.ExpireStaleRequests(time.Now()); err != nil {
				t.Fatal(err)
			}
			awaitTick(t, ch, "expiry")
			assertNoTick(t, other, "expiry on another org")

			// Cancel the watcher → channel closes.
			wctx, wcancel := context.WithCancel(context.Background())
			tmp := s.WatchRequests(wctx, "org")
			wcancel()
			select {
			case _, open := <-tmp:
				if open {
					t.Fatal("watch channel not closed after cancel")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("watch channel did not close after cancel")
			}
		})
	}
}

// TestWatchRequestsCancel covers CancelRequestsForAgent's fan-out — asks
// dying on a revoked edge still tick so the card drops them live.
func TestWatchRequestsCancel(t *testing.T) {
	for name, s := range requestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch := s.WatchRequests(ctx, "org")
			if pg, ok := s.(*Postgres); ok {
				select {
				case <-pg.listenReady:
				case <-time.After(10 * time.Second):
					t.Fatal("LISTEN never attached")
				}
			}
			req := openRequest("watch-cancel")
			if _, err := s.FileRequest(req); err != nil {
				t.Fatal(err)
			}
			awaitTick(t, ch, "file")
			if err := s.CancelRequestsForAgent(req.AgentID, time.Now()); err != nil {
				t.Fatal(err)
			}
			awaitTick(t, ch, "cancel")
		})
	}
}
