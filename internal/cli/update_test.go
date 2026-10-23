package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
				},
			})
		case "/" + assetName:
			_, _ = w.Write(tarBytes)
		case "/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", goodHex, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	// 1. --check reports update_available
	var out bytes.Buffer
	cmd := New("0.1.0")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", t.TempDir(), "update", "--check", "--api-base", srv.URL})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --check: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "update_available"`) || !strings.Contains(out.String(), `"latest": "0.2.0"`) {
		t.Fatalf("unexpected check output: %s", out.String())
	}

	// 2. applyReleaseUpdate verifies SHA-256 and replaces target binary
	ctx := context.Background()
	rel, err := fetchLatestRelease(ctx, srv.URL, "VortexNYC/veil")
	if err != nil {
		t.Fatal(err)
	}
	targetExe := filepath.Join(t.TempDir(), binName)
	if err := os.WriteFile(targetExe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyReleaseUpdate(ctx, rel, targetExe); err != nil {
		t.Fatalf("applyReleaseUpdate: %v", err)
	}
	gotBytes, err := os.ReadFile(targetExe)
	if err != nil || !bytes.Equal(gotBytes, wantPayload) {
		t.Fatalf("replaced binary mismatch: %q vs %q (%v)", gotBytes, wantPayload, err)
	}
}
