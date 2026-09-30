// Spool is a write-ahead fallback for audit events. The healthy path is a
// plain passthrough to the store — the Postgres outbox and the synchronous
// allow-path commit keep doing their jobs. When a store write fails, the
// event is appended to a local JSONL segment and fsynced before Append
// returns, then relayed in order once the store accepts writes again.
//
// Durability contract: an event Append reports success for is either in the
// store or in an fsynced file on local disk. A process crash loses nothing —
// the next Spool on the same directory replays leftover segments first.
// Only losing the disk itself (host eviction on ephemeral container fs) can
// drop events, and only those written inside the outage window.
//
// Ordering: while a backlog exists, new events spool behind it rather than
// racing the relay — the audit record stays insertion-ordered.
//
// Duplicates: an ambiguous store failure (timeout where the commit state is
// unknown) can produce a row both in the store and in the spool. Audit
// prefers a duplicate over a loss; spooled rows carry the original `at`.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

const (
	spoolActive   = "active.jsonl"
	spoolRelayExt = ".relay.jsonl"
	// spoolMaxBytes bounds on-disk backlog; past it Append reports failure
	// rather than letting an outage consume unbounded disk.
	spoolMaxBytes   = 256 << 20
	spoolRelayEvery = 250 * time.Millisecond
)

// Spool wraps the synchronous store path with a durable on-disk fallback.
type Spool struct {
	store store.Store
	dir   string

	mu      sync.Mutex
	f       *os.File
	pending int // events on disk awaiting relay (segments + active)
	closed  bool

	drainMu  sync.Mutex // serializes Close's final drain with the relay's
	wake     chan struct{}
	workerWg sync.WaitGroup

	errMu sync.Mutex
	err   error
}

// NewSpool creates a spooling auditor over s. dir holds the active segment
// and relay segments; it is created if missing, and leftover segments from a
// previous (crashed) instance are replayed first.
func NewSpool(s store.Store, dir string) *Spool {
	sp := &Spool{
		store: s,
		dir:   dir,
		wake:  make(chan struct{}, 1),
	}
	if err := sp.open(); err != nil {
		// A spool that cannot open its directory is inert-but-loud: Append
		// will fall back to plain store writes and report disk failures.
		slog.Error("audit spool unavailable", "dir", dir, "error", err)
	}
	sp.workerWg.Add(1)
	go sp.relay()
	return sp
}

// open creates the spool dir and the active segment, then counts any backlog
// left by a previous instance so ordering rules apply immediately.
func (sp *Spool) open() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if err := os.MkdirAll(sp.dir, 0o700); err != nil {
		return err
	}
	pending, err := sp.countPendingLocked()
	if err != nil {
		return err
	}
	sp.pending = pending
	return sp.openActiveLocked()
}

func (sp *Spool) openActiveLocked() error {
	f, err := os.OpenFile(filepath.Join(sp.dir, spoolActive), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	sp.f = f
	return nil
}

// countPendingLocked sums events in relay segments plus the active file.
func (sp *Spool) countPendingLocked() (int, error) {
	n := 0
	for _, name := range []string{spoolActive} {
		if c, err := countLines(filepath.Join(sp.dir, name)); err == nil {
			n += c
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	segs, err := sp.relaySegmentsLocked()
	if err != nil {
		return 0, err
	}
	for _, seg := range segs {
		c, err := countLines(seg)
		if err != nil {
			return 0, err
		}
		n += c
	}
	return n, nil
}

func (sp *Spool) relaySegmentsLocked() ([]string, error) {
	ents, err := filepath.Glob(filepath.Join(sp.dir, "*"+spoolRelayExt))
	if err != nil {
		return nil, err
	}
	sort.Strings(ents)
	return ents, nil
}

func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n, nil
}

// Append writes the event to the store, or to the fsynced spool when the
// store refuses. Returns nil once the event is durable in one of the two.
func (sp *Spool) Append(ctx context.Context, e protocol.AuditEvent) error {
	sp.mu.Lock()
	closed := sp.closed
	backlog := sp.pending > 0
	sp.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if !backlog {
		if err := sp.store.AppendAudit(e); err == nil {
			return nil
		} else {
			slog.Warn("audit store write failed; spooling", "error", err, "agent", e.AgentID, "item", e.ItemID)
		}
	}
	// Behind a backlog or on store failure: the event must land on disk.
	if err := sp.spool(e); err != nil {
		return err
	}
	select {
	case sp.wake <- struct{}{}:
	default:
	}
	return nil
}

// spool appends the event to the active segment and fsyncs it.
func (sp *Spool) spool(e protocol.AuditEvent) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit spool marshal: %w", err)
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.f == nil {
		return errors.New("audit: spool unavailable")
	}
	if fi, err := sp.f.Stat(); err == nil && fi.Size() > spoolMaxBytes {
		return errors.New("audit: spool full")
	}
	if _, err := sp.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("audit spool write: %w", err)
	}
	if err := sp.f.Sync(); err != nil {
		return fmt.Errorf("audit spool fsync: %w", err)
	}
	sp.pending++
	return nil
}

