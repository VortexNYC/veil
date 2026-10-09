package fill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	HostFile       = "native-host"
	HostConfigFile = "fill.json"
)

// HostConfig is how Chrome launches fill. Native messaging has no argv
// for our flags and no env we control. A shell wrapper is not a primitive.
type HostConfig struct {
	Origin       string `json:"origin,omitempty"`
	Home         string `json:"home,omitempty"`
	TokenFile    string `json:"human_token_file,omitempty"`
	LoginEmail   string `json:"login_email,omitempty"`
	PasswordFile string `json:"kratos_password_file,omitempty"`
	TOTPFile     string `json:"kratos_totp_file,omitempty"`
	Debug        bool   `json:"debug,omitempty"`
	// TouchID is Mac confirm. Chrome launches the host with no env we control.
	// nil = default on. false leaves Confirm unattached; confirm() fails closed.
	TouchID *bool `json:"touch_id,omitempty"`
	// Confirm is the confirmation policy: ""|"origin" (default) reuses one
	// Touch ID within a site for ConfirmTTL, "strict" prompts every release,
	// "session" reuses across sites. CVV prompts regardless.
	Confirm string `json:"confirm,omitempty"`
	// ConfirmTTL is the reuse window in seconds (default 30).
	ConfirmTTL int `json:"confirm_ttl_seconds,omitempty"`
}

type InstallEnv struct {
	Bin, VaultHome, UserHome, Origin, TokenFile string
	LoginEmail, PasswordFile, TOTPFile          string
}

// NativeHostArgs rewrites Chrome/Firefox's launch of the host binary into
// `fill`. The browser passes the extension origin as argv[1].
// VEIL_HOME is forced to the binary's directory so a login-shell
// VEIL_HOME/VEIL_ORIGIN cannot point the host at a different vault.
func NativeHostArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	if filepath.Base(args[0]) == HostFile || (len(args) > 1 && nativeMessagingOrigin(args[1])) {
		// VEIL_HOME is the binary's directory when it is a standalone host
		// (a fill.json sits next to it). Inside an app bundle the vault home
		// is still the user's ~/.veil — the bundle holds no state.
		dir := filepath.Dir(args[0])
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		if _, err := os.Stat(filepath.Join(dir, "fill.json")); err != nil {
			if user, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(user, ".veil")
			}
		}
		_ = os.Setenv("VEIL_HOME", dir)
		// Chrome inherits the launching shell. fill.json is the only switch.
		_ = os.Unsetenv("VEIL_FILL_TOUCHID")
		_ = os.Unsetenv("VEIL_AGENT")
		_ = os.Unsetenv("VEIL_OIDC_TOKEN")
		_ = os.Unsetenv("VEIL_OIDC_TOKEN_FILE")
		rest := args[1:]
		if len(rest) > 0 && nativeMessagingOrigin(rest[0]) {
			rest = rest[1:]
		}
		return append([]string{args[0], "fill"}, rest...)
	}
	return args
}

func nativeMessagingOrigin(s string) bool {
	return strings.HasPrefix(s, "chrome-extension://") || strings.HasPrefix(s, "moz-extension://")
}

func HostPath(vaultHome string) string {
	return filepath.Join(vaultHome, HostFile)
}

func ConfigPath(vaultHome string) string {
	return filepath.Join(vaultHome, HostConfigFile)
}

func WriteHostConfig(dir string, cfg HostConfig) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(dir), append(raw, '\n'), 0o600)
}

func ReadHostConfig(dir string) (HostConfig, error) {
	raw, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		return HostConfig{}, err
	}
	var cfg HostConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return HostConfig{}, fmt.Errorf("fill: %s: %w", HostConfigFile, err)
	}
	return cfg, nil
}

// ApplyHostConfig sets blank env from fill.json. Existing env wins.
func ApplyHostConfig(dir string) error {
	cfg, err := ReadHostConfig(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	setIfEmpty("VEIL_ORIGIN", cfg.Origin)
	setIfEmpty("VEIL_HOME", cfg.Home)
	setIfEmpty("VEIL_HUMAN_TOKEN_FILE", cfg.TokenFile)
	setIfEmpty("VEIL_LOGIN_EMAIL", cfg.LoginEmail)
	setIfEmpty("VEIL_KRATOS_PASSWORD_FILE", cfg.PasswordFile)
	setIfEmpty("VEIL_KRATOS_TOTP_FILE", cfg.TOTPFile)
	if cfg.Debug {
		setIfEmpty("VEIL_FILL_DEBUG", "1")
	}
	if cfg.TouchID != nil && !*cfg.TouchID {
		_ = os.Setenv("VEIL_FILL_TOUCHID", "0")
	}
	return nil
}

func setIfEmpty(key, val string) {
	if val == "" || strings.TrimSpace(os.Getenv(key)) != "" {
		return
	}
	_ = os.Setenv(key, val)
}

// DirBesideHost is the vault dir when Chrome launched native-host.
func DirBesideHost() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if filepath.Base(exe) != HostFile {
		return "", fmt.Errorf("fill: not %s", HostFile)
	}
	return filepath.Dir(exe), nil
}

func copyExecutable(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if sameFile(src, dst) {
		return os.Chmod(dst, 0o755)
	}
	// Write-then-rename, never truncate in place: macOS caches a binary's
	// code signature per inode, so rewriting the running host's file leaves
	// a stale signature and the next exec is SIGKILLed at launch.
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, in, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}
