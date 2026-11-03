//go:build linux

package confirm

import "fmt"

func confirmAvailable() bool { return promptAvailable() }

// Local is Linux's device-owner confirmation surface.
var Local = Prompt

func TouchID(reason string) error {
	return fmt.Errorf("fill: touch id is macOS")
}

func warmAppKit() {}

func callerBundle() string { return "" }

func callerLabel() string { return "" }
