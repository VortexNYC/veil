package fill

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Peer is the process on the other end of a bridge socket connection.
// The socket is a Use surface — a local agent can connect to it like any
// other client, so reuse of a Touch ID evaluation is bound to the peer
// that prompted for it, and metadata actions only serve attested peers.
type Peer struct {
	PID      int32
	Path     string
	Attested bool
}

// TrustedPeer is the stdio/native-messaging context: the host process was
// spawned by the client itself, so the launch is the credential.
var TrustedPeer = &Peer{Attested: true}

// peerFromConn identifies the unix-socket peer: the effective PID via
// LOCAL_PEEREPID, then the executable path and whether it's our own
// signed bundle. Unidentified peers are not attested.
func peerFromConn(c net.Conn) *Peer {
	p := &Peer{}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return p
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return p
	}
	var pid int32
	_ = raw.Control(func(fd uintptr) {
		// SOL_LOCAL=0, LOCAL_PEEREPID=0x2 on Darwin.
		v, err := unix.GetsockoptInt(int(fd), 0, 2)
		if err == nil {
			pid = int32(v)
		}
	})
	p.PID = pid
	if pid <= 0 {
		return p
	}
	p.Path = peerPath(pid)
	p.Attested = attested(p.Path)
	return p
}

// peerPath resolves a PID to its executable path. `ps` is a fork+exec —
// sockets live long enough that a few ms on accept is invisible.
func peerPath(pid int32) string {
	out, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "comm=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type attestedEntry struct {
	mtimeNs int64
	size    int64
	ok      bool
}

var (
	attestedMu    sync.Mutex
	attestedCache = map[string]attestedEntry{}
)

// attested reports whether the peer executable is a code-signed Veil
// process. `codesign -d` prints the CFBundleIdentifier — every Veil
// surface (app, extension appex, AutoFill appex, bundled binaries) signs
// under the nyc.veil.* tree; anything else connecting to the socket is
// not us. Cached by (path, mtime, size) so repeated socket connects do
// not fork `codesign` on every frame.
func attested(path string) bool {
	if !strings.Contains(path, "Veil.app/Contents/") &&
		!strings.Contains(path, "/veil-bin") &&
		!strings.Contains(path, "/native-host") {
		return false
	}
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	mtimeNs := st.ModTime().UnixNano()
	size := st.Size()
	attestedMu.Lock()
	if cached, found := attestedCache[path]; found && cached.mtimeNs == mtimeNs && cached.size == size {
		attestedMu.Unlock()
		return cached.ok
	}
	attestedMu.Unlock()

	out, err := exec.Command("codesign", "-dvv", path).CombinedOutput()
	if err != nil {
		return false
	}
	ok := false
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "Identifier=") {
			ok = strings.HasPrefix(strings.TrimPrefix(line, "Identifier="), "nyc.veil")
			break
		}
	}
	attestedMu.Lock()
	attestedCache[path] = attestedEntry{mtimeNs: mtimeNs, size: size, ok: ok}
	attestedMu.Unlock()
	return ok
}

// peerKey is the reuse-window key — the human's Touch ID covers this
// exact process, not whatever else connects to the socket afterward.
func (p *Peer) key() string {
	if p == nil {
		return ""
	}
	if p.Attested && p.PID == 0 {
		return "stdio"
	}
	return fmt.Sprintf("%d:%s", p.PID, p.Path)
}
