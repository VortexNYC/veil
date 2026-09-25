package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

type Memory struct {
	mu        sync.Mutex
	reqBus    *requestBus
	agents    map[string]protocol.Principal
	humans    map[string]protocol.Principal
	items     map[string]protocol.Item
	secrets   map[string]Secret
	grants    map[string]protocol.Grant // key: agentID+"\x00"+itemID
	approvals map[string]protocol.Approval
	requests  map[string]protocol.ApprovalRequest
	audit     []protocol.AuditEvent
	workloads map[string]protocol.Workload // key: issuer+"\x00"+subject
	sessions  map[string]protocol.Session  // key: hex(secret_hash)
	versions  []protocol.ItemVersion
	verSecret map[int64]Secret
	nextVer   int64
	billing   map[string]OrgBilling // key: org_id
	usage     map[string]int64      // key: org_id+"\x00"+window unix
}

func NewMemory() *Memory {
	return &Memory{
		reqBus:    newRequestBus(),
		agents:    map[string]protocol.Principal{},
		humans:    map[string]protocol.Principal{},
		items:     map[string]protocol.Item{},
		secrets:   map[string]Secret{},
		grants:    map[string]protocol.Grant{},
		approvals: map[string]protocol.Approval{},
		requests:  map[string]protocol.ApprovalRequest{},
		workloads: map[string]protocol.Workload{},
		sessions:  map[string]protocol.Session{},
		verSecret: map[int64]Secret{},
		billing:   map[string]OrgBilling{},
		usage:     map[string]int64{},
	}
}

func grantKey(agentID, itemID string) string { return agentID + "\x00" + itemID }

func (m *Memory) Close() error { return nil }

func (m *Memory) PutAgent(p protocol.Principal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.agents[p.ID]; ok {
		// Never reassign org/owner on conflict — only revocation merges.
		p.OrgID = existing.OrgID
		p.Owner = existing.Owner
		if p.RevokedAt == nil {
			p.RevokedAt = existing.RevokedAt
		}
	}
	m.agents[p.ID] = p
	return nil
}

func (m *Memory) ListAgents() ([]protocol.Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.Principal, 0, len(m.agents))
	for _, p := range m.agents {
		out = append(out, p)
	}
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func (m *Memory) Agent(id string) (protocol.Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.agents[id]
	if !ok {
		return protocol.Principal{}, ErrNotFound
	}
	return p, nil
}

func (m *Memory) RevokeAgent(id string, at time.Time, audit ...protocol.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.agents[id]
	if !ok {
		return ErrNotFound
	}
	if p.RevokedAt == nil {
		t := at.UTC()
		p.RevokedAt = &t
		m.agents[id] = p
	}
	m.audit = append(m.audit, audit...)
	return nil
}

func (m *Memory) PutHuman(p protocol.Principal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.humans[p.ID] = p
	return nil
}

func (m *Memory) PlantHuman(p protocol.Principal) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.humans[p.ID]; ok {
		return false, nil
	}
	m.humans[p.ID] = p
	return true, nil
}

// Memory holds plaintext and has no key hierarchy — org keys are provisioned.
func (m *Memory) EnsureOrgKey(context.Context, string, []byte) error { return nil }
func (m *Memory) HasOrgKey(context.Context, string) (bool, error)    { return true, nil }
func (m *Memory) RotateOrgKey(context.Context, string) error         { return ErrUnsupported }
func (m *Memory) RotateKEK(context.Context, []byte) error            { return ErrUnsupported }
func (m *Memory) StoreRecoveryWrap(context.Context, string, protocol.Owner, []byte, time.Time) error {
	return ErrUnsupported
}
func (m *Memory) OpenRecoveryWrap(context.Context, string, protocol.Owner, []byte) ([]byte, error) {
	return nil, ErrUnsupported
}
func (m *Memory) ReseedOrgKey(context.Context, string, []byte) error { return ErrUnsupported }
func (m *Memory) RecoverOrgKey(context.Context, string, protocol.Owner, []byte) error {
	return ErrUnsupported
}

func (m *Memory) Human(id string) (protocol.Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.humans[id]
	if !ok {
		return protocol.Principal{}, ErrNotFound
	}
	return p, nil
}

