//go:build linux

package confirm

import "fmt"

func confirmAvailable() bool { return trayPresent() || promptAvailable() }

// Local is Linux's device-owner confirmation surface: a running `veil tray`
// helper answers first; a standalone desktop prompt covers hosts with no
// daemon. Both render the same zenity/kdialog dialog — the tray is persistence
// (and the future chooser/icon seam), not a different gate.
var Local = dispatch

func dispatch(reason string) error {
	if answered, err := DialTray(reason); answered {
		return err
	}
	return Prompt(reason)
}

func TouchID(reason string) error {
	return fmt.Errorf("fill: touch id is macOS")
}

func warmAppKit() {}

func callerBundle() string { return "" }

func callerLabel() string { return "" }
