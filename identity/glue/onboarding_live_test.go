//go:build live

package glue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/publicapi"
	"github.com/google/uuid"
)

// TestLiveOnboarding is the private-alpha path end to end against the compose
// identity stack: owner invite -> invite email (link + code) -> invitee setup
// through the public self-service flows (recovery code, password, TOTP) -> a
// real Hydra id_token (amr totp) -> /v1/provision -> /v1/invites -> a second
// invitee who completes the same setup. Nothing is stubbed at the seams that
// production crosses: Kratos, Hydra, Keto, and the mail-worker HTTP contract
// are all real calls. Only the worker itself is an httptest recorder — its
// payload is the contract under test.
func TestLiveOnboarding(t *testing.T) {
	waitReady(t, liveKratosPublic+"/health/ready")
	waitReady(t, liveKratosAdmin+"/health/ready")
	waitReady(t, liveHydraAdmin+"/health/ready")
	waitReady(t, liveKetoRead+"/health/ready")

	var mails []map[string]any
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-mail-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		mails = append(mails, payload)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stub.Close)

	orgID := uuid.NewString()
	g, err := New(Config{
		KratosPublic: liveKratosPublic,
		KratosAdmin:  liveKratosAdmin,
		HydraAdmin:   liveHydraAdmin,
		KetoRead:     liveKetoRead,
		KetoWrite:    liveKetoWrite,
		OrgID:        orgID,
		MailURL:      stub.URL,
		MailToken:    "test-mail-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.EnsureFirstParty(context.Background(), FirstParty{RedirectURL: liveBrokerURL}); err != nil {
		t.Fatal(err)
	}
	serve(t, "127.0.0.1:4456", g) // hydra consent provider

	// The vault + public API the invitee will call. Human verification is the
	// real Hydra verifier; members/provision/invites are the live glue.
	hv, err := human.New(human.Config{
		Issuer:      liveHydraPublic,
		Audience:    DefaultClientID,
		RedirectURL: liveBrokerURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.Human = hv
	a.Members = g
	a.Provision = g
	a.Invites = inviterAdapter{g}
	mux := http.NewServeMux()
	publicapi.Mount(mux, a)
	origin := httptest.NewServer(mux)
	t.Cleanup(origin.Close)

	// --- invitee 1: founder invites (bootstrap actor ""), email carries the code
	email1 := fmt.Sprintf("onboard-%d@example.com", time.Now().UnixNano())
	if _, err := g.InviteIdentity(context.Background(), email1, "", orgID); err != nil {
		t.Fatal(err)
	}
	inv1 := lastInviteMail(t, mails, email1)
	idTok1 := onboardInvitee(t, inv1)

	// --- provision: mints the org, plants the human row, stamps Kratos+Keto
	org1 := provision(t, origin.URL, idTok1)
	if org1 == "" {
		t.Fatal("provision returned no org")
	}

	// --- invitee 1 invites invitee 2 through the public endpoint
	email2 := fmt.Sprintf("onboard2-%d@example.com", time.Now().UnixNano())
	res := invite(t, origin.URL, idTok1, email2)
	if !res.Emailed {
		t.Fatal("invite did not report emailed")
	}
	if res.RecoveryURL != "" {
		t.Fatal("recovery_url leaked in a mailed invite response")
	}
	inv2 := lastInviteMail(t, mails, email2)
	_ = onboardInvitee(t, inv2) // invitee 2 completes the same setup

	// --- negative: nobody gets in without a provisioned human bearer
	code := postInvite(t, origin.URL, "", email2)
	if code != http.StatusUnauthorized {
		t.Fatalf("no-auth invite status %d", code)
	}
	code = postInvite(t, origin.URL, "not-a-token", email2)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad-token invite status %d", code)
	}
}

// inviterAdapter mirrors cli.go's glueInviter — the app.Inviter seam is wired
// the same way the CLI does it.
type inviterAdapter struct{ g *Glue }

func (a inviterAdapter) Invite(ctx context.Context, email, actor, orgID string) (app.InviteResult, error) {
	inv, err := a.g.InviteIdentity(ctx, email, actor, orgID)
	if err != nil {
		return app.InviteResult{}, err
	}
	return app.InviteResult{IdentityID: inv.IdentityID, RecoveryURL: inv.RecoveryLink, Emailed: inv.Emailed}, nil
}

func lastInviteMail(t *testing.T, mails []map[string]any, email string) map[string]any {
	t.Helper()
	for i := len(mails) - 1; i >= 0; i-- {
		if mails[i]["kind"] == "invite" && mails[i]["to"] == email {
			return mails[i]
		}
	}
	t.Fatalf("no invite mail captured for %s", email)
	return nil
}

// onboardInvitee completes the emailed invite exactly as a human would: open
// the link, submit the code, set a password, enroll TOTP — then mints a real
// Hydra id_token through the password+TOTP login dance.
func onboardInvitee(t *testing.T, inv map[string]any) string {
	t.Helper()
	data, _ := inv["data"].(map[string]any)
	link, _ := data["url"].(string)
	code, _ := data["code"].(string)
	if link == "" || code == "" {
		t.Fatalf("invite mail missing url or code: %v", data)
	}
	flowID := flowIDFrom(link)
	if flowID == "" {
		t.Fatalf("invite link carried no flow: %q", link)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// The emailed link points at the (dead) UI host; the SPA it represents does
	// GET /flows?id= then POSTs the code as JSON. The JSON POST still mints the
	// privileged session cookie — the settings submits below need it.
	rec := getFlow(t, c, liveKratosPublic+"/self-service/recovery/flows?id="+flowID)
	if rec["state"] != "sent_email" {
		t.Fatalf("recovery flow state %v", rec["state"])
	}
	payload, _ := json.Marshal(map[string]string{"method": "code", "code": code})
	req, err := http.NewRequest(http.MethodPost,
		liveKratosPublic+"/self-service/recovery?flow="+flowID, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	// A successful code submit on a browser flow is 422
	// browser_location_change_required — the SPA would window.location to the
	// settings UI. The session cookie is already set on this response.
	var redirect string
	if res.StatusCode == http.StatusUnprocessableEntity {
		var env struct {
			RedirectBrowserTo string `json:"redirect_browser_to"`
		}
		_ = json.Unmarshal(out, &env)
		redirect = env.RedirectBrowserTo
	} else if res.StatusCode != http.StatusOK {
		t.Fatalf("recovery code submit: %d %s", res.StatusCode, string(out)[:300])
	}
	u, _ := url.Parse(liveKratosPublic)
	if len(c.Jar.Cookies(u)) == 0 {
		t.Fatal("recovery accepted the code but issued no session cookie")
	}
	setFlowID := flowIDFrom(redirect)
	if setFlowID == "" {
		t.Fatalf("no settings flow after recovery: %q", redirect)
	}

	submitSettings(t, c, setFlowID, map[string]string{
		"method":   "password",
		"password": livePassword,
	})

	// Enroll TOTP: the flow carries the seed in the totp_secret_key node; mint
	// the code with the same material.Mint the CLI uses.
	seed := totpSecret(t, c, setFlowID)
	totpCode, err := material.Mint(seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	submitSettings(t, c, setFlowID, map[string]string{
		"method":    "totp",
		"totp_code": totpCode,
	})
	// Enrollment is silent in the flow JSON; the directory is the assertion.
	requireCreds(t, inviteEmail(inv), "password", "totp")

	// The human token: password + TOTP through Hydra, amr must include totp.
	hv, err := human.New(human.Config{
		Issuer:      liveHydraPublic,
		Audience:    DefaultClientID,
		RedirectURL: liveBrokerURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hv.LoginHTTP(context.Background(), human.HTTPLogin{
		KratosPublic: liveKratosPublic,
		Email:        inviteEmail(inv),
		Password:     livePassword,
		TOTPSeed:     seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func inviteEmail(inv map[string]any) string {
	to, _ := inv["to"].(string)
	return to
}

// submitSettings submits a settings step as the SPA does: fetch the flow's
// csrf token first, then POST JSON with the session cookie, and fail on any
// ui error message — a 200 with an error flow is not a success.
func submitSettings(t *testing.T, c *http.Client, flowID string, fields map[string]string) {
	t.Helper()
	flow := getFlow(t, c, liveKratosPublic+"/self-service/settings/flows?id="+flowID)
	body := map[string]any{}
	for k, v := range fields {
		body[k] = v
	}
	if csrf := flowCSRF(flow); csrf != "" {
		body["csrf_token"] = csrf
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost,
		liveKratosPublic+"/self-service/settings?flow="+flowID, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings submit %v: %d %s", fields["method"], res.StatusCode, string(out)[:300])
	}
	var f struct {
		State string `json:"state"`
		UI    struct {
			Messages []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"messages"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(out, &f); err != nil {
		t.Fatalf("settings submit %v: %s", fields["method"], err)
	}
	for _, m := range f.UI.Messages {
		if m.Type == "error" {
			t.Fatalf("settings submit %v rejected: %s", fields["method"], m.Text)
		}
	}
	if f.State != "success" {
		t.Fatalf("settings submit %v state %q", fields["method"], f.State)
	}
}

// getFlow fetches a self-service flow as JSON.
func getFlow(t *testing.T, c *http.Client, rawURL string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var flow map[string]any
	if err := json.NewDecoder(res.Body).Decode(&flow); err != nil {
		t.Fatalf("get flow: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get flow %s: %d", rawURL, res.StatusCode)
	}
	return flow
}

func flowCSRF(flow map[string]any) string {
	ui, _ := flow["ui"].(map[string]any)
	nodes, _ := ui["nodes"].([]any)
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		attrs, _ := node["attributes"].(map[string]any)
		if attrs["name"] == "csrf_token" {
			s, _ := attrs["value"].(string)
			return s
		}
	}
	return ""
}

// requireCreds asserts the invitee's directory record carries the enrolled
// credential types — the flow JSON says "success" either way.
func requireCreds(t *testing.T, email string, want ...string) {
	t.Helper()
	res, err := http.Get(liveKratosAdmin + "/admin/identities?credentials_identifier=" + url.QueryEscape(email))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var ids []struct {
		Credentials map[string]any `json:"credentials"`
	}
	if err := json.NewDecoder(res.Body).Decode(&ids); err != nil || len(ids) == 0 {
		t.Fatalf("identity lookup for %s: %v", email, err)
	}
	for _, w := range want {
		if _, ok := ids[0].Credentials[w]; !ok {
			t.Fatalf("%s missing %s credential (has %v)", email, w, ids[0].Credentials)
		}
	}
}

// totpSecret reads the settings flow's totp_secret_key node, which carries the
// seed in attributes.text.context.secret.
func totpSecret(t *testing.T, c *http.Client, flowID string) string {
	t.Helper()
	flow := getFlow(t, c, liveKratosPublic+"/self-service/settings/flows?id="+flowID)
	ui, _ := flow["ui"].(map[string]any)
	nodes, _ := ui["nodes"].([]any)
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		attrs, _ := node["attributes"].(map[string]any)
		if attrs["id"] != "totp_secret_key" {
			continue
		}
		text, _ := attrs["text"].(map[string]any)
		ctx, _ := text["context"].(map[string]any)
		if secret, _ := ctx["secret"].(string); secret != "" {
			return secret
		}
	}
	t.Fatal("settings flow has no totp_secret_key")
	return ""
}

func provision(t *testing.T, base, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/v1/provision", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("provision %d: %s", res.StatusCode, string(b)[:200])
	}
	var out struct {
		OrgID string `json:"org_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.OrgID
}

func invite(t *testing.T, base, token, email string) app.InviteResult {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequest(http.MethodPost, base+"/v1/invites", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("invite %d: %s", res.StatusCode, string(b)[:200])
	}
	var out app.InviteResult
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out
}

func postInvite(t *testing.T, base, token, email string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequest(http.MethodPost, base+"/v1/invites", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	return res.StatusCode
}
