// Package kratos talks to Ory Kratos only.
//
// Humans: email, password, recovery, directory.
// Not tokens (Hydra). Not owner/member (Keto). Not grants.
// Session is Kratos self-service, not this client.
package kratos

import (
	"context"
	"fmt"
	"strings"

	ory "github.com/ory/kratos-client-go/v26"

	"github.com/VortexNYC/veil/identity/glue/internal/absurl"
)

type Client struct {
	admin *ory.APIClient
}

func New(publicURL, adminURL string) (*Client, error) {
	if strings.TrimSpace(adminURL) == "" {
		adminURL = publicURL
	}
	admin, err := absurl.Parse(adminURL)
	if err != nil {
		return nil, fmt.Errorf("kratos: admin: %w", err)
	}
	ac := ory.NewConfiguration()
	ac.Servers = ory.ServerConfigurations{{URL: admin}}
	return &Client{admin: ory.NewAPIClient(ac)}, nil
}

type Invite struct {
	IdentityID   string
	RecoveryLink string
	Code         string `json:"-"`
}

// Invite creates an identity and a recovery code. Email stays here.
func (c *Client) Invite(ctx context.Context, email, orgID string) (Invite, error) {
	if c == nil || c.admin == nil {
		return Invite{}, fmt.Errorf("kratos: admin is required")
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\n") {
		return Invite{}, fmt.Errorf("kratos: email")
	}
	body := ory.NewCreateIdentityBody("default", map[string]any{"email": email})
	if orgID != "" {
		body.SetOrganizationId(orgID)
	}
	id, _, err := c.admin.IdentityAPI.CreateIdentity(ctx).CreateIdentityBody(*body).Execute()
	if err != nil {
		return Invite{}, fmt.Errorf("kratos: invite identity: %w", err)
	}
	if id == nil || id.GetId() == "" {
		return Invite{}, fmt.Errorf("kratos: invite identity: empty id")
	}
	codeBody := ory.NewCreateRecoveryCodeForIdentityBody(id.GetId())
	got, _, err := c.admin.IdentityAPI.CreateRecoveryCodeForIdentity(ctx).
		CreateRecoveryCodeForIdentityBody(*codeBody).
		Execute()
	if err != nil {
		return Invite{}, fmt.Errorf("kratos: invite code: %w", err)
	}
	if got == nil || got.GetRecoveryCode() == "" {
		return Invite{}, fmt.Errorf("kratos: invite code: empty")
	}
	return Invite{
		IdentityID:   id.GetId(),
		RecoveryLink: got.GetRecoveryLink(),
		Code:         got.GetRecoveryCode(),
	}, nil
}

// List returns identity ids in org. Email stays in Kratos.
func (c *Client) List(ctx context.Context, orgID string) ([]string, error) {
	if c == nil || c.admin == nil {
		return nil, fmt.Errorf("kratos: admin is required")
	}
	ids, _, err := c.admin.IdentityAPI.ListIdentities(ctx).OrganizationId(orgID).Execute()
	if err != nil {
		return nil, fmt.Errorf("kratos: list identities: %w", err)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id.GetId() == "" {
			continue
		}
		if org := id.GetOrganizationId(); org != "" && org != orgID {
			continue
		}
		out = append(out, id.GetId())
	}
	return out, nil
}

func emailTrait(traits any) string {
	m, ok := traits.(map[string]any)
	if !ok {
		return ""
	}
	e, _ := m["email"].(string)
	return strings.TrimSpace(e)
}

func (c *Client) IdentityByEmail(ctx context.Context, email, orgID string) (string, error) {
	if c == nil || c.admin == nil {
		return "", fmt.Errorf("kratos: admin is required")
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\n") {
		return "", fmt.Errorf("kratos: email")
	}
	ids, _, err := c.admin.IdentityAPI.ListIdentities(ctx).OrganizationId(orgID).Execute()
	if err != nil {
		return "", fmt.Errorf("kratos: list identities: %w", err)
	}
	for _, id := range ids {
		if id.GetId() == "" {
			continue
		}
		if emailTrait(id.GetTraits()) == email {
			return id.GetId(), nil
		}
	}
	return "", fmt.Errorf("kratos: identity not found")
}

// SetOrganization binds an identity to an org. Provisioning owns this write:
// a signup gets its org stamped here, matching the vault humans row and the
// Keto object. JSON patch "add" is insert-or-replace for object members.
func (c *Client) SetOrganization(ctx context.Context, identityID, orgID string) error {
	if c == nil || c.admin == nil {
		return fmt.Errorf("kratos: admin is required")
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" || strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("kratos: identity and org required")
	}
	patch := ory.NewJsonPatch("add", "/organization_id")
	patch.SetValue(orgID)
	_, _, err := c.admin.IdentityAPI.PatchIdentity(ctx, identityID).
		JsonPatch([]ory.JsonPatch{*patch}).
		Execute()
	if err != nil {
		return fmt.Errorf("kratos: set organization: %w", err)
	}
	return nil
}

func (c *Client) Organization(ctx context.Context, identityID string) (string, error) {
	if c == nil || c.admin == nil {
		return "", fmt.Errorf("kratos: admin is required")
	}
	got, _, err := c.admin.IdentityAPI.GetIdentity(ctx, identityID).Execute()
	if err != nil {
		return "", fmt.Errorf("kratos: get identity: %w", err)
	}
	if got == nil {
		return "", fmt.Errorf("kratos: get identity: empty")
	}
	return got.GetOrganizationId(), nil
}
