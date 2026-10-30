//go:build liveorigin

// Origin proof. Hits veil.nyc. Not httptest. make prove-live.
package livetest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

var leakPat = regexp.MustCompile(`(?i)ghp_|gho_|ghu_|github_pat_|lin_api_|cfat_|cfut_|sk_live|sk_test|fc-[a-z0-9]{20,}|eyJ[A-Za-z0-9_-]{20,}\.`)

func TestOrigin(t *testing.T) {
	origin := strings.TrimRight(envOr("VEIL_ORIGIN", "https://veil.nyc"), "/")
	jwt := readJWT(t)
	client := &http.Client{Timeout: 25 * time.Second}

	t.Run("health", func(t *testing.T) {
		code, body := get(t, client, origin+"/health", "")
		assertNoLeak(t, body)
		if code != 200 || strings.TrimSpace(string(body)) != "ok" {
			t.Fatalf("health http=%d body=%q", code, clip(body))
		}
	})
	t.Run("ready", func(t *testing.T) {
		code, body := get(t, client, origin+"/ready", "")
		assertNoLeak(t, body)
		if code != 200 || strings.TrimSpace(string(body)) != "ok" {
			t.Fatalf("ready http=%d body=%q (issuer down)", code, clip(body))
		}
	})
	t.Run("items_no_bearer", func(t *testing.T) {
		code, body := get(t, client, origin+"/v1/items", "")
		assertNoLeak(t, body)
		if code != 401 {
			t.Fatalf("items no bearer http=%d", code)
		}
	})
	t.Run("mcp_no_bearer", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, origin+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
		assertNoLeak(t, body)
		if res.StatusCode != 401 {
			t.Fatalf("mcp no bearer http=%d", res.StatusCode)
		}
	})

	t.Run("items", func(t *testing.T) {
		code, body := get(t, client, origin+"/v1/items", jwt)
		assertNoLeak(t, body)
		if code != 200 {
			t.Fatalf("items http=%d", code)
		}
		names := itemNames(t, body)
		for _, want := range []string{"github", "linear", "firecrawl", "cloudflare"} {
			if !contains(names, want) {
				t.Fatalf("missing item %s in %v", want, names)
			}
		}
	})

	t.Run("use_github", func(t *testing.T) {
		out := useJSON(t, client, origin, jwt, map[string]any{
			"item": "github", "url": "https://api.github.com/user", "method": "GET",
		})
		if out.Decision != "allow" || out.Status != 200 || out.GitHubLogin == "" {
			t.Fatalf("github decision=%s status=%d login_set=%t", out.Decision, out.Status, out.GitHubLogin != "")
		}
	})
	t.Run("events", func(t *testing.T) {
		code, body := get(t, client, origin+"/v1/events", jwt)
		assertNoLeak(t, body)
		if code != 200 {
			t.Fatalf("events http=%d", code)
		}
		if !strings.Contains(string(body), `"item_id":"github"`) {
			t.Fatalf("events missing github: %s", clip(body))
		}
	})
	t.Run("use_linear", func(t *testing.T) {
		out := useJSON(t, client, origin, jwt, map[string]any{
			"item": "linear", "url": "https://api.linear.app/graphql", "method": "POST",
			"headers": map[string]string{"Content-Type": "application/json"},
			"body":    `{"query":"{ viewer { id } }"}`,
		})
		if out.Decision != "allow" || out.Status != 200 || !out.LinearViewer {
			t.Fatalf("linear decision=%s status=%d viewer=%t", out.Decision, out.Status, out.LinearViewer)
		}
	})
	t.Run("use_firecrawl", func(t *testing.T) {
		out := useJSON(t, client, origin, jwt, map[string]any{
			"item": "firecrawl", "url": "https://api.firecrawl.dev/v1/team/credit-usage", "method": "GET",
		})
		if out.Decision != "allow" || out.Status != 200 || !out.Firecrawl {
			t.Fatalf("firecrawl decision=%s status=%d credits=%t", out.Decision, out.Status, out.Firecrawl)
		}
	})
	t.Run("use_cloudflare", func(t *testing.T) {
		out := useJSON(t, client, origin, jwt, map[string]any{
			"item": "cloudflare", "url": "https://api.cloudflare.com/client/v4/user/tokens/verify", "method": "GET",
		})
		if out.Decision != "allow" || out.Status != 200 || !out.CFActive {
			t.Fatalf("cloudflare decision=%s status=%d active=%t", out.Decision, out.Status, out.CFActive)
		}
	})
	t.Run("deny_wrong_host", func(t *testing.T) {
		out := useJSON(t, client, origin, jwt, map[string]any{
			"item": "github", "url": "https://example.com/", "method": "GET",
		})
		if out.Decision != "deny" || out.Reason != "host_not_allowed" {
			t.Fatalf("deny decision=%s reason=%s", out.Decision, out.Reason)
		}
	})

	t.Run("mcp_list_and_fetch", func(t *testing.T) {
		initBody := mcpCall(t, client, origin, jwt, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "prove-live", "version": "0"},
			},
		})
		if initBody["error"] != nil {
			t.Fatalf("mcp initialize error")
		}
		list := mcpCall(t, client, origin, jwt, map[string]any{
			"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]any{"name": "list_items", "arguments": map[string]any{}},
		})
		names := mcpItemNames(t, list)
		if !contains(names, "github") {
			t.Fatalf("mcp list missing github in %v", names)
		}
		fetch := mcpCall(t, client, origin, jwt, map[string]any{
			"jsonrpc": "2.0", "id": 3, "method": "tools/call",
			"params": map[string]any{
				"name": "fetch",
				"arguments": map[string]any{
					"item": "github", "url": "https://api.github.com/user", "method": "GET",
				},
			},
		})
		sc := structured(t, fetch)
		if sc["decision"] != "allow" {
			t.Fatalf("mcp fetch decision=%v", sc["decision"])
		}
	})

	t.Run("device_link_unauth", func(t *testing.T) {
		pub := make([]byte, 32)
		for i := range pub {
			pub[i] = byte(i + 5)
		}
		code, body := postJSON(t, client, origin+"/v1/device-links", "", map[string]any{
			"label": "livetest", "pubkey": pub,
		})
		assertNoLeak(t, body)
		if code != 200 {
			t.Fatalf("device-link create http=%d body=%q", code, clip(body))
		}
		var link struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &link); err != nil || link.Token == "" {
			t.Fatalf("device-link create json")
		}
		code, body = postJSON(t, client, origin+"/v1/device-links/"+link.Token+"/claim", "", nil)
		assertNoLeak(t, body)
		if code != 400 {
			t.Fatalf("device-link claim before approve http=%d", code)
		}
	})
}

