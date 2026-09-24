// Package protocol is the one API every surface speaks: CLI, MCP, SDK, proxy.
//
// Agents never receive a Secret. They Resolve metadata, Use an action, and
// wait on Approve for level-1 grants. Injection happens inside the broker.
package protocol

import (
	"net/http"
	"time"
)

type PrincipalKind string

const (
	PrincipalHuman PrincipalKind = "human"
	PrincipalAgent PrincipalKind = "agent"
)

type OwnerKind string

const (
	OwnerUser OwnerKind = "user"
	OwnerOrg  OwnerKind = "org"
)

// LocalOrgID is the one VeilNYC organization. Same value in the vault,
// Kratos organization_id, and the Keto object. A second company is a
// new UUID when a second tenant exists. Not this slice.
const LocalOrgID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type GrantLevel string

const (
	// Level1: agent may prepare; a human must approve the last step.
	Level1 GrantLevel = "level1"
	// Level2: this identity was donated to agents. No human in the loop.
	Level2 GrantLevel = "level2"
)

type ItemKind string

const (
	ItemAPIKey   ItemKind = "api_key"
	ItemOAuth    ItemKind = "oauth"
	ItemSSH      ItemKind = "ssh"
	ItemFile     ItemKind = "file"
	ItemPasskey  ItemKind = "passkey"
	ItemCard     ItemKind = "card"
	ItemIdentity ItemKind = "identity"
)

// Injects is whether Use / child env may touch this kind. SSH stays on the
// agent socket. Files are owner write. Passkeys, cards, and identities are
// fill-host only. PAN/CVV never enter a child env.
func (k ItemKind) Injects() bool {
	switch k {
	case ItemSSH, ItemFile, ItemPasskey, ItemCard, ItemIdentity:
		return false
	default:
		return true
	}
}

// Fillable is whether the fill host / OS provider may write this kind.
func (k ItemKind) Fillable() bool {
	switch k {
	case ItemAPIKey, ItemPasskey, ItemCard, ItemIdentity:
		return true
	default:
		return false
	}
}

type ActionKind string

const (
	ActionFetch  ActionKind = "fetch"
	ActionEnv    ActionKind = "env"
	ActionRevoke ActionKind = "revoke"
	// Audit actions for the approval-request lifecycle. Never grant actions —
	// actionAllowed only passes fetch/env and Use only accepts fetch.
	ActionRequestFiled    ActionKind = "request_filed"
	ActionRequestApproved ActionKind = "request_approved"
	ActionRequestDenied   ActionKind = "request_denied"
	ActionRequestExpired  ActionKind = "request_expired"
)

type Decision string

const (
	DecisionAllow        Decision = "allow"
	DecisionDeny         Decision = "deny"
	DecisionNeedApproval Decision = "need_approval"
)

type Owner struct {
	Kind OwnerKind `json:"kind"`
	ID   string    `json:"id"`
}

type Principal struct {
	Kind  PrincipalKind `json:"kind"`
	ID    string        `json:"id"`
	OrgID string        `json:"org_id"`
	// Owner is who may create grants for this agent. Humans leave it empty.
	Owner Owner `json:"owner,omitempty"`
	// RevokedAt is set when an agent's grants and sessions are killed.
	// It is a timestamp, not a secret, and is included in agent lists.
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type Item struct {
	ID    string   `json:"id"`
	OrgID string   `json:"org_id"`
	Name  string   `json:"name"`
	Kind  ItemKind `json:"kind"`
	Owner Owner    `json:"owner"`
	// URIs are the hosts this item may be used against (Infisical "service").
	URIs []string `json:"uris"`
	// Tags are owner labels. Not ACL. Grants are ACL.
	Tags []string `json:"tags,omitempty"`
	// Archived items are hidden from Use, list, fill. History stays.
	Archived bool `json:"archived,omitempty"`
	// HasTOTP is metadata. The seed is not on this struct.
	HasTOTP bool `json:"has_totp,omitempty"`
	// HasFile is metadata. Bytes are not on this struct.
	HasFile bool `json:"has_file,omitempty"`
	// Login is the fill username. Metadata. Not a secret. Empty is honest.
	Login string `json:"login,omitempty"`
}

// ItemVersion is history metadata. The sealed blob is not here.
type ItemVersion struct {
	ID     int64
	ItemID string
	Time   time.Time
}

type Grant struct {
	ID        string
	OrgID     string
	AgentID   string
	ItemID    string
	Level     GrantLevel
	Actions   []ActionKind
	ExpiresAt *time.Time
}

type Approval struct {
	ID        string
	GrantID   string
	HumanID   string
	ExpiresAt time.Time
}

// RequestStatus is the approval-request lifecycle. Open is the only state a
// resolve may leave; every other state is terminal.
type RequestStatus string

const (
	RequestOpen      RequestStatus = "open"
	RequestApproved  RequestStatus = "approved"
	RequestDenied    RequestStatus = "denied"
	RequestExpired   RequestStatus = "expired"
	RequestCancelled RequestStatus = "cancelled"
)

// ApprovalRequest is a filed ask: this grant wants this action and a human
// must answer. One open row per (grant, action) — re-filing reuses it.
type ApprovalRequest struct {
	ID         string
	OrgID      string
	AgentID    string
	ItemID     string
	GrantID    string
	Action     ActionKind
	Status     RequestStatus
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ResolvedAt *time.Time
	ResolvedBy string
	ApprovalID string
}

// Workload is an Entra-style federation binding. We verify an OIDC ID token
// from someone else's issuer and map (issuer, subject) to an existing agent.
// We do not issue tokens.
type Workload struct {
	AgentID  string
	Issuer   string
	Subject  string
	Audience string
}

type Fetch struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

type UseRequest struct {
	ItemID string
	Action ActionKind
	Fetch  *Fetch
}

type FetchResult struct {
	Status int
	Header http.Header
	Body   []byte
}

type UseResult struct {
	Decision   Decision
	Reason     string
	ApprovalID string
	// RequestID and RequestExpiresAt carry the approval request filed on a
	// need_approval denial — the agent retries Use; the human resolves the ask.
	RequestID         string
	RequestExpiresAt *time.Time
	Fetch            *FetchResult
}

type AuditEvent struct {
	Time       time.Time  `json:"time"`
	OrgID      string     `json:"org_id"`
	AgentID    string     `json:"agent_id"`
	ItemID     string     `json:"item_id"`
	Action     ActionKind `json:"action"`
	Decision   Decision   `json:"decision"`
	Reason     string     `json:"reason,omitempty"`
	ApprovalID string     `json:"approval_id,omitempty"`
}

// Session is a short-lived Use lease onto an existing agent. The token
// is never on this type. Owner mint only. Not MCP. Not list.
type Session struct {
	ID        string     `json:"id"`
	OrgID     string     `json:"org_id"`
	AgentID   string     `json:"agent_id"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	RenewedAt *time.Time `json:"renewed_at,omitempty"`
	TTL       int64      `json:"ttl"`      // seconds
	MaxTTL    int64      `json:"max_ttl"`  // seconds
	MaxUses   int        `json:"max_uses"` // 0 = unlimited
	Uses      int        `json:"uses"`     // Use calls that passed authorization and reached upstream consumption
}
