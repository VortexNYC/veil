// Package keto talks to Ory Keto only.
//
// Owner / member of the org. Not grants. Not secrets. Not humans.
package keto

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	ory "github.com/ory/keto-client-go/v26"

	"github.com/VortexNYC/veil/identity/glue/internal/absurl"
	"github.com/VortexNYC/veil/internal/protocol"
)

const (
	LocalOrgID = protocol.LocalOrgID
	RelOwners  = "owners"
	RelMembers = "members"
	nsOrg      = "Organization"
)

type Client struct {
	read  *ory.APIClient
	write *ory.APIClient
	orgID string
}

func New(readURL, writeURL, orgID string) (*Client, error) {
	c := &Client{orgID: strings.TrimSpace(orgID)}
	var err error
	if strings.TrimSpace(readURL) != "" {
		c.read, err = ketoClient(readURL)
		if err != nil {
			return nil, fmt.Errorf("keto: read: %w", err)
		}
	}
	if strings.TrimSpace(writeURL) != "" {
		c.write, err = ketoClient(writeURL)
		if err != nil {
			return nil, fmt.Errorf("keto: write: %w", err)
		}
	}
	return c, nil
}

func ketoClient(raw string) (*ory.APIClient, error) {
	u, err := absurl.Parse(raw)
	if err != nil {
		return nil, err
	}
	cfg := ory.NewConfiguration()
	cfg.Servers = ory.ServerConfigurations{{URL: u}}
	return ory.NewAPIClient(cfg), nil
}

func (c *Client) Org() string {
	if c != nil && c.orgID != "" {
		return c.orgID
	}
	return LocalOrgID
}

// ForOrg returns a client scoped to orgID — the same connections, a different
// org object. Membership is per-org; one deployment holds many orgs.
func (c *Client) ForOrg(orgID string) *Client {
	orgID = strings.TrimSpace(orgID)
	if c == nil || orgID == "" || orgID == c.orgID {
		return c
	}
	cp := *c
	cp.orgID = orgID
	return &cp
}

func (c *Client) On() bool {
	return c != nil && c.read != nil && c.write != nil
}

func (c *Client) Allowed(ctx context.Context, relation, subject string) (bool, error) {
	if !c.On() {
		return false, fmt.Errorf("keto: required")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || relation == "" {
		return false, fmt.Errorf("keto: check")
	}
	got, resp, err := c.read.PermissionAPI.CheckPermission(ctx).
		Namespace(nsOrg).
		Object(c.Org()).
		Relation(relation).
		SubjectId(subject).
		Execute()
	if got != nil {
		return got.GetAllowed(), nil
	}
	if resp != nil && resp.StatusCode == http.StatusForbidden {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("keto: check: %w", err)
	}
	return false, nil
}

func (c *Client) IsMember(ctx context.Context, identityID string) (bool, error) {
	return c.Allowed(ctx, RelMembers, identityID)
}

func (c *Client) IsOwner(ctx context.Context, identityID string) (bool, error) {
	return c.Allowed(ctx, RelOwners, identityID)
}

func (c *Client) RequireOwner(ctx context.Context, actor string) error {
	if !c.On() {
		return nil
	}
	owners, err := c.HasOwners(ctx)
	if err != nil {
		return err
	}
	if !owners {
		return nil
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return fmt.Errorf("keto: owner required")
	}
	ok, err := c.Allowed(ctx, RelOwners, actor)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("keto: not an owner")
	}
	return nil
}

func (c *Client) writeRelation(ctx context.Context, relation, subject string) error {
	if !c.On() {
		return nil
	}
	body := ory.NewCreateRelationshipBody()
	body.SetNamespace(nsOrg)
	body.SetObject(c.Org())
	body.SetRelation(relation)
	body.SetSubjectId(subject)
	_, resp, err := c.write.RelationshipAPI.CreateRelationship(ctx).
		CreateRelationshipBody(*body).
		Execute()
	if err == nil {
		return nil
	}
	if resp != nil && resp.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("keto: write: %w", err)
}

func (c *Client) HasOwners(ctx context.Context) (bool, error) {
	if !c.On() {
		return false, nil
	}
	got, _, err := c.read.RelationshipAPI.GetRelationships(ctx).
		Namespace(nsOrg).
		Object(c.Org()).
		Relation(RelOwners).
		PageSize(1).
		Execute()
	if err != nil {
		return false, fmt.Errorf("keto: list: %w", err)
	}
	return got != nil && len(got.GetRelationTuples()) > 0, nil
}

func (c *Client) AddMember(ctx context.Context, identityID string) error {
	if err := c.writeRelation(ctx, RelMembers, identityID); err != nil {
		return err
	}
	owners, err := c.HasOwners(ctx)
	if err != nil {
		return err
	}
	if owners {
		return nil
	}
	return c.writeRelation(ctx, RelOwners, identityID)
}

// Promote adds the owners tuple — the caller already proved membership.
func (c *Client) Promote(ctx context.Context, identityID string) error {
	return c.writeRelation(ctx, RelOwners, identityID)
}

// ListRelation returns every subject holding (org, relation) — owner
// enumeration for last-owner protection.
func (c *Client) ListRelation(ctx context.Context, relation string) ([]string, error) {
	if !c.On() {
		return nil, fmt.Errorf("keto: required")
	}
	var out []string
	page := ""
	for {
		req := c.read.RelationshipAPI.GetRelationships(ctx).
			Namespace(nsOrg).
			Object(c.Org()).
			Relation(relation).
			PageSize(250)
		if page != "" {
			req = req.PageToken(page)
		}
		got, _, err := req.Execute()
		if err != nil {
			return nil, fmt.Errorf("keto: list: %w", err)
		}
		for _, t := range got.GetRelationTuples() {
			if t.HasSubjectId() {
				out = append(out, t.GetSubjectId())
			}
		}
		page = got.GetNextPageToken()
		if page == "" {
			return out, nil
		}
	}
}

// DeleteRelation removes one tuple (org, relation, subject). Missing tuples
// delete clean — offboarding is idempotent.
func (c *Client) DeleteRelation(ctx context.Context, relation, subject string) error {
	if !c.On() {
		return fmt.Errorf("keto: required")
	}
	_, err := c.write.RelationshipAPI.DeleteRelationships(ctx).
		Namespace(nsOrg).
		Object(c.Org()).
		Relation(relation).
		SubjectId(subject).
		Execute()
	if err != nil {
		return fmt.Errorf("keto: delete: %w", err)
	}
	return nil
}

// DeleteAllRelations removes every tuple for the org under one relation —
// org teardown. Missing is fine.
func (c *Client) DeleteAllRelations(ctx context.Context, relation string) error {
	if !c.On() {
		return fmt.Errorf("keto: required")
	}
	_, err := c.write.RelationshipAPI.DeleteRelationships(ctx).
		Namespace(nsOrg).
		Object(c.Org()).
		Relation(relation).
		Execute()
	if err != nil {
		return fmt.Errorf("keto: delete all: %w", err)
	}
	return nil
}
