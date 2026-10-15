package fill

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
)

// VOR-689 — agent-driven headless Chrome fills over CDP, no extension.
func TestFillCDPHeadless(t *testing.T) {
	if os.Getenv("VEIL_PROVE_CHROME") != "1" {
		t.Skip("VEIL_PROVE_CHROME=1")
	}
	chrome := chromeForTesting(t)
	if chrome == "" {
		t.Skip("Chrome for Testing not installed")
	}

	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<form><input id="u" name="username"><input id="p" type="password" name="password"><input type="submit"></form>`))
	}))
	t.Cleanup(page.Close)
	if _, err := a.PutItem(app.ItemOpts{
		Name:  "cdp-test",
		URI:   page.URL,
		Login: "ada@example.com",
		Token: []byte("cdp-secret-pass"),
	}); err != nil {
		t.Fatal(err)
	}

	profile := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cdpPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cmd := exec.Command(chrome,
		"--headless=new",
		"--user-data-dir="+profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--remote-debugging-port="+strconv.Itoa(cdpPort),
		"--remote-debugging-address=127.0.0.1",
		"--remote-allow-origins=*",
		page.URL,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	waitCDP(t, cdpPort)

	h := New(a)
	n, err := h.FillCDP(t.Context(), CDPPort(cdpPort), "", "")
	if err != nil {
		t.Fatalf("FillCDP: %v", err)
	}
	if n != 2 {
		t.Fatalf("filled %d fields, want 2", n)
	}
}
