// Package publicapi is the HTTP projection of the OpenAPI contract.
//
// Same Bearer as MCP. Responses never include vault secrets. CLI, MCP, and
// generated SDKs are adapters over these operations.
//
// POST /v1/fill/* is the native-host secret path. It is not on the OpenAPI
// contract and not an MCP tool.
package publicapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/broker"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/oneimport"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

type UseRequest struct {
	Item    string            `json:"item"`
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	BodyB64 string            `json:"body_b64,omitempty"`
}

type UseResponse struct {
	Decision          protocol.Decision `json:"decision"`
	Reason            string            `json:"reason,omitempty"`
	ApprovalID        string            `json:"approval_id,omitempty"`
	RequestID         string            `json:"request_id,omitempty"`
	RequestExpiresAt  *time.Time        `json:"request_expires_at,omitempty"`
	Status            int               `json:"status,omitempty"`
	Headers           http.Header       `json:"headers,omitempty"`
	Body              string            `json:"body,omitempty"`
	BodyB64           string            `json:"body_b64,omitempty"`
}

type ItemsResponse struct {
	Items []protocol.Item `json:"items"`
}

type RequestView struct {
	ID         string                 `json:"id"`
	AgentID    string                 `json:"agent_id"`
	ItemID     string                 `json:"item_id"`
	GrantID    string                 `json:"grant_id"`
	Action     protocol.ActionKind    `json:"action"`
	Status     protocol.RequestStatus `json:"status"`
	CreatedAt  time.Time              `json:"created_at"`
	ExpiresAt  time.Time              `json:"expires_at"`
	ResolvedAt *time.Time             `json:"resolved_at,omitempty"`
	ResolvedBy string                 `json:"resolved_by,omitempty"`
	ApprovalID string                 `json:"approval_id,omitempty"`
}

type RequestsResponse struct {
	Requests []RequestView `json:"requests"`
}

type EventsResponse struct {
	Events []protocol.AuditEvent `json:"events"`
}

type CreateItemRequest struct {
	Name     string          `json:"name"`
	URI      string          `json:"uri,omitempty"`
	URIs     []string        `json:"uris,omitempty"`
	Tags     []string        `json:"tags,omitempty"`
	Kind     string          `json:"kind,omitempty"`
	Secret   string          `json:"secret,omitempty"`
	TOTPSeed string          `json:"totp_seed,omitempty"`
	Login    string          `json:"login,omitempty"`
	Card     *CardFields     `json:"card,omitempty"`
	Identity *IdentityFields `json:"identity,omitempty"`
}

type CardFields struct {
	Number   string `json:"number,omitempty"`
	ExpMonth string `json:"exp_month,omitempty"`
	ExpYear  string `json:"exp_year,omitempty"`
	CVV      string `json:"cvv,omitempty"`
	Holder   string `json:"holder,omitempty"`
}

type IdentityFields struct {
	GivenName  string `json:"given_name,omitempty"`
	FamilyName string `json:"family_name,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	Postal     string `json:"postal,omitempty"`
	Country    string `json:"country,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
}

type ImportResponse struct {
	Names []string `json:"names"`
	Count int      `json:"count"`
}

