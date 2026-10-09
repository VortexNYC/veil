package fill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeHostArgsRewritesChromeLaunch(t *testing.T) {
	got := NativeHostArgs([]string{"/vault/" + HostFile, ChromeOrigin()})
	if len(got) != 2 || got[1] != "fill" {
		t.Fatalf("%v", got)
	}
	jsonHost := NativeHostArgs([]string{"/vault/" + HostFile, JSONChromeOrigin()})
	if len(jsonHost) != 2 || jsonHost[1] != "fill" {
		t.Fatalf("%v", jsonHost)
	}
	plain := NativeHostArgs([]string{"veil", "item", "list"})
	if len(plain) != 3 || plain[1] != "item" {
		t.Fatalf("%v", plain)
	}
}

func TestNativeHostArgsBindsHomeToBinaryDir(t *testing.T) {
	t.Setenv("VEIL_HOME", "/Users/someone/.veil")
	dir := t.TempDir()
	// Standalone host: a fill.json beside the binary is what makes its
	// directory the vault home. Bundled in an app there is none and the
	// home stays the user's ~/.veil.
	if err := os.WriteFile(filepath.Join(dir, "fill.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, HostFile)
	got := NativeHostArgs([]string{bin, JSONChromeOrigin()})
	if len(got) != 2 || got[1] != "fill" {
		t.Fatalf("%v", got)
	}
	if os.Getenv("VEIL_HOME") != dir {
		t.Fatalf("VEIL_HOME=%s want %s", os.Getenv("VEIL_HOME"), dir)
	}
}

func TestNativeHostArgsBundledBinaryKeepsUserHome(t *testing.T) {
	t.Setenv("VEIL_HOME", "/Users/someone/.veil")
	dir := t.TempDir()
	bin := filepath.Join(dir, HostFile)
	NativeHostArgs([]string{bin, JSONChromeOrigin()})
	user, _ := os.UserHomeDir()
	want := filepath.Join(user, ".veil")
	if os.Getenv("VEIL_HOME") != want {
		t.Fatalf("VEIL_HOME=%s want %s", os.Getenv("VEIL_HOME"), want)
	}
}

func TestNativeHostArgsStripsAgentShellEnv(t *testing.T) {
	t.Setenv("VEIL_FILL_TOUCHID", "0")
	t.Setenv("VEIL_AGENT", "cursor")
	t.Setenv("VEIL_OIDC_TOKEN_FILE", "/tmp/agent.jwt")
	t.Setenv("VEIL_OIDC_TOKEN", "no")
	dir := t.TempDir()
	bin := filepath.Join(dir, HostFile)
	NativeHostArgs([]string{bin, JSONChromeOrigin()})
	if os.Getenv("VEIL_FILL_TOUCHID") != "" {
		t.Fatal("VEIL_FILL_TOUCHID leaked from the launching shell")
	}
	if os.Getenv("VEIL_AGENT") != "" || os.Getenv("VEIL_OIDC_TOKEN_FILE") != "" || os.Getenv("VEIL_OIDC_TOKEN") != "" {
		t.Fatal("agent env leaked into the native host")
	}
}

func TestApplyHostConfigSetsBlankEnv(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHostConfig(dir, HostConfig{Origin: "https://veil.nyc", Home: dir}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ORIGIN", "")
	t.Setenv("VEIL_HOME", "")
	if err := ApplyHostConfig(dir); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VEIL_ORIGIN") != "https://veil.nyc" {
		t.Fatalf("%s", os.Getenv("VEIL_ORIGIN"))
	}
	if os.Getenv("VEIL_HOME") != dir {
		t.Fatalf("%s", os.Getenv("VEIL_HOME"))
	}
}

func TestApplyHostConfigDoesNotOverrideEnv(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHostConfig(dir, HostConfig{Origin: "https://veil.nyc"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ORIGIN", "https://example.invalid")
	if err := ApplyHostConfig(dir); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VEIL_ORIGIN") != "https://example.invalid" {
		t.Fatal("overrode VEIL_ORIGIN")
	}
}

func TestApplyHostConfigDebug(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHostConfig(dir, HostConfig{Debug: true}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_FILL_DEBUG", "")
	if err := ApplyHostConfig(dir); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VEIL_FILL_DEBUG") != "1" {
		t.Fatal("debug not applied")
	}
}

func TestApplyHostConfigTouchIDOff(t *testing.T) {
	dir := t.TempDir()
	off := false
	if err := WriteHostConfig(dir, HostConfig{TouchID: &off}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_FILL_TOUCHID", "")
	if err := ApplyHostConfig(dir); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VEIL_FILL_TOUCHID") != "0" {
		t.Fatal("touch id not off")
	}
}

func TestHostConfigMissingIsOK(t *testing.T) {
	if err := ApplyHostConfig(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestCopyExecutableWritesHostBytes(t *testing.T) {
	src := filepath.Join(t.TempDir(), "bin")
	dst := filepath.Join(t.TempDir(), HostFile)
	if err := os.WriteFile(src, []byte("veil-host-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if bytesHasShebang(got) {
		t.Fatal("copied a script")
	}
}

func bytesHasShebang(b []byte) bool {
	return len(b) >= 2 && b[0] == '#' && b[1] == '!'
}
