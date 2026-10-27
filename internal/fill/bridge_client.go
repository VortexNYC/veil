package fill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// BridgeToken asks a running fill bridge for the human's ID token via its
// "token" action — the same Touch ID-gated mint the vault webview uses.
// The CLI is only an attested peer when it is the signed app-bundled
// binary; a dev build or unsigned copy gets "untrusted client" and the
// caller falls back to the password+TOTP remint. Local transport only —
// the token never crosses a network.
func BridgeToken(ctx context.Context, sockPath string) (string, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
	}
	if err := Write(conn, []byte(`{"action":"token"}`)); err != nil {
		return "", err
	}
	raw, err := Read(conn)
	if err != nil {
		return "", err
	}
	var out struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return "", errors.New("fill: bad bridge reply")
	}
	if out.Error != "" {
		return "", fmt.Errorf("fill: bridge token: %s", out.Error)
	}
	if out.Token == "" {
		return "", errors.New("fill: bridge token empty")
	}
	return out.Token, nil
}