type UpdateItemRequest struct {
	URI   string   `json:"uri,omitempty"`
	URIs  []string `json:"uris,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Login string   `json:"login,omitempty"`
}

type CreateGrantRequest struct {
	Agent   string `json:"agent,omitempty"`
	Human   string `json:"human,omitempty"`
	Item    string `json:"item"`
	Level   string `json:"level"`
	Expires string `json:"expires,omitempty"`
}

type GrantView struct {
	ID        string     `json:"id"`
	OrgID     string     `json:"org_id"`
	AgentID   string     `json:"agent_id"`
	ItemID    string     `json:"item_id"`
	Level     string     `json:"level"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type GrantsResponse struct {
	Grants []GrantView `json:"grants"`
}

type CreateAgentRequest struct {
	Name string `json:"name"`
}

type AgentsResponse struct {
	Agents []protocol.Principal `json:"agents"`
}

type CreateSessionRequest struct {
	Agent   string `json:"agent"`
	TTL     string `json:"ttl,omitempty"`
	MaxUses int    `json:"max_uses,omitempty"`
}

type CreateSessionResponse struct {
	protocol.Session
	Token string `json:"token"`
}

type SessionsResponse struct {
	Sessions []protocol.Session `json:"sessions"`
}

type ProvisionResponse struct {
	Subject string `json:"subject"`
	OrgID   string `json:"org_id"`
}

type InviteRequest struct {
	Email string `json:"email"`
}

type InviteResponse struct {
	IdentityID  string `json:"identity_id"`
	Emailed     bool   `json:"emailed"`
	RecoveryURL string `json:"recovery_url,omitempty"`
}

type FillLoginsRequest struct {
	URL      string `json:"url,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	MintTOTP bool   `json:"mintTotp,omitempty"`
}

type FillLogin struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Password string `json:"password"`
	UUID     string `json:"uuid"`
	TOTP     string `json:"totp,omitempty"`
}

type FillLoginsResponse struct {
	Entries []FillLogin `json:"entries"`
}

type FillTOTPRequest struct {
	UUID string `json:"uuid"`
}

type FillTOTPResponse struct {
	TOTP string `json:"totp"`
}

type FillTOTPEnrollRequest struct {
	UUID     string `json:"uuid"`
	TOTPSeed string `json:"totp_seed"`
}

type FillTOTPEnrollResponse struct {
	UUID    string `json:"uuid"`
	HasTOTP bool   `json:"has_totp"`
}

type FillPasskeysRequest struct {
	Origin         string          `json:"origin"`
	PublicKey      json.RawMessage `json:"publicKey"`
	RelatedOrigins []string        `json:"relatedOrigins,omitempty"`
}

type FillPasskeysResponse struct {
	Response json.RawMessage `json:"response"`
}

type FillSyncRequest struct {
	Since string `json:"since,omitempty"`
}

type FillSyncItem struct {
	Item     protocol.Item `json:"item"`
	Material string        `json:"material"`
}

type FillSyncResponse struct {
	Items  []FillSyncItem `json:"items"`
	Cursor string         `json:"cursor"`
}

type Server struct {
	App      *app.App
	Identity func(ctx context.Context, token string) (protocol.Principal, error)
}

func Mount(mux *http.ServeMux, a *app.App) {
	(&Server{App: a}).Mount(mux)
}

func (s *Server) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", spec)
	mux.HandleFunc("GET /v1/items", s.listItems)
	mux.HandleFunc("POST /v1/items", s.createItem)
	mux.HandleFunc("POST /v1/import", s.importItems)
	mux.HandleFunc("PATCH /v1/items/{name}", s.updateItem)
	mux.HandleFunc("POST /v1/items/{name}/archive", s.archiveItem)
	mux.HandleFunc("DELETE /v1/items/{name}", s.deleteItem)
	mux.HandleFunc("GET /v1/grants", s.listGrants)
	mux.HandleFunc("POST /v1/grants", s.createGrant)
	mux.HandleFunc("GET /v1/agents", s.listAgents)
	mux.HandleFunc("POST /v1/agents", s.createAgent)
	mux.HandleFunc("POST /v1/agents/{name}/revoke", s.revokeAgent)
	mux.HandleFunc("GET /v1/sessions", s.listSessions)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("POST /v1/provision", s.provision)
	mux.HandleFunc("POST /v1/invites", s.createInvite)
	mux.HandleFunc("DELETE /v1/members/{id}", s.removeMember)
	mux.HandleFunc("POST /v1/members/{id}/owner", s.promoteOwner)
	mux.HandleFunc("DELETE /v1/members/{id}/owner", s.demoteOwner)
	mux.HandleFunc("DELETE /v1/me", s.deleteMe)
	mux.HandleFunc("DELETE /v1/org", s.deleteOrg)
	mux.HandleFunc("POST /v1/use", s.useItem)
	mux.HandleFunc("GET /v1/requests", s.listRequests)
	mux.HandleFunc("POST /v1/requests/{id}/approve", s.approveRequest)
	mux.HandleFunc("POST /v1/requests/{id}/deny", s.denyRequest)
	mux.HandleFunc("GET /v1/events", s.listEvents)
	mux.HandleFunc("POST /v1/fill/logins", s.fillLogins)
	mux.HandleFunc("POST /v1/fill/totp", s.fillTOTP)
	mux.HandleFunc("POST /v1/fill/totp/enroll", s.fillTOTPEnroll)
	mux.HandleFunc("POST /v1/fill/passkeys/register", s.fillPasskeyRegister)
	mux.HandleFunc("POST /v1/fill/passkeys/get", s.fillPasskeyGet)
	mux.HandleFunc("POST /v1/fill/sync", s.fillSync)
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.App.ItemsForPrincipal(p)
	if err != nil {
		http.Error(w, "list failed", http.StatusBadRequest)
		return
	}
	if items == nil {
		items = []protocol.Item{}
	}
	writeJSON(w, ItemsResponse{Items: items})
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in CreateItemRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	kind := protocol.ItemKind(in.Kind)
	token := []byte(in.Secret)
	switch kind {
	case protocol.ItemCard:
		if in.Card != nil {
			blob, err := material.PackCard(in.Card.Number, in.Card.ExpMonth, in.Card.ExpYear, in.Card.CVV, in.Card.Holder)
			if err != nil {
				http.Error(w, "create failed", http.StatusBadRequest)
				return
			}
			token = blob
		}
	case protocol.ItemIdentity:
		if in.Identity != nil {
			blob, err := material.PackIdentity(in.Identity.GivenName, in.Identity.FamilyName, in.Identity.Address, in.Identity.City, in.Identity.Region, in.Identity.Postal, in.Identity.Country, in.Identity.Phone, in.Identity.Email)
			if err != nil {
				http.Error(w, "create failed", http.StatusBadRequest)
				return
			}
			token = blob
		}
	}
	item, err := s.App.PutItemFor(p, app.ItemOpts{
		Name:     in.Name,
		URI:      in.URI,
		URIs:     in.URIs,
		Tags:     in.Tags,
		Kind:     kind,
		Token:    token,
		Login:    in.Login,
		TOTPSeed: []byte(in.TOTPSeed),
	})
	if err != nil {
		http.Error(w, "create failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, item)
}

func (s *Server) importItems(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("filename"))
	if name == "" {
		name = "import.csv"
	}
	rows, err := oneimport.Parse(name, raw)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	got, err := s.App.ImportItems(p, rows)
	if err != nil {
		http.Error(w, "import failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, ImportResponse{Names: got.Names, Count: got.Count})
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireItemWrite(w, r); !ok {
		return
	}
	var in UpdateItemRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var add []string
	if in.URI != "" {
		add = []string{in.URI}
	}
	item, err := s.App.UpdateItem(r.PathValue("name"), in.URIs, add, in.Tags, in.Login)
	if err != nil {
		http.Error(w, "update failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, item)
}

func (s *Server) archiveItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireItemWrite(w, r); !ok {
		return
	}
	if err := s.App.ArchiveItem(r.PathValue("name")); err != nil {
		http.Error(w, "archive failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"archived": true})
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireItemWrite(w, r); !ok {
		return
	}
	if err := s.App.DeleteItem(r.PathValue("name")); err != nil {
		http.Error(w, "delete failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}

func (s *Server) listGrants(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	grants, err := s.App.Store.ListGrants()
	if err != nil {
		http.Error(w, "list failed", http.StatusBadRequest)
		return
	}
	out := make([]GrantView, 0, len(grants))
	for _, g := range grants {
		if g.OrgID == owner.OrgID {
			out = append(out, grantView(g))
		}
	}
	writeJSON(w, GrantsResponse{Grants: out})
}

func (s *Server) createGrant(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	var in CreateGrantRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.Contains(in.Human, "@") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	grantee := strings.TrimSpace(in.Agent)
	human := strings.TrimSpace(in.Human)
	switch {
	case grantee != "" && human != "":
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	case human != "":
		grantee = human
	case grantee == "":
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var until *time.Time
	if in.Expires != "" {
		d, err := time.ParseDuration(in.Expires)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		t := time.Now().Add(d)
		until = &t
	}
	g, err := s.App.GrantUntil(owner, grantee, in.Item, protocol.GrantLevel(in.Level), until)
	if err != nil {
		http.Error(w, "grant failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, grantView(g))
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	agents, err := s.App.Store.ListAgents()
	if err != nil {
		http.Error(w, "list failed", http.StatusBadRequest)
		return
	}
	mine := make([]protocol.Principal, 0, len(agents))
	for _, a := range agents {
		if a.OrgID == owner.OrgID {
			mine = append(mine, a)
		}
	}
	writeJSON(w, AgentsResponse{Agents: mine})
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	var in CreateAgentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p, err := s.App.AddAgentFor(owner, in.Name)
	if err != nil {
		http.Error(w, "create failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, p)
}

func (s *Server) revokeAgent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.App.RevokeAgent(p, name); err != nil {
		if errors.Is(err, app.ErrForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "revoke failed", http.StatusBadRequest)
		return
	}
	agent, err := s.App.Store.Agent(name)
	if err != nil {
		http.Error(w, "revoke failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, agent)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	sessions, err := s.App.ListSessions(p)
	if err != nil {
		if errors.Is(err, app.ErrForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "list failed", http.StatusBadRequest)
		return
	}
	if sessions == nil {
		sessions = []protocol.Session{}
	}
	writeJSON(w, SessionsResponse{Sessions: sessions})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in CreateSessionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var ttl time.Duration
	if strings.TrimSpace(in.TTL) != "" {
		d, err := time.ParseDuration(in.TTL)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ttl = d
	}
	sess, token, err := s.App.CreateSession(p, strings.TrimSpace(in.Agent), ttl, in.MaxUses)
	if err != nil {
		if errors.Is(err, app.ErrForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "create failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, CreateSessionResponse{Session: sess, Token: token})
}

// provision is signup: the bearer token is subject-verified inside
// ProvisionHuman — there is no member check because this call is what creates
// the membership. Idempotent; safe to retry.
func (s *Server) provision(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	p, err := s.App.ProvisionHuman(r.Context(), raw)
	if err != nil {
		http.Error(w, "provision failed", http.StatusUnauthorized)
		return
	}
	writeJSON(w, ProvisionResponse{Subject: p.ID, OrgID: p.OrgID})
}

// createInvite is the private-alpha gate: a provisioned human invites an email
// into their org. The identity and recovery link are minted in Kratos and
// delivered by the mail worker; the link only appears in the response when
// mail is not configured (local dev).
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	var in InviteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil || in.Email == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	inv, err := s.App.InviteHuman(r.Context(), raw, in.Email)
	if err != nil {
		if errors.Is(err, app.ErrInviteLimit) {
			http.Error(w, "invite rate limit", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "invite failed", http.StatusUnauthorized)
		return
	}
	out := InviteResponse{IdentityID: inv.IdentityID, Emailed: inv.Emailed}
	if !inv.Emailed {
		out.RecoveryURL = inv.RecoveryURL
	}
	writeJSON(w, out)
}

// lifecycleStatus maps org-admin errors to status codes: 401 for unresolvable
// principals, 403 for members who aren't owners, 404 for unknown members.
func lifecycleStatus(err error) int {
	switch {
	case errors.Is(err, app.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, app.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

// DELETE /v1/members/{id} — owner offboards a member: tuple + humans row.
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.App.RemoveMember(r.Context(), raw, r.PathValue("id")); err != nil {
		http.Error(w, "remove failed", lifecycleStatus(err))
		return
	}
	writeJSON(w, map[string]bool{"removed": true})
}

// POST /v1/members/{id}/owner — owner promotes a member to co-owner.
func (s *Server) promoteOwner(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.App.PromoteOwner(r.Context(), raw, r.PathValue("id")); err != nil {
		http.Error(w, "promote failed", lifecycleStatus(err))
		return
	}
	writeJSON(w, map[string]bool{"owner": true})
}

// DELETE /v1/members/{id}/owner — owner demotes a co-owner; the last owner
// cannot be demoted.
func (s *Server) demoteOwner(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.App.DemoteOwner(r.Context(), raw, r.PathValue("id")); err != nil {
		http.Error(w, "demote failed", lifecycleStatus(err))
		return
	}
	writeJSON(w, map[string]bool{"owner": false})
}

// DELETE /v1/me — the human kill-switch: tuples and the humans row die, the
// token resolves nothing afterward. Sole owners are refused.
func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.App.DeleteMe(r.Context(), raw); err != nil {
		http.Error(w, "delete failed", lifecycleStatus(err))
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}

// DELETE /v1/org — owner teardown: tuples die, then every vault row for the
// org in one transaction. Audit rows survive for the record.
func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r.Header.Get("Authorization"))
	if raw == "" || app.IsSessionToken(raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rep, err := s.App.DeleteOrg(r.Context(), raw)
	if err != nil {
		http.Error(w, "delete failed", lifecycleStatus(err))
		return
	}
	writeJSON(w, rep)
}

func (s *Server) useItem(w http.ResponseWriter, r *http.Request) {
	var in UseRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Item == "" || in.URL == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h := http.Header{}
	for k, v := range in.Headers {
		h.Add(k, v)
	}
	if in.Body != "" && in.BodyB64 != "" {
		http.Error(w, "body and body_b64 are mutually exclusive", http.StatusBadRequest)
		return
	}
	body := []byte(in.Body)
	if in.BodyB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(in.BodyB64)
		if err != nil {
			http.Error(w, "bad body_b64", http.StatusBadRequest)
			return
		}
		body = decoded
	}
	fetch := protocol.Fetch{
		Method: in.Method,
		URL:    in.URL,
		Header: h,
		Body:   body,
	}
	raw := bearer(r.Header.Get("Authorization"))

	var got protocol.UseResult
	var err error
	if app.IsSessionToken(raw) {
		got, err = s.App.UseFetchSession(r.Context(), raw, in.Item, fetch)
	} else {
		var agentID string
		agentID, err = s.resolveAgentID(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		got, err = s.App.UseFetch(r.Context(), agentID, in.Item, fetch)
	}
	if err != nil {
		if errors.Is(err, broker.ErrOverloaded) {
			http.Error(w, "origin overloaded", http.StatusServiceUnavailable)
			return
		}
		if errors.Is(err, broker.ErrUnauthorized) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		slog.Warn("use failed", "item", in.Item, "err", err)
		http.Error(w, "use failed", http.StatusBadRequest)
		return
	}
	out := UseResponse{
		Decision: got.Decision, Reason: got.Reason, ApprovalID: got.ApprovalID,
		RequestID: got.RequestID, RequestExpiresAt: got.RequestExpiresAt,
	}
	if got.Fetch != nil {
		out.Status = got.Fetch.Status
		out.Headers = got.Fetch.Header
		if utf8.Valid(got.Fetch.Body) {
			out.Body = string(got.Fetch.Body)
		} else {
			out.BodyB64 = base64.StdEncoding.EncodeToString(got.Fetch.Body)
		}
	}
	writeJSON(w, out)
}

func requestView(r protocol.ApprovalRequest) RequestView {
	return RequestView{
		ID: r.ID, AgentID: r.AgentID, ItemID: r.ItemID, GrantID: r.GrantID,
		Action: r.Action, Status: r.Status, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		ResolvedAt: r.ResolvedAt, ResolvedBy: r.ResolvedBy, ApprovalID: r.ApprovalID,
	}
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.requireOwner(w, r)
	if !ok {
		return
	}
	status := protocol.RequestStatus(r.URL.Query().Get("status"))
	if status == "" {
		status = protocol.RequestOpen
	}
	switch status {
	case protocol.RequestOpen, protocol.RequestApproved, protocol.RequestDenied,
		protocol.RequestExpired, protocol.RequestCancelled:
	default:
		http.Error(w, "bad status", http.StatusBadRequest)
		return
	}
	reqs, err := s.App.Store.ListRequests(owner.OrgID, status, time.Now())
	if err != nil {
		http.Error(w, "list failed", http.StatusBadRequest)
		return
	}
	out := make([]RequestView, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, requestView(req))
	}
	writeJSON(w, RequestsResponse{Requests: out})
}

// ownerRequest loads the ask and scopes it to the caller's org — an
// open, unexpired request is the only one a human may still answer.
func (s *Server) ownerRequest(w http.ResponseWriter, r *http.Request) (protocol.Principal, protocol.ApprovalRequest, bool) {
	owner, ok := s.requireOwner(w, r)
	if !ok {
		return protocol.Principal{}, protocol.ApprovalRequest{}, false
	}
	req, err := s.App.Store.Request(r.PathValue("id"))
	if err != nil || req.OrgID != owner.OrgID {
		http.Error(w, "not found", http.StatusNotFound)
		return protocol.Principal{}, protocol.ApprovalRequest{}, false
	}
	return owner, req, true
}

func (s *Server) approveRequest(w http.ResponseWriter, r *http.Request) {
	owner, req, ok := s.ownerRequest(w, r)
	if !ok {
		return
	}
	if req.Status != protocol.RequestOpen || !time.Now().Before(req.ExpiresAt) {
		http.Error(w, "already resolved", http.StatusConflict)
		return
	}
	var in struct {
		TTL string `json:"ttl"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in)
	}
	ttl := 15 * time.Minute
	if in.TTL != "" {
		d, err := time.ParseDuration(in.TTL)
		if err != nil {
			http.Error(w, "bad ttl", http.StatusBadRequest)
			return
		}
		ttl = d
	}
	// An approval is "no asks for a while", not a silent level2: clamp to a
	// day. Longer trust is expressed by raising the grant to level2.
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if ttl > 24*time.Hour {
		ttl = 24 * time.Hour
	}
	resolved, err := s.App.Broker.ApproveRequest(owner, req.ID, ttl)
	if errors.Is(err, store.ErrRequestResolved) {
		http.Error(w, "already resolved", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "approve failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, requestView(resolved))
}

func (s *Server) denyRequest(w http.ResponseWriter, r *http.Request) {
	owner, req, ok := s.ownerRequest(w, r)
	if !ok {
		return
	}
	resolved, won, err := s.App.Store.ResolveRequest(req.ID, protocol.RequestDenied, owner.ID, "", time.Now())
	if err != nil {
		http.Error(w, "deny failed", http.StatusBadRequest)
		return
	}
	if !won {
		http.Error(w, "already resolved", http.StatusConflict)
		return
	}
	_ = s.App.Store.AppendAudit(protocol.AuditEvent{
		Time: time.Now().UTC(), OrgID: req.OrgID, AgentID: req.AgentID, ItemID: req.ItemID,
		Action: protocol.ActionRequestDenied, Decision: protocol.DecisionDeny, Reason: req.ID,
	})
	writeJSON(w, requestView(resolved))
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	if p.Kind == protocol.PrincipalHuman {
		owns, err := s.App.OwnsVault(p)
		if err != nil || !owns {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	all, err := s.App.Store.Audit()
	if err != nil {
		http.Error(w, "audit failed", http.StatusBadRequest)
		return
	}
	mine := make([]protocol.AuditEvent, 0, len(all))
	for _, e := range all {
		if e.OrgID != p.OrgID {
			continue
		}
		if p.Kind == protocol.PrincipalHuman || e.AgentID == p.ID {
			mine = append(mine, e)
		}
	}
	if n := len(mine); n > 100 {
		mine = mine[n-100:]
	}
	writeJSON(w, EventsResponse{Events: mine})
}

func (s *Server) fillLogins(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in FillLoginsRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.UUID) != "" {
		got, err := s.App.FillLogin(p, in.UUID, in.MintTOTP)
		if err != nil {
			http.Error(w, "fill failed", http.StatusBadRequest)
			return
		}
		writeJSON(w, FillLoginsResponse{Entries: []FillLogin{{
			Login:    got.Login,
			Name:     got.Name,
			Password: got.Password,
			UUID:     got.UUID,
			TOTP:     got.TOTP,
		}}})
		return
	}
	if in.MintTOTP {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	got, err := s.App.FillLogins(p, in.URL)
	if err != nil {
		http.Error(w, "fill failed", http.StatusBadRequest)
		return
	}
	entries := make([]FillLogin, 0, len(got))
	for _, e := range got {
		entries = append(entries, FillLogin{Login: e.Login, Name: e.Name, Password: e.Password, UUID: e.UUID, TOTP: e.TOTP})
	}
	writeJSON(w, FillLoginsResponse{Entries: entries})
}

func (s *Server) fillTOTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in FillTOTPRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	code, err := s.App.FillTOTP(p, in.UUID, time.Now())
	if err != nil {
		http.Error(w, "fill failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, FillTOTPResponse{TOTP: code})
}

func (s *Server) fillTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in FillTOTPEnrollRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	err := s.App.AttachTOTP(p, in.UUID, in.TOTPSeed)
	if err != nil {
		if errors.Is(err, app.ErrTOTPEnrollDenied) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "enroll failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, FillTOTPEnrollResponse{UUID: in.UUID, HasTOTP: true})
}

func (s *Server) fillSync(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in FillSyncRequest
	if r.Body != nil {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil && err != io.EOF {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	rows, cursor, err := s.App.FillSync(p, in.Since)
	if err != nil {
		http.Error(w, "fill failed", http.StatusBadRequest)
		return
	}
	items := make([]FillSyncItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, FillSyncItem{Item: row.Item, Material: string(row.Material)})
	}
	writeJSON(w, FillSyncResponse{Items: items, Cursor: cursor})
}

func (s *Server) fillPasskeyRegister(w http.ResponseWriter, r *http.Request) {
	s.fillPasskey(w, r, true)
}

func (s *Server) fillPasskeyGet(w http.ResponseWriter, r *http.Request) {
	s.fillPasskey(w, r, false)
}

func (s *Server) fillPasskey(w http.ResponseWriter, r *http.Request, register bool) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return
	}
	var in FillPasskeysRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var resp json.RawMessage
	var err error
	if register {
		resp, err = s.App.FillPasskeyRegister(p, in.Origin, in.PublicKey, in.RelatedOrigins)
	} else {
		resp, err = s.App.FillPasskeyGet(p, in.Origin, in.PublicKey)
	}
	if err != nil {
		http.Error(w, "fill failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, FillPasskeysResponse{Response: resp})
}

func (s *Server) resolve(r *http.Request) (protocol.Principal, error) {
	raw := bearer(r.Header.Get("Authorization"))
	if app.IsSessionToken(raw) {
		return s.App.PrincipalFromSession(raw)
	}
	if s.Identity != nil {
		return s.Identity(r.Context(), raw)
	}
	return s.App.PrincipalFromOIDC(r.Context(), raw)
}

func (s *Server) resolveAgentID(r *http.Request) (string, error) {
	raw := bearer(r.Header.Get("Authorization"))
	var p protocol.Principal
	var err error
	if s.Identity != nil {
		p, err = s.Identity(r.Context(), raw)
	} else {
		p, err = s.App.PrincipalFromOIDC(r.Context(), raw)
	}
	if err != nil {
		return "", err
	}
	if p.Kind != protocol.PrincipalAgent {
		return "", errors.New("not an agent")
	}
	return p.ID, nil
}

func (s *Server) requirePrincipal(w http.ResponseWriter, r *http.Request) (protocol.Principal, bool) {
	p, err := s.resolve(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return protocol.Principal{}, false
	}
	return p, true
}

func (s *Server) requireAgent(w http.ResponseWriter, r *http.Request) (protocol.Principal, bool) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return protocol.Principal{}, false
	}
	if p.Kind != protocol.PrincipalAgent {
		http.Error(w, "forbidden", http.StatusForbidden)
		return protocol.Principal{}, false
	}
	return p, true
}

func (s *Server) requireHuman(w http.ResponseWriter, r *http.Request) (protocol.Principal, bool) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return protocol.Principal{}, false
	}
	if p.Kind != protocol.PrincipalHuman {
		http.Error(w, "forbidden", http.StatusForbidden)
		return protocol.Principal{}, false
	}
	return p, true
}

func (s *Server) requireOwner(w http.ResponseWriter, r *http.Request) (protocol.Principal, bool) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return protocol.Principal{}, false
	}
	ok, err := s.App.OwnsVault(p)
	if err != nil || !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return protocol.Principal{}, false
	}
	return p, true
}

func (s *Server) requireItemWrite(w http.ResponseWriter, r *http.Request) (protocol.Principal, bool) {
	p, ok := s.requireHuman(w, r)
	if !ok {
		return protocol.Principal{}, false
	}
	item, err := s.App.Store.Item(r.PathValue("name"))
	if err != nil {
		http.Error(w, "not found", http.StatusBadRequest)
		return protocol.Principal{}, false
	}
	ok, err = s.App.MayWriteItem(p, item)
	if err != nil || !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return protocol.Principal{}, false
	}
	return p, true
}

func grantView(g protocol.Grant) GrantView {
	return GrantView{
		ID:        g.ID,
		OrgID:     g.OrgID,
		AgentID:   g.AgentID,
		ItemID:    g.ItemID,
		Level:     string(g.Level),
		ExpiresAt: g.ExpiresAt,
	}
}

func bearer(h string) string {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return strings.TrimSpace(h[len(p):])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func spec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(Spec)
}
