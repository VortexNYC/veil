package app

// Member recovery (VEIL-78): per-member nacl/box wrapped org-key shares and
// the initiator → confirmer ceremony. The store owns the crypto and the
// atomic commit; this file owns the AuthZ: who may enroll, start, view,
// confirm, or deny, and which windows apply. No secret ever rides these
// types — pubkeys and box.SealAnonymous ciphertexts only.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

const (
	// A confirmed path unlocks immediately and lapses in a day.
	recoveryExpiry = 24 * time.Hour
	// The sole-owner self-recovery timelock: a full day of deny window,
	// then a second day before the ask lapses.
	recoverySoloLock   = 24 * time.Hour
	recoverySoloExpiry = 48 * time.Hour
)

// RecoveryShareView is the member's enrollment state — pubkey and the
// sealed blob only; the private key and the plaintext master never ride it.
type RecoveryShareView struct {
	Enrolled        bool       `json:"enrolled"`
	Pubkey          []byte     `json:"pubkey,omitempty"`
	WrappedMaster   []byte     `json:"wrapped_master,omitempty"`
	WrapPubkeyEpoch int        `json:"wrap_pubkey_epoch"`
	OrgKeyVersion   int        `json:"org_key_version"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// RecoveryView is the confirmer-facing request — the row plus the member's
// resolved email and the current wrap epoch. Owners also get their own
// share's wrapped_master so the CLI can compute a proof_hmac without a
// second round trip. The member leg gets the bare row.
type RecoveryView struct {
	protocol.RecoveryRequest
	HumanEmail             string `json:"human_email,omitempty"`
	CurrentEpoch           int    `json:"current_epoch"`
	ConfirmerWrappedMaster []byte `json:"confirmer_wrapped_master,omitempty"`
}

// StartRecoveryInput is POST /v1/recovery/start — an empty HumanID means
// self-recovery; a named one is owner-initiated.
type StartRecoveryInput struct {
	HumanID   string
	NewPubkey []byte
	ResetMFA  bool
}

// requireMember resolves a provisioned human principal — the base leg every
// recovery verb shares (agent tokens and unprovisioned signups fail closed).
func (a *App) requireMember(ctx context.Context, rawToken string) (protocol.Principal, error) {
	p, err := a.PrincipalFromOIDC(ctx, rawToken)
	if err != nil {
		// A Keto/Hydra outage is a bad gateway, not a bad credential.
		if errors.Is(err, ErrIdentityUnavailable) {
			return protocol.Principal{}, err
		}
		return protocol.Principal{}, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	if p.Kind != protocol.PrincipalHuman || a.Members == nil {
		return protocol.Principal{}, ErrForbidden
	}
	return p, nil
}

// EnrollRecoveryShare plants the member's first org-key share — aal2 only
// and insert-if-absent: a pubkey never rotates outside the ceremony.
// created=false means the member was already enrolled (the API maps it to
// 409) and the returned share is the committed row.
func (a *App) EnrollRecoveryShare(ctx context.Context, rawToken string, pubkey []byte) (protocol.OrgKeyShare, bool, error) {
	p, err := a.requireMember(ctx, rawToken)
	if err != nil {
		return protocol.OrgKeyShare{}, false, err
	}
	if err := human.RequireMFA(rawToken); err != nil {
		return protocol.OrgKeyShare{}, false, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	if len(pubkey) != device.PublicSize {
		return protocol.OrgKeyShare{}, false, fmt.Errorf("app: recovery pubkey must be %d bytes", device.PublicSize)
	}
	return a.Store.EnrollOrgKeyShare(ctx, p.OrgID, p.ID, pubkey,
		adminEvent(p.ID, p.OrgID, "", protocol.ActionRecoveryShareEnrolled, "epoch=1"))
}

// RealignRecoveryCustodian re-wraps the member's org-key share under a new
// phone custodian pubkey without rotating the org master — aal2 only.
func (a *App) RealignRecoveryCustodian(ctx context.Context, rawToken string, pubkey []byte) (protocol.OrgKeyShare, error) {
	p, err := a.requireMember(ctx, rawToken)
	if err != nil {
		return protocol.OrgKeyShare{}, err
	}
	if err := human.RequireMFA(rawToken); err != nil {
		return protocol.OrgKeyShare{}, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	if len(pubkey) != device.PublicSize {
		return protocol.OrgKeyShare{}, fmt.Errorf("app: recovery pubkey must be %d bytes", device.PublicSize)
	}
	return a.Store.RealignOrgKeyShareCustodian(ctx, p.OrgID, p.ID, pubkey,
		adminEvent(p.ID, p.OrgID, "", protocol.ActionRecoveryCustodianRealigned, ""))
}

// GetRecoveryShare returns the caller's own share — enrolled:false when the
// member never enrolled. Any live member may read their own row; the blob
// is ciphertext only.
func (a *App) GetRecoveryShare(ctx context.Context, rawToken string) (RecoveryShareView, error) {
	p, err := a.requireMember(ctx, rawToken)
	if err != nil {
		return RecoveryShareView{}, err
	}
	share, err := a.Store.OrgKeyShare(ctx, p.OrgID, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		return RecoveryShareView{Enrolled: false}, nil
	}
	if err != nil {
		return RecoveryShareView{}, err
	}
	ca, ua := share.CreatedAt, share.UpdatedAt
	return RecoveryShareView{
		Enrolled:        true,
		Pubkey:          share.Pubkey,
		WrappedMaster:   share.WrappedMaster,
		WrapPubkeyEpoch: share.WrapPubkeyEpoch,
		OrgKeyVersion:   share.OrgKeyVersion,
		CreatedAt:       &ca,
		UpdatedAt:       &ua,
	}, nil
}

// StartRecovery files (or returns the existing open) recovery request.
// Self-start carries no RequireMFA gate — the locked-out member may hold
// only an aal1 token; owner-initiated starts require aal2 + owner + a live
// target membership. created=false means an open ask already exists and
// notifications are suppressed.
func (a *App) StartRecovery(ctx context.Context, rawToken string, in StartRecoveryInput) (protocol.RecoveryRequest, bool, error) {
	p, err := a.requireMember(ctx, rawToken)
	if err != nil {
		return protocol.RecoveryRequest{}, false, err
	}
	target := strings.TrimSpace(in.HumanID)
	if target == "" {
		target = p.ID
	}
	if target != p.ID {
		// Owner-initiated: the confirmer-in-waiting must be aal2 and own the
		// org, and the target must be a live member of it.
		if err := human.RequireMFA(rawToken); err != nil {
			return protocol.RecoveryRequest{}, false, fmt.Errorf("%w: %v", ErrForbidden, err)
		}
		owner, err := a.Members.IsOwner(ctx, p.OrgID, p.ID)
		if err != nil {
			return protocol.RecoveryRequest{}, false, err
		}
		if !owner {
			return protocol.RecoveryRequest{}, false, ErrForbidden
		}
		member, err := a.Members.IsMember(ctx, p.OrgID, target)
		if err != nil {
			return protocol.RecoveryRequest{}, false, err
		}
		if !member {
			return protocol.RecoveryRequest{}, false, store.ErrNotFound
		}
	}
	if len(in.NewPubkey) != device.PublicSize {
		return protocol.RecoveryRequest{}, false, fmt.Errorf("app: recovery pubkey must be %d bytes", device.PublicSize)
	}
	if !a.recoveryLim.allowRecovery(p.ID, p.OrgID) {
		return protocol.RecoveryRequest{}, false, ErrRecoveryLimit
	}
	// The timelock exists for exactly one shape: a sole owner recovering
	// themselves with no second confirmer. Every other path is
	// owner-confirmed and unlocks immediately.
	var owners []string
	if a.OrgAdmin != nil {
		owners, err = a.OrgAdmin.ListOwners(ctx, p.OrgID)
		if err != nil {
			return protocol.RecoveryRequest{}, false, err
		}
	}
	now := time.Now().UTC()
	unlock := now
	expires := now.Add(recoveryExpiry)
	if len(owners) == 1 && owners[0] == target {
		unlock = now.Add(recoverySoloLock)
		expires = now.Add(recoverySoloExpiry)
	}
	reqID, err := id.NewRecovery()
	if err != nil {
		return protocol.RecoveryRequest{}, false, err
	}
	req := protocol.RecoveryRequest{
		ID: reqID, OrgID: p.OrgID, HumanID: target, InitiatedBy: p.ID,
		NewPubkey: in.NewPubkey, ResetMFA: in.ResetMFA, Status: protocol.RecoveryOpen,
		CreatedAt: now, UnlockAt: unlock, ExpiresAt: expires,
	}
	ev := adminEvent(p.ID, p.OrgID, reqID, protocol.ActionRecoveryStarted,
		fmt.Sprintf("human=%s reset_mfa=%t", target, in.ResetMFA))
	ev.Decision = protocol.DecisionNeedApproval
	filed, created, err := a.Store.FileRecoveryRequest(ctx, req, ev)
	if err != nil {
		return protocol.RecoveryRequest{}, false, err
	}
	if created {
		a.notifyRecovery(filed, "started")
	}
	return filed, created, nil
}

// recoveryView renders the owner-facing request: member email resolved via
// the directory, the member's current wrap epoch, and the confirmer's own
// share blob for the proof_hmac fast path. All best-effort enrichment — a
// directory outage never hides the request row.
func (a *App) recoveryView(ctx context.Context, owner protocol.Principal, req protocol.RecoveryRequest) RecoveryView {
	v := RecoveryView{RecoveryRequest: req}
	if a.Directory != nil {
		if email, err := a.Directory.Email(ctx, req.HumanID); err == nil {
			v.HumanEmail = email
		}
	}
	if share, err := a.Store.OrgKeyShare(ctx, req.OrgID, req.HumanID); err == nil {
		v.CurrentEpoch = share.WrapPubkeyEpoch
	}
	if share, err := a.Store.OrgKeyShare(ctx, owner.OrgID, owner.ID); err == nil {
		v.ConfirmerWrappedMaster = share.WrappedMaster
	}
	return v
}

// GetRecovery returns one request. Owners see the full confirmer view
// (aal2); the initiator or the target member sees the bare row.
func (a *App) GetRecovery(ctx context.Context, rawToken, reqID string) (RecoveryView, error) {
	p, err := a.requireMember(ctx, rawToken)
	if err != nil {
		return RecoveryView{}, err
	}
	req, err := a.Store.RecoveryRequest(ctx, reqID)
	if err != nil || req.OrgID != p.OrgID {
		return RecoveryView{}, store.ErrNotFound
	}
	owner, err := a.Members.IsOwner(ctx, p.OrgID, p.ID)
	if err != nil {
		return RecoveryView{}, err
	}
	if !owner {
		if req.HumanID != p.ID && req.InitiatedBy != p.ID {
			return RecoveryView{}, ErrForbidden
		}
		return RecoveryView{RecoveryRequest: req}, nil
	}
	if err := human.RequireMFA(rawToken); err != nil {
		return RecoveryView{}, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	return a.recoveryView(ctx, p, req), nil
}

// ListRecoveries lists the org's requests by status — owner-only, aal2.
func (a *App) ListRecoveries(ctx context.Context, rawToken string, status protocol.RecoveryStatus) ([]RecoveryView, error) {
	p, err := a.requireOwner(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	if err := human.RequireMFA(rawToken); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	reqs, err := a.Store.ListRecoveryRequests(ctx, p.OrgID, status)
	if err != nil {
		return nil, err
	}
	out := make([]RecoveryView, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, a.recoveryView(ctx, p, req))
	}
	return out, nil
}

// CompleteRecovery is the confirmer's commit: owner + aal2, a live target
// membership re-check, two-person integrity on multi-owner orgs, then the
// store's atomic rotate-and-resolve. Post-commit, a requested MFA reset
// fires through the identity plane — best-effort, the vault-side commit
// already landed.
func (a *App) CompleteRecovery(ctx context.Context, rawToken, reqID string, proofHMAC []byte) (RecoveryView, error) {
	p, err := a.requireOwner(ctx, rawToken)
	if err != nil {
		return RecoveryView{}, err
	}
	if err := human.RequireMFA(rawToken); err != nil {
		return RecoveryView{}, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	req, err := a.Store.RecoveryRequest(ctx, reqID)
	if err != nil || req.OrgID != p.OrgID {
		return RecoveryView{}, store.ErrNotFound
	}
	// Offboard abort: a member removed since filing must not complete —
	// cancel the stale ask so nothing hangs open for a ghost.
	member, err := a.Members.IsMember(ctx, req.OrgID, req.HumanID)
	if err != nil {
		return RecoveryView{}, err
	}
	if !member {
		_, _ = a.Store.DenyRecovery(ctx, req.ID, p.ID, protocol.RecoveryCancelled)
		return RecoveryView{}, store.ErrNotFound
	}
	if a.OrgAdmin == nil {
		return RecoveryView{}, fmt.Errorf("app: org admin not configured")
	}
	owners, err := a.OrgAdmin.ListOwners(ctx, p.OrgID)
	if err != nil {
		return RecoveryView{}, err
	}
	if len(owners) >= 2 {
		// Strict two-person integrity: an owner never confirms their own
		// recovery, and an owner who initiated for someone else never
		// confirms it either — a second owner must be the confirmer.
		if p.ID == req.HumanID || (req.InitiatedBy != req.HumanID && p.ID == req.InitiatedBy) {
			return RecoveryView{}, ErrForbidden
		}
	}
	done, err := a.Store.CompleteRecovery(ctx, p.OrgID, reqID, p.ID, proofHMAC)
	if err != nil {
		return RecoveryView{}, err
	}
	if done.ResetMFA {
		if err := a.OrgAdmin.ResetMemberMFA(ctx, p.OrgID, done.HumanID); err != nil {
			// The vault-side commit already landed; a Kratos outage leaves
			// the member's factors wiped-or-not but never re-exposes the
			// request. Log loud — retrying here cannot undo the rotation.
			slog.Warn("mfa reset after recovery failed", "request", done.ID, "human", done.HumanID, "err", err)
		}
	}
	a.notifyRecovery(done, "completed")
	return a.recoveryView(ctx, p, done), nil
}

// DenyRecovery resolves an open request: an owner denies (aal2), the
// initiator or the target member cancels (any aal — the locked-out member
// may hold only aal1). Terminal statuses are first-write-wins in the store.
func (a *App) DenyRecovery(ctx context.Context, rawToken, reqID string) (protocol.RecoveryRequest, error) {
	p, err := a.requireMember(ctx, rawToken)
	if err != nil {
		return protocol.RecoveryRequest{}, err
	}
	req, err := a.Store.RecoveryRequest(ctx, reqID)
	if err != nil || req.OrgID != p.OrgID {
		return protocol.RecoveryRequest{}, store.ErrNotFound
	}
	owner, err := a.Members.IsOwner(ctx, p.OrgID, p.ID)
	if err != nil {
		return protocol.RecoveryRequest{}, err
	}
	status := protocol.RecoveryCancelled
	if owner {
		if err := human.RequireMFA(rawToken); err != nil {
			return protocol.RecoveryRequest{}, fmt.Errorf("%w: %v", ErrForbidden, err)
		}
		status = protocol.RecoveryDenied
	} else if req.HumanID != p.ID && req.InitiatedBy != p.ID {
		return protocol.RecoveryRequest{}, ErrForbidden
	}
	return a.Store.DenyRecovery(ctx, reqID, p.ID, status)
}

// notifyRecovery fans a lifecycle event to the mail channel (owners + the
// member) and to registered devices — detached like OnRequestFiled: remote
// hops never sit on the caller's path, and the row is the durable part.
func (a *App) notifyRecovery(req protocol.RecoveryRequest, event string) {
	send := func(n RecoveryNotify) {
		go func() {
			dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := n.NotifyRecovery(dctx, req, event); err != nil {
				slog.Warn("recovery notify failed", "request", req.ID, "event", event, "err", err)
			}
		}()
	}
	if n, ok := a.Notify.(RecoveryNotify); ok && n != nil {
		send(n)
	}
	if n, ok := a.Push.(RecoveryNotify); ok && n != nil {
		send(n)
	}
}
