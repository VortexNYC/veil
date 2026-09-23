package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/VortexNYC/veil/internal/cli"
	"github.com/VortexNYC/veil/internal/fill"
)

const version = "0.0.1"

func init() {
	// AppKit sheets (Touch ID) must run on the OS main thread. Chrome's
	// native host Serve is this goroutine; without the lock it migrates
	// and NSWindow aborts.
	runtime.LockOSThread()
}

func main() {
	os.Args = fill.NativeHostArgs(os.Args)
	if err := cli.New(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
