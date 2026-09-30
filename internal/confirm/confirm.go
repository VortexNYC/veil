// Package confirm is Touch ID at fill time. The native host prompts
// before a secret goes to the extension. Host.Confirm nil fails closed.
package confirm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Enabled is Mac + VEIL_FILL_TOUCHID not "0". Linux/Windows wait for 35–36.
func Enabled() bool {
	if os.Getenv("VEIL_FILL_TOUCHID") == "0" {
		return false
	}
	return touchIDAvailable
}

// Action is the allow-line verb. "Veil wants to fill a card" → "fill a card".
func Action(reason string) string {
	s := strings.TrimSpace(reason)
	s = strings.TrimPrefix(s, "Veil wants to ")
	if s == "" {
		return "use Veil"
	}
	return s
}

func accountLabel() string {
	e := strings.TrimSpace(os.Getenv("VEIL_LOGIN_EMAIL"))
	if e != "" {
		return e
	}
	return "Veil"
}

func interactive() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// callerKey is the stable consent key: "b:<bundleID>" for a real app,
// "p:<exe path>" for an unbundled parent (agent CLIs, `go run`, CI) —
// the full path, so a same-named binary elsewhere does not inherit it.
func callerKey() string {
	if b := callerBundle(); b != "" {
		return "b:" + b
	}
	if p := callerLabel(); p != "" {
		return "p:" + p
	}
	return ""
}

// commandReason names what the human is authorizing — "Allow {app} to
// run `veil grant add --agent claude`" — instead of a context-free
// "get CLI access". Secrets never travel argv (files only), so the
// full verb line is safe to show; it truncates at 72 chars.
func commandReason() string {
	verb := strings.Join(os.Args[1:], " ")
	if strings.TrimSpace(verb) == "" {
		return "run veil commands"
	}
	if len(verb) > 72 {
		verb = verb[:72] + "…"
	}
	return "run `veil " + verb + "`"
}

// TouchIDHelper runs the access sheet + eval in a short-lived child
// (`veil fill --confirm-prompt`). The bridge daemon must never initialize
// AppKit in-process: once it does, launchd/RBS manage it as an app and
// the OS kills it for efficiency mid-session — the socket then refuses
// connections. A transient helper carries the app lifecycle instead and
// exits cleanly per confirm.
//
// helperDeadline bounds the child: the in-process eval times out at 60s and
// the browser request at 90s, so a wedged child must die between the two —
// otherwise its ultra-late reply desyncs request/reply order on the socket.
const helperDeadline = 75 * time.Second

func TouchIDHelper(reason string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helperDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "fill", "--confirm-prompt")
	cmd.Env = append(os.Environ(), "VEIL_CONFIRM_REASON="+reason)
	// The child's veil-confirm logs land in the daemon's fill-bridge.log.
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fill: touch id declined")
	}
	return nil
}

// CLIAccess is the consent sheet: Allow {app} to run `veil <verb>`.
// Fill uses TouchID. Agents, tests, and VEIL_FILL_TOUCHID=0 skip.
func CLIAccess() error {
	if !Enabled() || !interactive() || testing.Testing() {
		return nil
	}
	key := callerKey()
	if key != "" && cliAllowed(key) {
		return nil
	}
	if err := TouchID(commandReason()); err != nil {
		return err
	}
	if key != "" {
		cliRemember(key)
	}
	return nil
}