func (m *Memory) ListHumans() ([]protocol.Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.Principal, 0, len(m.humans))
	for _, p := range m.humans {
		out = append(out, p)
	}
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func (m *Memory) PutItem(item protocol.Item, secret Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.items[item.ID]; ok {
		if existing.Owner != item.Owner || existing.OrgID != item.OrgID {
			return fmt.Errorf("store: cannot change item owner")
		}
	}
	if old, ok := m.secrets[item.ID]; ok {
		m.nextVer++
		id := m.nextVer
		cp := make(Secret, len(old))
		copy(cp, old)
		m.verSecret[id] = cp
		m.versions = append(m.versions, protocol.ItemVersion{ID: id, ItemID: item.ID, Time: time.Now().UTC()})
	}
	m.items[item.ID] = item
	m.secrets[item.ID] = append(Secret(nil), secret...)
	return nil
}

func (m *Memory) Item(id string) (protocol.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return protocol.Item{}, ErrNotFound
	}
	return item, nil
}

func (m *Memory) ItemByName(orgID, name string) (protocol.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.items {
		if item.OrgID == orgID && item.Name == name {
			return item, nil
		}
	}
	return protocol.Item{}, ErrNotFound
}

func (m *Memory) ListItems() ([]protocol.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.Item, 0, len(m.items))
	for _, item := range m.items {
		if item.Archived {
			continue
		}
		out = append(out, item)
	}
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func (m *Memory) ArchiveItem(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	item.Archived = true
	m.items[id] = item
	return nil
}

func (m *Memory) DeleteItem(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; !ok {
		return ErrNotFound
	}
	delete(m.items, id)
	delete(m.secrets, id)
	for k, g := range m.grants {
		if g.ItemID == id {
			delete(m.grants, k)
		}
	}
	kept := m.versions[:0]
	for _, v := range m.versions {
		if v.ItemID == id {
			delete(m.verSecret, v.ID)
			continue
		}
		kept = append(kept, v)
	}
	m.versions = kept
	return nil
}

func (m *Memory) Versions(itemID string) ([]protocol.ItemVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []protocol.ItemVersion
	for _, v := range m.versions {
		if v.ItemID == itemID {
			out = append(out, v)
		}
	}
	// Bound to the newest maxListResults and keep chronological order.
	if len(out) > maxListResults {
		out = out[len(out)-maxListResults:]
	}
	return out, nil
}

