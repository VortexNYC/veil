package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	fillext "github.com/VortexNYC/veil/apps/fill"
	"github.com/VortexNYC/veil/internal/fill"
	"github.com/VortexNYC/veil/internal/sshagent"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "drift", "warn", "fail"
	Detail string `json:"detail"`
}

func doctorCmd(home *string, version string) *cobra.Command {
	var fix bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Verify binary, extension, daemon socket, auth, and origin parity across all Veil surfaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveHome(*home)
			if err != nil {
				return err
			}
			userHome, _ := os.UserHomeDir()
			if fix {
				repairLocalSurfaces(dir, userHome)
			}

			var checks []doctorCheck
			allOK := true
			record := func(name, status, detail string) {
				if status == "fail" || status == "drift" {
					allOK = false
				}
				checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
			}

			// 1. Binary parity: running exe vs ~/.veil/native-host vs ~/.local/bin/veil vs Veil.app
			selfExe, _ := os.Executable()
			if r, err := filepath.EvalSymlinks(selfExe); err == nil && r != "" {
				selfExe = r
			}
			selfSum := fileSHA256Short(selfExe)
			binPaths := []struct {
				label string
				path  string
			}{
				{"native-host", filepath.Join(dir, "native-host")},
				{"local-cli", filepath.Join(userHome, ".local", "bin", "veil")},
			}
			if runtime.GOOS == "darwin" {
				binPaths = append(binPaths, struct {
					label string
					path  string
				}{"Veil.app/veil-bin", "/Applications/Veil.app/Contents/MacOS/veil-bin"})
			}
			for _, bp := range binPaths {
				sum := fileSHA256Short(bp.path)
				if sum == "" {
					record("binary:"+bp.label, "warn", bp.path+" not present")
				} else if selfSum != "" && sum != selfSum {
					record("binary:"+bp.label, "drift", fmt.Sprintf("sha256=%s (running veil=%s) — run `veil doctor --fix`", sum, selfSum))
				} else {
					record("binary:"+bp.label, "ok", fmt.Sprintf("sha256=%s (%s)", sum, bp.path))
				}
			}

			// 2. Browser extension parity against embedded apps/fill/popup.js
			wantPopup := fsSHA256Short(fillext.Files, "popup.js")
			extTargets := []struct {
				label string
				path  string
			}{
				{"ext:chrome", filepath.Join(dir, "extension", "popup.js")},
				{"ext:firefox", filepath.Join(dir, "extension-firefox", "popup.js")},
			}
			if runtime.GOOS == "darwin" {
				extTargets = append(extTargets, struct {
					label string
					path  string
				}{"ext:safari", "/Applications/Veil.app/Contents/PlugIns/Veil Extension.appex/Contents/Resources/popup.js"})
			}
			for _, et := range extTargets {
				got := fileSHA256Short(et.path)
				if got == "" {
					record(et.label, "warn", et.path+" not installed")
				} else if wantPopup != "" && got != wantPopup {
					record(et.label, "drift", fmt.Sprintf("popup.js sha256=%s (embedded=%s) — run `veil doctor --fix`", got, wantPopup))
				} else {
					record(et.label, "ok", fmt.Sprintf("sha256=%s", got))
				}
			}

			// 3. Bridge & SSH agent sockets
			fillSock := filepath.Join(dir, "fill.sock")
			if probeUnixSocket(fillSock) {
				record("socket:fill", "ok", fillSock+" listening")
			} else {
				record("socket:fill", "fail", fillSock+" unreachable — run `veil doctor --fix`")
			}
			sshSock := sshagent.DefaultPath(dir)
			if probeUnixSocket(sshSock) {
				record("socket:ssh", "ok", sshSock+" listening")
			} else {
				record("socket:ssh", "warn", sshSock+" not listening")
			}

			// 4. Origin & Vault SPA reachability
			origin := originBase()
			if origin == "" {
				origin = "https://veil.nyc"
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			if status, body := probeHTTP(ctx, strings.TrimRight(origin, "/")+"/ready"); status == http.StatusOK {
				record("origin:ready", "ok", strings.TrimSpace(body))
			} else {
				record("origin:ready", "fail", fmt.Sprintf("%s/ready -> status %d", origin, status))
			}
			if status, _ := probeHTTP(ctx, "https://app.veil.nyc/"); status == http.StatusOK {
				record("web:app.veil.nyc", "ok", "200 OK")
			} else {
				record("web:app.veil.nyc", "warn", fmt.Sprintf("status %d", status))
			}

			// 5. Human token state
			if tok := doctorHumanToken(dir); tok != "" {
				record("auth:human-jwt", "ok", "valid bearer token ready")
			} else {
				record("auth:human-jwt", "warn", "no live human JWT (will auto-remint on next Touch ID / login)")
			}

			status := "ok"
			if !allOK {
				status = "drift_or_failure"
			}
			return encode(cmd, map[string]any{
				"status":  status,
				"version": version,
				"checks":  checks,
			})
		},
	}
	c.Flags().BoolVar(&fix, "fix", false, "automatically sync binaries, unpack browser extensions, and kickstart the fill bridge")
	return c
}

func repairLocalSurfaces(dir, userHome string) {
	// Pick the newest binary between Veil.app/Contents/MacOS/veil-bin and the running executable.
	srcBin, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(srcBin); err == nil && r != "" {
		srcBin = r
	}
	appBin := "/Applications/Veil.app/Contents/MacOS/veil-bin"
	if stApp, err := os.Stat(appBin); err == nil {
		if stSelf, err2 := os.Stat(srcBin); err2 != nil || stApp.ModTime().After(stSelf.ModTime()) {
			srcBin = appBin
		}
	}
	if srcBin != "" {
		for _, dst := range []string{
			filepath.Join(dir, "native-host"),
			filepath.Join(userHome, ".local", "bin", "veil"),
		} {
			if dst == srcBin {
				continue
			}
			if _, err := os.Stat(filepath.Dir(dst)); err == nil {
				copyFileAtomic(srcBin, dst, 0o755)
			}
		}
	}
	_ = fill.InstallExtension(fillext.Files, filepath.Join(dir, "extension"))
	_ = fill.InstallExtensionFirefox(fillext.Files, filepath.Join(dir, "extension-firefox"))
	if runtime.GOOS == "darwin" && !probeUnixSocket(filepath.Join(dir, "fill.sock")) {
		uid := fmt.Sprintf("%d", os.Getuid())
		_ = exec.Command("/bin/launchctl", "kickstart", "-k", "gui/"+uid+"/nyc.veil.fill.bridge").Run()
		time.Sleep(400 * time.Millisecond)
	}
}

func copyFileAtomic(src, dst string, mode os.FileMode) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, raw, mode); err != nil {
		return
	}
	_ = os.Rename(tmp, dst)
}

func fileSHA256Short(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

func fsSHA256Short(fsys fs.FS, name string) string {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

func probeUnixSocket(path string) bool {
	c, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func probeHTTP(ctx context.Context, url string) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, ""
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, ""
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	return res.StatusCode, string(b)
}

func doctorHumanToken(dir string) string {
	if tok, _ := originHumanToken(); tok != "" {
		return tok
	}
	cfg, err := fill.ReadHostConfig(dir)
	if err != nil || cfg.TokenFile == "" {
		return ""
	}
	raw, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return ""
	}
	tok := strings.TrimSpace(string(raw))
	if tok != "" && !jwtNeedsRefresh(tok) {
		return tok
	}
	return ""
}
