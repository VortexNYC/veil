//go:build liveorigin

package livetest

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var liveFiledRequestIDs []string

func TestMain(m *testing.M) {
	code := m.Run()
	cleanupLiveRequests()
	os.Exit(code)
}

func cleanupLiveRequests() {
	humanPath := strings.TrimSpace(os.Getenv("VEIL_HUMAN_TOKEN_FILE"))
	if humanPath == "" {
		return
	}
	raw, err := os.ReadFile(humanPath)
	if err != nil {
		return
	}
	humanJWT := strings.TrimSpace(string(raw))
	if humanJWT == "" {
		return
	}
	origin := strings.TrimRight(envOr("VEIL_ORIGIN", "https://veil.nyc"), "/")
	client := &http.Client{Timeout: 15 * time.Second}
	for _, id := range liveFiledRequestIDs {
		if id == "" {
			continue
		}
		req, _ := http.NewRequest(http.MethodPost, origin+"/v1/requests/"+id+"/deny", nil)
		req.Header.Set("Authorization", "Bearer "+humanJWT)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		res, err := client.Do(req)
		if err != nil {
			continue
		}
		res.Body.Close()
	}
}

func trackLiveRequest(id string) {
	if strings.TrimSpace(id) != "" {
		liveFiledRequestIDs = append(liveFiledRequestIDs, id)
	}
}

// Dogfood agentic autofill HTTP + MCP on production origin. Files real
// approval rows — cancel/deny stale opens in the vault if tests abort.
func TestAgenticAutofillLive(t *testing.T) {
	origin := strings.TrimRight(envOr("VEIL_ORIGIN", "https://veil.nyc"), "/")
	client := &http.Client{Timeout: 25 * time.Second}

	cursorJWT := mintAgentJWT(t, "cursor")
	grokJWT := mintAgentJWT(t, "grok")

	t.Run("agent_fill_http_cursor", func(t *testing.T) {
		item, url := loginItemForAgent(t, client, origin, cursorJWT)
		if item == "" {
			t.Skip("no granted website login item for cursor")
		}
		code, body := postJSON(t, client, origin+"/v1/agent/fill/request", cursorJWT, map[string]any{
			"item_id": item,
			"url":     url,
		})
		assertNoLeak(t, body)
		if code == http.StatusNotFound {
			t.Skip("POST /v1/agent/fill/request not on origin yet — deploy agentic autofill routes")
		}
		if code != http.StatusOK {
			t.Fatalf("agent fill file http=%d body=%q", code, clip(body))
		}
		var filed struct {
			RequestID   string `json:"request_id"`
			Status      string `json:"status"`
			ItemID      string `json:"item_id"`
			ApprovalURL string `json:"approval_url"`
		}
		if err := json.Unmarshal(body, &filed); err != nil || filed.RequestID == "" {
			t.Fatalf("bad filed json")
		}
		if filed.Status != "open" || filed.ItemID != item || filed.ApprovalURL == "" {
			t.Fatalf("filed shape status=%s item=%s", filed.Status, filed.ItemID)
		}
		trackLiveRequest(filed.RequestID)

		code, body = get(t, client, origin+"/v1/agent/fill/request/"+filed.RequestID, cursorJWT)
		assertNoLeak(t, body)
		if code != http.StatusOK || !strings.Contains(string(body), `"open"`) {
			t.Fatalf("agent poll http=%d", code)
		}

		code, body = get(t, client, origin+"/v1/agent/fill/request/"+filed.RequestID, grokJWT)
		if code != http.StatusNotFound {
			t.Fatalf("other agent must not poll cursor ask http=%d", code)
		}
	})

	t.Run("mcp_request_fill_no_secret", func(t *testing.T) {
		item, url := loginItemForAgent(t, client, origin, grokJWT)
		if item == "" {
			t.Skip("no granted website login item for grok")
		}
		initBody := mcpCall(t, client, origin, grokJWT, map[string]any{
			"jsonrpc": "2.0", "id": 10, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "livetest-agentic", "version": "0"},
			},
		})
		if initBody["error"] != nil {
			t.Fatalf("mcp init")
		}
		fill := mcpCall(t, client, origin, grokJWT, map[string]any{
			"jsonrpc": "2.0", "id": 11, "method": "tools/call",
			"params": map[string]any{
				"name": "request_fill",
				"arguments": map[string]any{
					"item": item,
					"url":  url,
				},
			},
		})
		sc := structured(t, fill)
		rid, _ := sc["request_id"].(string)
		if rid == "" {
			t.Fatalf("request_fill missing request_id: %v", sc)
		}
		trackLiveRequest(rid)
		raw, _ := json.Marshal(fill)
		assertNoLeak(t, raw)
		if strings.Contains(strings.ToLower(string(raw)), "password") {
			t.Fatal("request_fill must not mention password")
		}
		status := mcpCall(t, client, origin, grokJWT, map[string]any{
			"jsonrpc": "2.0", "id": 12, "method": "tools/call",
			"params": map[string]any{
				"name":      "approval_status",
				"arguments": map[string]any{"request_id": rid},
			},
		})
		st := structured(t, status)
		if st["status"] != "open" {
			t.Fatalf("approval_status want open got %v", st["status"])
		}
		assertNoLeak(t, mustJSON(t, status))
	})

	t.Run("agent_fill_denies_bad_url", func(t *testing.T) {
		code, body := postJSON(t, client, origin+"/v1/agent/fill/request", cursorJWT, map[string]any{
			"item_id": "github",
			"url":     "https://evil.example/login",
		})
		assertNoLeak(t, body)
		if code == http.StatusNotFound {
			t.Skip("agent fill route not deployed")
		}
		if code != http.StatusBadRequest && code != http.StatusNotFound {
			t.Fatalf("bad url want 400 got %d", code)
		}
	})
}

