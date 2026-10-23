package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyMCPStdioDefaultsResolvesExplicitAgentOnly(t *testing.T) {
	tmpHome := t.TempDir()
	credDir := filepath.Join(tmpHome, ".config", "vortex", "veil")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"grok", "cursor"} {
		if err := os.WriteFile(filepath.Join(credDir, a+".hydra"), []byte(`{"secret":"s"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", tmpHome)
	t.Setenv("VEIL_ORIGIN", "https://veil.nyc")
	t.Setenv("VEIL_HYDRA_ISSUER", "")
	t.Setenv("VEIL_OIDC_TOKEN_FILE", "")
	t.Setenv("VEIL_OIDC_TOKEN", "")
	t.Setenv("VEIL_HYDRA_SECRET_FILE", "")
	t.Setenv("VEIL_AGENT", "grok")

	applyMCPStdioDefaults()

	if got := os.Getenv("VEIL_HYDRA_ISSUER"); got != "https://id.veil.nyc" {
		t.Fatalf("issuer=%q", got)
	}
	if got := os.Getenv("VEIL_HYDRA_SECRET_FILE"); got != filepath.Join(credDir, "grok.hydra") {
		t.Fatalf("secret_file=%q", got)
	}
	if got := os.Getenv("VEIL_OIDC_TOKEN_FILE"); got != filepath.Join(credDir, "grok.jwt") {
		t.Fatalf("token_file=%q", got)
	}
}
