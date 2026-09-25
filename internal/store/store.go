package store

import (
	"context"
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
	// ErrRequestResolved means an approval request was already resolved,
	// expired, or its grant died before the resolution landed — nothing was
	// written.
	ErrRequestResolved = errors.New("store: request already resolved")
	// ErrGrantNotLive means the grant's edge is dead — expired grant,
	// revoked agent, or archived item — so an approval minted on it could
	// never be used.
	ErrGrantNotLive = errors.New("store: grant no longer live")
	// ErrUnsupported means the store backend has no such surface — e.g.
	// key-rotation verbs on a single-key store.
	ErrUnsupported = errors.New("store: unsupported")
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

// FileOutcome is what one FileRequest did: the live ask, whether this call
// created it (a deduped refile returns Created=false and must not re-notify
// owners), and the stale open ask it expired on the way in, if any.
type FileOutcome struct {
	Request   protocol.ApprovalRequest
	Created   bool
	ExpiredID string
}

// SweepReport counts rows deleted by a Sweep — Requests marks, not deletes.
type SweepReport struct {
	Sessions  int64
	Grants    int64
	Approvals int64
	Requests  int64
}

// OrgBilling is the org's billing state as last reported by the billing
// webhook receiver — plan is what the Use gate enforces. An absent row reads
// as Plan "free".
type OrgBilling struct {
	OrgID      string
	Plan       string
	CustomerID string
	// BillingAccountID is the provider billing account usage events post to.
	BillingAccountID string
	UpdatedAt        time.Time
}

// UsageReportRow is one pending usage delta for the billing flusher: the
// locally metered units not yet reported to the billing provider. The billing
// link fields are empty when the org has no billing row yet.
type UsageReportRow struct {
	OrgID            string
	WindowStart      time.Time
	Used             int64
	Reported         int64
	CustomerID       string
	BillingAccountID string
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
	// PlantHuman inserts a humans row only if the id is absent — the
	// provisioning anchor that makes signup idempotent. Returns true when
	// this call created the row.
	PlantHuman(protocol.Principal) (bool, error)
	Human(id string) (protocol.Principal, error)
	ListHumans() ([]protocol.Principal, error)

	// EnsureOrgKey seals master as the org's org_keys row if absent; an
	// existing row under a different master fails ErrOrgKeyMismatch.
	// HasOrgKey reports whether the row exists. Single-key stores (sqlite,
	// memory) cover every org by construction.
	EnsureOrgKey(ctx context.Context, orgID string, master []byte) error
	HasOrgKey(ctx context.Context, orgID string) (bool, error)

	// RotateOrgKey mints a fresh org master and rewraps every owner DEK
	// under it (item ciphertexts are untouched). RotateKEK rewraps every
	// org master under newKEK. Single-key stores have no rotation surface
	// and return ErrUnsupported.
	RotateOrgKey(ctx context.Context, orgID string) error
	RotateKEK(ctx context.Context, newKEK []byte) error

	// StoreRecoveryWrap seals the org master under owner-held recoveryKey —
	// KEK-independent escrow for lost devices or a lost deployment KEK.
	// OpenRecoveryWrap verifies recoveryKey and returns the master; it is
	// single-use (used_at stamps on first open) and fails closed on wrong
	// material, expiry, or replay. ReseedOrgKey re-anchors that master under
	// the current KEK after the old KEK is lost. Single-key stores return
	// ErrUnsupported.
	StoreRecoveryWrap(ctx context.Context, orgID string, o protocol.Owner, recoveryKey []byte, expiresAt time.Time) error
	OpenRecoveryWrap(ctx context.Context, orgID string, o protocol.Owner, recoveryKey []byte) ([]byte, error)
	ReseedOrgKey(ctx context.Context, orgID string, master []byte) error
	// RecoverOrgKey opens the owner's recovery wrap, re-wraps the recovered
	// master under this store's KEK, and marks the wrap used — atomically,
	// in one transaction, so a failed reseed cannot burn the wrap. It is the
	// safe composition of OpenRecoveryWrap + ReseedOrgKey; prefer it.
	RecoverOrgKey(ctx context.Context, orgID string, o protocol.Owner, recoveryKey []byte) error

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

	// FileRequest records an open approval request for (grant, action) in
	// one transaction: a stale open on the same key is expired first
	// (ExpiredID names it), a live one is returned unchanged — Created=false
	// means the ask was already on file and must not re-notify owners.
	FileRequest(protocol.ApprovalRequest) (FileOutcome, error)
	Request(id string) (protocol.ApprovalRequest, error)
	// ListRequests returns org requests; status open lists only unexpired.
	ListRequests(orgID string, status protocol.RequestStatus, now time.Time) ([]protocol.ApprovalRequest, error)
	// ResolveRequest flips an open, unexpired request to a terminal status —
	// first write wins; won=false means it was already resolved or expired.
	ResolveRequest(id string, status protocol.RequestStatus, humanID, approvalID string, at time.Time) (req protocol.ApprovalRequest, won bool, err error)
	// ApproveRequest resolves ONE open ask by minting its grant's approval:
	// conditional resolve + approval insert + sibling asks resolve, all in
	// one transaction — and only while the ask AND its grant are live.
	// won=false means the ask was resolved, expired, or its grant died —
	// nothing was written, so a lost race leaves no side effects.
	// resolved[0] is the ask named by id; the rest are siblings the same
	// grant-approval answered.
	ApproveRequest(id string, appr protocol.Approval, at time.Time) (resolved []protocol.ApprovalRequest, won bool, err error)
	// ApproveGrant mints a grant-level approval and resolves every open ask
	// on it in one transaction — grant-scoped `veil approve`.
	ApproveGrant(grantID string, appr protocol.Approval, at time.Time) (resolved []protocol.ApprovalRequest, err error)
	CancelRequestsForAgent(agentID string, at time.Time) error
	CancelRequestsForItem(itemID string, at time.Time) error
	// ExpireStaleRequests marks open requests past expiry as expired and
	// returns the rows it marked — the sweep audits request_expired per row.
	ExpireStaleRequests(now time.Time) ([]protocol.ApprovalRequest, error)
	// WatchRequests returns a channel that ticks when approval requests for
	// orgID change — filed, resolved, expired, or cancelled. Ticks carry no
	// data: refetch the list. The channel closes when ctx ends.
	WatchRequests(ctx context.Context, orgID string) <-chan struct{}

	PutWorkload(protocol.Workload) error
	Workload(issuer, subject string) (*protocol.Workload, error)
	WorkloadsForIssuer(issuer string) ([]protocol.Workload, error)

	PutSession(s protocol.Session, secretHash []byte) error
	SessionByHash(secretHash []byte) (protocol.Session, error)
	ListSessions() ([]protocol.Session, error)

	AppendAudit(protocol.AuditEvent) error
	AppendAudits([]protocol.AuditEvent) error
	Audit() ([]protocol.AuditEvent, error)

	// Billing returns the org's billing state; an absent row is Plan "free".
	Billing(orgID string) (OrgBilling, error)
	// SetBilling upserts the org's billing state — the billing webhook
	// receiver is the writer.
	SetBilling(OrgBilling) error
	// SetBillingLink upserts only the billing link (customer + billing
	// account). Plan and UpdatedAt belong to the webhook receiver — link
	// writes must not disturb the staleness anchor.
	SetBillingLink(orgID, customerID, billingAccountID string) error
	// OrgByBillingCustomer resolves the org that owns a billing customer id
	// (Vortex cus_…), ErrNotFound when unlinked.
	OrgByBillingCustomer(customerID string) (string, error)
	// ConsumeUse atomically increments the org's counter for window and
	// reports whether the claim is within cap (cap <= 0 means unlimited —
	// the counter still accrues). Over-cap claims count too: blocked demand
	// is signal. Every claim lands exactly once, safe under concurrency.
	ConsumeUse(orgID string, window time.Time, cap int64) (used int64, ok bool, err error)
	// UsageReportPending returns the usage deltas the billing flusher has
	// not yet reported: rows where used exceeds the reported watermark.
	UsageReportPending(limit int) ([]UsageReportRow, error)
	// MarkUsageReported advances the reported watermark by amount, clamped
	// to used — over-marks (double flush, racing claims) never push the
	// watermark past the counter.
	MarkUsageReported(orgID string, window time.Time, amount int64) error
	// Usage reads the org's counter for window; absent rows read 0.
	Usage(orgID string, window time.Time) (int64, error)
	// FlushAuditOutbox relays queued audit events into the audit table.
	// Postgres queues events that fail the direct write so a transient
	// outage cannot lose them; stores without an outbox report 0.
	FlushAuditOutbox(limit int) (int, error)

	// Sweep deletes terminally-expired rows (sessions past expiry or revoked,
	// grants and approvals past expiry) older than the cutoff. It keeps the
	// hot-path indexes bounded; expiry semantics are unaffected because
	// expired rows are already invisible to authorization queries.
	Sweep(olderThan time.Time) (SweepReport, error)
	Close() error
}
