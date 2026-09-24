// Package glue is the seam. It wires Kratos, Hydra, and Keto.
//
// Official Ory Go clients live in the matching directory — not here:
//
//	glue/kratos  kratos-client-go     humans
//	glue/hydra   hydra-client-go     tokens
//	glue/keto    keto-client-go      owner / member
//
// Login is Kratos oauth2_provider, not this package.
// To leave Ory, replace those three directories. This package stays the wiring.
package glue

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/VortexNYC/veil/identity/glue/hydra"
	"github.com/VortexNYC/veil/identity/glue/keto"
	"github.com/VortexNYC/veil/identity/glue/kratos"
	"github.com/VortexNYC/veil/internal/protocol"
)

const (
	LocalOrgID        = protocol.LocalOrgID
	DefaultClientID   = hydra.DefaultClientID
	AgentClientPrefix = hydra.AgentClientPrefix
	relOwners         = keto.RelOwners
	relMembers        = keto.RelMembers
)

type (
	AgentClient = hydra.AgentClient
	AgentCred   = hydra.AgentCred
	FirstParty  = hydra.FirstParty
)

// Invite is a Kratos invite plus whether the delivery mail went out.
type Invite struct {
	kratos.Invite
	Emailed bool
}

type Config struct {
	KratosPublic string
	KratosAdmin  string
	HydraAdmin   string
	KetoRead     string
	KetoWrite    string
	OrgID        string
	MailURL      string
	MailToken    string
	// AppURL is where mail CTAs land — the vault UI. Default production is
	// https://app.veil.nyc; set VEIL_APP_URL for staging/preview deploys.
	AppURL string
}

type Glue struct {
	humans  *kratos.Client
	tokens  *hydra.Client
	members *keto.Client
	mail    *mailer
	appURL  string
}

func New(cfg Config) (*Glue, error) {
	humans, err := kratos.New(cfg.KratosPublic, cfg.KratosAdmin)
	if err != nil {
		return nil, fmt.Errorf("glue: %w", err)
	}
	tokens, err := hydra.New(cfg.HydraAdmin)
	if err != nil {
		return nil, fmt.Errorf("glue: %w", err)
	}
	members, err := keto.New(cfg.KetoRead, cfg.KetoWrite, cfg.OrgID)
	if err != nil {
		return nil, fmt.Errorf("glue: %w", err)
	}
	appURL := strings.TrimSpace(cfg.AppURL)
	if appURL == "" {
		appURL = "https://app.veil.nyc"
	}
	return &Glue{
		humans:  humans,
		tokens:  tokens,
		members: members,
		mail:    newMailer(cfg.MailURL, cfg.MailToken),
		appURL:  appURL,
	}, nil
}

func NewHydra(admin string) (*Glue, error) {
	tokens, err := hydra.New(admin)
	if err != nil {
		return nil, fmt.Errorf("glue: %w", err)
	}
	return &Glue{tokens: tokens}, nil
}

func (g *Glue) DefaultOrg() string {
	if g.members != nil {
		return g.members.Org()
	}
	return LocalOrgID
}

func (g *Glue) EnsureFirstParty(ctx context.Context, fp FirstParty) error {
	if g.tokens == nil {
		return fmt.Errorf("glue: hydra admin not configured")
	}
	return g.tokens.EnsureFirstParty(ctx, fp)
}

func (g *Glue) AcceptConsent(ctx context.Context, challenge string) (string, error) {
	if g.tokens == nil {
		return "", fmt.Errorf("glue: hydra admin not configured")
	}
	return g.tokens.AcceptConsent(ctx, challenge)
}