type useOut struct {
	Decision     string
	Status       int
	Reason       string
	GitHubLogin  string
	LinearViewer bool
	Firecrawl    bool
	CFActive     bool
}

func useJSON(t *testing.T, client *http.Client, origin, jwt string, payload map[string]any) useOut {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, origin+"/v1/use", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	assertNoLeak(t, body)
	if res.StatusCode != 200 {
		t.Fatalf("use http=%d", res.StatusCode)
	}
	var wrap struct {
		Decision string `json:"decision"`
		Status   int    `json:"status"`
		Reason   string `json:"reason"`
		Body     string `json:"body"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		t.Fatal("use json")
	}
	out := useOut{Decision: wrap.Decision, Status: wrap.Status, Reason: wrap.Reason}
	if wrap.Body == "" {
		return out
	}
	var inner map[string]any
	if err := json.Unmarshal([]byte(wrap.Body), &inner); err != nil {
		return out
	}
	if login, ok := inner["login"].(string); ok {
		out.GitHubLogin = login
	}
	if data, ok := inner["data"].(map[string]any); ok {
		if viewer, ok := data["viewer"].(map[string]any); ok {
			_, out.LinearViewer = viewer["id"]
		}
		if _, ok := data["remaining_credits"]; ok {
			out.Firecrawl = true
		}
	}
	if inner["success"] == true {
		if result, ok := inner["result"].(map[string]any); ok && result["status"] == "active" {
			out.CFActive = true
		}
		if data, ok := inner["data"].(map[string]any); ok {
			if _, ok := data["remaining_credits"]; ok {
				out.Firecrawl = true
			}
		}
	}
	return out
}

func mcpCall(t *testing.T, client *http.Client, origin, jwt string, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, origin+"/mcp", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	assertNoLeak(t, body)
	if res.StatusCode != 200 {
		t.Fatalf("mcp http=%d", res.StatusCode)
	}
	msg := sseJSON(body)
	if msg == nil {
		t.Fatal("mcp not json")
	}
	return msg
}

func sseJSON(raw []byte) map[string]any {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			var msg map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &msg) == nil {
				return msg
			}
		}
	}
	var msg map[string]any
	if json.Unmarshal(raw, &msg) == nil {
		return msg
	}
	return nil
}

func structured(t *testing.T, msg map[string]any) map[string]any {
	t.Helper()
	res, _ := msg["result"].(map[string]any)
	if res == nil {
		return map[string]any{}
	}
	if sc, ok := res["structuredContent"].(map[string]any); ok {
		return sc
	}
	return res
}

func mcpItemNames(t *testing.T, msg map[string]any) []string {
	t.Helper()
	sc := structured(t, msg)
	items, _ := sc["items"].([]any)
	var names []string
	for _, it := range items {
		m, _ := it.(map[string]any)
		if n, ok := m["name"].(string); ok {
			names = append(names, n)
		}
	}
	return names
}

func itemNames(t *testing.T, body []byte) []string {
	t.Helper()
	var wrap struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		t.Fatal("items json")
	}
	names := make([]string, 0, len(wrap.Items))
	for _, it := range wrap.Items {
		names = append(names, it.Name)
	}
	if len(names) == 0 {
		var list []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &list); err == nil {
			for _, it := range list {
				names = append(names, it.Name)
			}
		}
	}
	return names
}

func get(t *testing.T, client *http.Client, url, jwt string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

func postJSON(t *testing.T, client *http.Client, url, jwt string, payload any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

func readJWT(t *testing.T) string {
	t.Helper()
	path := os.Getenv("VEIL_LIVE_JWT_FILE")
	if path == "" {
		t.Fatal("VEIL_LIVE_JWT_FILE is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("jwt file")
	}
	tok := strings.TrimSpace(string(raw))
	if strings.Count(tok, ".") != 2 {
		t.Fatal("jwt shape")
	}
	return tok
}

func assertNoLeak(t *testing.T, body []byte) {
	t.Helper()
	if leakPat.Match(body) {
		t.Fatal("response matched a secret pattern")
	}
}

func clip(b []byte) string {
	s := string(b)
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func envOr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
