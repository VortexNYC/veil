package fill

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
	"github.com/VortexNYC/veil/internal/replica"
)

// warmReplica spins a real origin, creates one login, and returns a host whose
// replica has pulled it. The returned counter tracks POST /v1/fill/events calls.
func warmReplica(t *testing.T, uri string) (*Host, *app.App, *atomic.Int32) {
	t.Helper()
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "github", URI: uri, Secret: secret, Login: "ada@example.com", TOTPSeed: "JBSWY3DPEHPK3PXP",
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	var reported atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/events" {
			reported.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(NewOrigin(dir, srv.URL, "human"))
	h.Replica = box
	if err := h.PullReplica(); err != nil {
		t.Fatal(err)
	}
	return h, a, &reported
}

// A replica-served fill must still leave an audit row at origin — the secret
// comes from the sealed box, but the disclosure event is reported and lands in
// the same feed an online fill writes.
func TestJSONReplicaFillReportsAuditEvent(t *testing.T) {
	h, a, reported := warmReplica(t, "https://github.com")
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 || out.Entries[0].Password != secret {
		t.Fatalf("replica fill %s", got)
	}
	if len(out.Entries[0].TOTP) != 6 {
		t.Fatalf("replica fill did not mint totp: %+v", out.Entries[0])
	}
	// The fill spawns an async flusher; drain it so the post has landed.
	h.waitFlushes()
	if reported.Load() != 1 {
		t.Fatalf("fill/event posts %d", reported.Load())
	}
	events, err := a.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	var sawFill, sawMint bool
	for _, e := range events {
		if e.ItemID == "github" && e.Action == protocol.ActionFill {
			sawFill = true
		}
		if e.ItemID == "github" && e.Action == protocol.ActionTOTPMint {
			sawMint = true
		}
	}
	if !sawFill || !sawMint {
		t.Fatalf("audit rows missing: fill=%v totp_mint=%v", sawFill, sawMint)
	}
	if left, _ := filepath.Glob(filepath.Join(h.Dir, fillAuditQueueDir, "*.jsonl")); len(left) != 0 {
		t.Fatalf("audit queue not drained after flush: %v", left)
	}
}

// Airplane fill still works with origin dead — the event is fsynced to the
// local queue and ships on the next origin contact. Origin down + a fill is a
// queued row, never a lost one.
func TestJSONReplicaFillQueuesEventWhileOffline(t *testing.T) {
	h, a, reported := warmReplica(t, "https://github.com")
	live := h.Origin
	h.Origin = "http://127.0.0.1:1"
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 || out.Entries[0].Password != secret {
		t.Fatalf("airplane fill %s", got)
	}
	h.waitFlushes()
	// A failed flush leaves the published event file in place — still durable.
	pending, err := filepath.Glob(filepath.Join(h.Dir, fillAuditQueueDir, "*.jsonl"))
	if err != nil || len(pending) != 1 {
		t.Fatalf("offline fill left no queued audit event: %v %v", pending, err)
	}
	// Origin comes back; the queued event flushes and lands as an audit row.
	h.Origin = live
	h.flushFillEvents()
	if reported.Load() != 1 {
		t.Fatalf("queued event never reported: %d", reported.Load())
	}
	if left, _ := filepath.Glob(filepath.Join(h.Dir, fillAuditQueueDir, "*.jsonl")); len(left) != 0 {
		t.Fatalf("queue not drained after successful flush: %v", left)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		events, err := a.Store.Audit()
		if err != nil {
			t.Fatal(err)
		}
		ok := false
		for _, e := range events {
			if e.ItemID == "github" && e.Action == protocol.ActionFill {
				ok = true
			}
		}
		if ok || time.Now().After(deadline) {
			if !ok {
				t.Fatal("flushed event missing from audit")
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A replica fill whose audit queue append fails denies the reveal — the
// fail-closed contract is identical to origin's "audit write before secret".
func TestJSONReplicaFillDeniedWhenQueueUnwritable(t *testing.T) {
	h, _, _ := warmReplica(t, "https://github.com")
	if err := os.Chmod(h.Dir, 0o555); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(h.Dir, 0o755) })
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 0 {
		t.Fatalf("unwritable audit queue still released secret: %s", got)
	}
}

// Card and identity fills through the local envelope paths are disclosures too
// — the report rides the same queue.
func TestJSONCardFillReportsAuditEvent(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe-card", Kind: "card",
		Card: &publicapi.CardFields{Number: "4242424242424242", ExpMonth: "12", ExpYear: "2034", CVV: "314", Holder: "Ada"},
	})
	if code != http.StatusOK {
		t.Fatalf("create card %d %s", code, raw)
	}
	var reported atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/events" {
			reported.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(NewOrigin(dir, srv.URL, "human"))
	h.Replica = box
	if err := h.PullReplica(); err != nil {
		t.Fatal(err)
	}
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://stripe.com/pay", "uuid": "stripe-card"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 || out.Entries[0].Number != "4242424242424242" {
		t.Fatalf("card fill %s", got)
	}
	h.waitFlushes()
	if reported.Load() != 1 {
		t.Fatalf("card fill event posts %d", reported.Load())
	}
	events, err := a.Store.Audit()
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, e := range events {
		if e.ItemID == "stripe-card" && e.Action == protocol.ActionFill {
			ok = true
		}
	}
	if !ok {
		t.Fatal("card fill left no audit row")
	}
}

// A corrupt queue file is junk, not a wedge: the valid event behind it still
// ships, and both files drain.
func TestFlushSkipsMalformedQueueFile(t *testing.T) {
	h, _, reported := warmReplica(t, "https://github.com")
	dir := filepath.Join(h.Dir, fillAuditQueueDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Sorts before any real publish: corrupt junk wedged at the head.
	if err := os.WriteFile(filepath.Join(dir, "1-corrupt.jsonl"), []byte("not json\n{\"uuid\":\"\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 {
		t.Fatalf("fill %s", got)
	}
	h.waitFlushes()
	h.flushFillEvents()
	if reported.Load() != 1 {
		t.Fatalf("fill/event posts %d", reported.Load())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.jsonl")); len(left) != 0 {
		t.Fatalf("corrupt file wedged the queue: %v", left)
	}
}
