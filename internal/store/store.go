package store

import (
	"errors"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

const maxListResults = 10000

var (
	ErrNotFound       = errors.New("store: not found")
	ErrDenied         = errors.New("store: denied")
	ErrSessionExpired = errors.New("store: session expired")
	ErrSessionRevoked = errors.New("store: session revoked")
	// ErrOrgKeyMissing means the org has no org_keys row — a provisioning or
	// ops failure, distinct from a missing item so callers do not 404 a secret
	// that exists but cannot be decrypted.
	ErrOrgKeyMissing = errors.New("store: org has no key")
	// ErrOrgKeyMismatch means EnsureOrgKey was handed a master that does not
	// match the committed org_keys row — the asserted key is wrong.
	ErrOrgKeyMismatch = errors.New("store: org key mismatch")
)

// Secret is vault material. It never lives on protocol types.
type Secret []byte

// UseAuth is the consolidated authorization snapshot for a single Use call.
// It is returned by Store.UseAuth in one round trip and contains no secret.
type UseAuth struct {
	Agent    protocol.Principal
	Item     protocol.Item
	Grant    *protocol.Grant
	Approval *protocol.Approval
}

// SweepReport counts rows deleted by a Sweep.
type SweepReport struct {
	Sessions  int64
	Grants    int64
	Approvals int64
}

type Store interface {
	PutAgent(protocol.Principal) error
	Agent(id string) (protocol.Principal, error)
	ListAgents() ([]protocol.Principal, error)
	// RevokeAgent sets RevokedAt on the agent. It is idempotent and preserves
	// the earliest revocation time. It returns ErrNotFound if the agent does not exist.
	// If one or more audit events are provided, they are appended atomically with
	// the revocation in the same store transaction.
	RevokeAgent(id string, at time.Time, audit ...protocol.AuditEvent) error

	PutHuman(protocol.Principal) error
	Human(id string) (protocol.Principal, error)
	ListHumans() ([]protocol.Principal, error)

	PutItem(protocol.Item, Secret) error
	Item(id string) (protocol.Item, error)
	ItemByName(orgID, name string) (protocol.Item, error)
	ListItems() ([]protocol.Item, error)
	ArchiveItem(id string) error
	DeleteItem(id string) error
	Versions(itemID string) ([]protocol.ItemVersion, error)
	RestoreVersion(itemID string, versionID int64) error
	// Secret is for the broker only. There is no agent-facing reveal.
	Secret(id string) (Secret, error)
	// UseAuth returns the agent, item, grant, and live approval for a Use
	// request in a single round trip. It never returns a secret.
	UseAuth(agentID, itemID string, now time.Time) (UseAuth, error)
	// UseAuthSession is the same as UseAuth but resolves a session by its
	// secret hash first. It read-only validates the session is not expired,
	// revoked, or exhausted and then joins the authorization metadata. It does
	// not consume a use; the broker calls ConsumeSession after the pre-secret
	// authorization gates. If the session is missing, expired, revoked,
	// exhausted, or maps to a missing agent, it returns ErrNotFound,
	// ErrSessionExpired, ErrSessionRevoked, or ErrDenied.
	UseAuthSession(sessionHash []byte, itemID string, now time.Time) (UseAuth, error)

	// ConsumeSession atomically verifies the session is still valid (not
	// expired, revoked, or exhausted), that the bound agent has not been
	// revoked, and increments Uses. It returns the current agent principal on
	// success. It must be called only after all pre-secret authorization
	// checks have passed. It is the final revocation race guard for session
	// token Use calls.
	ConsumeSession(sessionHash []byte, now time.Time) (protocol.Principal, error)

	// ConsumeSessionAudited is ConsumeSession plus the audit event appended in
	// the same transaction: the consume and the audit row commit together or
	// not at all. e's OrgID/AgentID are overwritten with the consumed agent's
	// identity before insert, so callers may leave them unset. The broker uses
	// this on the allow path so an allow can never release a credential
	// without its audit row already durable.
	ConsumeSessionAudited(sessionHash []byte, now time.Time, e protocol.AuditEvent) (protocol.Principal, error)

	SessionByID(id string) (protocol.Session, error)
	RevokeSession(id string, at time.Time) error
	RenewSession(id string, at time.Time) (protocol.Session, error)

	PutGrant(protocol.Grant) error
	Grant(id string) (*protocol.Grant, error)
	GrantFor(agentID, itemID string) (*protocol.Grant, error)
	ListGrants() ([]protocol.Grant, error)

	PutApproval(protocol.Approval) error
	LiveApproval(grantID string, now time.Time) (*protocol.Approval, error)

	PutWorkload(protocol.Workload) error
	Workload(issuer, subject string) (*protocol.Workload, error)
	WorkloadsForIssuer(issuer string) ([]protocol.Workload, error)

	PutSession(s protocol.Session, secretHash []byte) error
	SessionByHash(secretHash []byte) (protocol.Session, error)
	ListSessions() ([]protocol.Session, error)

	AppendAudit(protocol.AuditEvent) error
	AppendAudits([]protocol.AuditEvent) error
	Audit() ([]protocol.AuditEvent, error)

	// Sweep deletes terminally-expired rows (sessions past expiry or revoked,
	// grants and approvals past expiry) older than the cutoff. It keeps the
	// hot-path indexes bounded; expiry semantics are unaffected because
	// expired rows are already invisible to authorization queries.
	Sweep(olderThan time.Time) (SweepReport, error)
	Close() error
}
