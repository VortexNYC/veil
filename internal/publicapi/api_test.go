package publicapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
	"github.com/VortexNYC/veil/internal/store"
)

const secret = "sk_live_API_SECRET"

func testApp(t *testing.T) *app.App {
	t.Helper()
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func identity(a *app.App) func(context.Context, string) (protocol.Principal, error) {
	return func(_ context.Context, raw string) (protocol.Principal, error) {
		switch {
		case raw == "human":
			return protocol.Principal{Kind: protocol.PrincipalHuman, ID: "self", OrgID: protocol.LocalOrgID}, nil
		case raw == "member":
			return protocol.Principal{Kind: protocol.PrincipalHuman, ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OrgID: protocol.LocalOrgID}, nil
		case strings.HasPrefix(raw, "agent"):
			agentID := "claude"
			if rest := strings.TrimPrefix(raw, "agent"); rest != "" {
				agentID = strings.TrimPrefix(rest, "-")
			}
			if agentID == "" {
				agentID = "claude"
			}
			p, err := a.Store.Agent(agentID)
			if err == nil {
				return p, nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return protocol.Principal{}, err
			}
			// Tests that only need an agent principal kind may run without creating that agent.
			return protocol.Principal{Kind: protocol.PrincipalAgent, ID: agentID, OrgID: protocol.LocalOrgID}, nil
		default:
			return protocol.Principal{}, fmt.Errorf("unauthorized")
		}
	}
}

func apiServer(t *testing.T, a *app.App) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	(&Server{App: a, Identity: identity(a)}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func doRaw(t *testing.T, srv *httptest.Server, method, path, token string, body []byte, contentType string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func TestOwnerCreateItemSecretAbsentFromResponse(t *testing.T) {
	a := testApp(t)
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name:   "github",
		URI:    "https://api.github.com",
		Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("create echoed secret")
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("list leaked secret")
	}
	if !bytes.Contains(raw, []byte("github")) {
		t.Fatalf("%s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Agent: "claude",
		Item:  "github",
		Level: "level2",
	})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("grant leaked secret")
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "agent", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte("github")) {
		t.Fatalf("agent list %s", raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("agent list leaked secret")
	}
}

func TestAgentCannotCreateItemOrFill(t *testing.T) {
	a := testApp(t)
	if _, err := a.AddItem("github", "https://api.github.com", []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "github", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "agent", CreateItemRequest{Name: "x", Secret: secret})
	if code != http.StatusForbidden {
		t.Fatalf("%d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "agent", FillLoginsRequest{URL: "https://api.github.com"})
	if code != http.StatusForbidden {
		t.Fatalf("fill %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/sync", "agent", FillSyncRequest{})
	if code != http.StatusForbidden {
		t.Fatalf("sync %d %s", code, raw)
	}
	code, raw = doRaw(t, srv, http.MethodPost, "/v1/import?filename=x.csv", "agent", []byte("name,url,username,password\nx,https://x,a,p\n"), "text/csv")
	if code != http.StatusForbidden {
		t.Fatalf("import %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{URL: "https://api.github.com/user"})
	if code != http.StatusOK {
		t.Fatalf("human fill %d %s", code, raw)
	}
	if !scrub.Contains(raw, []byte(secret)) {
		t.Fatal("human fill missing password")
	}
	code, specRaw := doJSON(t, srv, http.MethodGet, "/openapi.json", "", nil)
	if code != http.StatusOK {
		t.Fatalf("spec %d", code)
	}
	if bytes.Contains(specRaw, []byte("/v1/fill")) {
		t.Fatal("fill is on the generated contract")
	}
	pk, err := json.Marshal(map[string]any{
		"challenge": "dGVzdGNoYWxsZW5nZQ",
		"rpId":      "github.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/passkeys/get", "agent", FillPasskeysRequest{
		Origin:    "https://github.com",
		PublicKey: pk,
	})
	if code != http.StatusForbidden {
		t.Fatalf("agent passkeys %d %s", code, raw)
	}
}

func TestMemberCreateItemTheyOwn(t *testing.T) {
	const member = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	const nf = "nf_login_secret"
	a := testApp(t)
	a.Members = fakeMembers{members: map[string]bool{member: true}}
	if _, err := a.AddItem("github", "https://api.github.com", []byte(secret)); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "member", CreateItemRequest{
		Name:   "netflix",
		URI:    "https://www.netflix.com",
		Secret: nf,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(nf)) {
		t.Fatal("create echoed secret")
	}
	var created protocol.Item
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if created.Owner.Kind != protocol.OwnerUser || created.Owner.ID != member {
		t.Fatalf("owner stamp %+v", created.Owner)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "member", nil)
	if code != http.StatusOK {
		t.Fatalf("member list %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte("netflix")) || bytes.Contains(raw, []byte("github")) {
		t.Fatalf("member list %s", raw)
	}
	if scrub.Contains(raw, []byte(nf)) || scrub.Contains(raw, []byte(secret)) {
		t.Fatal("member list leaked secret")
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("owner list %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte("github")) || bytes.Contains(raw, []byte("netflix")) {
		t.Fatalf("owner list %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "member", FillLoginsRequest{URL: "https://www.netflix.com/login"})
	if code != http.StatusOK {
		t.Fatalf("member fill own %d %s", code, raw)
	}
	if !scrub.Contains(raw, []byte(nf)) {
		t.Fatal("member fill missing own password")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "member", FillLoginsRequest{URL: "https://api.github.com/user"})
	if code != http.StatusOK {
		t.Fatalf("member fill other %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("member filled ungranted github")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{URL: "https://www.netflix.com/login"})
	if code != http.StatusOK {
		t.Fatalf("owner fill member %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(nf)) {
		t.Fatal("owner filled member netflix without grant")
	}
	code, raw = doJSON(t, srv, http.MethodPatch, "/v1/items/netflix", "member", UpdateItemRequest{Login: "ada"})
	if code != http.StatusOK {
		t.Fatalf("member patch own %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPatch, "/v1/items/netflix", "human", UpdateItemRequest{Login: "other"})
	if code != http.StatusForbidden {
		t.Fatalf("owner patch member %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/items", "agent", CreateItemRequest{Name: "x", Secret: secret})
	if code != http.StatusForbidden {
		t.Fatalf("agent create %d %s", code, raw)
	}
	code, raw = doRaw(t, srv, http.MethodPost, "/v1/import?filename=x.csv", "member", []byte("name,url,username,password\nx,https://x,a,p\n"), "text/csv")
	if code != http.StatusForbidden {
		t.Fatalf("member import %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPatch, "/v1/items/github", "member", UpdateItemRequest{Login: "ada"})
	if code != http.StatusForbidden {
		t.Fatalf("member patch org %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodDelete, "/v1/items/github", "member", nil)
	if code != http.StatusForbidden {
		t.Fatalf("member delete org %d %s", code, raw)
	}
}

func TestFillPathNotInOpenAPI(t *testing.T) {
	if bytes.Contains(Spec, []byte("/v1/fill")) {
		t.Fatal("fill is GetSecret; not on the generated contract")
	}
}

func TestFillSyncHumanOnly(t *testing.T) {
	a := testApp(t)
	if _, err := a.AddItem("github", "https://github.com", []byte(secret)); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("list leaked secret")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/sync", "agent", FillSyncRequest{})
	if code != http.StatusForbidden {
		t.Fatalf("agent sync %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/sync", "human", FillSyncRequest{})
	if code != http.StatusOK {
		t.Fatalf("sync %d %s", code, raw)
	}
	if !scrub.Contains(raw, []byte(secret)) {
		t.Fatal("sync missing material")
	}
	var out FillSyncResponse
	if json.Unmarshal(raw, &out) != nil || len(out.Items) != 1 || out.Items[0].Item.Name != "github" {
		t.Fatalf("sync body %s", raw)
	}
}

func TestFillSyncIncludesCardMaterialNotList(t *testing.T) {
	const pan = "4111111111111111"
	a := testApp(t)
	blob, err := material.PackCard(pan, "12", "2030", "123", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItem(app.ItemOpts{Name: "amex", Kind: protocol.ItemCard, Token: blob}); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(pan)) {
		t.Fatal("list leaked pan")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/sync", "human", FillSyncRequest{})
	if code != http.StatusOK {
		t.Fatalf("sync %d %s", code, raw)
	}
	if !scrub.Contains(raw, []byte(pan)) {
		t.Fatal("sync missing pan")
	}
	var out FillSyncResponse
	if json.Unmarshal(raw, &out) != nil || len(out.Items) != 1 || out.Items[0].Item.Kind != protocol.ItemCard {
		t.Fatalf("sync body %s", raw)
	}
}

func TestFillLoginIsUsernameNotName(t *testing.T) {
	const login = "stripe@example.com"
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name:   "stripe",
		URI:    "https://dashboard.stripe.com",
		Secret: secret,
		Login:  login,
	})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("create echoed secret")
	}
	if !scrub.Contains(raw, []byte(login)) {
		t.Fatal("create omitted login")
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("list leaked secret")
	}
	if !scrub.Contains(raw, []byte(login)) {
		t.Fatal("list omitted login")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{URL: "https://dashboard.stripe.com/login"})
	if code != http.StatusOK {
		t.Fatalf("fill %d %s", code, raw)
	}
	var got FillLoginsResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Login != login || got.Entries[0].Name != "stripe" || got.Entries[0].Password != secret {
		t.Fatalf("%+v", got)
	}
	code, raw = doJSON(t, srv, http.MethodPatch, "/v1/items/stripe", "human", UpdateItemRequest{Login: "other@example.com"})
	if code != http.StatusOK {
		t.Fatalf("update %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("update echoed secret")
	}
	if !scrub.Contains(raw, []byte("other@example.com")) {
		t.Fatal("update omitted login")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{URL: "https://dashboard.stripe.com/login"})
	if code != http.StatusOK {
		t.Fatalf("fill after update %d %s", code, raw)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Login != "other@example.com" || got.Entries[0].Password != secret {
		t.Fatalf("update rotated or missed login: %+v", got)
	}
}

func TestListIsChooseFillIsExecute(t *testing.T) {
	const login = "stripe@example.com"
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name:   "stripe",
		URI:    "https://dashboard.stripe.com",
		Secret: secret,
		Login:  login,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("list leaked secret")
	}
	var listed ItemsResponse
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	var hit []protocol.Item
	for _, item := range listed.Items {
		if grant.HostAllowed(item, "https://dashboard.stripe.com/login") {
			hit = append(hit, item)
		}
	}
	if len(hit) != 1 || hit[0].Login != login || hit[0].Name != "stripe" {
		t.Fatalf("choose from GET /v1/items without fill: %+v", hit)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{URL: "https://dashboard.stripe.com/login"})
	if code != http.StatusOK {
		t.Fatalf("fill %d %s", code, raw)
	}
	var filled FillLoginsResponse
	if err := json.Unmarshal(raw, &filled); err != nil {
		t.Fatal(err)
	}
	if len(filled.Entries) != 1 || filled.Entries[0].Login != login || filled.Entries[0].Password != secret {
		t.Fatalf("execute %+v", filled)
	}
}

func TestFillLoginsByUUIDDecryptsOne(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	secretA := secret + "-a"
	secretB := secret + "-b"
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "stripe-a", URI: "https://dashboard.stripe.com", Secret: secretA, Login: "a@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("a %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "stripe-b", URI: "https://dashboard.stripe.com", Secret: secretB, Login: "b@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("b %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{URL: "https://dashboard.stripe.com/login"})
	if code != http.StatusOK {
		t.Fatalf("url %d %s", code, raw)
	}
	var all FillLoginsResponse
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 2 {
		t.Fatalf("url form %+v", all)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{UUID: "stripe-a"})
	if code != http.StatusOK {
		t.Fatalf("uuid %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secretB)) {
		t.Fatal("uuid fill decrypted the other item")
	}
	var one FillLoginsResponse
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Entries) != 1 || one.Entries[0].UUID != "stripe-a" || one.Entries[0].Login != "a@example.com" || one.Entries[0].Password != secretA {
		t.Fatalf("uuid form %+v", one)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{UUID: "missing"})
	if code != http.StatusBadRequest {
		t.Fatalf("missing %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "agent", FillLoginsRequest{UUID: "stripe-a"})
	if code != http.StatusForbidden {
		t.Fatalf("agent %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{MintTOTP: true})
	if code != http.StatusBadRequest {
		t.Fatalf("mint without uuid %d %s", code, raw)
	}
}

func TestFillLoginsMintTotp(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "stripe", URI: "https://dashboard.stripe.com", Secret: secret, TOTPSeed: seed,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{UUID: "stripe"})
	if code != http.StatusOK {
		t.Fatalf("star %d %s", code, raw)
	}
	var star FillLoginsResponse
	if err := json.Unmarshal(raw, &star); err != nil {
		t.Fatal(err)
	}
	if len(star.Entries) != 1 || star.Entries[0].TOTP != "*" || star.Entries[0].Password != secret {
		t.Fatalf("%+v", star)
	}
	if star.Entries[0].TOTP == seed {
		t.Fatal("returned the seed")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", FillLoginsRequest{UUID: "stripe", MintTOTP: true})
	if code != http.StatusOK {
		t.Fatalf("mint %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(seed)) {
		t.Fatal("mint leaked seed")
	}
	var minted FillLoginsResponse
	if err := json.Unmarshal(raw, &minted); err != nil {
		t.Fatal(err)
	}
	if len(minted.Entries) != 1 || len(minted.Entries[0].TOTP) != 6 || minted.Entries[0].TOTP == "*" || minted.Entries[0].Password != secret {
		t.Fatalf("mint %+v", minted)
	}
}

func TestUpdateItemURIAddsWithoutDropping(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name:   "github",
		URI:    "https://api.github.com",
		Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPatch, "/v1/items/github", "human", UpdateItemRequest{URI: "https://github.com"})
	if code != http.StatusOK {
		t.Fatalf("add %d %s", code, raw)
	}
	var item protocol.Item
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if len(item.URIs) != 2 || item.URIs[0] != "https://api.github.com" || item.URIs[1] != "https://github.com" {
		t.Fatalf("uri dropped a host: %+v", item.URIs)
	}
	code, raw = doJSON(t, srv, http.MethodPatch, "/v1/items/github", "human", UpdateItemRequest{URIs: []string{"https://github.com"}})
	if code != http.StatusOK {
		t.Fatalf("replace %d %s", code, raw)
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if len(item.URIs) != 1 || item.URIs[0] != "https://github.com" {
		t.Fatalf("uris did not replace: %+v", item.URIs)
	}
}

type fakeMembers struct {
	owners  map[string]bool
	members map[string]bool
}

func (f fakeMembers) IsMember(_ context.Context, _, id string) (bool, error) {
	return f.members[id], nil
}

func (f fakeMembers) IsOwner(_ context.Context, _, id string) (bool, error) {
	return f.owners[id], nil
}

func TestHumanGrantAPI(t *testing.T) {
	const member = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	a := testApp(t)
	a.Members = fakeMembers{members: map[string]bool{member: true}}
	if err := a.Store.PutHuman(protocol.Principal{Kind: protocol.PrincipalHuman, ID: member, OrgID: a.OrgID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddItem("github", "https://api.github.com", []byte(secret)); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "member", FillLoginsRequest{URL: "https://api.github.com/user"})
	if code != http.StatusOK {
		t.Fatalf("member fill before grant %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("member filled without grant")
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/grants", "member", CreateGrantRequest{
		Human: member,
		Item:  "github",
		Level: "level2",
	})
	if code != http.StatusForbidden {
		t.Fatalf("member created grant %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/grants", "member", nil)
	if code != http.StatusForbidden {
		t.Fatalf("member listed grants %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Human: "not-an-email@example.com",
		Item:  "github",
		Level: "level2",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("email as human %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Agent: "claude",
		Human: member,
		Item:  "github",
		Level: "level2",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("agent and human %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Human: member,
		Item:  "github",
		Level: "level2",
	})
	if code != http.StatusOK {
		t.Fatalf("human grant %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("grant leaked secret")
	}
	if !bytes.Contains(raw, []byte(member)) {
		t.Fatalf("grant missing grantee %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/logins", "member", FillLoginsRequest{URL: "https://api.github.com/user"})
	if code != http.StatusOK {
		t.Fatalf("member fill %d %s", code, raw)
	}
	if !scrub.Contains(raw, []byte(secret)) {
		t.Fatal("granted member missing password")
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "member", nil)
	if code != http.StatusOK {
		t.Fatalf("member list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("member list leaked secret")
	}
	if !bytes.Contains(raw, []byte("github")) {
		t.Fatalf("member list %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/grants", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("owner list grants %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte(member)) || !bytes.Contains(raw, []byte("github")) {
		t.Fatalf("owner grants %s", raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("grant list leaked secret")
	}
}

func TestOwnerAgentsNoSecret(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/agents", "human", CreateAgentRequest{Name: "flue"})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("create agent leaked secret")
	}
	if !bytes.Contains(raw, []byte(`"flue"`)) {
		t.Fatalf("create %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/agents", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte("flue")) {
		t.Fatalf("list %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/agents", "agent", nil)
	if code != http.StatusForbidden {
		t.Fatalf("agent list %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/agents", "member", nil)
	if code != http.StatusForbidden {
		t.Fatalf("member list agents %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/agents", "member", CreateAgentRequest{Name: "nope"})
	if code != http.StatusForbidden {
		t.Fatalf("member create agent %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/events", "member", nil)
	if code != http.StatusForbidden {
		t.Fatalf("member listed events %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/events", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("owner events %d %s", code, raw)
	}
}

func TestCORSPreflightVaultOrigin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/items", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(CORS(mux))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/v1/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.veil.nyc")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "authorization")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://app.veil.nyc" {
		t.Fatalf("origin %q", got)
	}
}

func TestCORSUnknownOrigin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/items", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(CORS(mux))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("origin %q", got)
	}
}

func TestCreateCardFieldsNeverReturned(t *testing.T) {
	const pan = "4111111111111111"
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "amex",
		Kind: "card",
		Card: &CardFields{Number: pan, ExpMonth: "12", ExpYear: "2030", CVV: "123", Holder: "Ada"},
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(pan)) || scrub.Contains(raw, []byte("123")) {
		t.Fatal("create echoed card")
	}
	var item protocol.Item
	if json.Unmarshal(raw, &item) != nil || item.Kind != protocol.ItemCard {
		t.Fatalf("%s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(pan)) {
		t.Fatal("list leaked pan")
	}
}

func TestCreateIdentityFieldsNeverReturned(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name:     "home",
		Kind:     "identity",
		Identity: &IdentityFields{GivenName: "Ada", FamilyName: "Lovelace", Address: "1 Street", Phone: "+44"},
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte("1 Street")) || scrub.Contains(raw, []byte("+44")) {
		t.Fatal("create echoed identity")
	}
}

func TestImportCSVNoSecretInResponse(t *testing.T) {
	const pass = "s3cret"
	a := testApp(t)
	srv := apiServer(t, a)
	body := []byte("name,url,username,password\nGitHub,https://github.com,ada," + pass + "\n")
	code, raw := doRaw(t, srv, http.MethodPost, "/v1/import?filename=chrome.csv", "human", body, "text/csv")
	if code != http.StatusOK {
		t.Fatalf("import %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(pass)) {
		t.Fatal("import echoed password")
	}
	var got ImportResponse
	if json.Unmarshal(raw, &got) != nil || got.Count != 1 || got.Names[0] != "GitHub" {
		t.Fatalf("%s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(pass)) {
		t.Fatal("list leaked password")
	}
}

func TestSandboxSessionMintUsesAsAgent(t *testing.T) {
	a := testApp(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"echo":"`+r.Header.Get("Authorization")+`"}`)
	}))
	t.Cleanup(upstream.Close)
	if _, err := a.AddItem("stripe", upstream.URL, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)

	code, raw := doJSON(t, srv, http.MethodPost, "/v1/sessions", "human", CreateSessionRequest{Agent: "claude", TTL: "15m"})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	var created CreateSessionResponse
	if err := json.Unmarshal(raw, &created); err != nil || created.Token == "" || created.AgentID != "claude" {
		t.Fatalf("create body %s", raw)
	}
	if !app.IsSessionToken(created.Token) {
		t.Fatalf("token %s", created.Token)
	}

	code, raw = doJSON(t, srv, http.MethodGet, "/v1/sessions", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(created.Token)) {
		t.Fatal("list leaked session token")
	}
	if bytes.Contains(raw, []byte(`"token"`)) {
		t.Fatalf("list included token field %s", raw)
	}

	code, raw = doJSON(t, srv, http.MethodPost, "/v1/use", created.Token, UseRequest{
		Item:   "stripe",
		URL:    upstream.URL + "/v1",
		Method: http.MethodGet,
	})
	if code != http.StatusOK {
		t.Fatalf("use %d %s", code, raw)
	}
	var used UseResponse
	if err := json.Unmarshal(raw, &used); err != nil {
		t.Fatal(err)
	}
	if used.Decision != protocol.DecisionAllow {
		t.Fatalf("use %+v %s", used, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("use leaked secret")
	}

	code, raw = doJSON(t, srv, http.MethodPost, "/v1/items", created.Token, CreateItemRequest{Name: "x", Secret: secret})
	if code != http.StatusForbidden {
		t.Fatalf("session create item %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/sessions", created.Token, CreateSessionRequest{Agent: "claude"})
	if code != http.StatusForbidden {
		t.Fatalf("session mint session %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/sessions", "agent", CreateSessionRequest{Agent: "claude"})
	if code != http.StatusForbidden {
		t.Fatalf("agent mint %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/sessions", "member", CreateSessionRequest{Agent: "claude"})
	if code != http.StatusForbidden {
		t.Fatalf("member mint %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/sessions", "member", nil)
	if code != http.StatusForbidden {
		t.Fatalf("member list %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/sessions", "human", CreateSessionRequest{Agent: "claude", TTL: "2h"})
	if code != http.StatusBadRequest {
		t.Fatalf("ttl %d %s", code, raw)
	}
}

func TestSandboxSessionExpiredUnauthorized(t *testing.T) {
	a := testApp(t)
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	srv := apiServer(t, a)
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/sessions", "human", CreateSessionRequest{Agent: "claude", TTL: "2s"})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	var created CreateSessionResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/use", created.Token, UseRequest{
		Item: "stripe",
		URL:  "https://example.com",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("expired %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/sessions", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if bytes.Contains(raw, []byte(created.ID)) {
		t.Fatalf("expired session still listed %s", raw)
	}
}

func TestFillTOTPEnroll(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	const seed = "JBSWY3DPEHPK3PXP"
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "github", URI: "https://github.com", Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("github %d %s", code, raw)
	}
	var gh protocol.Item
	if err := json.Unmarshal(raw, &gh); err != nil || gh.ID == "" {
		t.Fatalf("github item %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/items", "member", CreateItemRequest{
		Name: "netflix", URI: "https://www.netflix.com", Secret: "nf_secret", Login: "ada",
	})
	if code != http.StatusOK {
		t.Fatalf("netflix %d %s", code, raw)
	}
	var nf protocol.Item
	if err := json.Unmarshal(raw, &nf); err != nil || nf.ID == "" {
		t.Fatalf("netflix item %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/totp/enroll", "member", FillTOTPEnrollRequest{
		UUID: nf.ID, TOTPSeed: seed,
	})
	if code != http.StatusOK {
		t.Fatalf("member enroll own %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(seed)) {
		t.Fatal("enroll echoed seed")
	}
	var out FillTOTPEnrollResponse
	if err := json.Unmarshal(raw, &out); err != nil || !out.HasTOTP || out.UUID != nf.ID {
		t.Fatalf("enroll %s", raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/totp/enroll", "member", FillTOTPEnrollRequest{
		UUID: gh.ID, TOTPSeed: seed,
	})
	if code != http.StatusForbidden {
		t.Fatalf("member enroll org %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/totp/enroll", "agent", FillTOTPEnrollRequest{
		UUID: nf.ID, TOTPSeed: seed,
	})
	if code != http.StatusForbidden {
		t.Fatalf("agent enroll %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/fill/totp", "member", FillTOTPRequest{UUID: nf.ID})
	if code != http.StatusOK {
		t.Fatalf("mint %d %s", code, raw)
	}
	var minted FillTOTPResponse
	if err := json.Unmarshal(raw, &minted); err != nil || len(minted.TOTP) != 6 {
		t.Fatalf("mint %s", raw)
	}
	if minted.TOTP == seed {
		t.Fatal("mint returned seed")
	}
}

func TestUseBinaryBodySurvivesWire(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write([]byte(`{"ok":true}`))
	_ = gw.Close()
	gzipped := gz.Bytes()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gzipped)
	}))
	defer upstream.Close()

	doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "cf", URI: upstream.URL, Secret: secret,
	})
	doJSON(t, srv, http.MethodPost, "/v1/agents", "human", CreateAgentRequest{Name: "flue"})
	doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Agent: "flue", Item: "cf", Level: "level2",
	})

	code, raw := doJSON(t, srv, http.MethodPost, "/v1/use", "agent-flue", UseRequest{
		Item: "cf", URL: upstream.URL, Method: http.MethodGet,
		Headers: map[string]string{"Accept-Encoding": "gzip"},
	})
	if code != http.StatusOK {
		t.Fatalf("use %d %s", code, raw)
	}
	var out UseResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Body != "" {
		t.Fatalf("binary body must not be emitted as text: %q", out.Body)
	}
	got, err := base64.StdEncoding.DecodeString(out.BodyB64)
	if err != nil {
		t.Fatalf("body_b64 decode: %v", err)
	}
	if !bytes.Equal(got, gzipped) {
		t.Fatalf("binary body corrupted: got %x want %x", got, gzipped)
	}
	if out.Headers.Get("Content-Encoding") != "gzip" {
		t.Fatalf("headers missing Content-Encoding: %v", out.Headers)
	}
	if out.Headers.Get("Content-Type") != "application/json" {
		t.Fatalf("headers missing Content-Type: %v", out.Headers)
	}
}

func TestUseBinaryRequestBodyB64(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	want := []byte{0x1f, 0x8b, 0x08, 0x00, 0xde, 0xad, 0xbe, 0xef}
	var saw []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer upstream.Close()

	doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name: "cf", URI: upstream.URL, Secret: secret,
	})
	doJSON(t, srv, http.MethodPost, "/v1/agents", "human", CreateAgentRequest{Name: "flue"})
	doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Agent: "flue", Item: "cf", Level: "level2",
	})

	code, raw := doJSON(t, srv, http.MethodPost, "/v1/use", "agent-flue", map[string]any{
		"item": "cf", "url": upstream.URL, "method": http.MethodPost,
		"body_b64": base64.StdEncoding.EncodeToString(want),
	})
	if code != http.StatusOK {
		t.Fatalf("use %d %s", code, raw)
	}
	if !bytes.Equal(saw, want) {
		t.Fatalf("binary request body corrupted: got %x want %x", saw, want)
	}

	code, _ = doJSON(t, srv, http.MethodPost, "/v1/use", "agent-flue", map[string]any{
		"item": "cf", "url": upstream.URL, "method": http.MethodPost,
		"body": "x", "body_b64": base64.StdEncoding.EncodeToString(want),
	})
	if code != http.StatusBadRequest {
		t.Fatalf("body+body_b64 must 400, got %d", code)
	}
}

func TestRevokeAgentEndpoint(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer upstream.Close()

	// Owner creates item, agent, and grant.
	doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{
		Name:   "stripe",
		URI:    upstream.URL,
		Secret: secret,
	})
	doJSON(t, srv, http.MethodPost, "/v1/agents", "human", CreateAgentRequest{Name: "flue"})
	doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Agent: "flue",
		Item:  "stripe",
		Level: "level2",
	})

	// Agent Use is allowed before revoke.
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/use", "agent-flue", UseRequest{
		Item:   "stripe",
		URL:    upstream.URL,
		Method: http.MethodGet,
	})
	if code != http.StatusOK {
		t.Fatalf("use before %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte(`"allow"`)) {
		t.Fatalf("use not allowed: %s", raw)
	}

	// Member cannot revoke.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/agents/flue/revoke", "member", nil)
	if code != http.StatusForbidden {
		t.Fatalf("member revoke %d", code)
	}

	// Owner revokes.
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/agents/flue/revoke", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("revoke %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte(`"revoked_at"`)) {
		t.Fatalf("response missing revoked_at: %s", raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("revoke response leaked secret")
	}

	// Same agent Use now denies and does not leak the secret.
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/use", "agent-flue", UseRequest{
		Item:   "stripe",
		URL:    upstream.URL,
		Method: http.MethodGet,
	})
	if code != http.StatusOK {
		t.Fatalf("use after %d %s", code, raw)
	}
	if !bytes.Contains(raw, []byte(`"deny"`)) || !bytes.Contains(raw, []byte(`agent_revoked`)) {
		t.Fatalf("use after not denied: %s", raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("denied use leaked secret")
	}

	// List is empty for revoked agent.
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/items", "agent-flue", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	var items ItemsResponse
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	if len(items.Items) != 0 {
		t.Fatalf("items for revoked agent: %+v", items.Items)
	}

	// New session mint is denied.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/sessions", "human", CreateSessionRequest{
		Agent: "flue",
		TTL:   "15m",
	})
	if code != http.StatusForbidden {
		t.Fatalf("session %d", code)
	}

	// New grant is denied.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/grants", "human", CreateGrantRequest{
		Agent: "flue",
		Item:  "stripe",
		Level: "level2",
	})
	if code == http.StatusOK {
		t.Fatal("grant after revoke succeeded")
	}

	// Revoke is idempotent.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/agents/flue/revoke", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("idempotent revoke %d", code)
	}

	// Unknown agent returns bad request, not a crash.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/agents/unknown/revoke", "human", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown agent %d", code)
	}

	// Audit includes the revoke event and contains no secret or token.
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/events", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("events %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("audit leaked secret")
	}
	if !bytes.Contains(raw, []byte(`"revoke"`)) {
		t.Fatalf("audit missing revoke: %s", raw)
	}
}

type fakeSubjectVerifier struct{ sub string }

func (f fakeSubjectVerifier) Subject(context.Context, string) (string, error) { return f.sub, nil }

type fakeProvisioner struct{}

func (fakeProvisioner) ProvisionMember(context.Context, string, string) error { return nil }
func (fakeProvisioner) SetIdentityOrg(context.Context, string, string) error  { return nil }

// POST /v1/provision is signup: subject-auth only (no member check), and a
// repeat call returns the same org — provisioning is idempotent.
func TestProvisionEndpoint(t *testing.T) {
	a := testApp(t)
	a.Human = fakeSubjectVerifier{sub: "sub-new"}
	a.Provision = fakeProvisioner{}
	a.Members = fakeMembers{members: map[string]bool{"sub-new": true}}
	// No Identity seam — provisioned humans must resolve through PrincipalFromOIDC.
	mux := http.NewServeMux()
	(&Server{App: a}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	code, raw := doJSON(t, srv, http.MethodPost, "/v1/provision", "tok-new", nil)
	if code != http.StatusOK {
		t.Fatalf("provision %d %s", code, raw)
	}
	var out ProvisionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Subject != "sub-new" || out.OrgID == "" {
		t.Fatalf("provision %+v", out)
	}

	code, raw = doJSON(t, srv, http.MethodPost, "/v1/provision", "tok-new", nil)
	if code != http.StatusOK {
		t.Fatalf("reprovision %d %s", code, raw)
	}
	var again ProvisionResponse
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatal(err)
	}
	if again.OrgID != out.OrgID {
		t.Fatalf("reprovision moved orgs: %q vs %q", out.OrgID, again.OrgID)
	}

	// The provisioned human's token now resolves through the humans row.
	code, _ = doJSON(t, srv, http.MethodGet, "/v1/items", "tok-new", nil)
	if code == http.StatusUnauthorized {
		t.Fatal("provisioned human unauthorized on /v1/items")
	}

	code, _ = doJSON(t, srv, http.MethodPost, "/v1/provision", "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("no-token provision %d", code)
	}
}

type fakeInviter struct{ res app.InviteResult }

func (f fakeInviter) Invite(_ context.Context, _, _, _ string) (app.InviteResult, error) {
	return f.res, nil
}

// POST /v1/invites is the private-alpha gate: provisioned human bearer, email
// body, recovery link stays server-side once mail delivered it.
func TestInviteEndpoint(t *testing.T) {
	a := testApp(t)
	a.Human = fakeSubjectVerifier{sub: "sub-owner"}
	a.Provision = fakeProvisioner{}
	a.Invites = fakeInviter{res: app.InviteResult{IdentityID: "id-1", RecoveryURL: "https://x/recovery", Emailed: true}}
	mux := http.NewServeMux()
	(&Server{App: a}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if code, raw := doJSON(t, srv, http.MethodPost, "/v1/provision", "tok-owner", nil); code != http.StatusOK {
		t.Fatalf("provision %d %s", code, raw)
	}

	code, raw := doJSON(t, srv, http.MethodPost, "/v1/invites", "tok-owner", map[string]string{"email": "new@example.com"})
	if code != http.StatusOK {
		t.Fatalf("invite %d %s", code, raw)
	}
	var out InviteResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.IdentityID != "id-1" || !out.Emailed || out.RecoveryURL != "" {
		t.Fatalf("invite %+v", out)
	}

	code, _ = doJSON(t, srv, http.MethodPost, "/v1/invites", "", map[string]string{"email": "x@example.com"})
	if code != http.StatusUnauthorized {
		t.Fatalf("no-token invite %d", code)
	}
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/invites", "tok-owner", map[string]string{})
	if code != http.StatusBadRequest {
		t.Fatalf("no-email invite %d", code)
	}
}

type countInviter struct{ n int }

func (f *countInviter) Invite(_ context.Context, _, _, _ string) (app.InviteResult, error) {
	f.n++
	return app.InviteResult{IdentityID: "id-n", Emailed: true}, nil
}

// Invite abuse has two caps: three sends a day to one address, twenty sends a
// day per inviter. Refused sends never reach the inviter or burn allowance.
func TestInviteRateLimit(t *testing.T) {
	a := testApp(t)
	a.Human = fakeSubjectVerifier{sub: "sub-owner"}
	a.Provision = fakeProvisioner{}
	fi := &countInviter{}
	a.Invites = fi
	mux := http.NewServeMux()
	(&Server{App: a}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if code, raw := doJSON(t, srv, http.MethodPost, "/v1/provision", "tok-owner", nil); code != http.StatusOK {
		t.Fatalf("provision %d %s", code, raw)
	}

	for i := 0; i < 3; i++ {
		if code, _ := doJSON(t, srv, http.MethodPost, "/v1/invites", "tok-owner", map[string]string{"email": "victim@example.com"}); code != http.StatusOK {
			t.Fatalf("recipient invite %d: %d", i, code)
		}
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/invites", "tok-owner", map[string]string{"email": "victim@example.com"}); code != http.StatusTooManyRequests {
		t.Fatalf("4th to same recipient: %d", code)
	}

	// 3 inviter hits used; 17 more unique recipients exhaust the day.
	for i := 0; i < 17; i++ {
		if code, _ := doJSON(t, srv, http.MethodPost, "/v1/invites", "tok-owner", map[string]string{"email": "u" + string(rune('a'+i)) + "@example.com"}); code != http.StatusOK {
			t.Fatalf("inviter invite %d: %d", i, code)
		}
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/invites", "tok-owner", map[string]string{"email": "one-more@example.com"}); code != http.StatusTooManyRequests {
		t.Fatalf("21st invite: %d", code)
	}
	if fi.n != 20 {
		t.Fatalf("inviter called %d times, want 20", fi.n)
	}
}
