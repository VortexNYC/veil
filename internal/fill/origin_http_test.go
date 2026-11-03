package fill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/dpop"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
	"github.com/VortexNYC/veil/internal/scrub"
)

func originAPI(t *testing.T, a *app.App) *httptest.Server {
	t.Helper()
	dpop.ResetDefaultVerifierForTest()
	mux := http.NewServeMux()
	ident := func(_ context.Context, raw string) (protocol.Principal, error) {
		if raw == "human" {
			return protocol.Principal{Kind: protocol.PrincipalHuman, ID: app.DefaultHuman, OrgID: a.OrgID}, nil
		}
		return protocol.Principal{}, fmt.Errorf("unauthorized")
	}
	(&publicapi.Server{App: a, Identity: ident}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func originJSON(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, []byte) {
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

func TestNativeHostFillsRealOriginHTTP(t *testing.T) {
	const login = "stripe@example.com"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name:   "stripe",
		URI:    "https://dashboard.stripe.com",
		Secret: secret,
		Login:  login,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("create echoed secret")
	}
	code, raw = originJSON(t, srv, http.MethodGet, "/v1/items", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("list leaked secret")
	}
	var listed publicapi.ItemsResponse
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	var hit []protocol.Item
	for _, item := range listed.Items {
		if grant.HostAllowed(item, "https://dashboard.stripe.com/login") {
			hit = append(hit, item)
		}
	}
	if len(hit) != 1 || hit[0].Login != login {
		t.Fatalf("choose from list without fill: %+v body=%s", hit, raw)
	}

	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	c := associated(t, h)
	req, err := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{
		Action: "get-logins",
		URL:    "https://dashboard.stripe.com/login",
		Keys:   []assocKey{{ID: assocID, Key: c.idKey}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var filled loginReply
	if err := json.Unmarshal(c.send(t, h, req), &filled); err != nil {
		t.Fatal(err)
	}
	if filled.Success != "true" || len(filled.Entries) != 1 {
		t.Fatalf("origin fill %+v", filled)
	}
	if filled.Entries[0].Login != login || filled.Entries[0].Password != secret {
		t.Fatalf("origin fill entry %+v", filled.Entries[0])
	}

	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name:   "stripe-work",
		URI:    "https://dashboard.stripe.com",
		Secret: secret + "-work",
		Login:  "work@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("second %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/fill/logins", "human", publicapi.FillLoginsRequest{UUID: "stripe"})
	if code != http.StatusOK {
		t.Fatalf("uuid %d %s", code, raw)
	}
	if scrub.Contains(raw, []byte(secret+"-work")) {
		t.Fatal("uuid fill decrypted the other item")
	}
	var one publicapi.FillLoginsResponse
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Entries) != 1 || one.Entries[0].UUID != "stripe" || one.Entries[0].Login != login || one.Entries[0].Password != secret {
		t.Fatalf("uuid fill %+v", one)
	}
}
