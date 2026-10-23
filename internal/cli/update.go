package cli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	fillext "github.com/VortexNYC/veil/apps/fill"
	"github.com/VortexNYC/veil/internal/fill"
)

const (
	defaultUpdateRepo    = "VortexNYC/veil"
	defaultUpdateAPIBase = "https://api.github.com"
	// Pinned Ed25519 release signing public key (same key as Sparkle SUPublicEDKey).
	defaultUpdatePubKeyB64 = "QFpn84Z7FgTWnIPf+KNRfvhjO7Upig1Exp4WOo72Ig0="
)

var (
	updateAPIBase      = defaultUpdateAPIBase
	updateVerifyPubB64 = defaultUpdatePubKeyB64
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func updateCmd(home *string, currentVersion string) *cobra.Command {
	var checkOnly, syncOnly bool
	c := &cobra.Command{
		Use:   "update",
		Short: "Update the veil CLI binary and local browser extension payloads from signed GitHub Releases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if syncOnly {
				dir, err := resolveHome(*home)
				if err != nil {
					return err
				}
				if err := syncLocalExtensions(dir); err != nil {
					return err
				}
				return encode(cmd, map[string]any{"status": "synced", "version": currentVersion})
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			rel, err := fetchLatestRelease(ctx, updateAPIBase, defaultUpdateRepo)
			if err != nil {
				return fmt.Errorf("update check failed: %w", err)
			}
			latestVer := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
			curVer := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")
			if latestVer == "" || latestVer == curVer {
				if dir, derr := resolveHome(*home); derr == nil {
					_ = syncLocalExtensions(dir)
				}
				return encode(cmd, map[string]any{
					"status":  "up_to_date",
					"version": currentVersion,
				})
			}
			if checkOnly {
				return encode(cmd, map[string]any{
					"status":  "update_available",
					"current": currentVersion,
					"latest":  latestVer,
					"url":     rel.HTMLURL,
				})
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			if err := applyReleaseUpdate(ctx, rel, exe); err != nil {
				return fmt.Errorf("update binary: %w", err)
			}
			if dir, derr := resolveHome(*home); derr == nil {
				_ = syncLocalExtensions(dir)
			}
			return encode(cmd, map[string]any{
				"status":  "updated",
				"from":    currentVersion,
				"version": latestVer,
			})
		},
	}
	c.Flags().BoolVar(&checkOnly, "check", false, "only check if a newer release is available without updating")
	c.Flags().BoolVar(&syncOnly, "sync-only", false, "only refresh local unpacked browser extensions in ~/.veil")
	return c
}

func fetchLatestRelease(ctx context.Context, apiBase, repo string) (*ghRelease, error) {
	u := strings.TrimRight(apiBase, "/") + "/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases status %d", res.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func applyReleaseUpdate(ctx context.Context, rel *ghRelease, exePath string) error {
	pubRaw, err := base64.StdEncoding.DecodeString(updateVerifyPubB64)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid pinned release public key")
	}
	pubKey := ed25519.PublicKey(pubRaw)

	wantAsset := fmt.Sprintf("veil_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var assetURL, sumsURL, sigURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case wantAsset:
			assetURL = a.BrowserDownloadURL
		case "checksums.txt":
			sumsURL = a.BrowserDownloadURL
		case "checksums.txt.sig":
			sigURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" || sumsURL == "" || sigURL == "" {
		return fmt.Errorf("release %s missing %s, checksums.txt, or checksums.txt.sig", rel.TagName, wantAsset)
	}
	sumsRaw, err := downloadBytes(ctx, sumsURL, 1<<20)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	sigRaw, err := downloadBytes(ctx, sigURL, 64*1024)
	if err != nil {
		return fmt.Errorf("download checksums.txt.sig: %w", err)
	}
	sigBytes, err := decodeSignature(sigRaw)
	if err != nil || !ed25519.Verify(pubKey, sumsRaw, sigBytes) {
		return fmt.Errorf("checksums.txt.sig Ed25519 signature verification failed")
	}
	wantHex := ""
	for line := range strings.Lines(string(sumsRaw)) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == wantAsset {
			wantHex = strings.ToLower(fields[0])
			break
		}
	}
	if len(wantHex) != 64 {
		return fmt.Errorf("checksum for %s not found in checksums.txt", wantAsset)
	}
	tarRaw, err := downloadBytes(ctx, assetURL, 128<<20)
	if err != nil {
		return fmt.Errorf("download %s: %w", wantAsset, err)
	}
	sum := sha256.Sum256(tarRaw)
	if gotHex := hex.EncodeToString(sum[:]); gotHex != wantHex {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", wantAsset, gotHex, wantHex)
	}
	binName := "veil"
	if runtime.GOOS == "windows" {
		binName = "veil.exe"
	}
	binBytes, err := extractBinaryFromTarGz(tarRaw, binName)
	if err != nil {
		return err
	}
	tmp := exePath + ".new"
	if err := os.WriteFile(tmp, binBytes, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(exePath + ".old")
		_ = os.Rename(exePath, exePath+".old")
	}
	if err := os.Rename(tmp, exePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func decodeSignature(raw []byte) ([]byte, error) {
	if len(raw) == ed25519.SignatureSize {
		return raw, nil
	}
	s := strings.TrimSpace(string(raw))
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == ed25519.SignatureSize {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == ed25519.SignatureSize {
		return b, nil
	}
	return nil, fmt.Errorf("invalid signature encoding")
}

func downloadBytes(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxBytes))
}

func extractBinaryFromTarGz(raw []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == name && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, 128<<20))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", name)
}

// syncLocalExtensions refreshes ~/.veil/extension and ~/.veil/extension-firefox
// if ~/.veil/fill.json exists so unpacked Chrome/Firefox extensions stay in
// lockstep with the updated CLI / Veil.app binary.
func syncLocalExtensions(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, fill.HostConfigFile)); err != nil {
		return nil
	}
	if err := fill.InstallExtension(fillext.Files, filepath.Join(dir, "extension")); err != nil {
		return err
	}
	return fill.InstallExtensionFirefox(fillext.Files, filepath.Join(dir, "extension-firefox"))
}
