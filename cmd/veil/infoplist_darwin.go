//go:build darwin

package main

/*
#cgo LDFLAGS: -Wl,-sectcreate,__TEXT,__info_plist,${SRCDIR}/Info.plist

// Embed Info.plist into __TEXT,__info_plist — the only way a raw Mach-O
// binary (no .app bundle) gets a bundle identity. LocalAuthentication,
// keychain prompts, and the WindowServer read it: the user sees "Veil",
// never "native-host", and code-signing "nyc.veil.host" is stable across
// rebuilds so keychain ACLs keep trusting the binary.
*/
import "C"
