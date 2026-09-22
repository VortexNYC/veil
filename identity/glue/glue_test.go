package glue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	identityID = "id-human-1"
	humanEmail = "human@example.com"
	sessionTok = "secret-session-cookie"
	inviteCode = "123456"
)

type fakeOry struct {
	client      map[string]any
	putClient   int
	inviteEmail string
	inviteOrg   string
	inviteN     int
	identities  []fakeIdent
}

type fakeIdent struct {
	id, org, email string
}

func (f *fakeOry) start(t *testing.T) (kratosURL, hydraURL string) {
	t.Helper()
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/identities":
			want := r.URL.Query().Get("organization_id")
			var out []map[string]any
			for _, id := range f.identities {
				if want != "" && id.org != want {
					continue
				}
				out = append(out, map[string]any{
					"id":              id.id,
					"schema_id":       "default",
					"schema_url":      "http://127.0.0.1:4434/schemas/default",
					"state":           "active",
					"organization_id": id.org,
					"traits":          map[string]any{"email": id.email},
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && r.URL.Path == "/admin/identities":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body struct {
				Traits         map[string]string `json:"traits"`
				OrganizationID string            `json:"organization_id"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if body.Traits["email"] == "" {
				http.Error(w, "traits", http.StatusBadRequest)
				return
			}
			f.inviteN++
			id := identityID
			if f.inviteN > 1 {
				id = fmt.Sprintf("id-human-%d", f.inviteN)
			}
			f.inviteEmail = body.Traits["email"]
			f.inviteOrg = body.OrganizationID
			f.identities = append(f.identities, fakeIdent{id: id, org: body.OrganizationID, email: body.Traits["email"]})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              id,
				"schema_id":       "default",
				"schema_url":      "http://127.0.0.1:4434/schemas/default",
				"state":           "active",
				"organization_id": body.OrganizationID,
				"traits":          map[string]any{"email": body.Traits["email"]},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/admin/recovery/code":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"recovery_code": inviteCode,
				"recovery_link": "http://127.0.0.1:4455/recovery?flow=flow-1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	hydra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/oauth2/auth/requests/consent":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"challenge":                       r.URL.Query().Get("consent_challenge"),
				"skip":                            true,
				"subject":                         identityID,
				"requested_scope":                 []string{"openid"},
				"requested_access_token_audience": []string{DefaultClientID},
				"client":                          map[string]any{"client_id": DefaultClientID},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/admin/oauth2/auth/requests/consent/accept":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), humanEmail) || strings.Contains(string(raw), sessionTok) {
				t.Fatal("consent accept leaked a secret")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"redirect_to": "http://127.0.0.1:4460/oidc/callback?code=ok"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/clients/"):
			if f.client == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(f.client)
		case (r.Method == http.MethodPost && r.URL.Path == "/admin/clients") ||
			(r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/admin/clients/")):
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			f.putClient++
			f.client = body
			if r.Method == http.MethodPost {
				if grants, ok := body["grant_types"].([]any); ok && len(grants) == 1 && grants[0] == "client_credentials" {
					body["client_secret"] = "hydra-agent-secret"
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(kratos.Close)
	t.Cleanup(hydra.Close)
	return kratos.URL, hydra.URL
}

func newGlue(t *testing.T, kratosURL, hydraURL string) *Glue {
	t.Helper()
	g, err := New(Config{
		KratosPublic: kratosURL,
		HydraAdmin:   hydraURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestHandlerConsentAcceptsWithoutScreen(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)

	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?consent_challenge=c-1", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://127.0.0.1:4460/oidc/callback") {
		t.Fatalf("location %q", loc)
	}
	if strings.Contains(loc, humanEmail) || strings.Contains(loc, sessionTok) {
		t.Fatal("secret leaked")
	}
}

func TestEnsureFirstPartySkipsConsent(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)

	err := g.EnsureFirstParty(context.Background(), FirstParty{
		RedirectURL: "http://127.0.0.1:4460/oidc/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ory.putClient != 1 {
		t.Fatalf("write %d", ory.putClient)
	}
	if ory.client["client_id"] != DefaultClientID {
		t.Fatalf("client_id %v", ory.client["client_id"])
	}
	if ory.client["skip_consent"] != true {
		t.Fatalf("skip_consent %v", ory.client["skip_consent"])
	}
	if ory.client["token_endpoint_auth_method"] != "none" {
		t.Fatalf("auth %v", ory.client["token_endpoint_auth_method"])
	}
	uris, _ := ory.client["redirect_uris"].([]any)
	if len(uris) != 1 || uris[0] != "http://127.0.0.1:4460/oidc/callback" {
		t.Fatalf("redirect %v", ory.client["redirect_uris"])
	}
	raw, _ := json.Marshal(ory.client)
	if strings.Contains(string(raw), humanEmail) || strings.Contains(string(raw), sessionTok) {
		t.Fatal("secret leaked into client")
	}
}

func TestEnsureFirstPartyAddsSPAWithoutDroppingLaptop(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)

	err := g.EnsureFirstParty(context.Background(), FirstParty{
		RedirectURL: "http://127.0.0.1:4460/oidc/callback",
		RedirectURLs: []string{
			"https://app.veil.nyc/oidc/callback",
			"http://127.0.0.1:4470/oidc/callback",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	uris, _ := ory.client["redirect_uris"].([]any)
	if len(uris) != 3 {
		t.Fatalf("redirect %v", ory.client["redirect_uris"])
	}
	joined := fmt.Sprint(uris)
	if !strings.Contains(joined, "127.0.0.1:4460") || !strings.Contains(joined, "app.veil.nyc") {
		t.Fatalf("redirect %v", uris)
	}
}

func TestEnsureAgentIsNotAHuman(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)

	got, err := g.EnsureAgent(context.Background(), AgentClient{ID: AgentClientID("flue")})
	if err != nil {
		t.Fatal(err)
	}
	if ory.inviteN != 0 {
		t.Fatal("called kratos")
	}
	if got.ID != "agent-flue" {
		t.Fatalf("id %q", got.ID)
	}
	if got.Secret != "hydra-agent-secret" {
		t.Fatalf("secret %q", got.Secret)
	}
	if ory.client["access_token_strategy"] != "jwt" {
		t.Fatalf("strategy %v", ory.client["access_token_strategy"])
	}
	if ory.client["token_endpoint_auth_method"] != "client_secret_basic" {
		t.Fatalf("auth %v", ory.client["token_endpoint_auth_method"])
	}
	grants, _ := ory.client["grant_types"].([]any)
	if len(grants) != 1 || grants[0] != "client_credentials" {
		t.Fatalf("grants %v", ory.client["grant_types"])
	}
	if _, ok := ory.client["redirect_uris"]; ok {
		t.Fatalf("redirect %v", ory.client["redirect_uris"])
	}
	raw, _ := json.Marshal(ory.client)
	if strings.Contains(string(raw), humanEmail) || strings.Contains(string(raw), sessionTok) {
		t.Fatal("human leaked into agent client")
	}

	again, err := g.EnsureAgent(context.Background(), AgentClient{ID: AgentClientID("flue")})
	if err != nil {
		t.Fatal(err)
	}
	if again.Secret != "" {
		t.Fatal("reissued the secret")
	}
}

func TestEnsureAgentRejectsFirstPartyID(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)
	if _, err := g.EnsureAgent(context.Background(), AgentClient{ID: DefaultClientID}); err == nil {
		t.Fatal("created the human client as an agent")
	}
	if ory.putClient != 0 {
		t.Fatal("wrote hydra")
	}
}

func TestInviteIdentityUsesKratosRecoveryCode(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)
	got, err := g.InviteIdentity(context.Background(), "second@example.com", "", LocalOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IdentityID != identityID {
		t.Fatalf("id %q", got.IdentityID)
	}
	if got.Code != inviteCode {
		t.Fatalf("code %q", got.Code)
	}
	if ory.inviteEmail != "second@example.com" {
		t.Fatalf("email %q", ory.inviteEmail)
	}
	if ory.inviteOrg != LocalOrgID {
		t.Fatalf("org %q", ory.inviteOrg)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), inviteCode) {
		t.Fatalf("recovery code in json: %s", raw)
	}
}

func TestInviteIdentityRejectsBadEmail(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g := newGlue(t, k, h)
	if _, err := g.InviteIdentity(context.Background(), "not-an-email", "", LocalOrgID); err == nil {
		t.Fatal("accepted")
	}
}

type ketoTuple struct {
	Namespace string `json:"namespace"`
	Object    string `json:"object"`
	Relation  string `json:"relation"`
	SubjectID string `json:"subject_id"`
}

type fakeKeto struct {
	mu     sync.Mutex
	tuples []ketoTuple
}

func (f *fakeKeto) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/admin/relation-tuples":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body ketoTuple
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.tuples = append(f.tuples, body)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(body)
		case r.URL.Path == "/relation-tuples/check/openapi" || r.URL.Path == "/relation-tuples/check":
			q := r.URL.Query()
			ok := f.has(q.Get("namespace"), q.Get("object"), q.Get("relation"), q.Get("subject_id"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"allowed": ok})
		case r.Method == http.MethodGet && r.URL.Path == "/relation-tuples":
			q := r.URL.Query()
			f.mu.Lock()
			var out []ketoTuple
			for _, tup := range f.tuples {
				if tup.Namespace == q.Get("namespace") && tup.Object == q.Get("object") && tup.Relation == q.Get("relation") {
					out = append(out, tup)
				}
			}
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"relation_tuples": out})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeKeto) has(ns, object, relation, subject string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, tup := range f.tuples {
		if tup.Namespace == ns && tup.Object == object && tup.Relation == relation && tup.SubjectID == subject {
			return true
		}
	}
	return false
}

func TestInviteWritesKetoOwnerAndMember(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	ketoURL := (&fakeKeto{}).start(t)
	g, err := New(Config{
		KratosPublic: k,
		KratosAdmin:  k,
		HydraAdmin:   h,
		KetoRead:     ketoURL,
		KetoWrite:    ketoURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.InviteIdentity(context.Background(), "second@example.com", "", LocalOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if ory.inviteOrg != LocalOrgID {
		t.Fatalf("org %q", ory.inviteOrg)
	}
	member, err := g.Allowed(context.Background(), LocalOrgID, relMembers, got.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if !member {
		t.Fatal("invited human is not a member")
	}
	owner, err := g.Allowed(context.Background(), LocalOrgID, relOwners, got.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if !owner {
		t.Fatal("first human is not the owner")
	}
	stranger, err := g.Allowed(context.Background(), LocalOrgID, relMembers, "stranger")
	if err != nil {
		t.Fatal(err)
	}
	if stranger {
		t.Fatal("stranger is a member")
	}
}

func TestInviteSecondRequiresOwner(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	ketoURL := (&fakeKeto{}).start(t)
	g, err := New(Config{
		KratosPublic: k,
		KratosAdmin:  k,
		HydraAdmin:   h,
		KetoRead:     ketoURL,
		KetoWrite:    ketoURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := g.InviteIdentity(context.Background(), "owner@example.com", "", LocalOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.InviteIdentity(context.Background(), "second@example.com", "", LocalOrgID); err == nil {
		t.Fatal("second invite without owner")
	}
	if _, err := g.InviteIdentity(context.Background(), "second@example.com", "stranger", LocalOrgID); err == nil {
		t.Fatal("stranger invited")
	}
	second, err := g.InviteIdentity(context.Background(), "second@example.com", first.IdentityID, LocalOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if second.IdentityID == first.IdentityID {
		t.Fatal("second invite reused the owner id")
	}
	owner, err := g.Allowed(context.Background(), LocalOrgID, relOwners, second.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if owner {
		t.Fatal("second human is an owner")
	}
	member, err := g.IsMember(context.Background(), LocalOrgID, second.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if !member {
		t.Fatal("second human is not a member")
	}
}

func TestListMembersReturnsIDsNotEmail(t *testing.T) {
	ory := &fakeOry{}
	k, h := ory.start(t)
	g, err := New(Config{
		KratosPublic: k,
		KratosAdmin:  k,
		HydraAdmin:   h,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.InviteIdentity(context.Background(), "second@example.com", "", LocalOrgID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := g.ListMembers(context.Background(), LocalOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != got.IdentityID {
		t.Fatalf("%v", ids)
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "@") || strings.Contains(string(raw), inviteCode) {
		t.Fatalf("secret in list: %s", raw)
	}
}
