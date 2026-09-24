// Package confirm is Touch ID at fill time. The native host prompts
// before a secret goes to the extension. Host.Confirm nil fails closed.
package confirm

import (
	"os"
	"strings"
	"testing"
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
// "p:<exe>" for an unbundled parent (agent CLIs, `go run`, CI).
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
