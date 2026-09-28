package fill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExtensionManifestFirefox(t *testing.T) {
	chrome := []byte(`{
		"manifest_version": 3,
		"name": "Veil",
		"key": "MIIBIjAN",
		"permissions": ["nativeMessaging", "tabs", "scripting"],
		"background": {"service_worker": "background.js"},
		"content_scripts": [{"matches": ["https://*/*"], "js": ["passkeys-page.js"], "world": "MAIN"}]
	}`)
	raw, err := ExtensionManifestFirefox(chrome)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["key"]; ok {
		t.Fatal("key is Chrome-only — must not be in the Firefox manifest")
	}
	bg, _ := m["background"].(map[string]any)
	scripts, _ := bg["scripts"].([]any)
	if len(scripts) != 2 || scripts[0] != "tab.js" || scripts[1] != "background.js" {
		t.Fatalf("firefox background scripts = %v", bg)
	}
	if _, sw := bg["service_worker"]; sw {
		t.Fatal("service_worker survives into the Firefox manifest")
	}
	bss, _ := m["browser_specific_settings"].(map[string]any)
	gecko, _ := bss["gecko"].(map[string]any)
	if gecko["id"] != JSONFirefoxID() {
		t.Fatalf("gecko id = %v, want %s", gecko["id"], JSONFirefoxID())
	}
	cs, _ := m["content_scripts"].([]any)
	first, _ := cs[0].(map[string]any)
	if first["world"] != "MAIN" {
		t.Fatal("MAIN-world passkeys content script dropped")
	}
}

func TestInstallExtensionFirefox(t *testing.T) {
	dir := t.TempDir()
	dist := fstest.MapFS{
		"manifest.json": {Data: []byte(`{"manifest_version":3,"key":"abc","background":{"service_worker":"background.js"}}`)},
		"background.js": {Data: []byte("x")},
	}
	if err := InstallExtensionFirefox(dist, dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["key"]; ok {
		t.Fatal("installed Firefox manifest still carries the Chrome key")
	}
	if _, ok := m["browser_specific_settings"]; !ok {
		t.Fatal("installed Firefox manifest missing gecko settings")
	}
}
