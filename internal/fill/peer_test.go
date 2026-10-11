package fill

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The bridge socket is reachable by any same-uid process — an agent can
// connect like any client. Actions other than ping must only serve
// attested (signed Veil) peers.
func TestUnattestedPeerDenied(t *testing.T) {
	h := &Host{}
	untrusted := &Peer{PID: 4242, Path: "/usr/bin/python3"}
	for _, action := range []string{"list", "match", "fill", "bind", "save", "relock"} {
		out := string(h.handlePeer([]byte(`{"action":"`+action+`"}`), untrusted))
		if !strings.Contains(out, "untrusted client") {
			t.Fatalf("%s should be denied for untrusted peer, got %s", action, out)
		}
	}
	if out := string(h.handlePeer([]byte(`{"action":"ping"}`), untrusted)); !strings.Contains(out, "version") {
		t.Fatalf("ping should stay open, got %s", out)
	}
}

// A Touch ID that covered one client must never cover another — the
// reuse window is keyed to the peer that prompted for it.
func TestConfirmReuseIsPeerScoped(t *testing.T) {
	calls := 0
	h := &Host{
		Confirm:    func(string) error { calls++; return nil },
		ConfirmTTL: time.Minute,
	}
	a := &Peer{PID: 100, Path: "/Applications/Veil.app/Contents/MacOS/Veil"}
	b := &Peer{PID: 200, Path: "/usr/local/bin/agent"}
	if err := h.confirm("pw", "github.com", true, a); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("first eval %d", calls)
	}
	// Same peer, same scope: covered.
	if err := h.confirm("pw", "github.com", true, a); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("same peer should reuse, got %d evals", calls)
	}
	// A different peer inside the window: fresh eval required.
	if err := h.confirm("pw", "github.com", true, b); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("different peer must reprompt, got %d evals", calls)
	}
}

// Session mode covers every site *for the same client* — never across
// peers.
func TestSessionModeStillPeerScoped(t *testing.T) {
	calls := 0
	h := &Host{
		Confirm:     func(string) error { calls++; return nil },
		ConfirmMode: "session",
		ConfirmTTL:  time.Minute,
	}
	a := &Peer{PID: 100, Path: "/Applications/Veil.app/Contents/MacOS/Veil"}
	b := &Peer{PID: 200, Path: "/usr/local/bin/agent"}
	_ = h.confirm("pw", "github.com", true, a)
	_ = h.confirm("pw", "amazon.com", true, a) // session: same peer, any site
	if calls != 1 {
		t.Fatalf("session should cover same peer across sites, got %d", calls)
	}
	_ = h.confirm("pw", "amazon.com", true, b)
	if calls != 2 {
		t.Fatalf("session must not cover a different peer, got %d", calls)
	}
}

// The nacl envelope on the socket: unattested peers cannot open assoc
// sessions — the prompt-spam path is closed.
func TestUnattestedPeerDeniedEnvelope(t *testing.T) {
	h := &Host{}
	untrusted := &Peer{PID: 4242, Path: "/usr/bin/python3"}
	raw, _ := json.Marshal(map[string]any{
		"action": "change-public-keys", "clientID": "x", "publicKey": "AA", "nonce": "BB",
	})
	out := string(h.handlePeer(raw, untrusted))
	if !strings.Contains(out, "untrusted client") {
		t.Fatalf("envelope should be denied, got %s", out)
	}
}
