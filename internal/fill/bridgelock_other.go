//go:build !unix

package fill

import "io"

// BridgeLock is a no-op off unix — flock does not exist there and the
// bridge's only production spawners are macOS launchd + the Veil app.
func BridgeLock(vaultHome string) (io.Closer, error) {
	return nopCloser{}, nil
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
