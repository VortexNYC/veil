// filldist writes an extension payload dir for one browser: the embedded
// apps/fill files with that browser's manifest. Used by `veil fill install`
// (chrome/firefox dirs) and `make fill-safari` (the containing app's
// extension Resources input).
package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"

	fillext "github.com/VortexNYC/veil/apps/fill"
	"github.com/VortexNYC/veil/internal/fill"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: filldist chrome|firefox|safari <dir>")
		os.Exit(2)
	}
	var install func(fs.FS, string) error
	switch os.Args[1] {
	case "chrome":
		install = fill.InstallExtension
	case "firefox":
		install = fill.InstallExtensionFirefox
	case "safari":
		install = fill.InstallExtensionSafari
	default:
		log.Fatalf("filldist: unknown kind %q", os.Args[1])
	}
	if err := install(fillext.Files, os.Args[2]); err != nil {
		log.Fatal(err)
	}
}
