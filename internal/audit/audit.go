// Package audit decouples security-event persistence from request handling.
//
// The synchronous path writes every audit event to the store before the caller
// returns. The asynchronous path queues events in memory and flushes them from
// a worker, so the hot Use path does not wait for a store round trip per event.
//
// Durability semantics are explicit: Async buffers in memory. A process crash
// before a flush loses queued events. Callers must call Close to drain.
package audit

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// Auditor persists security events. Implementations may be synchronous or
// buffered. Close must be called before the owning store is closed.
type Auditor interface {
	Append(ctx context.Context, e protocol.AuditEvent) error
	Close() error
}

// Sync writes every event directly to the store.
type Sync struct {
	Store store.Store
}

func (s *Sync) Append(ctx context.Context, e protocol.AuditEvent) error {
	return s.Store.AppendAudit(e)
}

func (s *Sync) Close() error { return nil }

// ErrClosed is returned by Async.Append after Close has started.
var ErrClosed = errors.New("audit: closed")

// Async queues events in memory and flushes them from a worker.
//
// Backpressure: Append blocks until the queue has room, the auditor is closed,
// or the caller's context is canceled. If the caller's context is canceled
// before the event is queued, the event is dropped and ctx.Err() is returned.
type Async struct {
	store         store.Store
	ch            chan protocol.AuditEvent
	batchSize     int
	flushInterval time.Duration

	mu       sync.Mutex
	closed   bool
	senderWg sync.WaitGroup
	stopCh   chan struct{}

	workerDone chan struct{}
	workerWg   sync.WaitGroup

	errMu sync.Mutex
	err   error

	closeOnce sync.Once
}

// NewAsync creates an async auditor that flushes when the batch is full or
// when no more events are immediately available in the channel.
func NewAsync(s store.Store, cap int) *Async {
	return NewAsyncWithInterval(s, cap, 0)
}

// NewAsyncWithInterval creates an async auditor that waits up to interval for
// the batch to fill before flushing. A zero interval flushes immediately after
// the channel is drained.
func NewAsyncWithInterval(s store.Store, cap int, interval time.Duration) *Async {
	batchSize := cap
	if batchSize < 1 {
		batchSize = 1
	}
	if batchSize > 256 {
		batchSize = 256
	}
	a := &Async{
		store:         s,
		ch:            make(chan protocol.AuditEvent, cap),
		batchSize:     batchSize,
		flushInterval: interval,
		stopCh:        make(chan struct{}),
		workerDone:    make(chan struct{}),
	}
	a.workerWg.Add(1)
	go a.loop()
	return a
}

func (a *Async) Append(ctx context.Context, e protocol.AuditEvent) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return ErrClosed
	}
	a.senderWg.Add(1)
	a.mu.Unlock()

	select {
	case a.ch <- e:
		a.senderWg.Done()
		return nil
	case <-a.stopCh:
		a.senderWg.Done()
		return ErrClosed
	case <-ctx.Done():
		a.senderWg.Done()
		return ctx.Err()
	}
}

func (a *Async) loop() {
	defer a.workerWg.Done()
	defer close(a.workerDone)
	batch := make([]protocol.AuditEvent, 0, a.batchSize)
	var pending []protocol.AuditEvent
loop:
	for {
		// A failed batch is never dropped: it moves to pending and is retried
		// before new events are read. While a large backlog stays unflushed
		// the loop stops consuming so channel backpressure reaches Append
		// callers instead of silently growing memory.
		if len(pending) > 0 {
			if a.flush(pending) {
				pending = nil
			}
		}
		if len(pending) >= a.batchSize*4 {
			select {
			case <-a.stopCh:
				break loop
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		// With a sub-cap backlog, retry it on a short timer rather than
		// blocking forever on a quiet channel.
		var retry <-chan time.Time
		if len(pending) > 0 {
			retry = time.After(100 * time.Millisecond)
		}
		select {
		case e, ok := <-a.ch:
			if !ok {
				break loop
			}
			batch = append(batch, e)
		case <-retry:
			continue
		}

		if a.flushInterval <= 0 {
			a.drainBatch(&batch)
			// Events queue behind an unflushed backlog — insertion order is
			// the audit record, so newer batches never overtake pending ones.
			if len(pending) > 0 || !a.flush(batch) {
				pending = append(pending, batch...)
			}
			batch = batch[:0]
			continue
		}

		timer := time.NewTimer(a.flushInterval)
		timed := false
		for !timed && len(batch) < a.batchSize {
			select {
			case e2, ok := <-a.ch:
				if !ok {
					if !timer.Stop() {
						<-timer.C
					}
					break loop
				}
				batch = append(batch, e2)
			case <-timer.C:
				timed = true
			}
		}
		if !timed {
			if !timer.Stop() {
				<-timer.C
			}
		} else {
			// The interval elapsed with a partial batch. Pull whatever else is
			// already queued so a backed-up channel drains at batch size, not
			// at the rate events arrived inside the interval.
			a.drainBatch(&batch)
		}
		if len(batch) > 0 {
			if len(pending) > 0 || !a.flush(batch) {
				pending = append(pending, batch...)
			}
			batch = batch[:0]
		}
	}
	// Final drain on close: batch (mid-loop leftovers) then whatever is still
	// buffered in the channel join pending in order — pending is always the
	// oldest unflushed run — before the last flush attempt.
	pending = append(pending, batch...)
	for {
		select {
		case e, ok := <-a.ch:
			if !ok {
				goto drained
			}
			pending = append(pending, e)
		default:
			goto drained
		}
	}
drained:
	if len(pending) > 0 {
		a.flush(pending)
	}
}

func (a *Async) drainBatch(batch *[]protocol.AuditEvent) {
	for len(*batch) < a.batchSize {
		select {
		case e, ok := <-a.ch:
			if !ok {
				return
			}
			*batch = append(*batch, e)
		default:
			return
		}
	}
}

func (a *Async) flush(batch []protocol.AuditEvent) bool {
	if len(batch) == 0 {
		return true
	}
	if err := a.store.AppendAudits(batch); err != nil {
		a.errMu.Lock()
		if a.err == nil {
			a.err = err
		}
		a.errMu.Unlock()
		return false
	}
	return true
}

func (a *Async) Close() error {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()

		close(a.stopCh)
		a.senderWg.Wait()
		close(a.ch)
	})

	<-a.workerDone
	a.errMu.Lock()
	defer a.errMu.Unlock()
	return a.err
}

var (
	_ Auditor = (*Sync)(nil)
	_ Auditor = (*Async)(nil)
)

// IsDropped returns true if an Append returned because the auditor was closed
// or the caller's context was canceled before the event could be queued. This
// is a best-effort signal and should be logged; it does not make the event durable.
func IsDropped(err error) bool {
	if errors.Is(err, ErrClosed) {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
