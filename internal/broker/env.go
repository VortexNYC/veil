package broker

import (
	"context"
	"fmt"
	"strings"

	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
)

// EnvName is the Infisical vault-run name: item name, uppercased, hyphen to underscore.
func EnvName(itemName string) string {
	return strings.ToUpper(strings.ReplaceAll(itemName, "-", "_"))
}

// ChildEnv injects granted material into a child process. The child sees the
// secret. Broker stdout, MCP, and audit do not. Level 1 without a live
// approval is skipped — never a prompt. SSH stays on the agent socket.
func (b *Broker) ChildEnv(ctx context.Context, agent protocol.Principal) ([]string, error) {
	if agent.Kind != protocol.PrincipalAgent {
		return nil, fmt.Errorf("broker: human cannot run")
	}
	grants, err := b.Store.ListGrants()
	if err != nil {
		return nil, err
	}
	now := b.now()
	var pairs []string
	for _, g := range grants {
		if g.AgentID != agent.ID {
			continue
		}
		item, err := b.Store.Item(g.ItemID)
		if err != nil {
			return nil, err
		}
		if !item.Kind.Injects() || item.Archived {
			continue
		}
		cp := g
		appr, err := b.Store.LiveApproval(g.ID, now)
		if err != nil {
			return nil, err
		}
		dec := grant.Evaluate(grant.Input{
			Principal: agent,
			Item:      item,
			Grant:     &cp,
			Action:    protocol.ActionEnv,
			Now:       now,
			Approval:  appr,
		})
		event := protocol.AuditEvent{
			Time:       now,
			OrgID:      agent.OrgID,
			AgentID:    agent.ID,
			ItemID:     item.ID,
			Action:     protocol.ActionEnv,
			Decision:   dec.Decision,
			Reason:     dec.Reason,
			ApprovalID: dec.ApprovalID,
		}
		if dec.Decision != protocol.DecisionAllow {
			if dec.Decision == protocol.DecisionNeedApproval {
				_, _ = b.FileRequest(ctx, agent, item, &cp, protocol.ActionEnv)
			}
			_ = b.appendAudit(ctx, event)
			LogEvent(event, item.Name, "", 0)
			continue
		}
		secret, err := b.Store.Secret(item.ID)
		if err != nil {
			return nil, err
		}
		env := material.Unpack(secret)
		val := env.Token
		if env.Refresh != "" {
			access, err := material.AccessToken(ctx, env, b.client())
			if err != nil {
				event.Decision = protocol.DecisionDeny
				event.Reason = "oauth_failed"
				_ = b.appendAudit(ctx, event)
				LogEvent(event, item.Name, "", 0)
				continue
			}
			val = access
		}
		if val == "" {
			event.Decision = protocol.DecisionDeny
			event.Reason = "empty_secret"
			_ = b.appendAudit(ctx, event)
			LogEvent(event, item.Name, "", 0)
			continue
		}
		// Fail closed: a grant whose audit write fails is not injected, and
		// the skip is logged rather than silently shrinking the child env.
		if err := b.appendAudit(ctx, event); err != nil {
			LogEvent(event, item.Name, "audit_unavailable", 0)
			continue
		}
		pairs = append(pairs, EnvName(item.Name)+"="+val)
		if env.TOTP != "" {
			code, err := material.Mint(env.TOTP, now)
			if err == nil && code != "" {
				pairs = append(pairs, EnvName(item.Name)+"_TOTP="+code)
			}
		}
		LogEvent(event, item.Name, "", 0)
	}
	return pairs, nil
}
