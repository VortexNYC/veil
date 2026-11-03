package trayd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/confirm"
)

func start(t *testing.T, render func(reason string) error) (string, context.CancelFunc) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tray.sock")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = Serve(ctx, path, render) }()
	for i := 0; i < 50; i++ {
		if _, err := net.DialTimeout("unix", path, 50*time.Millisecond); err == nil {
			return path, cancel
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("tray socket never came up")
	return "", cancel
}

func ask(t *testing.T, path, reason string) confirm.TrayResponse {
	t.Helper()
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(confirm.TrayRequest{Kind: "confirm", Reason: reason}); err != nil {
		t.Fatal(err)
	}
	var res confirm.TrayResponse
	if err := json.NewDecoder(conn).Decode(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestTrayAllowsAndDenies(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	path, cancel := start(t, func(reason string) error {
		mu.Lock()
		defer mu.Unlock()
		seen[reason] = true
		if reason == "deny me" {
			return errors.New("denied")
		}
		return nil
	})
	defer cancel()

	if res := ask(t, path, "fill github"); !res.Allow {
		t.Fatalf("expected allow, got %+v", res)
	}
	if res := ask(t, path, "deny me"); res.Allow || res.Error == "" {
		t.Fatalf("expected deny with error, got %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if !seen["fill github"] || !seen["deny me"] {
		t.Fatalf("render never saw requests: %v", seen)
	}
}

func TestTrayRequestsSerialize(t *testing.T) {
	inRender := make(chan struct{})
	release := make(chan struct{})
	var concurrent int32
	var maxSeen int32
	var mu sync.Mutex
	path, cancel := start(t, func(reason string) error {
		mu.Lock()
		concurrent++
		if concurrent > maxSeen {
			maxSeen = concurrent
		}
		mu.Unlock()
		closeOnce := func() {
			select {
			case inRender <- struct{}{}:
			default:
			}
		}
		closeOnce()
		<-release
		mu.Lock()
		concurrent--
		mu.Unlock()
		return nil
	})
	defer cancel()

	done := make(chan struct{})
	go func() { ask(t, path, "first"); close(done) }()
	<-inRender // first render is holding the slot
	second := make(chan confirm.TrayResponse, 1)
	go func() { second <- ask(t, path, "second") }()
	select {
	case <-second:
		t.Fatal("second request was answered while first still rendered")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	if res := <-second; !res.Allow {
		t.Fatalf("second request denied after release: %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if maxSeen != 1 {
		t.Fatalf("renders overlapped: max concurrency %d", maxSeen)
	}
}

func TestDialTrayFallsBackWhenAbsent(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("VEIL_HOME", t.TempDir())
	answered, err := confirm.DialTray("fill github")
	if answered || err == nil {
		t.Fatalf("expected unanswered dial, got answered=%v err=%v", answered, err)
	}
}

func TestDialTrayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("VEIL_HOME", dir)
	path := confirm.TraySocketPath()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = Serve(ctx, path, func(string) error { return nil }) }()
	defer cancel()
	for i := 0; i < 50; i++ {
		if _, err := net.DialTimeout("unix", path, 50*time.Millisecond); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	answered, err := confirm.DialTray("fill github")
	if err != nil || !answered {
		t.Fatalf("expected answered allow, got answered=%v err=%v", answered, err)
	}
}
