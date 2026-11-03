package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/protocol"
)

// RequestAgentFill files a website-login approval ask for a granted item — the
// 1Password for Agents posture: the agent names the login, the human approves in
// the vault, fill happens on the device (extension + host), never on MCP.
func (a *App) RequestAgentFill(ctx context.Context, agentID, itemRef, pageURL string) (protocol.ApprovalRequest, error) {
	agentID = strings.TrimSpace(agentID)
	itemRef = strings.TrimSpace(itemRef)
	if agentID == "" || itemRef == "" {
		return protocol.ApprovalRequest{}, fmt.Errorf("app: agent and item required")
	}
	agent, err := a.Store.Agent(agentID)
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	granted, err := a.ItemsForAgent(agentID)
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	var item protocol.Item
	for _, it := range granted {
		if it.Archived {
			continue
		}
		if it.ID == itemRef || it.Name == itemRef {
			item = it
			break
		}
	}
	if item.ID == "" {
		return protocol.ApprovalRequest{}, fmt.Errorf("app: not granted")
	}
	pageURL = strings.TrimSpace(pageURL)
	if pageURL != "" && !grant.HostAllowed(item, pageURL) {
		return protocol.ApprovalRequest{}, fmt.Errorf("app: url does not match item")
	}
	now := time.Now()
	reqID, err := id.NewRequest()
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	out, err := a.Store.FileRequest(protocol.ApprovalRequest{
		ID:        reqID,
		OrgID:     agent.OrgID,
		AgentID:   agentID,
		ItemID:    item.ID,
		GrantID:   "fill:" + item.ID,
		Action:    protocol.ActionFill,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	if out.Created && a.Broker != nil && a.Broker.OnRequestFiled != nil {
		a.Broker.OnRequestFiled(ctx, out.Request)
	}
	return out.Request, nil
}
