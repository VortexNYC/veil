// Package broker is the only path that ever sees a secret.
//
// Same inject-at-the-edge pattern as Infisical Agent Vault: the agent calls
// the real URL (or asks us to); we attach the credential at the edge and
// return the upstream result. The agent-visible UseResult is scrubbed.
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"

	"github.com/VortexNYC/veil/internal/audit"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
	"github.com/VortexNYC/veil/internal/store"
)

var (
	ErrOverloaded   = errors.New("broker: origin overloaded")
	ErrUnauthorized = errors.New("broker: unauthorized")
)

const defaultAuditTimeout = 500 * time.Millisecond

// requestTTL bounds an approval ask. An owner answering within the hour
// resolves it; past it the next Use re-files — and re-notifies.
const requestTTL = time.Hour

type Clock func() time.Time

type Broker struct {
	Store        store.Store
	Auditor      audit.Auditor
	AuditTimeout time.Duration
	HTTP         *http.Client
	Now          Clock
	// OnRequestFiled fires once per newly filed approval request — the app
	// wires owner notification here. Deduped asks do not re-fire.
	OnRequestFiled func(context.Context, protocol.ApprovalRequest)
	// FreeUseCap is the per-org per-window use allowance on the free plan —
	// the Paper-style gate (VEIL-60). 0 disables metering entirely.
	FreeUseCap int64
	// AccessCheck is the VEIL-62 billing backstop: on a capped deny it asks
	// Vortex whether the org's entitlement is live — a definitive answer
	// heals a plan a missed webhook left stale. Caching lives inside the
	// implementation; the deny path is the only caller, and any error inside
	// the check resolves to false (the local denial stands).
	AccessCheck func(ctx context.Context, orgID, customerID string) bool
	useLimit    *semaphore.Weighted
}

func New(s store.Store) *Broker {
	return newBroker(s, nil, 0)
}

// NewWithInFlight creates a Broker that allows at most n concurrent Use calls.
// n <= 0 means unlimited.
func NewWithInFlight(s store.Store, n int) *Broker {
	return newBroker(s, nil, n)
}

func newBroker(s store.Store, a audit.Auditor, inFlight int) *Broker {
	// Clone the default transport so outbound connections to the same upstream
	// are reused instead of churned. The default MaxIdleConnsPerHost is only 2.
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 100
	if a == nil {
		a = &audit.Sync{Store: s}
	}
	b := &Broker{
		Store:        s,
		Auditor:      a,
		AuditTimeout: defaultAuditTimeout,
		Now:          time.Now,
		HTTP: &http.Client{
			Timeout:   15 * time.Second,
			Transport: t,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				prev := via[len(via)-1].URL.Hostname()
				if req.URL.Hostname() != prev {
					req.Header.Del("Authorization")
					req.Header.Del(material.HeaderTOTP)
				}
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
	}
	if inFlight > 0 {
		b.useLimit = semaphore.NewWeighted(int64(inFlight))
	}
	return b
}

func (b *Broker) now() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}

func (b *Broker) appendAudit(ctx context.Context, e protocol.AuditEvent) error {
	timeout := b.AuditTimeout
	if timeout <= 0 {
		timeout = defaultAuditTimeout
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	err := b.Auditor.Append(auditCtx, e)
	if err != nil {
		if audit.IsDropped(err) {
			slog.Warn("audit event dropped", "error", err, "agent", e.AgentID, "item", e.ItemID)
		} else {
			slog.Error("audit append failed", "error", err, "agent", e.AgentID, "item", e.ItemID)
		}
	}
	return err
}

func (b *Broker) client() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return http.DefaultClient
}

// Use performs an action for an agent. It never puts a secret on UseResult.
func (b *Broker) Use(ctx context.Context, agent protocol.Principal, req protocol.UseRequest) (protocol.UseResult, error) {
	ctx, span := otel.Tracer("veil").Start(ctx, "use")
	defer span.End()
	now := b.now()

	// Admission: bound in-flight Use calls so a flood cannot hold unlimited
	// goroutines and upstream connections.
	if b.useLimit != nil {
		if !b.useLimit.TryAcquire(1) {
			span.SetStatus(codes.Error, "origin_overload")
			return protocol.UseResult{}, ErrOverloaded
		}
		defer b.useLimit.Release(1)
	}

	auth, err := b.Store.UseAuth(agent.ID, req.ItemID, now)
	if err != nil {
		return protocol.UseResult{}, err
	}
	return b.useAuthorized(ctx, span, agent, req, auth, nil, now)
}

