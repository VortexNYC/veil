package audit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// A store outage must not lose audit events: Append durably spools to disk
// and the relay lands them in order once the store recovers.
func TestSpoolPersistsEventsThroughOutage(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	dir := t.TempDir()
	s := NewSpool(fs, dir)
	defer func() { _ = s.Close() }()

	fs.fail.Store(100)
	for i := 0; i < 5; i++ {
		e := protocol.AuditEvent{AgentID: "a", ItemID: fmt.Sprintf("i%d", i), Decision: protocol.DecisionDeny}
		if err := s.Append(context.Background(), e); err != nil {
			t.Fatalf("spooled append must succeed: %v", err)
		}
	}
	fs.fail.Store(0)

	deadline := time.Now().Add(3 * time.Second)
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
}

// Durability means the event survives the process, not just the outage:
// a spool whose owner never got to Close (crash) replays on the next open.
func TestSpoolReplaysAfterUncleanShutdown(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	dir := t.TempDir()

	fs.fail.Store(100)
	s := NewSpool(fs, dir)
	e := protocol.AuditEvent{AgentID: "a", ItemID: "crash-me", Decision: protocol.DecisionDeny}
	if err := s.Append(context.Background(), e); err != nil {
		t.Fatalf("spool append: %v", err)
	}
	// No Close — the process "dies" with the event on disk only.

	fs.fail.Store(0)
	s2 := NewSpool(fs, dir)
	defer func() { _ = s2.Close() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := m.Audit()
		if len(events) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ItemID != "crash-me" {
		t.Fatalf("want crashed event replayed, got %+v", events)
	}
}

// While a backlog exists, new events must spool behind it — replaying ahead
// of live appends would invert the audit order.
func TestSpoolOrderingDuringRecovery(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	dir := t.TempDir()
	s := NewSpool(fs, dir)
	defer func() { _ = s.Close() }()

	fs.fail.Store(100)
	for i := 0; i < 3; i++ {
		if err := s.Append(context.Background(), protocol.AuditEvent{ItemID: fmt.Sprintf("old%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	fs.fail.Store(0)

	// Give the relay a moment to *start* draining, then append live events.
	// Whether they arrive before or after the drain, final order must hold.
	if err := s.Append(context.Background(), protocol.AuditEvent{ItemID: "new0"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := m.Audit()
		if len(events) == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	events, _ := m.Audit()
	want := []string{"old0", "old1", "old2", "new0"}
	if len(events) != 4 {
		t.Fatalf("want 4 events, got %d", len(events))
	}
	for i, w := range want {
		if events[i].ItemID != w {
			t.Fatalf("order broken at %d: %+v", i, events)
		}
	}
}

// When even the spool write fails, Append must report the failure loudly —
// a silently-dropped audit is exactly what this type exists to prevent.
func TestSpoolReportsDiskFailure(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	fs.fail.Store(100)
	dir := filepath.Join(t.TempDir(), "missing", "deeper")
	// Make the dir exist but be unwritable by pointing the spool at a file
	// path that cannot be created: a directory named like the spool file.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "active.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewSpool(fs, dir)
	defer func() { _ = s.Close() }()

	err := s.Append(context.Background(), protocol.AuditEvent{ItemID: "x"})
	if err == nil {
		t.Fatal("append to a broken spool must return an error")
	}
}

// Close must attempt one final relay so a graceful shutdown loses nothing
// that was only waiting for the next tick.
func TestSpoolCloseFlushesPending(t *testing.T) {
	m := store.NewMemory()
	fs := &flakyStore{Memory: m}
	dir := t.TempDir()
	s := NewSpool(fs, dir)

	fs.fail.Store(100)
	if err := s.Append(context.Background(), protocol.AuditEvent{ItemID: "tail"}); err != nil {
		t.Fatal(err)
	}
	fs.fail.Store(0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := m.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ItemID != "tail" {
		t.Fatalf("want tail event flushed on close, got %+v", events)
	}
}

// Healthy-path: with a working store the spool adds no rows and Append is a
// plain passthrough — no segment files should remain after a clean run.
func TestSpoolHealthyPathLeavesNoSegments(t *testing.T) {
	m := store.NewMemory()
	dir := t.TempDir()
	s := NewSpool(m, dir)
	if err := s.Append(context.Background(), protocol.AuditEvent{ItemID: "i"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var leftover []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			leftover = append(leftover, e.Name())
		}
	}
	if len(leftover) > 0 {
		t.Fatalf("spool files left on healthy path: %v", leftover)
	}
}

// Compile-time: spool is an Auditor.
var _ Auditor = (*Spool)(nil)
