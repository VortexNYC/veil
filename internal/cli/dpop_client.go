package cli

import (
	"crypto/ecdsa"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/VortexNYC/veil/internal/dpop"
)

// originDPoPEnabled is on by default for origin HTTP. Set VEIL_DPOP=0 to disable.
func originDPoPEnabled() bool {
	v := strings.TrimSpace(os.Getenv("VEIL_DPOP"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func originDPoPKeyPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("VEIL_DPOP_KEY_FILE")); p != "" {
		return p, nil
	}
	for _, env := range []string{"VEIL_OIDC_TOKEN_FILE", "VEIL_HUMAN_TOKEN_FILE"} {
		if f := strings.TrimSpace(os.Getenv(env)); f != "" {
			return dpopKeySiblingPath(f), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("origin dpop: set VEIL_DPOP_KEY_FILE or a token file env")
	}
	return filepath.Join(home, ".config", "vortex", "veil", "_default.dpop"), nil
}

func dpopKeySiblingPath(tokenFile string) string {
	base := strings.TrimSuffix(tokenFile, filepath.Ext(tokenFile))
	if base == tokenFile {
		return tokenFile + ".dpop"
	}
	return base + ".dpop"
}

var (
	originDPoPCacheMu sync.Mutex
	originDPoPCache   struct {
		path string
		key  *ecdsa.PrivateKey
	}
)

func originDPoPPrivateKey() (*ecdsa.PrivateKey, error) {
	if !originDPoPEnabled() {
		return nil, nil
	}
	path, err := originDPoPKeyPath()
	if err != nil {
		return nil, err
	}
	originDPoPCacheMu.Lock()
	defer originDPoPCacheMu.Unlock()
	if originDPoPCache.path == path && originDPoPCache.key != nil {
		return originDPoPCache.key, nil
	}
	key, err := dpop.LoadOrGenerateKey(path)
	if err != nil {
		return nil, err
	}
	originDPoPCache.path = path
	originDPoPCache.key = key
	return key, nil
}

func originAttachAuth(req *http.Request, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("origin: empty token")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if !originDPoPEnabled() {
		return nil
	}
	key, err := originDPoPPrivateKey()
	if err != nil {
		return err
	}
	if key == nil {
		return nil
	}
	proof, err := dpop.MintProof(key, req.Method, req.URL.String(), token, time.Now())
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "DPoP "+token)
	req.Header.Set("DPoP", proof)
	return nil
}

// ensureAgentDPoPKeyFile creates the persistent DPoP key beside the agent JWT
// file after Hydra bootstrap so the first remint/origin call is already keyed.
func ensureAgentDPoPKeyFile(agentName, secretFile string) error {
	if !originDPoPEnabled() {
		return nil
	}
	jwtPath := strings.TrimSpace(os.Getenv("VEIL_OIDC_TOKEN_FILE"))
	if jwtPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		jwtPath = filepath.Join(home, ".config", "vortex", "veil", strings.TrimSpace(agentName)+".jwt")
	}
	_, err := dpop.LoadOrGenerateKey(dpopKeySiblingPath(jwtPath))
	return err
}