func (b *Broker) UseSession(ctx context.Context, sessionHash []byte, req protocol.UseRequest) (protocol.UseResult, error) {
	ctx, span := otel.Tracer("veil").Start(ctx, "use")
	defer span.End()
	now := b.now()

	if b.useLimit != nil {
		if !b.useLimit.TryAcquire(1) {
			span.SetStatus(codes.Error, "origin_overload")
			return protocol.UseResult{}, ErrOverloaded
		}
		defer b.useLimit.Release(1)
	}

	auth, err := b.Store.UseAuthSession(sessionHash, req.ItemID, now)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrSessionExpired) || errors.Is(err, store.ErrSessionRevoked) || errors.Is(err, store.ErrDenied) {
			return protocol.UseResult{}, ErrUnauthorized
		}
		return protocol.UseResult{}, fmt.Errorf("useauth: %w", err)
	}
	if auth.Agent.ID == "" {
		return protocol.UseResult{}, ErrUnauthorized
	}
	return b.useAuthorized(ctx, span, auth.Agent, req, auth, sessionHash, now)
}

func (b *Broker) useAuthorized(ctx context.Context, span trace.Span, agent protocol.Principal, req protocol.UseRequest, auth store.UseAuth, sessionHash []byte, now time.Time) (protocol.UseResult, error) {
	// Tests may pass a bare principal with no store entry; do not fail those.
	// A real store error fails closed above. Unknown agents fall through to
	// grant evaluation, which will deny as no_grant.
	if auth.Agent.ID != "" {
		agent = auth.Agent
	}

	if auth.Item.ID == "" {
		dec := protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "item_not_found"}
		evt := protocol.AuditEvent{
			Time: now, OrgID: agent.OrgID, AgentID: agent.ID, ItemID: req.ItemID,
			Action: req.Action, Decision: dec.Decision, Reason: dec.Reason,
		}
		auditErr := b.appendAudit(ctx, evt)
		LogEvent(evt, req.ItemID, "", 0)
		spanUse(span, agent.ID, req.ItemID, dec, 0, "")
		if auditErr != nil {
			span.RecordError(auditErr)
			span.SetStatus(codes.Error, "audit_append_failed")
		}
		return dec, nil
	}

	item := auth.Item
	target := ""
	if req.Fetch != nil {
		target = req.Fetch.URL
	}
	dec := grant.Evaluate(grant.Input{
		Principal: agent,
		Item:      item,
		Grant:     auth.Grant,
		Action:    req.Action,
		TargetURL: target,
		Approval:  auth.Approval,
		Now:       now,
	})

	// For agent-token Use calls, reload the agent immediately before touching
	// the secret to catch a revocation that happened during grant/approval
	// lookups. For session-token calls, ConsumeSession below provides the same
	// final revocation check and consumes the session use atomically.
	if sessionHash == nil {
		if current, err := b.Store.Agent(agent.ID); err == nil {
			agent = current
		} else if !errors.Is(err, store.ErrNotFound) {
			return protocol.UseResult{}, err
		}
		if agent.RevokedAt != nil {
			dec = protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "agent_revoked"}
		}
	}

	if dec.Decision != protocol.DecisionAllow {
		if dec.Decision == protocol.DecisionNeedApproval && auth.Grant != nil {
			dec = b.fileRequest(ctx, agent, item, auth.Grant, req.Action, dec)
		}
		return b.auditUse(ctx, span, agent, item, req, dec, target, 0, now), nil
	}
	if !item.Kind.Injects() {
		dec = protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "not_injectable"}
		return b.auditUse(ctx, span, agent, item, req, dec, target, 0, now), nil
	}
	if req.Action != protocol.ActionFetch || req.Fetch == nil {
		dec = protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "unsupported_action"}
		return b.auditUse(ctx, span, agent, item, req, dec, target, 0, now), nil
	}

	// Free-tier gate (VEIL-60): an authorized use claims a unit on the org's
	// window counter before the secret is released. Over cap → deny
	// payment_required, audited like every other decision. Denials and
	// need_approval asks never consume.
	ok, err := b.claimUse(ctx, agent.OrgID, now)
	if err != nil {
		return protocol.UseResult{}, err
	}
	if !ok {
		dec = protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "payment_required"}
		return b.auditUse(ctx, span, agent, item, req, dec, target, 0, now), nil
	}

	// The audit row commits before the secret is released: session-token
	// calls write it inside the ConsumeSession transaction, agent-token calls
	// write it synchronously here. A failed audit write fails the request
	// closed — no credential ever leaves the origin unaudited.
	event := protocol.AuditEvent{
		Time: now, OrgID: agent.OrgID, AgentID: agent.ID, ItemID: req.ItemID,
		Action: req.Action, Decision: dec.Decision, ApprovalID: dec.ApprovalID,
	}
	if sessionHash != nil {
		current, err := b.Store.ConsumeSessionAudited(sessionHash, now, event)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrSessionExpired) || errors.Is(err, store.ErrSessionRevoked) || errors.Is(err, store.ErrDenied) {
				dec = protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "session_revoked"}
				return b.auditUse(ctx, span, agent, item, req, dec, target, 0, now), nil
			}
			return protocol.UseResult{}, fmt.Errorf("consume: %w", err)
		}
		agent = current
		event.OrgID, event.AgentID = agent.OrgID, agent.ID
	} else if err := b.appendAudit(ctx, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "audit_append_failed")
		return protocol.UseResult{}, fmt.Errorf("audit: %w", err)
	}

	secret, err := b.Store.Secret(item.ID)
	if err != nil {
		return protocol.UseResult{}, fmt.Errorf("secret: %w", err)
	}
	env := material.Unpack(secret)
	fr, code, access, err := b.fetch(ctx, req.Fetch, env, now)
	// The allow row is already durable — it records the authorization and the
	// credential release, which happened regardless of the upstream outcome.
	// Upstream status stays in logs and spans.
	if err != nil {
		dec = protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "fetch_failed"}
		LogEvent(event, item.Name, hostPath(target), 0)
		spanUse(span, agent.ID, item.Name, dec, 0, hostPath(target))
		return dec, err
	}
	status := fr.Status
	hide := material.ScrubList(env, secret, []byte(code), []byte(access))
	fr.Body = scrub.Bytes(fr.Body, hide...)
	for k, vs := range fr.Header {
		cleaned := make([]string, len(vs))
		for i, v := range vs {
			cleaned[i] = string(scrub.Bytes([]byte(v), hide...))
		}
		fr.Header[k] = cleaned
	}
	dec.Fetch = fr
	LogEvent(event, item.Name, hostPath(target), status)
	spanUse(span, agent.ID, item.Name, dec, status, hostPath(target))
	return dec, nil
}

