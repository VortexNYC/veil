package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/fill"
)

func TestUpdateSyncOnlyRefreshesInstalledExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := fill.WriteHostConfig(dir, fill.HostConfig{Origin: "https://veil.nyc"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := New("0.1.0")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", dir, "update", "--sync-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --sync-only: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "synced"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	for _, sub := range []string{"extension/manifest.json", "extension-firefox/manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Fatalf("expected %s written: %v", sub, err)
		}
	}
}

func TestUpdateCheckAndApplyReleaseUpdate(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	prevPub := updateVerifyPubB64
	prevBase := updateAPIBase
	t.Cleanup(func() {
		updateVerifyPubB64 = prevPub
		updateAPIBase = prevBase
	})
	updateVerifyPubB64 = base64.StdEncoding.EncodeToString(pub)

	assetName := fmt.Sprintf("veil_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	binName := "veil"
	if runtime.GOOS == "windows" {
		binName = "veil.exe"
	}
	wantPayload := []byte("#!/bin/sh\necho veil-0.2.0\n")

	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     binName,
		Mode:     0o755,
		Size:     int64(len(wantPayload)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(wantPayload); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	tarBytes := tarBuf.Bytes()
	sum := sha256.Sum256(tarBytes)
	goodHex := hex.EncodeToString(sum[:])
	sumsBody := fmt.Sprintf("%s  %s\n", goodHex, assetName)
	sigBody := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(sumsBody)))

	tamperSig := false
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/VortexNYC/veil/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.2.0",
				"html_url": srvURL + "/releases/v0.2.0",
				"assets": []map[string]any{
					{"name": assetName, "browser_download_url": srvURL + "/" + assetName},
					{"name": "checksums.txt", "browser_download_url": srvURL + "/checksums.txt"},
					{"name": "checksums.txt.sig", "browser_download_url": srvURL + "/checksums.txt.sig"},
				},
			})
		case "/" + assetName:
			_, _ = w.Write(tarBytes)
		case "/checksums.txt":
			_, _ = w.Write([]byte(sumsBody))
		case "/checksums.txt.sig":
			if tamperSig {
				_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))))
				return
			}
			_, _ = w.Write([]byte(sigBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL
	updateAPIBase = srv.URL

	// 1. --check reports update_available
	var out bytes.Buffer
	cmd := New("0.1.0")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", t.TempDir(), "update", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --check: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "update_available"`) || !strings.Contains(out.String(), `"latest": "0.2.0"`) {
		t.Fatalf("unexpected check output: %s", out.String())
	}

	// 2. applyReleaseUpdate rejects invalid Ed25519 signature
	ctx := context.Background()
	rel, err := fetchLatestRelease(ctx, srv.URL, "VortexNYC/veil")
	if err != nil {
		t.Fatal(err)
	}
	targetExe := filepath.Join(t.TempDir(), binName)
	if err := os.WriteFile(targetExe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	tamperSig = true
	if err := applyReleaseUpdate(ctx, rel, targetExe); err == nil {
		t.Fatal("expected invalid Ed25519 signature to be rejected")
	}
	tamperSig = false

	// 3. applyReleaseUpdate verifies Ed25519 + SHA-256 and replaces target binary
	if err := applyReleaseUpdate(ctx, rel, targetExe); err != nil {
		t.Fatalf("applyReleaseUpdate: %v", err)
	}
	gotBytes, err := os.ReadFile(targetExe)
	if err != nil || !bytes.Equal(gotBytes, wantPayload) {
		t.Fatalf("replaced binary mismatch: %q vs %q (%v)", gotBytes, wantPayload, err)
	}
}
