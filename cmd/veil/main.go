package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/VortexNYC/veil/internal/cli"
	"github.com/VortexNYC/veil/internal/fill"
)

var version = "dev"

func resolvedVersion() string {
	if version != "" && version != "dev" {
		return strings.TrimPrefix(version, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		v := strings.TrimSpace(info.Main.Version)
		if v != "" && v != "(devel)" && !strings.HasPrefix(v, "v0.0.0-") {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "0.1.0"
}

func init() {
	// AppKit sheets (Touch ID) must run on the OS main thread. Chrome's
	// native host Serve is this goroutine; without the lock it migrates
	// and NSWindow aborts.
	runtime.LockOSThread()
}

func main() {
	os.Args = fill.NativeHostArgs(os.Args)
	if err := cli.New(resolvedVersion()).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