func (m *Memory) RestoreVersion(itemID string, versionID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[itemID]
	if !ok {
		return ErrNotFound
	}
	sec, ok := m.verSecret[versionID]
	if !ok {
		return ErrNotFound
	}
	found := false
	for _, v := range m.versions {
		if v.ID == versionID && v.ItemID == itemID {
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}
	if old, ok := m.secrets[itemID]; ok {
		m.nextVer++
		nid := m.nextVer
		cp := make(Secret, len(old))
		copy(cp, old)
		m.verSecret[nid] = cp
		m.versions = append(m.versions, protocol.ItemVersion{ID: nid, ItemID: itemID, Time: time.Now().UTC()})
	}
	n := make(Secret, len(sec))
	copy(n, sec)
	m.secrets[itemID] = n
	m.items[itemID] = item
	return nil
}

func (m *Memory) Secret(id string) (Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.secrets[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := make(Secret, len(s))
	copy(out, s)
	return out, nil
}

func (m *Memory) PutGrant(g protocol.Grant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grants[grantKey(g.AgentID, g.ItemID)] = g
	return nil
}

func (m *Memory) Grant(id string) (*protocol.Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.grants {
		if g.ID == id {
			cp := g
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) UseAuth(agentID, itemID string, now time.Time) (UseAuth, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.useAuthLocked(agentID, itemID, now), nil
}

func (m *Memory) useAuthLocked(agentID, itemID string, now time.Time) UseAuth {
	var r UseAuth
	if a, ok := m.agents[agentID]; ok {
		r.Agent = a
	}
	if item, ok := m.items[itemID]; ok {
		r.Item = item
	}
	if g, ok := m.grants[grantKey(agentID, itemID)]; ok {
		cp := g
		r.Grant = &cp
		if a, ok := m.approvals[g.ID]; ok && now.Before(a.ExpiresAt) {
			ap := a
			r.Approval = &ap
		}
	}
	return r
}

func (m *Memory) UseAuthSession(sessionHash []byte, itemID string, now time.Time) (UseAuth, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[sessionHashKey(sessionHash)]
	if !ok {
		return UseAuth{}, ErrNotFound
	}
	if sess.RevokedAt != nil {
		return UseAuth{}, ErrSessionRevoked
	}
	if !now.Before(sess.ExpiresAt) {
		return UseAuth{}, ErrSessionExpired
	}
	if sess.MaxUses > 0 && sess.Uses >= sess.MaxUses {
		return UseAuth{}, ErrDenied
	}
	return m.useAuthLocked(sess.AgentID, itemID, now), nil
}

func (m *Memory) ConsumeSession(sessionHash []byte, now time.Time) (protocol.Principal, error) {
	return m.consumeSession(sessionHash, now, nil)
}

func (m *Memory) ConsumeSessionAudited(sessionHash []byte, now time.Time, e protocol.AuditEvent) (protocol.Principal, error) {
	return m.consumeSession(sessionHash, now, &e)
}

func (m *Memory) consumeSession(sessionHash []byte, now time.Time, e *protocol.AuditEvent) (protocol.Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[sessionHashKey(sessionHash)]
	if !ok {
		return protocol.Principal{}, ErrNotFound
	}
	if sess.RevokedAt != nil {
		return protocol.Principal{}, ErrSessionRevoked
	}
	if !now.Before(sess.ExpiresAt) {
		return protocol.Principal{}, ErrSessionExpired
	}
	if sess.MaxUses > 0 && sess.Uses >= sess.MaxUses {
		return protocol.Principal{}, ErrDenied
	}
	a, ok := m.agents[sess.AgentID]
	if !ok || a.RevokedAt != nil {
		return protocol.Principal{}, ErrNotFound
	}
	sess.Uses++
	m.sessions[sessionHashKey(sessionHash)] = sess
	if e != nil {
		e.OrgID = a.OrgID
		e.AgentID = a.ID
		m.audit = append(m.audit, *e)
	}
	return a, nil
}

func (m *Memory) GrantFor(agentID, itemID string) (*protocol.Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[grantKey(agentID, itemID)]
	if !ok {
		return nil, nil
	}
	cp := g
	return &cp, nil
}

func (m *Memory) ListGrants() ([]protocol.Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.Grant, 0, len(m.grants))
	for _, g := range m.grants {
		out = append(out, g)
	}
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func (m *Memory) PutApproval(a protocol.Approval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvals[a.GrantID] = a
	return nil
}

func (m *Memory) LiveApproval(grantID string, now time.Time) (*protocol.Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.approvals[grantID]
	if !ok {
		return nil, nil
	}
	if !now.Before(a.ExpiresAt) {
		return nil, nil
	}
	cp := a
	return &cp, nil
}

func (m *Memory) FileRequest(req protocol.ApprovalRequest) (FileOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var expiredID string
	for id, r := range m.requests {
		if r.GrantID == req.GrantID && r.Action == req.Action && r.Status == protocol.RequestOpen {
			if req.CreatedAt.Before(r.ExpiresAt) {
				return FileOutcome{Request: r}, nil
			}
			r.Status = protocol.RequestExpired
			at := req.CreatedAt
			r.ResolvedAt = &at
			m.requests[id] = r
			expiredID = r.ID
			m.audit = append(m.audit, auditEventFor(protocol.ActionRequestExpired, r, req.CreatedAt, ""))
		}
	}
	req.Status = protocol.RequestOpen
	m.requests[req.ID] = req
	m.audit = append(m.audit, auditEventFor(protocol.ActionRequestFiled, req, req.CreatedAt, ""))
	m.reqBus.notify(req.OrgID)
	return FileOutcome{Request: req, Created: true, ExpiredID: expiredID}, nil
}

// auditEventFor builds the lifecycle audit row for a request — reason names
// the request so the event joins back to the authoritative row; decision
// follows the action.
func auditEventFor(action protocol.ActionKind, r protocol.ApprovalRequest, at time.Time, approvalID string) protocol.AuditEvent {
	decision := protocol.DecisionAllow
	switch action {
	case protocol.ActionRequestDenied, protocol.ActionRequestCancelled:
		decision = protocol.DecisionDeny
	case protocol.ActionRequestFiled, protocol.ActionRequestExpired:
		decision = protocol.DecisionNeedApproval
	}
	return protocol.AuditEvent{Time: at.UTC(), OrgID: r.OrgID, AgentID: r.AgentID, ItemID: r.ItemID,
		Action: action, Decision: decision, Reason: r.ID, ApprovalID: approvalID}
}

func (m *Memory) Request(id string) (protocol.ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return protocol.ApprovalRequest{}, ErrNotFound
	}
	return r, nil
}

func (m *Memory) ListRequests(orgID string, status protocol.RequestStatus, now time.Time) ([]protocol.ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []protocol.ApprovalRequest
	for _, r := range m.requests {
		if r.OrgID != orgID || r.Status != status {
			continue
		}
		if status == protocol.RequestOpen && !now.Before(r.ExpiresAt) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func (m *Memory) ResolveRequest(id string, status protocol.RequestStatus, humanID, approvalID string, at time.Time) (protocol.ApprovalRequest, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return protocol.ApprovalRequest{}, false, ErrNotFound
	}
	if r.Status != protocol.RequestOpen || !at.Before(r.ExpiresAt) {
		return r, false, nil
	}
	r.Status, r.ResolvedBy, r.ApprovalID = status, humanID, approvalID
	r.ResolvedAt = &at
	m.requests[id] = r
	m.audit = append(m.audit, auditEventFor(requestActionForStatus(status), r, at, approvalID))
	m.reqBus.notify(r.OrgID)
	return r, true, nil
}

// approveRequestsForGrant resolves every open, unexpired ask on a grant.
// Callers hold m.mu.
func (m *Memory) approveRequestsForGrant(grantID, humanID, approvalID string, at time.Time) []protocol.ApprovalRequest {
	var out []protocol.ApprovalRequest
	for id, r := range m.requests {
		if r.GrantID == grantID && r.Status == protocol.RequestOpen && at.Before(r.ExpiresAt) {
			r.Status = protocol.RequestApproved
			r.ResolvedBy, r.ApprovalID = humanID, approvalID
			r.ResolvedAt = &at
			m.requests[id] = r
			out = append(out, r)
		}
	}
	return out
}

func (m *Memory) ApproveRequest(id string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return nil, false, ErrNotFound
	}
	if r.Status != protocol.RequestOpen || !at.Before(r.ExpiresAt) {
		return nil, false, nil
	}
	// The grant's whole edge must be live — grant unexpired, agent
	// unrevoked, item not archived — or the approval mints on a dead edge.
	if !m.grantEdgeLive(r.GrantID, at) {
		return nil, false, nil
	}
	r.Status = protocol.RequestApproved
	r.ResolvedBy, r.ApprovalID = appr.HumanID, appr.ID
	r.ResolvedAt = &at
	m.requests[id] = r
	appr.GrantID = r.GrantID
	m.approvals[r.GrantID] = appr
	resolved := append([]protocol.ApprovalRequest{r}, m.approveRequestsForGrant(r.GrantID, appr.HumanID, appr.ID, at)...)
	for _, rr := range resolved {
		m.audit = append(m.audit, auditEventFor(protocol.ActionRequestApproved, rr, at, appr.ID))
	}
	m.reqBus.notify(r.OrgID)
	return resolved, true, nil
}

// grantEdgeLive reports whether a grant's edge is still usable: grant
// exists and unexpired, agent unrevoked, item not archived.
// Callers hold m.mu.
func (m *Memory) grantEdgeLive(grantID string, at time.Time) bool {
	var g *protocol.Grant
	for i := range m.grants {
		if m.grants[i].ID == grantID {
			gg := m.grants[i]
			g = &gg
			break
		}
	}
	if g == nil || (g.ExpiresAt != nil && !at.Before(*g.ExpiresAt)) {
		return false
	}
	if a, ok := m.agents[g.AgentID]; ok && a.RevokedAt != nil {
		return false
	}
	if it, ok := m.items[g.ItemID]; ok && it.Archived {
		return false
	}
	return true
}

func (m *Memory) ApproveGrant(grantID string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.grantEdgeLive(grantID, at) {
		return nil, ErrGrantNotLive
	}
	appr.GrantID = grantID
	m.approvals[grantID] = appr
	resolved := m.approveRequestsForGrant(grantID, appr.HumanID, appr.ID, at)
	for _, rr := range resolved {
		m.audit = append(m.audit, auditEventFor(protocol.ActionRequestApproved, rr, at, appr.ID))
	}
	if len(resolved) > 0 {
		m.reqBus.notify(resolved[0].OrgID)
	}
	return resolved, nil
}

func (m *Memory) CancelRequestsForItem(itemID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	orgs := map[string]struct{}{}
	for id, r := range m.requests {
		if r.ItemID == itemID && r.Status == protocol.RequestOpen {
			r.Status = protocol.RequestCancelled
			r.ResolvedAt = &at
			m.requests[id] = r
			m.audit = append(m.audit, auditEventFor(protocol.ActionRequestCancelled, r, at, ""))
			orgs[r.OrgID] = struct{}{}
		}
	}
	m.reqBus.notifyOrgs(orgs)
	return nil
}

func (m *Memory) CancelRequestsForAgent(agentID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	orgs := map[string]struct{}{}
	for id, r := range m.requests {
		if r.AgentID == agentID && r.Status == protocol.RequestOpen {
			r.Status = protocol.RequestCancelled
			r.ResolvedAt = &at
			m.requests[id] = r
			m.audit = append(m.audit, auditEventFor(protocol.ActionRequestCancelled, r, at, ""))
			orgs[r.OrgID] = struct{}{}
		}
	}
	m.reqBus.notifyOrgs(orgs)
	return nil
}

func (m *Memory) ExpireStaleRequests(now time.Time) ([]protocol.ApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []protocol.ApprovalRequest
	for id, r := range m.requests {
		if r.Status == protocol.RequestOpen && !now.Before(r.ExpiresAt) {
			r.Status = protocol.RequestExpired
			r.ResolvedAt = &now
			m.requests[id] = r
			out = append(out, r)
		}
	}
	if len(out) > 0 {
		orgs := map[string]struct{}{}
		for _, r := range out {
			orgs[r.OrgID] = struct{}{}
		}
		m.reqBus.notifyOrgs(orgs)
	}
	return out, nil
}

// WatchRequests ticks on approval-request changes for orgID — see Store.
func (m *Memory) WatchRequests(ctx context.Context, orgID string) <-chan struct{} {
	return m.reqBus.watch(ctx, orgID)
}

func (m *Memory) AppendAudit(e protocol.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, e)
	return nil
}

func (m *Memory) AppendAudits(events []protocol.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, events...)
	return nil
}

func (m *Memory) Billing(orgID string) (OrgBilling, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ob, ok := m.billing[orgID]; ok {
		return ob, nil
	}
	return OrgBilling{OrgID: orgID, Plan: "free"}, nil
}

func (m *Memory) SetBilling(ob OrgBilling) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.billing[ob.OrgID] = ob
	return nil
}

