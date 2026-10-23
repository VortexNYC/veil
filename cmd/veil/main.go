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

var version = "0.1.0"

func resolvedVersion() string {
	if version != "" && version != "0.1.0" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		v := strings.TrimSpace(info.Main.Version)
		if v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return version
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
