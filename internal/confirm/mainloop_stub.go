//go:build !darwin

package confirm

import "context"

// PumpMain is a no-op off darwin — AppKit does not exist there.
func PumpMain(ctx context.Context) {
	<-ctx.Done()
}
