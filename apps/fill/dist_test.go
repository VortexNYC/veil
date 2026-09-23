package fill

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded payload must be exactly the runtime files in this directory —
// a new .js/.html added here but not embedded silently ships an extension
// missing a piece. Tests, fixtures, and package.json are not runtime.
func TestFilesMatchDirectory(t *testing.T) {
	want := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "package.json" || strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, ".test.cjs") || strings.Contains(name, "fixture") {
			continue
		}
		if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".html") || name == "manifest.json" {
			want[name] = true
		}
	}
	got := map[string]bool{}
	err = fs.WalkDir(Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		got[filepath.Base(path)] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("%s is runtime but not embedded", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Fatalf("%s is embedded but not a runtime file", name)
		}
	}
}
