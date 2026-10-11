package fill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIconCachedHit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "example.com.icon"), []byte{0x89, 'P', 'N', 'G', 1, 2, 3}, 0o600)
	url, fetch := iconCached(dir, "example.com")
	if !strings.HasPrefix(url, "data:image/png;base64,") || fetch {
		t.Fatalf("cached hit = %q fetch=%v", url, fetch)
	}
}

func TestIconCachedNegative(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "empty.test.icon.none"), nil, 0o600)
	if url, fetch := iconCached(dir, "empty.test"); url != "" || fetch {
		t.Fatalf("negative marker should suppress fetch, got %q %v", url, fetch)
	}
}

func TestIconCachedMissWantsFetch(t *testing.T) {
	dir := t.TempDir()
	if url, fetch := iconCached(dir, "new.test"); url != "" || !fetch {
		t.Fatalf("cold host should request fetch, got %q %v", url, fetch)
	}
}

func TestIconFileSanitizes(t *testing.T) {
	if iconFile("/tmp/x", "../etc/passwd") != "" {
		t.Fatal("traversal host must be rejected")
	}
	if iconFile("/tmp/x", "good.host.com") == "" {
		t.Fatal("plain host should produce a path")
	}
}

func TestIconHostSkipsAppBindings(t *testing.T) {
	if iconHost("com.apple.TextEdit") != "com.apple.textedit" {
		t.Fatal("bundle IDs lowercase like hosts but the fetch gate is below")
	}
	if iconHost("app://com.apple.TextEdit") != "" {
		t.Fatal("app:// bindings are not domains")
	}
	if iconHost("EXAMPLE.com") != "example.com" {
		t.Fatal("hosts lowercase")
	}
	if iconHost("example.com:8443") != "example.com" {
		t.Fatal("ports strip")
	}
}

func TestIconDataURLMime(t *testing.T) {
	if u := iconDataURL([]byte{0x89, 'P', 'N', 'G'}); !strings.HasPrefix(u, "data:image/png") {
		t.Fatalf("png sniffed wrong: %q", u)
	}
	if u := iconDataURL([]byte{0, 0, 1, 0}); !strings.HasPrefix(u, "data:image/x-icon") {
		t.Fatalf("ico sniffed wrong: %q", u)
	}
}
