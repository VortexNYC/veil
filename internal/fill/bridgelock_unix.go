//go:build unix

package fill

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// BridgeLockFile names the advisory lock one fill bridge holds for its
// vault home. The socket path is not the lock — ServeBridge unlinks and
// rebinds it, so a second spawn steals it silently and the first idles
// forever. flock dies with the process: a crash frees the next spawn to
// win, no stale pidfile.
const BridgeLockFile = "fill.lock"

// BridgeLock takes the exclusive bridge lock for vaultHome. A nil
// closer means another live bridge holds it — the caller must exit
// quietly. The returned closer must stay open for the process lifetime;
// closing it (or dying) releases the lock.
func BridgeLock(vaultHome string) (io.Closer, error) {
	if err := os.MkdirAll(vaultHome, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(vaultHome, BridgeLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}
