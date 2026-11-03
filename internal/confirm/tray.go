package confirm

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tray protocol: one JSON line in, one JSON line out.
// Request:  {"kind":"confirm","reason":"Veil wants to fill ..."}
// Response: {"allow":true} or {"allow":false,"error":"denied"}
type TrayRequest struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type TrayResponse struct {
	Allow bool   `json:"allow"`
	Error string `json:"error,omitempty"`
}

// TraySocketPath is where `veil tray` listens. $XDG_RUNTIME_DIR/veil-tray.sock
// when set (per-session dir on systemd hosts), else ~/.veil/tray.sock.
func TraySocketPath() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); dir != "" {
		return filepath.Join(dir, "veil-tray.sock")
	}
	if dir := strings.TrimSpace(os.Getenv("VEIL_HOME")); dir != "" {
		return filepath.Join(dir, "tray.sock")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".veil", "tray.sock")
	}
	return ""
}

// DialTray asks a running `veil tray` daemon to confirm. answered=false means
// no helper is listening — the caller falls back and never hangs on an absent
// tray. answered=true is authoritative even on denial.
func DialTray(reason string) (answered bool, err error) {
	path := TraySocketPath()
	if path == "" {
		return false, fmt.Errorf("fill: no tray socket path")
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(promptDeadline + 5*time.Second))
	if err := json.NewEncoder(conn).Encode(TrayRequest{Kind: "confirm", Reason: reason}); err != nil {
		return false, err
	}
	var res TrayResponse
	if err := json.NewDecoder(conn).Decode(&res); err != nil {
		return false, err
	}
	if !res.Allow {
		if res.Error != "" {
			return true, fmt.Errorf("fill: %s", res.Error)
		}
		return true, fmt.Errorf("fill: confirmation denied")
	}
	return true, nil
}

// trayPresent reports a socket file worth dialing — used by confirmAvailable
// so a live helper counts as a confirm surface even on a host with no
// zenity/kdialog (the daemon may render its own UI).
func trayPresent() bool {
	path := TraySocketPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
