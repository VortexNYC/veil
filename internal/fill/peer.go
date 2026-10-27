package fill

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

const veilTeamID = "VFWGNKKT4G"

func allowedPeerPath(path string) bool {
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, "/Applications/Veil.app/Contents/") {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if clean == filepath.Join(home, ".veil", "native-host") {
			return true
		}
	}
	return false
}

// attested reports whether the peer executable is a code-signed Veil
// process. `codesign -d` prints the CFBundleIdentifier and TeamIdentifier —
// every Veil surface (app, extension appex, AutoFill appex, bundled binaries)
// signs under the nyc.veil.* tree and TeamIdentifier VFWGNKKT4G; ad-hoc or
// third-party binaries are rejected. Cached by (path, mtime, size) so repeated
// socket connects do not fork `codesign` on every frame.
func attested(path string) bool {
	if !allowedPeerPath(path) {
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
	hasID := false
	hasTeam := false
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Identifier=") {
			hasID = strings.HasPrefix(strings.TrimPrefix(line, "Identifier="), "nyc.veil")
		} else if strings.HasPrefix(line, "TeamIdentifier=") {
			hasTeam = strings.TrimPrefix(line, "TeamIdentifier=") == veilTeamID
		}
	}
	ok := hasID && hasTeam
	attestedMu.Lock()
	attestedCache[path] = attestedEntry{mtimeNs: mtimeNs, size: size, ok: ok}
	attestedMu.Unlock()
	return ok
}

// SelfAttested reports whether this process would pass the bridge peer
// check — the signed, app-bundled binary the socket trusts. Callers use it
// to decide between serving a socket action directly or re-executing
// through the attested native-host binary.
func SelfAttested() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return attested(exe)
}

// DisplayName is the human-readable name of the connecting Veil surface —
// the Touch ID prompt says "in Safari" so the human sees which piece of
// the product is asking, not just which site.
func (p *Peer) DisplayName() string {
	if p == nil {
		return ""
	}
	if strings.Contains(p.Path, "Veil Extension.appex") {
		return "Safari"
	}
	if strings.Contains(p.Path, "Veil AutoFill.appex") {
		return "AutoFill"
	}
	if strings.HasSuffix(p.Path, "/Contents/MacOS/Veil") {
		return "Veil"
	}
	return ""
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
