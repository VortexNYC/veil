package confirm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// cli-allow.json records which client apps a human already consented to run
// veil commands as — the byte-level '1' it stores is a consent flag, not a
// secret, so it lives in the vault dir at 0600 instead of the Keychain.
// The Keychain version prompted for the login password on every unsigned or
// freshly built binary (each rebuild is a new "app" to the item's ACL) and
// could never remember unbundled callers at all.
type allowList struct {
	Approved []string `json:"approved,omitempty"`
}

func allowPath() string {
	dir := strings.TrimSpace(os.Getenv("VEIL_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".veil")
	}
	return filepath.Join(dir, "cli-allow.json")
}

func loadAllow() allowList {
	var out allowList
	path := allowPath()
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func cliAllowed(key string) bool {
	if key == "" {
		return false
	}
	for _, k := range loadAllow().Approved {
		if k == key {
			return true
		}
	}
	return false
}

func cliRemember(key string) {
	if key == "" {
		return
	}
	list := loadAllow()
	for _, k := range list.Approved {
		if k == key {
			return
		}
	}
	list.Approved = append(list.Approved, key)
	path := allowPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(raw, '\n'), 0o600)
}
