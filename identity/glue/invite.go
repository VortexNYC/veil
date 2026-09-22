package glue

import (
	"context"
	"fmt"
	"strings"
)

// InviteIdentity is Kratos invite + Keto owner/member.
// Kratos creates the human. Keto records whether they may act in the org.
// orgID is the inviter's org — membership is per-org, never the client's default.
func (g *Glue) InviteIdentity(ctx context.Context, email, actor, orgID string) (Invite, error) {
	if g.humans == nil {
		return Invite{}, fmt.Errorf("glue: kratos admin is required")
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\n") {
		return Invite{}, fmt.Errorf("glue: email")
	}
	if g.members != nil {
		if err := g.members.ForOrg(orgID).RequireOwner(ctx, actor); err != nil {
			return Invite{}, err
		}
	}
	inv, err := g.humans.Invite(ctx, email, orgID)
	if err != nil {
		return Invite{}, err
	}
	if g.members != nil {
		if err := g.members.ForOrg(orgID).AddMember(ctx, inv.IdentityID); err != nil {
			return Invite{}, err
		}
	}
	return inv, nil
}

// ListMembers is the Kratos directory for this org. Membership checks are Keto.
func (g *Glue) ListMembers(ctx context.Context, orgID string) ([]string, error) {
	if g.humans == nil {
		return nil, fmt.Errorf("glue: kratos admin is required")
	}
	return g.humans.List(ctx, orgID)
}
