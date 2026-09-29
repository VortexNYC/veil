package fill

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

// SocketFile is the unix socket a sandboxed client (the Safari web extension
// appex) uses to reach this host. The appex cannot spawn processes, so the
// browser-native exec model is replaced by a same-uid socket: file perms are
// the credential, frames are unchanged.
const SocketFile = "fill.sock"

// SafariExtensionID is the appex bundle id. Its sandbox only reaches its own
// container, so the bridge places a second socket inside it — no entitlement
// exceptions needed, and the socket dies with the container.
const SafariExtensionID = "nyc.veil.fill.Extension"

func SocketPath(vaultHome string) string {
	return filepath.Join(vaultHome, SocketFile)
}

// SafariSocketPath is the socket inside the appex's sandbox container —
// the only filesystem location both the daemon and the appex can reach.
func SafariSocketPath(userHome string) string {
	return filepath.Join(userHome, "Library", "Containers", SafariExtensionID, "Data", SocketFile)
}

// BridgeSocketPaths is every path the bridge listens on: the vault socket for
// CLI/debugging, and the appex container socket for Safari.
func BridgeSocketPaths(vaultHome, userHome string) []string {
	return []string{SocketPath(vaultHome), SafariSocketPath(userHome)}
}

// ServeBridge listens on each sockPath and runs the normal Serve loop per
// connection. Same frames, same JSON — transport only.
func ServeBridge(ctx context.Context, h *Host, sockPaths ...string) error {
	var lns []net.Listener
	for _, p := range sockPaths {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			continue
		}
		_ = os.Remove(p)
		ln, err := net.Listen("unix", p)
		if err != nil {
			continue
		}
		if err := os.Chmod(p, 0o600); err != nil {
			_ = ln.Close()
			continue
		}
		lns = append(lns, ln)
	}
	if len(lns) == 0 {
		return &os.PathError{Op: "listen", Path: sockPaths[0], Err: os.ErrPermission}
	}
	done := make(chan struct{})
	for _, ln := range lns {
		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go func() {
					defer conn.Close()
					_ = h.Serve(conn, conn)
				}()
			}
		}(ln)
	}
	go func() {
		<-ctx.Done()
		for _, l := range lns {
			_ = l.Close()
		}
		close(done)
	}()
	<-ctx.Done()
	<-done
	return nil
}
