package agenticfill

import (
	"os"
	"path/filepath"
)

// WorkerDir returns a directory containing worker-fill.mjs and, when present,
// node_modules/playwright-core (repo examples tree or staged deps).
func WorkerDir() (script string, dir string, err error) {
	if p := os.Getenv("VEIL_AGENTIC_WORKER"); p != "" {
		return p, filepath.Dir(p), nil
	}
	if root := os.Getenv("VEIL_ROOT"); root != "" {
		d := filepath.Join(root, "examples/agentic-autofill")
		s := filepath.Join(d, "worker-fill.mjs")
		if st, e := os.Stat(s); e == nil && !st.IsDir() {
			return s, d, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	for d := cwd; d != filepath.Dir(d); d = filepath.Dir(d) {
		ex := filepath.Join(d, "examples/agentic-autofill")
		s := filepath.Join(ex, "worker-fill.mjs")
		if st, e := os.Stat(s); e == nil && !st.IsDir() {
			return s, ex, nil
		}
	}
	// Bundled worker next to extracted embed (see cli.agentFillWorkerMaterial).
	return "", "", os.ErrNotExist
}

func PlaywrightInstalled(dir string) bool {
	p := filepath.Join(dir, "node_modules", "playwright-core", "package.json")
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