// FileRequest turns a need_approval into the ask: one open row per
// (grant, action), deduped by the store. A fresh file audits request_filed
// and fires OnRequestFiled once; a deduped refile just returns the live ask.
func (b *Broker) FileRequest(ctx context.Context, agent protocol.Principal, item protocol.Item, g *protocol.Grant, action protocol.ActionKind) (protocol.ApprovalRequest, error) {
	now := b.now()
	reqID, err := id.NewRequest()
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	out, err := b.Store.FileRequest(protocol.ApprovalRequest{
		ID:        reqID,
		OrgID:     agent.OrgID,
		AgentID:   agent.ID,
		ItemID:    item.ID,
		GrantID:   g.ID,
		Action:    action,
		CreatedAt: now,
		ExpiresAt: now.Add(requestTTL),
	})
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	// request_expired (for the dead predecessor) and request_filed (for a
	// fresh ask) are written by the store inside the file transaction —
	// a filed ask always lands with its audit line.
	if !out.Created {
		return out.Request, nil
	}
	if b.OnRequestFiled != nil {
		b.OnRequestFiled(ctx, out.Request)
	}
	return out.Request, nil
}

// fileRequest puts the filed ask on the denial so the agent — and the human
// it reports to — can name what is pending. A filing failure keeps the
// denial; it never upgrades it.
func (b *Broker) fileRequest(ctx context.Context, agent protocol.Principal, item protocol.Item, g *protocol.Grant, action protocol.ActionKind, dec protocol.UseResult) protocol.UseResult {
	filed, err := b.FileRequest(ctx, agent, item, g, action)
	if err != nil {
		slog.Warn("approval request file failed", "grant", g.ID, "err", err)
		return dec
	}
	exp := filed.ExpiresAt
	dec.RequestID, dec.RequestExpiresAt = filed.ID, &exp
	return dec
}

