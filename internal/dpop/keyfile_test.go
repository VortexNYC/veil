package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.dpop")
	k1, err := LoadOrGenerateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadOrGenerateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if k1.D.Cmp(k2.D) != 0 {
		t.Fatal("reload changed private key")
	}
	if k1.Curve != elliptic.P256() {
		t.Fatal("expected P-256")
	}
	var _ *ecdsa.PrivateKey = k1
}
