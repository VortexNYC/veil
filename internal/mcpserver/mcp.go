// Package mcpserver is the agent surface: Streamable HTTP MCP.
//
// Official SDK: github.com/modelcontextprotocol/go-sdk/mcp
// Source: https://github.com/modelcontextprotocol/go-sdk (v1.7.0)
// Spec: https://modelcontextprotocol.io/specification/2025-03-26/basic/transports
//
// Identity is the Bearer token (Hydra JWT / bound OIDC). The host is not
// identity. Tools return Use results, never secrets.
package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/protocol"
)

type FetchIn struct {
	Item    string            `json:"item" jsonschema:"item name the agent is granted"`
	URL     string            `json:"url" jsonschema:"URL to fetch; host must match the item"`
	Method  string            `json:"method,omitempty" jsonschema:"HTTP method, default GET"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"extra request headers. never the vault secret"`
	Body    string            `json:"body,omitempty" jsonschema:"request body. never the vault secret"`
	BodyB64 string            `json:"body_b64,omitempty" jsonschema:"base64 request body for binary payloads. never the vault secret"`
}

type FetchOut struct {
	Decision         string      `json:"decision"`
	Reason           string      `json:"reason,omitempty"`
	Status           int         `json:"status,omitempty"`
	Headers          http.Header `json:"headers,omitempty"`
	Body             string      `json:"body,omitempty"`
	BodyB64          string      `json:"body_b64,omitempty"`
	RequestID        string      `json:"request_id,omitempty"`
	RequestExpiresAt *time.Time  `json:"request_expires_at,omitempty"`
}

type ListOut struct {
	Items []protocol.Item `json:"items"`
}

type MatchIn struct {
	URL string `json:"url" jsonschema:"URL to match against granted item URIs"`
}

type ApprovalStatusIn struct {
	RequestID string `json:"request_id" jsonschema:"approval request id from a fetch decision=need_approval"`
}

