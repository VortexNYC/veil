package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/confirm"
	"github.com/VortexNYC/veil/internal/fill"
)

// bridgeTimeout bounds one unlock ask: Touch ID needs the human at the
// keyboard, so it mirrors the browser fill request deadline.
const bridgeTimeout = 90 * time.Second

var errBridgeUnavailable = errors.New("bridge token unavailable")

var errNoNativeHost = errors.New("no attested native-host binary")

// bridgeChildEnv marks the re-executed native-host child so an unsigned
// dev build cannot recurse `unlock --print` forever.
const bridgeChildEnv = "VEIL_UNLOCK_BRIDGE_CHILD"

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
	// The socket only serves signed Veil peers. A standalone CLI (brew,
	// go build) is unattested, so the ask rides through the attested
	// native-host binary the app installed — same gate, one Touch ID.
	if !fill.SelfAttested() && os.Getenv(bridgeChildEnv) == "" {
		if tok, err := reexecBridgeToken(ctx, home); !errors.Is(err, errNoNativeHost) {
			return tok, err
		}
	}
	return fill.BridgeToken(ctx, sock)
}

// reexecBridgeToken runs `native-host unlock --print` — the installed,
// code-signed copy of this binary — so the socket's peer attestation sees
// a trusted client. The token returns on the child's stdout pipe only.
func reexecBridgeToken(ctx context.Context, home string) (string, error) {
	nh := fill.HostPath(home)
	self, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
			self = resolved
		}
	}
	if self == nh || self == "" {
		return "", errNoNativeHost
	}
	if _, err := os.Stat(nh); err != nil {
		return "", errNoNativeHost
	}
	cmd := exec.CommandContext(ctx, nh, "unlock", "--print")
	cmd.Env = append(os.Environ(), bridgeChildEnv+"=1")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", errBridgeUnavailable
	}
	return tok, nil
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
	var printTok bool
	c := &cobra.Command{
		Use:   "unlock",
		Short: "Unlock the vault with Touch ID — mints a human JWT via the fill bridge",
		Long: "On macOS with the Veil bridge installed, `veil unlock` asks the " +
			"running bridge for a human token — one Touch ID prompt instead of the " +
			"password+TOTP remint. Writes the token to --out-file (or " +
			"VEIL_HUMAN_TOKEN_FILE). Never stdout.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// --print is the internal child mode: an unattested CLI
			// re-execs the signed native-host binary so the bridge sees a
			// trusted peer, and the token returns on the stdout pipe.
			if printTok {
				tok, err := bridgeTokenFn(cmd.Context())
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), tok)
				return err
			}
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
	c.Flags().BoolVar(&printTok, "print", false, "internal: print the token on stdout (re-exec child mode)")
	_ = c.Flags().MarkHidden("print")
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
