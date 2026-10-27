package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/confirm"
	"github.com/VortexNYC/veil/internal/fill"
)

// bridgeTimeout bounds one unlock ask: Touch ID needs the human at the
// keyboard, so it mirrors the browser fill request deadline.
const bridgeTimeout = 90 * time.Second

var errBridgeUnavailable = errors.New("bridge token unavailable")

// bridgeTokenFn / remintHTTPFn are the mint order's two legs — tests stub
// them to prove the bridge is tried before the password+TOTP remint.
var (
	bridgeTokenFn = bridgeHumanToken
	remintHTTPFn  = remintHumanHTTP
)

// bridgeHumanToken mints a human JWT through the running fill bridge's
// Touch ID-gated `token` action. It only engages on macOS with the bridge
// installed (fill.json beside fill.sock) — anything else fails fast so the
// caller falls back to the HTTP remint. VEIL_FILL_TOUCHID=0 disables it.
func bridgeHumanToken(ctx context.Context) (string, error) {
	if runtime.GOOS != "darwin" || !confirm.Enabled() {
		return "", errBridgeUnavailable
	}
	home, err := resolveFillHome("")
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(home, fill.HostConfigFile)); err != nil {
		return "", errBridgeUnavailable
	}
	sock := fill.SocketPath(home)
	if _, err := os.Stat(sock); err != nil {
		return "", errBridgeUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, bridgeTimeout)
	defer cancel()
	return fill.BridgeToken(ctx, sock)
}

// mintHumanToken prefers the bridge's Touch ID mint over the
// password+TOTP remint. On success the token is cached to outFile so
// later commands read it like a human-login mint.
func mintHumanToken(ctx context.Context, outFile string) (string, error) {
	if tok, err := bridgeTokenFn(ctx); err == nil && tok != "" {
		if outFile != "" {
			if werr := os.WriteFile(outFile, []byte(tok+"\n"), 0o600); werr != nil {
				return "", werr
			}
		}
		return tok, nil
	}
	return remintHTTPFn(ctx, outFile)
}

func unlockCmd(home *string) *cobra.Command {
	var outFile string
	c := &cobra.Command{
		Use:   "unlock",
		Short: "Unlock the vault with Touch ID — mints a human JWT via the fill bridge",
		Long: "On macOS with the Veil bridge installed, `veil unlock` asks the " +
			"running bridge for a human token — one Touch ID prompt instead of the " +
			"password+TOTP remint. Writes the token to --out-file (or " +
			"VEIL_HUMAN_TOKEN_FILE). Never stdout.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := outFile
			if out == "" {
				out = resolveFillHomeSafe()
			}
			if out == "" {
				return fmt.Errorf("unlock: --out-file or VEIL_HUMAN_TOKEN_FILE is required")
			}
			tok, err := bridgeTokenFn(cmd.Context())
			if err != nil {
				return fmt.Errorf("unlock: %w (bridge needs macOS + `veil fill install`)", err)
			}
			if werr := os.WriteFile(out, []byte(tok+"\n"), 0o600); werr != nil {
				return werr
			}
			return encode(cmd, map[string]string{"out_file": out})
		},
	}
	c.Flags().StringVar(&outFile, "out-file", "", "write the ID token here. never stdout.")
	_ = home // vault home resolution lives in resolveFillHome inside bridgeHumanToken
	return c
}

// resolveFillHomeSafe finds the human token file the way the bridge would:
// fill.json's token_file first, VEIL_HUMAN_TOKEN_FILE otherwise.
func resolveFillHomeSafe() string {
	if f := os.Getenv("VEIL_HUMAN_TOKEN_FILE"); f != "" {
		return f
	}
	if home, err := resolveFillHome(""); err == nil {
		if cfg, err := fill.ReadHostConfig(home); err == nil && cfg.TokenFile != "" {
			return cfg.TokenFile
		}
	}
	return ""
}
