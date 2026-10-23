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

func TestMCPHTTPBlockEmitsCredentialBearer(t *testing.T) {
	tmpHome := t.TempDir()
	credDir := filepath.Join(tmpHome, ".config", "vortex", "veil")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "grok.hydra"), []byte(`{"secret":"sekrit1","audience":"veil"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)
	t.Setenv("VEIL_ORIGIN", "https://veil.nyc")
	t.Setenv("VEIL_AGENT", "grok")
	t.Setenv("VEIL_HYDRA_SECRET_FILE", "")

	cfg, err := mcpHTTPBlock("https://veil.nyc")
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	veil, _ := servers["veil"].(map[string]any)
	if veil["type"] != "http" || veil["url"] != "https://veil.nyc/mcp" {
		t.Fatalf("%+v", veil)
	}
	headers, _ := veil["headers"].(map[string]string)
	if got := headers["Authorization"]; got != "Bearer agent-grok:sekrit1" {
		t.Fatalf("Authorization=%q", got)
	}
}

func TestMCPHTTPBlockRequiresAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VEIL_ORIGIN", "https://veil.nyc")
	t.Setenv("VEIL_AGENT", "")
	t.Setenv("VEIL_HYDRA_SECRET_FILE", "")
	if _, err := mcpHTTPBlock("https://veil.nyc"); err == nil {
		t.Fatal("expected error without VEIL_AGENT")
	}
}
