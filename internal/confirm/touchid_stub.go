//go:build !darwin

package confirm

import "fmt"

const touchIDAvailable = false

func TouchID(reason string) error {
	return fmt.Errorf("fill: touch id is macOS")
}

func warmAppKit() {}

func callerBundle() string { return "" }

func callerLabel() string { return "" }
