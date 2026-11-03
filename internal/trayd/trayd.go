// Package trayd is the persistent Linux fill helper: a per-user daemon on a
// Unix socket that renders confirm requests for the native host. The native
// host dials the socket first (confirm.Local) and falls back to a standalone
// dialog when no helper runs — the tray adds persistence and the seam a
// StatusNotifierItem icon and match chooser attach to, not a new gate.
package trayd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/VortexNYC/veil/internal/confirm"
)

// renderFunc shows the human the confirm request; nil error = allowed.
type renderFunc func(reason string) error

// Serve listens on path until ctx is done, answering one confirm request per
// connection. Requests serialize on a single render slot — two fills never
// stack dialogs on the human. render defaults to confirm.Prompt.
func Serve(ctx context.Context, path string, render renderFunc) error {
	if path == "" {
		return fmt.Errorf("tray: no socket path")
	}
	if render == nil {
		render = confirm.Prompt
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// A stale socket file from a dead daemon is safe to replace; a live one
	// refuses so two helpers never fight over requests.
	l, err := net.Listen("unix", path)
	if err != nil {
		if rerr := os.Remove(path); rerr != nil {
			return err
		}
		if l, err = net.Listen("unix", path); err != nil {
			return err
		}
	}
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	go func() { <-ctx.Done(); l.Close() }()

	var slot sync.Mutex
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go serveConn(conn, render, &slot)
	}
}

func serveConn(conn net.Conn, render renderFunc, slot *sync.Mutex) {
	defer conn.Close()
	var req confirm.TrayRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil || req.Kind != "confirm" {
		return
	}
	res := confirm.TrayResponse{Allow: true}
	slot.Lock()
	err := render(req.Reason)
	slot.Unlock()
	if err != nil {
		res = confirm.TrayResponse{Allow: false, Error: "confirmation denied"}
	}
	_ = json.NewEncoder(conn).Encode(res)
}
