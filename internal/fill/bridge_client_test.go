package fill

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// serveOne answers a single framed request with reply, reporting the raw
// request back on got.
func serveOne(t *testing.T, reply []byte) (sockPath string, got chan []byte) {
	t.Helper()
	dir := t.TempDir()
	sockPath = filepath.Join(dir, SocketFile)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got = make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		raw, err := Read(conn)
		if err != nil {
			return
		}
		got <- raw
		_ = Write(conn, reply)
	}()
	return sockPath, got
}

func TestBridgeTokenRoundTrip(t *testing.T) {
	sock, got := serveOne(t, []byte(`{"token":"human-jwt"}`))
	tok, err := BridgeToken(context.Background(), sock)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "human-jwt" {
		t.Fatalf("tok = %q", tok)
	}
	select {
	case raw := <-got:
		if string(raw) != `{"action":"token"}` {
			t.Fatalf("request = %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge never saw the request")
	}
}

func TestBridgeTokenErrorReply(t *testing.T) {
	sock, _ := serveOne(t, []byte(`{"error":"untrusted client"}`))
	if _, err := BridgeToken(context.Background(), sock); err == nil {
		t.Fatal("expected error")
	}
}

func TestBridgeTokenDeadSocket(t *testing.T) {
	if _, err := BridgeToken(context.Background(), filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Fatal("expected dial error")
	}
}