type ApprovalStatusOut struct {
	RequestID string    `json:"request_id"`
	Status    string    `json:"status"`
	ItemID    string    `json:"item_id"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// ResolvedAt is set by the resolver/sweeper — a derived "expired" status
	// can show a nil ResolvedAt until the sweeper writes the row.
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

func New(a *app.App) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "veil", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_items",
		Description: "List items this agent may Use. Names, URIs, login. Never secrets.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListOut, error) {
		agentID, err := principal(req)
		if err != nil {
			return nil, ListOut{}, err
		}
		items, err := a.ItemsForAgent(agentID)
		if err != nil {
			return nil, ListOut{}, err
		}
		if items == nil {
			items = []protocol.Item{}
		}
		slog.Info("mcp", "tool", "list_items", "agent", agentID, "n", len(items))
		return nil, ListOut{Items: items}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "fetch",
		Description: "Call a URL as this agent. The broker injects the credential. You never receive the secret. Level-1 items return decision=need_approval until a human runs `veil approve`.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in FetchIn) (*mcp.CallToolResult, FetchOut, error) {
		agentID, err := principal(req)
		if err != nil {
			return nil, FetchOut{}, err
		}
		out, err := Fetch(ctx, a, agentID, in)
		return nil, out, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check",
		Description: "Dry-run a fetch: evaluates the grant decision without calling the URL, claiming a use, or touching the secret. On need_approval it files the ask and returns request_id — also how an agent explicitly requests approval.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in FetchIn) (*mcp.CallToolResult, FetchOut, error) {
		agentID, err := principal(req)
		if err != nil {
			return nil, FetchOut{}, err
		}
		got, err := a.UseCheck(ctx, agentID, in.Item, in.Method, in.URL)
		if err != nil {
			return nil, FetchOut{}, err
		}
		return nil, FetchOut{
			Decision:         string(got.Decision),
			Reason:           got.Reason,
			RequestID:        got.RequestID,
			RequestExpiresAt: got.RequestExpiresAt,
		}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "match_items",
		Description: "List items this agent may Use whose URIs match a URL — which item to name in fetch. Names and URIs only, never secrets.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in MatchIn) (*mcp.CallToolResult, ListOut, error) {
		agentID, err := principal(req)
		if err != nil {
			return nil, ListOut{}, err
		}
		items, err := a.ItemsForAgent(agentID)
		if err != nil {
			return nil, ListOut{}, err
		}
		out := make([]protocol.Item, 0, len(items))
		for _, item := range items {
			if !item.Archived && grant.HostAllowed(item, in.URL) {
				out = append(out, item)
			}
		}
		slog.Info("mcp", "tool", "match_items", "agent", agentID, "n", len(out))
		return nil, ListOut{Items: out}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "approval_status",
		Description: "Poll one of this agent's approval requests after fetch returned decision=need_approval — open, approved, denied, or expired. Retry fetch once approved.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ApprovalStatusIn) (*mcp.CallToolResult, ApprovalStatusOut, error) {
		agentID, err := principal(req)
		if err != nil {
			return nil, ApprovalStatusOut{}, err
		}
		out, err := approvalStatus(a, agentID, in.RequestID)
		return nil, out, err
	})
	return server
}

func principal(req *mcp.CallToolRequest) (string, error) {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return "", fmt.Errorf("mcp: unauthorized")
	}
	id := strings.TrimSpace(req.Extra.TokenInfo.UserID)
	if id == "" {
		return "", fmt.Errorf("mcp: unauthorized")
	}
	return id, nil
}

func Fetch(ctx context.Context, a *app.App, agentID string, in FetchIn) (FetchOut, error) {
	h := http.Header{}
	for k, v := range in.Headers {
		h.Add(k, v)
	}
	if in.Body != "" && in.BodyB64 != "" {
		return FetchOut{}, fmt.Errorf("fetch: body and body_b64 are mutually exclusive")
	}
	body := []byte(in.Body)
	if in.BodyB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(in.BodyB64)
		if err != nil {
			return FetchOut{}, fmt.Errorf("fetch: bad body_b64: %w", err)
		}
		body = decoded
	}
	got, err := a.UseFetch(ctx, agentID, in.Item, protocol.Fetch{
		Method: in.Method,
		URL:    in.URL,
		Header: h,
		Body:   body,
	})
	if err != nil {
		return FetchOut{}, err
	}
	out := FetchOut{
		Decision:         string(got.Decision),
		Reason:           got.Reason,
		RequestID:        got.RequestID,
		RequestExpiresAt: got.RequestExpiresAt,
	}
	if got.Fetch != nil {
		out.Status = got.Fetch.Status
		out.Headers = got.Fetch.Header
		if utf8.Valid(got.Fetch.Body) {
			out.Body = string(got.Fetch.Body)
		} else {
			out.BodyB64 = base64.StdEncoding.EncodeToString(got.Fetch.Body)
		}
	}
	return out, nil
}

// approvalStatus reads one ask. It is scoped to the caller — an agent may only
// see its own requests, never another principal's.
func approvalStatus(a *app.App, agentID, requestID string) (ApprovalStatusOut, error) {
	req, err := a.Store.Request(strings.TrimSpace(requestID))
	if err != nil {
		return ApprovalStatusOut{}, err
	}
	if req.AgentID != agentID {
		return ApprovalStatusOut{}, fmt.Errorf("mcp: not found")
	}
	status := req.Status
	if status == protocol.RequestOpen && !time.Now().Before(req.ExpiresAt) {
		status = protocol.RequestExpired
	}
	return ApprovalStatusOut{
		RequestID:  req.ID,
		Status:     string(status),
		ItemID:     req.ItemID,
		Action:     string(req.Action),
		CreatedAt:  req.CreatedAt,
		ExpiresAt:  req.ExpiresAt,
		ResolvedAt: req.ResolvedAt,
	}, nil
}
