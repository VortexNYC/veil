//go:build live

package glue

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
	"github.com/VortexNYC/veil/internal/socket"
	"github.com/google/uuid"
	kratos "github.com/ory/kratos-client-go/v26"
)

const (
	liveKratosPublic = "http://127.0.0.1:4433"
	liveKratosAdmin  = "http://127.0.0.1:4434"
	liveHydraPublic  = "http://127.0.0.1:4444"
	liveHydraAdmin   = "http://127.0.0.1:4445"
	liveKetoRead     = "http://127.0.0.1:4466"
	liveKetoWrite    = "http://127.0.0.1:4467"
	liveBrokerAddr   = "127.0.0.1:4460"
	liveBrokerURL    = "http://127.0.0.1:4460/oidc/callback"
	livePassword     = "live-prove-password-not-a-secret"
)

func TestLiveAuthorize(t *testing.T) {
	waitReady(t, liveKratosPublic+"/health/ready")
	waitReady(t, liveHydraAdmin+"/health/ready")
	waitReady(t, liveKetoRead+"/health/ready")

	g, err := New(Config{
		KratosPublic: liveKratosPublic,
		KratosAdmin:  liveKratosAdmin,
		HydraAdmin:   liveHydraAdmin,
		KetoRead:     liveKetoRead,
		KetoWrite:    liveKetoWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.EnsureFirstParty(context.Background(), FirstParty{RedirectURL: liveBrokerURL}); err != nil {
		t.Fatal(err)
	}

	serve(t, "127.0.0.1:4456", g)
	gotCode := make(chan string, 1)
	serve(t, liveBrokerAddr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		select {
		case gotCode <- code:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	email := fmt.Sprintf("live-%d@example.com", time.Now().UnixNano())
	id, err := createHuman(email)
	if err != nil {
		t.Fatal(err)
	}

	_, ch := pkceChallenge(t)
	noSess, loc := authorize(t, nil, ch)
	if noSess.StatusCode != http.StatusFound && noSess.StatusCode != http.StatusSeeOther {
		t.Fatalf("no session status %d location %q", noSess.StatusCode, loc)
	}
	if !strings.HasPrefix(loc, "http://127.0.0.1:4455/login") {
		t.Fatalf("no session location %q", loc)
	}
	if strings.Contains(loc, "/consent") {
		t.Fatal("consent without a session")
	}

	verifier, ch := pkceChallenge(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	stopped, loc := authorize(t, jar, ch)
	if stopped.StatusCode != http.StatusFound && stopped.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(stopped.Body)
		t.Fatalf("login status %d location %q body %s", stopped.StatusCode, loc, body)
	}
	flowID := flowIDFrom(loc)
	if flowID == "" {
		t.Fatalf("no flow in %q", loc)
	}
	next := completeLogin(t, jar, flowID, email)
	if strings.Contains(next, "/consent") {
		t.Fatal("consent was not skipped")
	}
	withSess, loc := follow(t, jar, next)
	if withSess.StatusCode != http.StatusFound && withSess.StatusCode != http.StatusSeeOther && withSess.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(withSess.Body)
		t.Fatalf("session status %d location %q body %s", withSess.StatusCode, loc, body)
	}
	if strings.Contains(loc, "/consent") || strings.Contains(strings.Join(redirects(withSess), " "), "/consent") {
		t.Fatal("consent was not skipped")
	}
	code := takeCode(t, loc, gotCode)
	if code == "" {
		t.Fatal("no authorization code")
	}

	hv, err := human.New(human.Config{
		Issuer:      liveHydraPublic,
		Audience:    DefaultClientID,
		RedirectURL: liveBrokerURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	idTok, err := hv.Exchange(context.Background(), code, verifier)
	if err != nil {
		t.Fatal(err)
	}
	p, err := hv.Human(context.Background(), idTok)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != id {
		t.Fatalf("subject %q want kratos id %q", p.ID, id)
	}
	if p.Kind != protocol.PrincipalHuman {
		t.Fatalf("%+v", p)
	}
	if strings.Contains(idTok, email) || strings.Contains(idTok, livePassword) {
		t.Fatal("id token carried a secret we mint")
	}

	t.Setenv("VEIL_HYDRA_ISSUER", liveHydraPublic)
	t.Setenv("VEIL_HYDRA_CLIENT_ID", DefaultClientID)
	t.Setenv("VEIL_HYDRA_REDIRECT", liveBrokerURL)
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.Members = g
	a.Provision = g
	p, perr := a.ProvisionHuman(context.Background(), idTok)
	if perr != nil {
		t.Fatal(perr)
	}
	agent, err := a.AddAgentFor(p, "claude")
	if err != nil {
		t.Fatal(err)
	}
	item, err := a.PutItemFor(p, app.ItemOpts{
		Name:  "stripe",
		URI:   "https://example.com",
		Token: []byte("not-the-live-password"),
	})
	if err != nil {
		t.Fatal(err)
	}
	gr, err := a.GrantUntil(p, agent.ID, item.ID, protocol.Level1, nil)
	if err != nil {
		t.Fatal(err)
	}
	ap, err := a.ApproveOIDC(context.Background(), gr.ID, idTok, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ap.HumanID != id {
		t.Fatalf("approved as %q want kratos id %q", ap.HumanID, id)
	}
	if ap.HumanID == app.DefaultHuman {
		t.Fatal("approved as the planted local human")
	}

	joined := loc
	if strings.Contains(joined, livePassword) || strings.Contains(joined, email) {
		t.Fatal("secret leaked in redirect")
	}
}

func TestLiveInvite(t *testing.T) {
	waitReady(t, liveKratosAdmin+"/health/ready")
	waitReady(t, liveKetoRead+"/health/ready")
	orgID := uuid.NewString()
	g, err := New(Config{
		KratosPublic: liveKratosPublic,
		KratosAdmin:  liveKratosAdmin,
		HydraAdmin:   liveHydraAdmin,
		KetoRead:     liveKetoRead,
		KetoWrite:    liveKetoWrite,
		OrgID:        orgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("invite-%d@example.com", time.Now().UnixNano())
	inv, err := g.InviteIdentity(context.Background(), email, "", orgID)
	if err != nil {
		t.Fatal(err)
	}
	if inv.IdentityID == "" || inv.Code == "" {
		t.Fatalf("%+v", inv)
	}
	dir := t.TempDir()
	a, err := app.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	raw, err := os.ReadFile(filepath.Join(dir, "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(inv.Code)) || bytes.Contains(raw, []byte(email)) {
		t.Fatal("invite code or email in the vault")
	}
	humans, err := a.Store.ListHumans()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range humans {
		if h.ID == inv.IdentityID {
			t.Fatal("kratos id cached in sqlite")
		}
	}
	ids, err := g.ListMembers(context.Background(), orgID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == inv.IdentityID {
			found = true
		}
		if strings.Contains(id, "@") {
			t.Fatalf("email in member list: %v", ids)
		}
	}
	if !found {
		t.Fatal("member not in kratos list")
	}
	member, err := g.Allowed(context.Background(), orgID, relMembers, inv.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if !member {
		t.Fatal("keto denied the invited member")
	}
	owner, err := g.Allowed(context.Background(), orgID, relOwners, inv.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if !owner {
		t.Fatal("first invite is not keto owner")
	}
	stranger, err := g.Allowed(context.Background(), orgID, relMembers, "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if stranger {
		t.Fatal("stranger is a keto member")
	}
	org, err := g.IdentityOrg(context.Background(), inv.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if org != orgID {
		t.Fatalf("organization_id %q", org)
	}
	email2 := fmt.Sprintf("invite2-%d@example.com", time.Now().UnixNano())
	if _, err := g.InviteIdentity(context.Background(), email2, "", orgID); err == nil {
		t.Fatal("second invite without owner")
	}
	second, err := g.InviteIdentity(context.Background(), email2, inv.IdentityID, orgID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := g.IsMember(context.Background(), orgID, second.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("second invite is not a member")
	}
	owner2, err := g.Allowed(context.Background(), orgID, relOwners, second.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if owner2 {
		t.Fatal("second invite is an owner")
	}
}

func TestLiveAgentClient(t *testing.T) {
	waitReady(t, liveHydraAdmin+"/health/ready")

	g, err := NewHydra(liveHydraAdmin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("a%x", time.Now().UnixNano())
	cred, err := g.EnsureAgent(context.Background(), AgentClient{
		ID:       AgentClientID(name),
		Audience: DefaultClientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Secret == "" {
		t.Fatal("hydra did not mint a secret")
	}
	raw, err := ClientCredentials(context.Background(), liveHydraPublic, cred.ID, cred.Secret, cred.Audience)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, cred.Secret) {
		t.Fatal("jwt carried the client secret")
	}

	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.AddAgent(name); err != nil {
		t.Fatal(err)
	}
	w, err := a.BindWorkload(name, liveHydraPublic, cred.ID, cred.Audience)
	if err != nil {
		t.Fatal(err)
	}
	if w.Subject != cred.ID {
		t.Fatalf("bound subject %q", w.Subject)
	}

	got, err := a.AgentFromOIDC(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != name || got.Kind != protocol.PrincipalAgent {
		t.Fatalf("%+v", got)
	}
}

func TestLiveLaptopSocket(t *testing.T) {
	waitReady(t, liveHydraAdmin+"/health/ready")

	g, err := NewHydra(liveHydraAdmin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("a%x", time.Now().UnixNano())
	cred, err := g.EnsureAgent(context.Background(), AgentClient{
		ID:       AgentClientID(name),
		Audience: DefaultClientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ClientCredentials(context.Background(), liveHydraPublic, cred.ID, cred.Secret, cred.Audience)
	if err != nil {
		t.Fatal(err)
	}

	const itemSecret = "sk_live_SOCKET_LIVE"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok:"+r.Header.Get("Authorization"))
	}))
	t.Cleanup(upstream.Close)

	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.AddAgent(name); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddItem("stripe", upstream.URL, []byte(itemSecret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant(name, "stripe", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindWorkload(name, liveHydraPublic, cred.ID, cred.Audience); err != nil {
		t.Fatal(err)
	}

	sockDir, err := os.MkdirTemp("/tmp", "veil")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	srv, err := socket.Listen(a, filepath.Join(sockDir, socket.Name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	c := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", srv.Path)
			},
		},
	}
	body := `{"item":"stripe","url":"` + upstream.URL + `","method":"GET"}`
	req, err := http.NewRequest(http.MethodPost, "http://veil/use", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", resp.StatusCode, out)
	}
	if scrub.Contains(out, []byte(itemSecret)) || strings.Contains(string(out), cred.Secret) {
		t.Fatalf("secret leaked: %s", out)
	}
	var got struct {
		Decision protocol.Decision `json:"decision"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("%s", out)
	}
}

func waitReady(t *testing.T, raw string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		resp, err := http.Get(raw)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("identity stack is not up (%s): %v — run make identity-up", raw, last)
}

func serve(t *testing.T, addr string, h http.Handler) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func createHuman(email string) (string, error) {
	cfg := kratos.NewConfiguration()
	cfg.Servers = kratos.ServerConfigurations{{URL: liveKratosAdmin}}
	admin := kratos.NewAPIClient(cfg)
	pwCfg := kratos.NewIdentityWithCredentialsPasswordConfig()
	pwCfg.SetPassword(livePassword)
	pwc := kratos.NewIdentityWithCredentialsPassword()
	pwc.SetConfig(*pwCfg)
	creds := kratos.NewIdentityWithCredentials()
	creds.SetPassword(*pwc)
	body := kratos.NewCreateIdentityBody("default", map[string]any{"email": email})
	body.SetCredentials(*creds)
	body.SetOrganizationId(LocalOrgID)
	id, _, err := admin.IdentityAPI.CreateIdentity(context.Background()).CreateIdentityBody(*body).Execute()
	if err != nil {
		return "", err
	}
	return id.GetId(), nil
}

func completeLogin(t *testing.T, jar http.CookieJar, flowID, email string) string {
	t.Helper()
	u, err := url.Parse(liveKratosPublic)
	if err != nil {
		t.Fatal(err)
	}
	cfg := kratos.NewConfiguration()
	cfg.Servers = kratos.ServerConfigurations{{URL: liveKratosPublic}}
	front := kratos.NewAPIClient(cfg)
	flow, resp, err := front.FrontendAPI.GetLoginFlow(context.Background()).
		Id(flowID).
		Cookie(cookieHeader(jar, liveKratosPublic)).
		Execute()
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		jar.SetCookies(u, resp.Cookies())
	}
	csrf := loginCSRF(flow)
	if csrf == "" {
		t.Fatal("no csrf token")
	}
	pw := kratos.NewUpdateLoginFlowWithPasswordMethod(email, "password", livePassword)
	pw.SetCsrfToken(csrf)
	body := kratos.UpdateLoginFlowWithPasswordMethodAsUpdateLoginFlowBody(pw)
	out, resp, err := front.FrontendAPI.UpdateLoginFlow(context.Background()).
		Flow(flowID).
		Cookie(cookieHeader(jar, liveKratosPublic)).
		UpdateLoginFlowBody(body).
		Execute()
	if resp != nil {
		jar.SetCookies(u, resp.Cookies())
	}
	if err != nil {
		if next := hydraRedirect(err); next != "" {
			return next
		}
		t.Fatal(err)
	}
	for _, c := range out.GetContinueWith() {
		if c.ContinueWithRedirectBrowserTo != nil {
			next := c.ContinueWithRedirectBrowserTo.GetRedirectBrowserTo()
			if next != "" {
				return next
			}
		}
	}
	t.Fatal("login did not return hydra redirect")
	return ""
}

func hydraRedirect(err error) string {
	var gen *kratos.GenericOpenAPIError
	if !errors.As(err, &gen) {
		return ""
	}
	var body struct {
		RedirectBrowserTo string `json:"redirect_browser_to"`
	}
	if json.Unmarshal(gen.Body(), &body) != nil {
		return ""
	}
	return body.RedirectBrowserTo
}

func loginCSRF(flow *kratos.LoginFlow) string {
	if flow == nil {
		return ""
	}
	for _, n := range flow.Ui.Nodes {
		in := n.Attributes.UiNodeInputAttributes
		if in == nil || in.GetName() != "csrf_token" {
			continue
		}
		if s, ok := in.GetValue().(string); ok {
			return s
		}
	}
	return ""
}

func cookieHeader(jar http.CookieJar, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	var parts []string
	for _, c := range jar.Cookies(u) {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func pkceChallenge(t *testing.T) (verifier, challenge string) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorize(t *testing.T, jar http.CookieJar, challenge string) (*http.Response, string) {
	t.Helper()
	state := fmt.Sprintf("st-%d", time.Now().UnixNano())
	q := url.Values{
		"client_id":             {DefaultClientID},
		"redirect_uri":          {liveBrokerURL},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return follow(t, jar, liveHydraPublic+"/oauth2/auth?"+q.Encode())
}

func follow(t *testing.T, jar http.CookieJar, start string) (*http.Response, string) {
	t.Helper()
	if jar == nil {
		var err error
		jar, err = cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	var last string
	c := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			last = req.URL.String()
			if strings.HasPrefix(req.URL.String(), "http://127.0.0.1:4455/") {
				return http.ErrUseLastResponse
			}
			if len(via) > 16 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, start, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	loc := resp.Header.Get("Location")
	if loc == "" {
		loc = last
		if resp.Request != nil {
			loc = resp.Request.URL.String()
		}
	}
	return resp, loc
}

func flowIDFrom(loc string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return ""
	}
	return u.Query().Get("flow")
}

func takeCode(t *testing.T, loc string, gotCode <-chan string) string {
	t.Helper()
	if u, err := url.Parse(loc); err == nil {
		if c := u.Query().Get("code"); c != "" {
			return c
		}
	}
	select {
	case c := <-gotCode:
		return c
	case <-time.After(2 * time.Second):
		t.Fatalf("no code. last location %q", loc)
		return ""
	}
}

func redirects(resp *http.Response) []string {
	if resp == nil || resp.Request == nil {
		return nil
	}
	return []string{resp.Request.URL.String(), resp.Header.Get("Location")}
}
