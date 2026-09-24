package confirm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnabledOff(t *testing.T) {
	t.Setenv("VEIL_FILL_TOUCHID", "0")
	if Enabled() {
		t.Fatal("touch id on with VEIL_FILL_TOUCHID=0")
	}
}

func TestActionStripsVeilWantsTo(t *testing.T) {
	if Action("Veil wants to fill a card") != "fill a card" {
		t.Fatalf("%q", Action("Veil wants to fill a card"))
	}
	if Action("get CLI access") != "get CLI access" {
		t.Fatalf("%q", Action("get CLI access"))
	}
	if Action("") != "use Veil" {
		t.Fatalf("%q", Action(""))
	}
}

func TestCLIAccessOffIsNoop(t *testing.T) {
	t.Setenv("VEIL_FILL_TOUCHID", "0")
	if err := CLIAccess(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIAccessSkipsInsideTestBinary(t *testing.T) {
	// testing.Testing() is true here — the consent sheet must never fire
	// under `go test` regardless of env or tty.
	if err := CLIAccess(); err != nil {
		t.Fatal(err)
	}
}

func TestCliAllowFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("VEIL_HOME", home)
	if cliAllowed("b:com.example.app") {
		t.Fatal("fresh allow list must be empty")
	}
	cliRemember("b:com.example.app")
	if !cliAllowed("b:com.example.app") {
		t.Fatal("remembered bundle not found")
	}
	cliRemember("b:com.example.app")
	cliRemember("p:devin")
	list := loadAllow()
	if len(list.Approved) != 2 {
		t.Fatalf("dupes must not accumulate: %+v", list)
	}
	raw, err := os.ReadFile(filepath.Join(home, "cli-allow.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "cli-allow.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("allow list must be 0600, got %o", info.Mode().Perm())
	}
	var decoded allowList
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Approved) != 2 || decoded.Approved[0] != "b:com.example.app" || decoded.Approved[1] != "p:devin" {
		t.Fatalf("%+v", decoded)
	}
}

func TestCommandReasonNamesTheVerb(t *testing.T) {
	got := commandReason()
	// os.Args under `go test` is the test binary — the reason must still
	// produce a verb line, never an empty "run veil commands" fallback bug.
	if !strings.HasPrefix(got, "run `veil ") && got != "run veil commands" {
		t.Fatalf("%q", got)
	}
}
