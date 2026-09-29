package fill

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestServeBridgeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := SocketPath(dir)
	h := NewOrigin(dir, "http://127.0.0.1:1", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ServeBridge(ctx, h, sock) }()

	deadline := time.Now().Add(5 * time.Second)
	var conn net.Conn
	for {
		var err error
		conn, err = net.DialTimeout("unix", sock, time.Second)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bridge never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer conn.Close()

	for i := 0; i < 2; i++ {
		if err := Write(conn, []byte("not json")); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
		raw, err := Read(conn)
		if err != nil {
			t.Fatalf("read frame %d: %v", i, err)
		}
		var reply struct {
			Success string `json:"success"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Fatalf("frame %d not json: %v", i, err)
		}
		if reply.Success != "false" {
			t.Fatalf("frame %d: %+v", i, reply)
		}
	}
}

func TestSocketPath(t *testing.T) {
	if got := SocketPath("/v"); got != filepath.Join("/v", SocketFile) {
		t.Fatal(got)
	}
}