func (b *Broker) auditUse(ctx context.Context, span trace.Span, agent protocol.Principal, item protocol.Item, req protocol.UseRequest, dec protocol.UseResult, target string, status int, now time.Time) protocol.UseResult {
	host := hostPath(target)
	event := protocol.AuditEvent{
		Time:       now,
		OrgID:      agent.OrgID,
		AgentID:    agent.ID,
		ItemID:     req.ItemID,
		Action:     req.Action,
		Decision:   dec.Decision,
		Reason:     dec.Reason,
		ApprovalID: dec.ApprovalID,
	}
	auditErr := b.appendAudit(ctx, event)
	LogEvent(event, item.Name, host, status)
	spanUse(span, agent.ID, item.Name, dec, status, host)
	// Audit failure is a lost security event — mark the span so it surfaces
	// in traces, not just logs. Set last so spanUse cannot overwrite it.
	if auditErr != nil {
		span.RecordError(auditErr)
		span.SetStatus(codes.Error, "audit_append_failed")
	}
	return dec
}

func (b *Broker) fetch(ctx context.Context, f *protocol.Fetch, env material.Envelope, now time.Time) (*protocol.FetchResult, string, string, error) {
	method := f.Method
	if method == "" {
		method = http.MethodGet
	}
	ctx, span := otel.Tracer("veil").Start(ctx, "upstream")
	defer span.End()
	span.SetAttributes(
		attribute.String("http.request.method", method),
		attribute.String("veil.host", hostPath(f.URL)),
	)
	var body io.Reader
	if len(f.Body) > 0 {
		body = bytes.NewReader(f.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.URL, body)
	if err != nil {
		return nil, "", "", err
	}
	for k, vs := range f.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	access, err := material.AccessToken(ctx, env, b.client())
	if err != nil {
		return nil, "", "", err
	}
	if access != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", material.AuthorizationValue(access))
	}
	code, err := material.Apply(req.Header, env, now)
	if err != nil {
		return nil, "", "", err
	}
	res, err := b.client().Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "upstream")
		return nil, "", "", err
	}
	defer res.Body.Close()
	span.SetAttributes(attribute.Int("http.response.status_code", res.StatusCode))
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, "", "", err
	}
	return &protocol.FetchResult{
		Status: res.StatusCode,
		Header: res.Header.Clone(),
		Body:   raw,
	}, code, access, nil
}

// Approve records a human approval for a level-1 grant — approval insert
// and ask resolution commit together, so a grant can never be approved
// while its asks still read open.
// Membership is Keto via App.ApproveOIDC. This store does not decide who is a human.
func (b *Broker) Approve(human protocol.Principal, grantID string, ttl time.Duration) (protocol.Approval, error) {
	if human.Kind != protocol.PrincipalHuman {
		return protocol.Approval{}, store.ErrDenied
	}
	g, err := b.Store.Grant(grantID)
	if err != nil {
		return protocol.Approval{}, err
	}
	if human.OrgID != "" && g.OrgID != human.OrgID {
		return protocol.Approval{}, store.ErrDenied
	}
	apprID, err := id.NewApproval()
	if err != nil {
		return protocol.Approval{}, err
	}
	a := protocol.Approval{
		ID:        apprID,
		GrantID:   grantID,
		HumanID:   human.ID,
		ExpiresAt: b.now().Add(ttl),
	}
	resolved, err := b.Store.ApproveGrant(grantID, a, b.now())
	if err != nil {
		return protocol.Approval{}, err
	}
	// request_approved events for every resolved ask are written by the
	// store inside the approval transaction.
	slog.Info("approve", "human", human.ID, "grant", grantID, "resolved", len(resolved))
	return a, nil
}

// ApproveRequest resolves ONE pending ask: the minted approval lands only
// while the ask is still open and its grant still live — a lost race
// (another owner resolved it, it expired, the grant died) writes nothing
// and returns ErrRequestResolved.
func (b *Broker) ApproveRequest(human protocol.Principal, reqID string, ttl time.Duration) (protocol.ApprovalRequest, error) {
	if human.Kind != protocol.PrincipalHuman {
		return protocol.ApprovalRequest{}, store.ErrDenied
	}
	req, err := b.Store.Request(reqID)
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	if human.OrgID != "" && req.OrgID != human.OrgID {
		return protocol.ApprovalRequest{}, store.ErrDenied
	}
	apprID, err := id.NewApproval()
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	a := protocol.Approval{
		ID:        apprID,
		GrantID:   req.GrantID,
		HumanID:   human.ID,
		ExpiresAt: b.now().Add(ttl),
	}
	resolved, won, err := b.Store.ApproveRequest(reqID, a, b.now())
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	if !won {
		return protocol.ApprovalRequest{}, store.ErrRequestResolved
	}
	// request_approved events for the target and every sibling are written
	// by the store inside the resolution transaction.
	slog.Info("approve", "human", human.ID, "request", reqID)
	return resolved[0], nil
}