func (m *Memory) OrgByBillingCustomer(customerID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ob := range m.billing {
		if ob.CustomerID == customerID {
			return ob.OrgID, nil
		}
	}
	return "", ErrNotFound
}

func usageKey(orgID string, window time.Time) string {
	return orgID + "\x00" + window.UTC().Format(time.RFC3339Nano)
}

func (m *Memory) ConsumeUse(orgID string, window time.Time, cap int64) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := usageKey(orgID, window)
	m.usage[k]++
	used := m.usage[k]
	return used, cap <= 0 || used <= cap, nil
}

func (m *Memory) Usage(orgID string, window time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage[usageKey(orgID, window)], nil
}

// FlushAuditOutbox: memory has no outbox.
func (m *Memory) FlushAuditOutbox(int) (int, error) { return 0, nil }

func (m *Memory) Audit() ([]protocol.AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.audit)
	if n > maxListResults {
		n = maxListResults
	}
	out := make([]protocol.AuditEvent, n)
	copy(out, m.audit[len(m.audit)-n:])
	return out, nil
}

func workloadKey(issuer, subject string) string { return issuer + "\x00" + subject }

func (m *Memory) PutWorkload(w protocol.Workload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workloads[workloadKey(w.Issuer, w.Subject)] = w
	return nil
}

