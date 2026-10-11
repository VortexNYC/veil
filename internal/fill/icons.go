package fill

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Per-site icons, the way 1Password's rich icons make rows recognizable.
// The host resolves a favicon once per canonical host and caches it under
// the vault's icons/ dir; match/list replies carry it as a data URL so the
// Swift panel and the browser extension both render it without file or
// scheme special-casing. Icon bytes are public site art — never secrets.

// iconsDir is vault-home/icons when the host knows its dir, else
// ~/.veil/icons — the same place the installed app keeps its extension.
func (h *Host) iconsDir() string {
	if h.Dir != "" {
		return filepath.Join(h.Dir, "icons")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".veil", "icons")
	}
	return ""
}

var iconHostRe = regexp.MustCompile(`^[a-z0-9.-]+$`)

// iconFile sanitizes the canonical host into a cache filename. Anything
// outside the host alphabet gets no icon — no path traversal through
// crafted URIs.
func iconFile(dir, host string) string {
	if !iconHostRe.MatchString(host) {
		return ""
	}
	return filepath.Join(dir, host+".icon")
}

// iconPath returns a data URL for a cached icon, "" if the file is absent
// or a negative marker. A second return reports whether a fetch should run.
func iconCached(dir, host string) (string, bool) {
	if dir == "" {
		return "", false
	}
	if p := iconFile(dir, host); p != "" {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return iconDataURL(b), false
		}
		if _, err := os.Stat(p + ".none"); err == nil {
			return "", false // known no-icon host — don't refetch every call
		}
		return "", true
	}
	return "", false
}

func iconDataURL(b []byte) string {
	mime := "image/x-icon"
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// fetchIcon pulls a favicon for host into the cache. DuckDuckGo's icon
// service is the primary source — it follows the same resolve chain
// password managers use and returns real brand art, not just /favicon.ico.
// Failures leave a .none marker so cold sites cost one fetch ever.
func (h *Host) fetchIcon(host string) {
	dir := h.iconsDir()
	path := iconFile(dir, host)
	if path == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	cli := &http.Client{Timeout: 4 * time.Second}
	for _, u := range []string{
		"https://icons.duckduckgo.com/ip3/" + host + ".ico",
		"https://" + host + "/favicon.ico",
	} {
		resp, err := cli.Get(u)
		if err != nil {
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 200<<10))
		resp.Body.Close()
		if rerr != nil || resp.StatusCode != 200 || len(body) < 64 {
			continue
		}
		// DDG answers 200 with a tiny blank for unknown hosts — the 64-byte
		// floor filters its placeholder pixels out.
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, body, 0o600); err == nil {
			os.Rename(tmp, path)
		}
		return
	}
	os.WriteFile(path+".none", nil, 0o600)
}

// attachIcons resolves each entry's icon, fetching missing ones in
// parallel. First call against cold hosts waits briefly for the fetches;
// everything after is a cache hit.
func (h *Host) attachIcons(entries []jsonMatchEntry) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	dir := h.iconsDir()
	for i := range entries {
		host := iconHost(entries[i].SavedFor)
		if host == "" {
			continue
		}
		url, fetch := iconCached(dir, host)
		if url != "" {
			entries[i].Icon = url
			continue
		}
		if !fetch {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, host string) {
			defer wg.Done()
			defer func() { <-sem }()
			h.fetchIcon(host)
			if url, _ := iconCached(dir, host); url != "" {
				entries[i].Icon = url
			}
		}(i, host)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
	}
}

// iconHost extracts the fetchable domain from a SavedFor canonical host.
// app:// bindings and scheme-looking hosts aren't real domains.
func iconHost(saved string) string {
	if strings.Contains(saved, "://") {
		u := saved
		if i := strings.Index(u, "://"); i >= 0 {
			u = u[i+3:]
		}
		if i := strings.Index(u, "/"); i >= 0 {
			u = u[:i]
		}
		if strings.HasPrefix(saved, "app://") {
			return "" // bundle IDs aren't domains
		}
		saved = u
	}
	if h, _, err := net.SplitHostPort(saved); err == nil {
		saved = h
	}
	return strings.ToLower(saved)
}
