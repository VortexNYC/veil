package fill

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

// remoteApprove files the ask then polls; approved arms the peer's reuse
// window like a local Touch ID would.
func TestRemoteApproveApproved(t *testing.T) {
	polls := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/fill/request":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req-1", "status": "open"})
		case "/v1/fill/request/req-1":
			atomic.AddInt64(&polls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req-1", "status": "approved"})
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	h := &Host{Origin: srv.URL, Token: "t", Device: "mini", ConfirmTTL: time.Minute}
	item := protocol.Item{ID: "it-1", Name: "github"}
	peer := &Peer{PID: 1, Path: "/Applications/Veil.app/Contents/MacOS/Veil"}
	if err := h.remoteApprove(item, "github.com", peer); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	armed := h.confirmPeer == peer.key() && h.confirmScope == "github.com"
	h.mu.Unlock()
	if !armed {
		t.Fatal("remote approval did not arm the peer reuse window")
	}
	// A same-peer confirm reuses the armed window — no prompt needed.
	calls := 0
	h.Confirm = func(string) error { calls++; return nil }
	if err := h.confirm("pw", "github.com", true, peer); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("armed window should reuse, got %d evals", calls)
	}
}

func TestRemoteApproveDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req-2", "status": "open"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "req-2", "status": "denied"})
	}))
	defer srv.Close()
	h := &Host{Origin: srv.URL, Token: "t"}
	if err := h.remoteApprove(protocol.Item{ID: "it-1"}, "github.com", TrustedPeer); err == nil {
		t.Fatal("denied should error")
	}
}

// The gate: "tagged" covers veil:remote-approve only; "all" covers
// everything; off covers nothing.
func TestRemoteGate(t *testing.T) {
	tagged := protocol.Item{Tags: []string{protocol.TagRemoteApprove}}
	plain := protocol.Item{}
	cases := []struct {
		mode      string
		item      protocol.Item
		want      bool
	}{
		{"tagged", tagged, true},
		{"tagged", plain, false},
		{"all", tagged, true},
		{"all", plain, true},
		{"", tagged, false},
		{"off", tagged, false},
	}
	for _, c := range cases {
		if got := (&Host{RemoteApproveMode: c.mode}).remoteGate(c.item); got != c.want {
			t.Fatalf("mode %s tags=%v got %v want %v", c.mode, len(c.item.Tags) > 0, got, c.want)
		}
	}
}