// LogEvent writes a grant event to slog. No secrets, no query string, no body.
// HTTP request logs are Railway (`railway logs --http`). This is the grant line.
func LogEvent(e protocol.AuditEvent, item, host string, status int) {
	attrs := []any{
		"agent", e.AgentID,
		"item", item,
		"action", string(e.Action),
		"decision", string(e.Decision),
	}
	if e.Reason != "" {
		attrs = append(attrs, "reason", e.Reason)
	}
	if host != "" {
		if h := hostPath(host); h != "" {
			host = h
		}
		attrs = append(attrs, "host", host)
	}
	if status > 0 {
		attrs = append(attrs, "status", status)
	}
	slog.Info("use", attrs...)
}

// MonthWindow is the UTC calendar month containing t — the billing window a
// use claims against.
func MonthWindow(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// claimUse meters one authorized use for the org. Free plans are capped at
// FreeUseCap per window; paid plans accrue usage uncapped. Metering is off
// when FreeUseCap <= 0. Store errors fail closed like every other store
// error on the Use path.
func (b *Broker) claimUse(ctx context.Context, orgID string, now time.Time) (bool, error) {
	if b.FreeUseCap <= 0 {
		return true, nil
	}
	ob, err := b.Store.Billing(orgID)
	if err != nil {
		return false, fmt.Errorf("billing: %w", err)
	}
	var cap int64
	if ob.Plan == "" || ob.Plan == "free" {
		cap = b.FreeUseCap
	}
	used, ok, err := b.Store.ConsumeUse(orgID, MonthWindow(now), cap)
	if err != nil {
		return false, fmt.Errorf("meter: %w", err)
	}
	if ok {
		return true, nil
	}
	// Over cap on a free plan — before denying, ask the access backstop
	// whether Vortex knows a webhook missed (VEIL-62). A definitive allow
	// heals the local plan row so the next claim never re-checks; the
	// webhook remains the durable truth and converges it afterward.
	if b.AccessCheck == nil {
		return false, nil
	}
	customerID := ob.CustomerID
	if customerID == "" {
		customerID = orgID // Veil provisions customer ids caller-chosen = org id
	}
	if !b.AccessCheck(ctx, orgID, customerID) {
		return false, nil
	}
	slog.Info("billing access check healed plan", "org", orgID, "used", used)
	heal := ob
	heal.Plan = "active"
	heal.UpdatedAt = now.UTC()
	if err := b.Store.SetBilling(heal); err != nil {
		slog.Warn("access-check heal write failed", "org", orgID, "err", err)
	}
	// Best-effort heal audit — a failed append must not deny an entitled org.
	_ = b.appendAudit(ctx, protocol.AuditEvent{
		Time: now, OrgID: orgID, AgentID: "vortex-access-check",
		Action: protocol.ActionBillingPlanChanged, Decision: protocol.DecisionAllow,
		Reason: "access_check:active",
	})
	return true, nil
}

// hostPath reduces an upstream URL to scheme://host for the veil.host span
// attribute. The path stays out: upstream paths can carry embedded
// credentials (webhook tokens, signed path segments), and telemetry must
// never be a place a secret can leak to.
func hostPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func spanUse(span trace.Span, agent, item string, dec protocol.UseResult, status int, host string) {
	span.SetAttributes(
		attribute.String("veil.agent", agent),
		attribute.String("veil.item", item),
		attribute.String("veil.decision", string(dec.Decision)),
	)
	if dec.Reason != "" {
		span.SetAttributes(attribute.String("veil.reason", dec.Reason))
	}
	if host != "" {
		span.SetAttributes(attribute.String("veil.host", host))
	}
	if status > 0 {
		span.SetAttributes(attribute.Int("http.response.status_code", status))
	}
	if dec.Decision != protocol.DecisionAllow {
		span.SetStatus(codes.Error, dec.Reason)
	}
}

// AssertNoSecret marshals v and fails the test helper contract if secret appears.
func AssertNoSecret(v any, secret []byte) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if scrub.Contains(b, secret) {
		return fmt.Errorf("secret leaked in %s", b)
	}
	return nil
}
