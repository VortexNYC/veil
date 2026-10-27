package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mint order is the contract: bridge Touch ID first, password+TOTP
// remint only when the bridge can't mint. Stubbing both legs proves the
// order without a Mac or a live socket.
func TestMintHumanTokenPrefersBridge(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tok")
	var remintCalled bool
	origBridge, origRemint := bridgeTokenFn, remintHTTPFn
	bridgeTokenFn = func(context.Context) (string, error) { return "bridge-tok", nil }
	remintHTTPFn = func(context.Context, string) (string, error) {
		remintCalled = true
		return "", errors.New("should not run")
	}
	defer func() { bridgeTokenFn, remintHTTPFn = origBridge, origRemint }()

	tok, err := mintHumanToken(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "bridge-tok" {
		t.Fatalf("tok = %q", tok)
	}
	if remintCalled {
		t.Fatal("remint ran after a successful bridge mint")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "bridge-tok\n" {
		t.Fatalf("out file = %q", b)
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
		t.Fatalf("out file mode = %o", fi.Mode().Perm())
	}
}

func TestMintHumanTokenFallsBackToRemint(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tok")
	var remintOut string
	origBridge, origRemint := bridgeTokenFn, remintHTTPFn
	bridgeTokenFn = func(context.Context) (string, error) { return "", errBridgeUnavailable }
	remintHTTPFn = func(_ context.Context, o string) (string, error) {
		remintOut = o
		return "remint-tok", nil
	}
	defer func() { bridgeTokenFn, remintHTTPFn = origBridge, origRemint }()

	tok, err := mintHumanToken(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "remint-tok" {
		t.Fatalf("tok = %q", tok)
	}
	if remintOut != out {
		t.Fatalf("remint outFile = %q, want %q", remintOut, out)
	}
}

func TestMintHumanTokenSurfacesRemintError(t *testing.T) {
	origBridge, origRemint := bridgeTokenFn, remintHTTPFn
	bridgeTokenFn = func(context.Context) (string, error) { return "", errBridgeUnavailable }
	want := errors.New("no credentials")
	remintHTTPFn = func(context.Context, string) (string, error) { return "", want }
	defer func() { bridgeTokenFn, remintHTTPFn = origBridge, origRemint }()

	if _, err := mintHumanToken(context.Background(), ""); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

// On a Mac without the bridge (or VEIL_FILL_TOUCHID=0 as the test suite
// forces) the gate fails fast instead of hanging on a socket.
func TestBridgeHumanTokenGateOff(t *testing.T) {
	t.Setenv("VEIL_FILL_TOUCHID", "0")
	if _, err := bridgeHumanToken(context.Background()); !errors.Is(err, errBridgeUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

// A home with no native-host binary means the re-exec leg reports
// errNoNativeHost so the caller falls through to the direct socket ask.
func TestReexecBridgeTokenNoHost(t *testing.T) {
	home := t.TempDir()
	if _, err := reexecBridgeToken(context.Background(), home); !errors.Is(err, errNoNativeHost) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnlockPrintWritesTokenToStdout(t *testing.T) {
	orig := bridgeTokenFn
	bridgeTokenFn = func(context.Context) (string, error) { return "child-tok", nil }
	defer func() { bridgeTokenFn = orig }()

	cmd := unlockCmd(new(string))
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--print"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "child-tok" {
		t.Fatalf("stdout = %q", buf.String())
	}
}

func TestUnlockCmdRequiresOutFile(t *testing.T) {
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", "")
	t.Setenv("VEIL_HOME", t.TempDir())
	// No fill.json → no token file to default to → the flag error fires
	// before any socket dial.
	cmd := unlockCmd(new(string))
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected --out-file error")
	}
}
