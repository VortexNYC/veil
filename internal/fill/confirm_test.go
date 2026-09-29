package fill

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestConfirmMissingFailsClosed(t *testing.T) {
	h := &Host{}
	if err := h.confirm("Veil wants to fill a password", "github.com", true); err == nil {
		t.Fatal("nil Confirm must fail closed")
	}
}

func TestConfirmScopeReuseWindowAndCVV(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		h := &Host{Confirm: func(string) error {
			n.Add(1)
			return nil
		}}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 1 {
			t.Fatalf("same scope %d", n.Load())
		}
		if err := h.confirm("pw", "amazon.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 2 {
			t.Fatalf("cross scope %d", n.Load())
		}
		if err := h.confirm("cvv", "amazon.com", false); err != nil {
			t.Fatal(err)
		}
		if err := h.confirm("cvv", "amazon.com", false); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 4 {
			t.Fatalf("cvv reused %d", n.Load())
		}

		n.Store(0)
		h = &Host{Confirm: func(string) error {
			n.Add(1)
			return nil
		}}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		time.Sleep(confirmReuse)
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 2 {
			t.Fatalf("window %d", n.Load())
		}
	})
}

func TestConfirmDeniedClearsReuse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		h := &Host{Confirm: func(string) error {
			n.Add(1)
			return nil
		}}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		h.Confirm = func(string) error { return errors.New("no") }
		if err := h.confirm("pw", "amazon.com", true); err == nil {
			t.Fatal("denied")
		}
		h.Confirm = func(string) error {
			n.Add(1)
			return nil
		}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 2 {
			t.Fatalf("deny must not leave github armed %d", n.Load())
		}
	})
}

func TestConfirmStrictNeverReuses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		h := &Host{
			ConfirmMode: "strict",
			Confirm: func(string) error {
				n.Add(1)
				return nil
			},
		}
		for range 3 {
			if err := h.confirm("pw", "github.com", true); err != nil {
				t.Fatal(err)
			}
		}
		if n.Load() != 3 {
			t.Fatalf("strict must prompt every time, got %d", n.Load())
		}
	})
}

func TestConfirmSessionCrossesOrigins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		h := &Host{
			ConfirmMode: "session",
			Confirm: func(string) error {
				n.Add(1)
				return nil
			},
		}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if err := h.confirm("pw", "amazon.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 1 {
			t.Fatalf("session must reuse across origins, got %d", n.Load())
		}
		// reuse=false callsites (CVV) still prompt even in session mode
		if err := h.confirm("cvv", "amazon.com", false); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 2 {
			t.Fatalf("cvv must prompt in session mode, got %d", n.Load())
		}
	})
}

func TestConfirmTTLConfig(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		h := &Host{
			ConfirmTTL: 5 * time.Minute,
			Confirm: func(string) error {
				n.Add(1)
				return nil
			},
		}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		time.Sleep(confirmReuse + time.Second)
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 1 {
			t.Fatalf("custom TTL must outlive the default window, got %d", n.Load())
		}
		time.Sleep(h.ConfirmTTL)
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 2 {
			t.Fatalf("expired custom TTL must reprompt, got %d", n.Load())
		}
	})
}

func TestInvalidateConfirmDropsReuse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		h := &Host{
			ConfirmMode: "session",
			ConfirmTTL:  time.Hour,
			Confirm: func(string) error {
				n.Add(1)
				return nil
			},
		}
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		h.InvalidateConfirm()
		if err := h.confirm("pw", "github.com", true); err != nil {
			t.Fatal(err)
		}
		if n.Load() != 2 {
			t.Fatalf("invalidate must force a fresh prompt, got %d", n.Load())
		}
	})
}

func TestRelockActionInvalidates(t *testing.T) {
	var n atomic.Int32
	h := &Host{
		ConfirmMode: "session",
		ConfirmTTL:  time.Hour,
		Confirm: func(string) error {
			n.Add(1)
			return nil
		},
	}
	if err := h.confirm("pw", "github.com", true); err != nil {
		t.Fatal(err)
	}
	out := h.handleJSON([]byte(`{"action":"relock"}`))
	if err := h.confirm("pw", "github.com", true); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("relock must force a fresh prompt, got %d", n.Load())
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(out, &ack) != nil || !ack.OK {
		t.Fatalf("relock reply: %s", out)
	}
}
