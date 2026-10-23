package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/mcpserver"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

// originMCPServer is the laptop stdio MCP adapter (for Grok CLI, Cursor,
// Claude Code, and Devin CLI). Exposes the exact same 5-tool surface as
// origin HTTP MCP (list_items, match_items, check, fetch, approval_status).
// Bearer is re-read from the token file on every call. Secrets never return.
func originMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "veil", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_items",
		Description: "List items this agent may Use. Names and URIs only. Never secrets.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpserver.ListOut, error) {
		tok, err := originTokenLive(ctx, "")
		if err != nil {
			return nil, mcpserver.ListOut{}, err
		}
		raw, err := originDo(ctx, http.MethodGet, "/v1/items", tok, nil)
		if err != nil {
			return nil, mcpserver.ListOut{}, err
		}
		var out publicapi.ItemsResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, mcpserver.ListOut{}, err
		}
		if out.Items == nil {
			out.Items = []protocol.Item{}
		}
		return nil, mcpserver.ListOut{Items: out.Items}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "match_items",
		Description: "List items this agent may Use whose URIs match a URL — which item to name in fetch. Names and URIs only, never secrets.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpserver.MatchIn) (*mcp.CallToolResult, mcpserver.ListOut, error) {
		tok, err := originTokenLive(ctx, "")
		if err != nil {
			return nil, mcpserver.ListOut{}, err
		}
		raw, err := originDo(ctx, http.MethodGet, "/v1/items", tok, nil)
		if err != nil {
			return nil, mcpserver.ListOut{}, err
		}
		var listed publicapi.ItemsResponse
		if err := json.Unmarshal(raw, &listed); err != nil {
			return nil, mcpserver.ListOut{}, err
		}
		out := make([]protocol.Item, 0, len(listed.Items))
		for _, item := range listed.Items {
			if !item.Archived && grant.HostAllowed(item, in.URL) {
				out = append(out, item)
			}
		}
		return nil, mcpserver.ListOut{Items: out}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "fetch",
		Description: "Call a URL as this agent. The broker injects the credential. You never receive the secret. Level-1 items return decision=need_approval until a human runs `veil approve`.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpserver.FetchIn) (*mcp.CallToolResult, mcpserver.FetchOut, error) {
		tok, err := originTokenLive(ctx, "")
		if err != nil {
			return nil, mcpserver.FetchOut{}, err
		}
		payload, err := json.Marshal(publicapi.UseRequest{
			Item:    in.Item,
			URL:     in.URL,
			Method:  in.Method,
			Headers: in.Headers,
			Body:    in.Body,
			BodyB64: in.BodyB64,
		})
		if err != nil {
			return nil, mcpserver.FetchOut{}, err
		}
		raw, err := originDo(ctx, http.MethodPost, "/v1/use", tok, payload)
		if err != nil {
			return nil, mcpserver.FetchOut{}, err
		}
		var got publicapi.UseResponse
		if err := json.Unmarshal(raw, &got); err != nil {
			return nil, mcpserver.FetchOut{}, err
		}
		return nil, mcpserver.FetchOut{
			Decision:         string(got.Decision),
			Reason:           got.Reason,
			Status:           got.Status,
			Headers:          got.Headers,
			Body:             got.Body,
			BodyB64:          got.BodyB64,
			RequestID:        got.RequestID,
			RequestExpiresAt: got.RequestExpiresAt,
		}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check",
		Description: "Dry-run a fetch: evaluates the grant decision without calling the URL, claiming a use, or touching the secret. On need_approval it files the ask and returns request_id — also how an agent explicitly requests approval.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpserver.FetchIn) (*mcp.CallToolResult, mcpserver.FetchOut, error) {
		var out mcpserver.FetchOut
		if err := callOriginMCPTool(ctx, "check", in, &out); err != nil {
			return nil, mcpserver.FetchOut{}, err
		}
		return nil, out, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "approval_status",
		Description: "Poll one of this agent's approval requests after fetch returned decision=need_approval — open, approved, denied, or expired. Retry fetch once approved.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpserver.ApprovalStatusIn) (*mcp.CallToolResult, mcpserver.ApprovalStatusOut, error) {
		var out mcpserver.ApprovalStatusOut
		if err := callOriginMCPTool(ctx, "approval_status", in, &out); err != nil {
			return nil, mcpserver.ApprovalStatusOut{}, err
		}
		return nil, out, nil
	})
	return server
}

