package publicapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/store"
)

const webhookSecret = "whsec_test_0123456789abcdef"

func webhookServer(t *testing.T, secret string) (*httptest.Server, *app.App) {
	t.Helper()
	a := testApp(t)
	mux := http.NewServeMux()
	(&Server{App: a, Identity: identity(a), BillingSecret: secret}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, a
}

func signBody(secret string, body string) string {
	ts := time.Now().UnixMilli()
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "%d.", ts)
	m.Write([]byte(body))
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(m.Sum(nil)))
}

func postWebhook(t *testing.T, srv *httptest.Server, body, sig string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/billing/webhook", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if sig != "" {
		req.Header.Set("Vortex-Signature", sig)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

// Feature off: no secret configured → the route does not exist.
func TestBillingWebhookDisabled(t *testing.T) {
	srv, _ := webhookServer(t, "")
	body := `{"id":"e1","type":"subscription.updated","createdAt":1,"data":{}}`
	if got := postWebhook(t, srv, body, signBody("x", body)); got != http.StatusNotFound {
		t.Fatalf("disabled webhook: %d", got)
	}
}

func TestBillingWebhookAuth(t *testing.T) {
	srv, _ := webhookServer(t, webhookSecret)
	body := `{"id":"e1","type":"subscription.updated","createdAt":1,"data":{}}`
	if got := postWebhook(t, srv, body, ""); got != http.StatusUnauthorized {
		t.Fatalf("no signature: %d", got)
	}
	if got := postWebhook(t, srv, body, "t=1,v1=bad"); got != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", got)
	}
	if got := postWebhook(t, srv, body, signBody("wrong-secret", body)); got != http.StatusUnauthorized {
		t.Fatalf("wrong secret: %d", got)
	}
}

// The full loop: signed subscription.updated → org plan flips → the Use gate
// would now read "active".
func TestBillingWebhookFlipsPlan(t *testing.T) {
	srv, a := webhookServer(t, webhookSecret)
	// LocalOrgID is what testApp provisions — the org the webhook targets.
	body := fmt.Sprintf(`{"id":"e1","type":"subscription.created","createdAt":%d,"data":{"subscription":{"customerExternalId":%q,"status":"active"}}}`, time.Now().UnixMilli(), a.OrgID)
	if got := postWebhook(t, srv, body, signBody(webhookSecret, body)); got != http.StatusNoContent {
		t.Fatalf("valid webhook: %d", got)
	}
	ob, err := a.Store.Billing(a.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	if ob.Plan != "active" {
		t.Fatalf("plan=%q want active", ob.Plan)
	}
	// Replay is harmless — same event again is a no-op, not an error.
	if got := postWebhook(t, srv, body, signBody(webhookSecret, body)); got != http.StatusNoContent {
		t.Fatalf("replay: %d", got)
	}
}

// GET /v1/billing is the cap banner's data source: owner-only, absent billing
// row reads plan=free with the configured cap as included.
func TestGetBilling(t *testing.T) {
	a := testApp(t)
	a.Broker.FreeUseCap = 5
	mux := http.NewServeMux()
	(&Server{App: a, Identity: identity(a), BillingUpgradeURL: "https://pay.vortex.nyc/x"}).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	get := func(token string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/billing", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body
	}

	if code, _ := get(""); code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", code)
	}
	if code, _ := get("member"); code != http.StatusForbidden {
		t.Fatalf("member: %d", code)
	}
	code, body := get("human")
	if code != http.StatusOK {
		t.Fatalf("owner: %d", code)
	}
	if body["plan"] != "free" || body["included"] != float64(5) || body["used"] != float64(0) {
		t.Fatalf("billing view: %v", body)
	}
	if body["upgrade_url"] != "https://pay.vortex.nyc/x" {
		t.Fatalf("upgrade_url: %v", body["upgrade_url"])
	}
	// Paid plan → included is null (uncapped).
	if err := a.Store.SetBilling(store.OrgBilling{OrgID: a.OrgID, Plan: "active"}); err != nil {
		t.Fatal(err)
	}
	_, body = get("human")
	if body["plan"] != "active" || body["included"] != nil {
		t.Fatalf("paid view: %v", body)
	}
}
