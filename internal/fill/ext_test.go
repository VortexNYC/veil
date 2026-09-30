package fill

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFieldsJS(t *testing.T) {
	cmd := exec.Command("node", "--test", "fields.test.cjs", "passkeys.test.cjs", "tab.test.cjs", "background.test.cjs")
	cmd.Dir = filepath.Join(repoRoot(t), "apps/fill")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
}

func TestExtensionIDMatchesManifestKey(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "apps/fill/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(man.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	hex := strings.Builder{}
	const digits = "0123456789abcdef"
	for _, b := range sum[:16] {
		hex.WriteByte(digits[b>>4])
		hex.WriteByte(digits[b&0xf])
	}
	id := strings.Builder{}
	for _, c := range hex.String() {
		n := 0
		if c >= 'a' {
			n = int(c - 'a' + 10)
		} else {
			n = int(c - '0')
		}
		id.WriteByte(byte('a' + n))
	}
	want := "chrome-extension://" + id.String() + "/"
	if JSONChromeOrigin() != want {
		t.Fatalf("host %s manifest %s", JSONChromeOrigin(), want)
	}
}
