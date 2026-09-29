package fill

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func Install(bin, vaultHome, userHome string) error {
	return InstallOrigin(InstallEnv{Bin: bin, VaultHome: vaultHome, UserHome: userHome})
}

func InstallOrigin(env InstallEnv) error {
	if err := os.MkdirAll(env.VaultHome, 0o700); err != nil {
		return err
	}
	host := HostPath(env.VaultHome)
	if err := copyExecutable(env.Bin, host); err != nil {
		return err
	}
	// fill.json outlives reinstalls: an existing config is the human's, env
	// values override per-field, and the token path default only fills a gap.
	// Wiping login_email/touch_id on every `fill install` strands the host.
	cfg, _ := ReadHostConfig(env.VaultHome)
	if env.Origin != "" {
		cfg.Origin = env.Origin
	}
	cfg.Home = env.VaultHome
	if env.Origin != "" {
		if env.TokenFile != "" {
			cfg.TokenFile = env.TokenFile
		}
		if cfg.TokenFile == "" {
			cfg.TokenFile = filepath.Join(env.UserHome, ".config/veil/human.jwt")
		}
		if env.LoginEmail != "" {
			cfg.LoginEmail = env.LoginEmail
		}
		if env.PasswordFile != "" {
			cfg.PasswordFile = env.PasswordFile
		}
		if env.TOTPFile != "" {
			cfg.TOTPFile = env.TOTPFile
		}
	}
	if err := WriteHostConfig(env.VaultHome, cfg); err != nil {
		return err
	}
	chromeDir := filepath.Join(env.UserHome, "Library/Application Support/Google/Chrome/NativeMessagingHosts")
	ffDir := filepath.Join(env.UserHome, "Library/Application Support/Mozilla/NativeMessagingHosts")
	if err := os.MkdirAll(chromeDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(ffDir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(chromeDir, NativeHostName+".json"))
	_ = os.Remove(filepath.Join(ffDir, NativeHostName+".json"))
	if err := os.WriteFile(filepath.Join(chromeDir, JSONHostName+".json"), ManifestJSONChrome(host), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ffDir, JSONHostName+".json"), ManifestJSONFirefox(host), 0o644); err != nil {
		return err
	}
	return InstallBridgeAgent(env)
}

// BridgeLabel is the launchd job Safari's sandboxed appex reaches over
// <vault-home>/fill.sock. Appexes cannot spawn the host — the agent runs it.
const BridgeLabel = "nyc.veil.fill.bridge"

// InstallBridgeAgent registers the fill socket bridge as a per-user
// LaunchAgent and (re)starts it. macOS-only — a no-op elsewhere.
func InstallBridgeAgent(env InstallEnv) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	dir := filepath.Join(env.UserHome, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>--bridge</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>EnvironmentVariables</key>
	<dict>
		<key>VEIL_REPLICA_KEYSTORE</key>
		<string>mem</string>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, BridgeLabel, HostPath(env.VaultHome), filepath.Join(env.VaultHome, "fill-bridge.log"), filepath.Join(env.VaultHome, "fill-bridge.log"))
	path := filepath.Join(dir, BridgeLabel+".plist")
	return os.WriteFile(path, []byte(plist), 0o644)
}

// BootstrapBridgeAgent (re)starts the LaunchAgent written by
// InstallBridgeAgent. Lives at the CLI boundary so tests that exercise
// InstallOrigin never touch launchd.
func BootstrapBridgeAgent(userHome string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	path := filepath.Join(userHome, "Library", "LaunchAgents", BridgeLabel+".plist")
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// Already-bootstrapped is fine — kickstart -k restarts it.
	if err := exec.Command("launchctl", "bootstrap", domain, path).Run(); err != nil {
		return exec.Command("launchctl", "kickstart", "-k", domain+"/"+BridgeLabel).Run()
	}
	return exec.Command("launchctl", "kickstart", "-k", domain+"/"+BridgeLabel).Run()
}

// InstallExtension writes the embedded MV3 payload to dir — the stable path a
// human "load unpacked"s in chrome://extensions. The manifest key pins the
// extension id the native host manifest allowlists; flat files only.
func InstallExtension(dist fs.FS, dir string) error {
	return installExtension(dist, dir, chrome)
}

// InstallExtensionFirefox lays down the same payload with the transformed
// Firefox manifest (background scripts array, gecko id) — load it as a
// temporary add-on via about:debugging or package with `web-ext build`.
func InstallExtensionFirefox(dist fs.FS, dir string) error {
	return installExtension(dist, dir, firefox)
}

// InstallExtensionSafari lays down the payload the Safari web-extension
// converter consumes — the same files with the Safari manifest. The
// containing .app is apps/fill-safari; this directory is its Resources input.
func InstallExtensionSafari(dist fs.FS, dir string) error {
	return installExtension(dist, dir, safari)
}

type extKind int

const (
	chrome extKind = iota
	firefox
	safari
)

func installExtension(dist fs.FS, dir string, kind extKind) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(dist, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(dist, path)
		if err != nil {
			return err
		}
		if kind != chrome && filepath.Base(path) == "manifest.json" {
			if kind == firefox {
				raw, err = ExtensionManifestFirefox(raw)
			} else {
				raw, err = ExtensionManifestSafari(raw)
			}
			if err != nil {
				return err
			}
		}
		return os.WriteFile(filepath.Join(dir, filepath.Base(path)), raw, 0o644)
	})
}
