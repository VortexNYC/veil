//go:build darwin

package fill

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
)

// HostSigningIdentity is the stable code-signing identifier installed
// binaries carry. Keychain ACLs, LocalAuthentication, and TCC all key off
// the signature — an ad-hoc signature changes every build and is why a
// rebuilt host kept demanding the login keychain password. Any real
// identity pins the requirement to the cert, not the cdhash.
const HostSigningIdentity = "nyc.veil.host"

var identityPattern = regexp.MustCompile(`"([^"]+)"`)

// codesignHost signs the installed host with the best available identity —
// Developer ID first (the same team identity Veil.app ships under), then
// Apple Development. Unsigned stays unsigned rather than erroring: on a
// machine with no Apple identity the binary still runs, it just keeps the
// ad-hoc prompt behavior.
func codesignHost(path string) error {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return nil
	}
	var identity string
	for _, line := range strings.Split(string(out), "\n") {
		m := identityPattern.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		name := m[1]
		switch {
		case strings.HasPrefix(name, "Developer ID Application:"):
			identity = name
		case identity == "" && strings.HasPrefix(name, "Apple Development:"):
			identity = name
		}
	}
	if identity == "" {
		return nil
	}
	cmd := exec.Command("codesign", "--force", "--sign", identity,
		"--identifier", HostSigningIdentity, "--timestamp=none", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil // signing is best-effort — an unsigned host still works
	}
	return nil
}
