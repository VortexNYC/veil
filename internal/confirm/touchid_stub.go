//go:build !darwin && !linux

package confirm

import "fmt"

func confirmAvailable() bool { return false }

func TouchID(reason string) error {
	return fmt.Errorf("fill: touch id is macOS")
}

func unsupportedConfirm(reason string) error {
	return fmt.Errorf("fill: no local confirmation surface on this platform")
}

var Local = unsupportedConfirm

func warmAppKit() {}

func callerBundle() string { return "" }

func callerLabel() string { return "" }