func (m *Memory) Workload(issuer, subject string) (*protocol.Workload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workloads[workloadKey(issuer, subject)]
	if !ok {
		return nil, nil
	}
	cp := w
	return &cp, nil
}

func (m *Memory) WorkloadsForIssuer(issuer string) ([]protocol.Workload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []protocol.Workload
	for _, w := range m.workloads {
		if w.Issuer == issuer {
			out = append(out, w)
		}
	}
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func sessionHashKey(secretHash []byte) string { return hex.EncodeToString(secretHash) }

func (m *Memory) PutSession(s protocol.Session, secretHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionHashKey(secretHash)] = s
	return nil
}

func (m *Memory) SessionByHash(secretHash []byte) (protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionHashKey(secretHash)]
	if !ok {
		return protocol.Session{}, ErrNotFound
	}
	return s, nil
}

func (m *Memory) ListSessions() ([]protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	if len(out) > maxListResults {
		out = out[:maxListResults]
	}
	return out, nil
}

func (m *Memory) SessionByID(id string) (protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return protocol.Session{}, ErrNotFound
}

func (m *Memory) RevokeSession(id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.sessions {
		if s.ID == id {
			if s.RevokedAt == nil {
				t := at.UTC()
				s.RevokedAt = &t
				m.sessions[k] = s
			}
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) RenewSession(id string, at time.Time) (protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.sessions {
		if s.ID == id {
			if s.RevokedAt != nil {
				return protocol.Session{}, ErrSessionRevoked
			}
			if !s.ExpiresAt.After(at) {
				return protocol.Session{}, ErrSessionExpired
			}
			maxExpires := s.CreatedAt.Add(time.Duration(s.MaxTTL) * time.Second)
			newExpires := s.ExpiresAt.Add(time.Duration(s.TTL) * time.Second)
			if newExpires.After(maxExpires) {
				newExpires = maxExpires
			}
			if !newExpires.After(s.ExpiresAt) {
				newExpires = s.ExpiresAt
			}
			t := at.UTC()
			s.ExpiresAt = newExpires.UTC()
			s.RenewedAt = &t
			m.sessions[k] = s
			return s, nil
		}
	}
	return protocol.Session{}, ErrNotFound
}

func (m *Memory) Sweep(olderThan time.Time) (SweepReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rep SweepReport
	cut := olderThan.UTC()
	for k, s := range m.sessions {
		expired := s.ExpiresAt.Before(cut)
		revoked := s.RevokedAt != nil && s.RevokedAt.Before(cut)
		if expired || revoked {
			delete(m.sessions, k)
			rep.Sessions++
		}
	}
	for k, g := range m.grants {
		if g.ExpiresAt != nil && g.ExpiresAt.Before(cut) {
			delete(m.grants, k)
			rep.Grants++
		}
	}
	for k, a := range m.approvals {
		if a.ExpiresAt.Before(cut) {
			delete(m.approvals, k)
			rep.Approvals++
		}
	}
	now := time.Now().UTC()
	orgs := map[string]struct{}{}
	for k, r := range m.requests {
		if r.Status == protocol.RequestOpen && !now.Before(r.ExpiresAt) {
			r.Status = protocol.RequestExpired
			r.ResolvedAt = &now
			m.requests[k] = r
			rep.Requests++
			m.audit = append(m.audit, protocol.AuditEvent{
				Time: now, OrgID: r.OrgID, AgentID: r.AgentID, ItemID: r.ItemID,
				Action: protocol.ActionRequestExpired, Decision: protocol.DecisionNeedApproval, Reason: r.ID,
			})
			orgs[r.OrgID] = struct{}{}
		}
	}
	m.reqBus.notifyOrgs(orgs)
	return rep, nil
}
