package audit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

func TestSyncAuditorWritesImmediately(t *testing.T) {
	m := store.NewMemory()
	a := &Sync{Store: m}
	e := protocol.AuditEvent{OrgID: "o", AgentID: "a", ItemID: "i", Action: protocol.ActionFetch, Decision: protocol.DecisionAllow}
	if err := a.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events", len(events))
	}
	if events[0].AgentID != "a" {
		t.Fatalf("wrong event: %+v", events[0])
	}
}

func TestAsyncAuditorFlushesOnClose(t *testing.T) {
	m := store.NewMemory()
	a := NewAsync(m, 8)
	e := protocol.AuditEvent{OrgID: "o", AgentID: "a", ItemID: "i", Action: protocol.ActionFetch, Decision: protocol.DecisionAllow}
	if err := a.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].AgentID != "a" {
		t.Fatalf("events=%+v", events)
	}
}

func TestAsyncAuditorRespectsContextCancellation(t *testing.T) {
	m := &slowStore{Memory: store.NewMemory(), delay: 5 * time.Second}
	a := NewAsync(m, 0)

	// Occupy the worker so the next send cannot proceed.
	if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "first"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Append(ctx, protocol.AuditEvent{AgentID: "second"})
	if err != context.Canceled {
		t.Fatalf("want context.Canceled, got %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	events, _ := m.Audit()
	if len(events) != 1 || events[0].AgentID != "first" {
		t.Fatalf("flushed events=%+v", events)
	}
}

func TestAsyncAuditorDropsWhenBackloggedAndContextExpires(t *testing.T) {
	m := &slowStore{Memory: store.NewMemory(), delay: 5 * time.Second}
	a := NewAsync(m, 0)

	// Send one event. The worker reads it and then blocks in the slow store.
	if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "first"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := a.Append(ctx, protocol.AuditEvent{AgentID: "second"})
	if !IsDropped(err) {
		t.Fatalf("want dropped, got %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

// A backed-up auditor must flush full batches, not only what arrived inside
// one flush interval. Under sustained load the channel always has queued
// events; the worker should pull up to its batch size each cycle so drain
// capacity stays above arrival rate.
func TestAsyncAuditorDrainsBacklogInLargeBatches(t *testing.T) {
	m := store.NewMemory()
	rec := &batchRecorder{Memory: m, gate: make(chan struct{})}
	a := NewAsyncWithInterval(rec, 512, time.Second)

	const total = 200
	for i := 0; i < total; i++ {
		if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	// All events are queued before the worker's first flush proceeds.
	close(rec.gate)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.sizes) != 1 || rec.sizes[0] != total {
		t.Fatalf("want one flush of %d events, got batches %v", total, rec.sizes)
	}
}

type batchRecorder struct {
	*store.Memory
	mu    sync.Mutex
	gate  chan struct{}
	sizes []int
}

func (s *batchRecorder) AppendAudits(events []protocol.AuditEvent) error {
	<-s.gate
	s.mu.Lock()
	s.sizes = append(s.sizes, len(events))
	s.mu.Unlock()
	return s.Memory.AppendAudits(events)
}

type slowStore struct {
	*store.Memory
	delay time.Duration
}

func (s *slowStore) AppendAudit(e protocol.AuditEvent) error {
	time.Sleep(s.delay)
	return s.Memory.AppendAudit(e)
}

func (s *slowStore) AppendAudits(events []protocol.AuditEvent) error {
	time.Sleep(s.delay)
	return s.Memory.AppendAudits(events)
}

func TestAsyncAuditorWaitsForInterval(t *testing.T) {
	m := store.NewMemory()
	a := NewAsyncWithInterval(m, 4, 200*time.Millisecond)

	for i := 0; i < 3; i++ {
		if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "a"}); err != nil {
			t.Fatal(err)
		}
	}

	// Events should not be persisted until the flush interval elapses.
	time.Sleep(50 * time.Millisecond)
	events, _ := m.Audit()
	if len(events) != 0 {
		t.Fatalf("want 0 events, got %d", len(events))
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	var err error
	for time.Now().Before(deadline) {
		events, err = m.Audit()
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncAuditorFlushesWhenBatchFullBeforeInterval(t *testing.T) {
	m := store.NewMemory()
	a := NewAsyncWithInterval(m, 2, time.Second)

	if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "first"}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "second"}); err != nil {
			t.Error(err)
		}
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("append blocked even though batch is full")
	}

	var events []protocol.AuditEvent
	var err error
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		events, err = m.Audit()
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncAuditorFlushesPendingOnClose(t *testing.T) {
	m := store.NewMemory()
	a := NewAsyncWithInterval(m, 8, time.Second)

	if err := a.Append(context.Background(), protocol.AuditEvent{AgentID: "pending"}); err != nil {
		t.Fatal(err)
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].AgentID != "pending" {
		t.Fatalf("events=%+v", events)
	}
}

// A failed flush must not drop the batch: events ride pending until the
// store recovers, in order, with newer events queued behind the backlog.
func TestAsyncAuditorRetriesFailedFlush(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	a := NewAsync(fs, 4)

	// Three failures, then the store recovers.
	fs.fail.Store(3)
	for i := 0; i < 5; i++ {
		e := protocol.AuditEvent{AgentID: "a", ItemID: fmt.Sprintf("i%d", i), Decision: protocol.DecisionAllow}
		if err := a.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := m.Audit()
		if len(events) == 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("want 5 events after recovery, got %d", len(events))
	}
	for i, e := range events {
		if e.ItemID != fmt.Sprintf("i%d", i) {
			t.Fatalf("order broken at %d: %+v", i, events)
		}
	}
	if err := a.Close(); err == nil {
		t.Fatal("want recorded flush error from the outage window")
	}
}

// flakyStore fails AppendAudits fail-times before delegating to Memory.
type flakyStore struct {
	*store.Memory
	fail atomic.Int64
}

func (s *flakyStore) AppendAudits(events []protocol.AuditEvent) error {
	if s.fail.Add(-1) >= 0 {
		return errors.New("store down")
	}
	return s.Memory.AppendAudits(events)
}

// Close during a store outage must not drop events still buffered in the
// channel or queued in pending: everything is offered to the final flush.
func TestAsyncAuditorCloseDrainsBacklog(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	a := NewAsync(fs, 4)

	// The first several flushes fail, filling pending; events keep arriving.
	fs.fail.Store(100)
	const total = 20
	for i := 0; i < total; i++ {
		e := protocol.AuditEvent{AgentID: "a", ItemID: fmt.Sprintf("i%d", i), Decision: protocol.DecisionAllow}
		_ = a.Append(context.Background(), e)
	}
	// Store recovers just before Close — the final drain must deliver all.
	fs.fail.Store(0)
	_ = a.Close()
	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != total {
		t.Fatalf("want %d events delivered on close, got %d", total, len(events))
	}
}