func callOriginMCPTool(ctx context.Context, toolName string, args any, dst any) error {
	tok, err := originTokenLive(ctx, "")
	if err != nil {
		return err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "veil-stdio", Version: "0.1.0"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint: originBase() + "/mcp",
		HTTPClient: &http.Client{
			Transport: bearerRoundTripper{tok: tok, base: http.DefaultTransport},
		},
	}
	sess, err := client.Connect(ctx, tr, nil)
	if err != nil {
		return err
	}
	defer sess.Close()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return err
	}
	if res.IsError {
		return fmt.Errorf("mcp %s failed", toolName)
	}
	if res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, dst)
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc.Text != "" {
			return json.Unmarshal([]byte(tc.Text), dst)
		}
	}
	return fmt.Errorf("mcp %s: empty response", toolName)
}

type bearerRoundTripper struct {
	tok  string
	base http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.tok)
	return b.base.RoundTrip(clone)
}

func applyMCPStdioDefaults() {
	if originBase() == "" {
		return
	}
	if strings.TrimSpace(os.Getenv("VEIL_HYDRA_ISSUER")) == "" {
		_ = os.Setenv("VEIL_HYDRA_ISSUER", "https://id.veil.nyc")
	}
	if strings.TrimSpace(os.Getenv("VEIL_OIDC_TOKEN_FILE")) != "" ||
		strings.TrimSpace(os.Getenv("VEIL_OIDC_TOKEN")) != "" ||
		strings.TrimSpace(os.Getenv("VEIL_HYDRA_SECRET_FILE")) != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	credDir := filepath.Join(home, ".config", "vortex", "veil")
	agent := strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	if agent == "" {
		agent = inferCallerAgent()
	}
	if agent == "" {
		return
	}
	sec := filepath.Join(credDir, agent+".hydra")
	if _, err := os.Stat(sec); err == nil {
		_ = os.Setenv("VEIL_AGENT", agent)
		_ = os.Setenv("VEIL_HYDRA_SECRET_FILE", sec)
		_ = os.Setenv("VEIL_OIDC_TOKEN_FILE", filepath.Join(credDir, agent+".jwt"))
	}
}

func inferCallerAgent() string {
	pid := os.Getppid()
	for range 6 {
		if pid <= 1 {
			break
		}
		out, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "ppid=,comm=").Output()
		if err != nil {
			break
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) < 2 {
			break
		}
		comm := strings.ToLower(strings.Join(fields[1:], " "))
		switch {
		case strings.Contains(comm, "grok"):
			return "grok"
		case strings.Contains(comm, "cursor"):
			return "cursor"
		case strings.Contains(comm, "claude"):
			return "claude"
		case strings.Contains(comm, "devin"):
			return "devin"
		case strings.Contains(comm, "codex"):
			return "codex"
		}
		var next int
		if _, err := fmt.Sscan(fields[0], &next); err != nil || next == pid {
			break
		}
		pid = next
	}
	return ""
}

func runOriginMCPStdio(ctx context.Context) error {
	applyMCPStdioDefaults()
	if originBase() == "" {
		return fmt.Errorf("mcp stdio: VEIL_ORIGIN is required")
	}
	if _, err := originTokenLive(ctx, ""); err != nil {
		return err
	}
	return originMCPServer().Run(ctx, &mcp.StdioTransport{})
}