// relay rotates the active segment into a relay file and replays segments in
// order until the backlog is empty or a flush fails. Runs on a timer and on
// every spooled append.
func (sp *Spool) relay() {
	defer sp.workerWg.Done()
	tick := time.NewTicker(spoolRelayEvery)
	defer tick.Stop()
	for {
		if sp.isClosed() {
			return
		}
		select {
		case <-tick.C:
		case <-sp.wake:
		}
		sp.drain()
	}
}

// drain moves whatever is on disk toward the store: oldest segment first,
// then the rotated active file. Stops at the first failure so order holds.
func (sp *Spool) drain() {
	sp.drainMu.Lock()
	defer sp.drainMu.Unlock()
	sp.mu.Lock()
	if sp.f != nil {
		// Rotate the active segment when it has content: it becomes a
		// numbered relay file and a fresh active opens for new appends.
		if fi, err := sp.f.Stat(); err == nil && fi.Size() > 0 {
			if err := sp.f.Sync(); err == nil {
				relayName := filepath.Join(sp.dir, fmt.Sprintf("%d%s", time.Now().UnixNano(), spoolRelayExt))
				active := filepath.Join(sp.dir, spoolActive)
				if err := os.Rename(active, relayName); err == nil {
					sp.f = nil
					if err := sp.openActiveLocked(); err != nil {
						slog.Error("audit spool reopen failed", "error", err)
					}
					syncDir(sp.dir)
				}
			}
		}
	}
	segs, err := sp.relaySegmentsLocked()
	sp.mu.Unlock()
	if err != nil {
		slog.Error("audit spool glob failed", "error", err)
		return
	}
	for _, seg := range segs {
		n, err := sp.relaySegment(seg)
		sp.mu.Lock()
		sp.pending -= n
		sp.mu.Unlock()
		if err != nil {
			slog.Warn("audit spool relay failed; will retry", "segment", filepath.Base(seg), "error", err)
			return
		}
	}
}

// relaySegment replays one segment file through AppendAudits and removes it
// on success. Returns the number of events relayed (for pending accounting).
func (sp *Spool) relaySegment(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var events []protocol.AuditEvent
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		var e protocol.AuditEvent
		// A torn trailing line (crash between write and fsync) is skipped —
		// it was never reported durable to its caller.
		if err := json.Unmarshal(data[start:i], &e); err == nil {
			events = append(events, e)
		} else {
			slog.Warn("audit spool skipping torn line", "segment", filepath.Base(path), "error", err)
		}
		start = i + 1
	}
	if len(events) > 0 {
		if err := sp.store.AppendAudits(events); err != nil {
			return 0, err
		}
	}
	if err := os.Remove(path); err != nil {
		// The events landed but the file stayed: report 0 relayed so pending
		// still reflects on-disk truth. The next retry re-appends the segment
		// — duplicate rows, the preferred failure mode over a lost event.
		return 0, err
	}
	syncDir(sp.dir)
	return len(events), nil
}

func (sp *Spool) isClosed() bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.closed
}

// Close stops the relay, runs a final drain, and closes the segment file.
// Events that still cannot reach the store stay on disk for the next run.
func (sp *Spool) Close() error {
	sp.mu.Lock()
	if sp.closed {
		sp.mu.Unlock()
		return nil
	}
	sp.closed = true
	sp.mu.Unlock()

	sp.drain() // final relay attempt while the process is still up
	sp.mu.Lock()
	if sp.f != nil {
		// An empty active segment has nothing to replay — remove it so a
		// clean shutdown leaves no files behind.
		if fi, err := sp.f.Stat(); err == nil && fi.Size() == 0 {
			_ = sp.f.Close()
			_ = os.Remove(filepath.Join(sp.dir, spoolActive))
		} else {
			_ = sp.f.Sync()
			_ = sp.f.Close()
		}
		sp.f = nil
	}
	left := sp.pending
	sp.mu.Unlock()

	sp.workerWg.Wait() // relay goroutine exits on the closed check

	sp.errMu.Lock()
	defer sp.errMu.Unlock()
	if sp.err == nil && left > 0 {
		sp.err = fmt.Errorf("audit: %d events remain spooled", left)
	}
	return sp.err
}

// Pending reports how many events sit on disk awaiting relay — the lag
// signal a monitor wants.
func (sp *Spool) Pending() int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.pending
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer func() { _ = d.Close() }()
	_ = d.Sync()
}

var _ Auditor = (*Spool)(nil)