func (g *Glue) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	consent := r.URL.Query().Get("consent_challenge")
	if consent == "" {
		http.Error(w, "missing consent_challenge", http.StatusBadRequest)
		return
	}
	redirectTo, err := g.AcceptConsent(r.Context(), consent)
	if err != nil {
		http.Error(w, "consent request failed", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func (g *Glue) EnsureAgent(ctx context.Context, ac AgentClient) (AgentCred, error) {
	if g.tokens == nil {
		return AgentCred{}, fmt.Errorf("glue: hydra admin not configured")
	}
	return g.tokens.EnsureAgent(ctx, ac)
}

func ClientCredentials(ctx context.Context, issuer, clientID, secret, audience string) (string, error) {
	return hydra.ClientCredentials(ctx, issuer, clientID, secret, audience)
}

func AgentClientID(agentID string) string {
	return hydra.AgentClientID(agentID)
}

func (g *Glue) Allowed(ctx context.Context, orgID, relation, subject string) (bool, error) {
	if g.members == nil {
		return false, fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).Allowed(ctx, relation, subject)
}

func (g *Glue) IsMember(ctx context.Context, orgID, identityID string) (bool, error) {
	if g.members == nil {
		return false, fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).IsMember(ctx, identityID)
}

func (g *Glue) IsOwner(ctx context.Context, orgID, identityID string) (bool, error) {
	if g.members == nil {
		return false, fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).IsOwner(ctx, identityID)
}

// ProvisionMember plants owner+member tuples for an identity in orgID.
// Provisioning calls this once the org exists in the vault; first member of a
// fresh org becomes owner (Keto AddMember bootstrap semantics).
func (g *Glue) ProvisionMember(ctx context.Context, orgID, identityID string) error {
	if g.members == nil {
		return fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).AddMember(ctx, identityID)
}

// SetIdentityOrg stamps a Kratos identity with its org — the same join key as
// the vault humans row and the Keto object.
func (g *Glue) SetIdentityOrg(ctx context.Context, identityID, orgID string) error {
	if g.humans == nil {
		return fmt.Errorf("glue: kratos admin is required")
	}
	return g.humans.SetOrganization(ctx, identityID, orgID)
}

// IdentityID is the Kratos id for an email. CLI grant --human. Broker never
// calls this. Email stays in Kratos.
func (g *Glue) IdentityID(ctx context.Context, email, orgID string) (string, error) {
	if g.humans == nil {
		return "", fmt.Errorf("glue: kratos admin is required")
	}
	return g.humans.IdentityByEmail(ctx, email, orgID)
}

func (g *Glue) IdentityOrg(ctx context.Context, identityID string) (string, error) {
	if g.humans == nil {
		return "", fmt.Errorf("glue: kratos admin is required")
	}
	return g.humans.Organization(ctx, identityID)
}

// RemoveMember strips the member tuple — offboarding. The humans row goes via
// the store; Kratos keeps the identity (it belongs to the human, not the org).
func (g *Glue) RemoveMember(ctx context.Context, orgID, identityID string) error {
	if g.members == nil {
		return fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).DeleteRelation(ctx, keto.RelMembers, identityID)
}

// RemoveOwner strips an owner tuple — demotion, or teardown cleanup.
func (g *Glue) RemoveOwner(ctx context.Context, orgID, identityID string) error {
	if g.members == nil {
		return fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).DeleteRelation(ctx, keto.RelOwners, identityID)
}

// PromoteOwner grants the owners tuple to an existing member.
func (g *Glue) PromoteOwner(ctx context.Context, orgID, identityID string) error {
	if g.members == nil {
		return fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).Promote(ctx, identityID)
}

// ListOwners returns every identity holding the owners tuple — last-owner
// protection needs the count, not a boolean.
func (g *Glue) ListOwners(ctx context.Context, orgID string) ([]string, error) {
	if g.members == nil {
		return nil, fmt.Errorf("glue: keto is required")
	}
	return g.members.ForOrg(orgID).ListRelation(ctx, keto.RelOwners)
}

// RemoveOrgTuples deletes every owner+member tuple for the org — teardown.
func (g *Glue) RemoveOrgTuples(ctx context.Context, orgID string) error {
	if g.members == nil {
		return fmt.Errorf("glue: keto is required")
	}
	if err := g.members.ForOrg(orgID).DeleteAllRelations(ctx, keto.RelMembers); err != nil {
		return err
	}
	return g.members.ForOrg(orgID).DeleteAllRelations(ctx, keto.RelOwners)
}

// OwnerEmails resolves every owner identity to a deliverable address —
// approval-request notify. Identities without an email trait are skipped.
func (g *Glue) OwnerEmails(ctx context.Context, orgID string) ([]string, error) {
	if g.humans == nil {
		return nil, fmt.Errorf("glue: kratos admin is required")
	}
	ids, err := g.ListOwners(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		email, err := g.humans.Email(ctx, id)
		if err != nil || email == "" {
			continue
		}
		out = append(out, email)
	}
	return out, nil
}

// NotifyRequest mails every org owner that an agent asked — best-effort:
// the request row is durable; a mail outage loses the ping, never the ask.
// The payload is metadata only — no secret, no approve-token.
func (g *Glue) NotifyRequest(ctx context.Context, req protocol.ApprovalRequest) error {
	if g == nil || g.mail == nil {
		return nil
	}
	emails, err := g.OwnerEmails(ctx, req.OrgID)
	if err != nil {
		slog.Warn("notify owners failed", "org", req.OrgID, "err", err)
		return nil
	}
	data := map[string]string{
		"agent":   req.AgentID,
		"item":    req.ItemID,
		"action":  string(req.Action),
		"expires": req.ExpiresAt.Format(time.RFC3339),
		"url":     g.appURL,
	}
	for _, to := range emails {
		if err := g.mail.send(ctx, to, "request", data); err != nil {
			slog.Warn("notify send failed", "request", req.ID, "err", err)
		}
	}
	return nil
}