func mintAgentJWT(t *testing.T, agent string) string {
	t.Helper()
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME unset")
	}
	path := os.Getenv("VEIL_LIVE_JWT_FILE_" + strings.ToUpper(agent))
	if path == "" && agent == "cursor" {
		path = os.Getenv("VEIL_LIVE_JWT_FILE")
	}
	if path == "" {
		path = home + "/.config/vortex/veil/" + agent + ".jwt"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("agent %s: read jwt: %v", agent, err)
	}
	tok := strings.TrimSpace(string(raw))
	if strings.Count(tok, ".") != 2 {
		secret := home + "/.config/vortex/veil/" + agent + ".hydra"
		t.Fatalf("agent %s jwt stale — remint: VEIL_ORIGIN=https://veil.nyc veil agent token %s --secret-file %s --out-file %s", agent, agent, secret, path)
	}
	return tok
}

func loginItemForAgent(t *testing.T, client *http.Client, origin, jwt string) (itemID, pageURL string) {
	t.Helper()
	match := mcpCall(t, client, origin, jwt, map[string]any{
		"jsonrpc": "2.0", "id": 20, "method": "tools/call",
		"params": map[string]any{
			"name":      "match_items",
			"arguments": map[string]any{"url": "https://dash.cloudflare.com/login"},
		},
	})
	sc := structured(t, match)
	items, _ := sc["items"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		id, _ := m["id"].(string)
		if id != "" {
			return id, "https://dash.cloudflare.com/login"
		}
	}
	match = mcpCall(t, client, origin, jwt, map[string]any{
		"jsonrpc": "2.0", "id": 21, "method": "tools/call",
		"params": map[string]any{
			"name":      "match_items",
			"arguments": map[string]any{"url": "https://github.com/login"},
		},
	})
	sc = structured(t, match)
	items, _ = sc["items"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		id, _ := m["id"].(string)
		if id != "" {
			return id, "https://github.com/login"
		}
	}
	return "", ""
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
