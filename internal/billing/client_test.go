package billing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The wire contract, verified against vortex packages/worker
// (merchant-schemas.ts createBillingCustomerCommandSchema +
// listBillingCustomersQuerySchema + billingCustomerSnapshotSchema): ensure
// means GET /v1/customers?externalCustomerRef=<org> first — create collides
// 409 on an existing customerId, it does not upsert. POST /v1/customers
// carries environment + merchantAccountId in the body, Idempotency-Key
// required on mutations, Bearer vp_ merchant key. Responses are
// {data: {customerId, ...}} and {data: {items: [...]}}.
func TestEnsureCustomer(t *testing.T) {
	var got map[string]any
	var auth, idem string
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/customers" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("externalCustomerRef") != "org-1" {
				http.Error(w, "missing ref filter", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
			return
		}
		posts++
		auth = r.Header.Get("Authorization")
		idem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"customerId":"cus_42","externalCustomerRef":"org-1"},"requestId":"req_1"}`))
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox"}
	id, err := c.EnsureCustomer(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "cus_42" {
		t.Fatalf("customerId %q", id)
	}
	if posts != 1 {
		t.Fatalf("posts=%d want 1", posts)
	}
	if auth != "Bearer vp_test" {
		t.Fatalf("auth %q", auth)
	}
	if idem != "veil-customer-org-1" {
		t.Fatalf("idempotency-key %q", idem)
	}
	if got["externalCustomerRef"] != "org-1" || got["merchantAccountId"] != "ma_7" || got["environment"] != "sandbox" || got["customerId"] != "org-1" {
		t.Fatalf("body %v", got)
	}
	if got["name"] == "" || got["defaultCurrency"] == "" {
		t.Fatalf("required fields missing: %v", got)
	}
}

// A customer already linked by externalCustomerRef must not be re-created —
// create would 409, not upsert.
func TestEnsureCustomerAlreadyLinked(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":{"items":[{"customerId":"cus_9","externalCustomerRef":"org-1"}]}}`))
			return
		}
		posts++
		http.Error(w, "conflict", http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox"}
	id, err := c.EnsureCustomer(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "cus_9" {
		t.Fatalf("customerId %q", id)
	}
	if posts != 0 {
		t.Fatalf("posts=%d want 0 — existing customer re-created", posts)
	}
}

// Lookup-miss then create-409 is the cross-writer race: re-resolve and take
// the existing customer rather than failing the provision.
func TestEnsureCustomerConflictResolves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":{"items":[{"customerId":"cus_raced","externalCustomerRef":"org-1"}]}}`))
			return
		}
		http.Error(w, "conflict", http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox"}
	id, err := c.EnsureCustomer(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "cus_raced" {
		t.Fatalf("customerId %q", id)
	}
}

func TestEnsureCustomerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox"}
	if _, err := c.EnsureCustomer(context.Background(), "org-1"); err == nil {
		t.Fatal("expected error on 500")
	}
}

// VEIL-65 — usage events post to a billing account; EnsureBillingAccount is
// lookup-first like EnsureCustomer: GET the customer's accounts, create on
// miss. Sandbox posture is api_only + manual collection — the account exists
// to bind usage, not to charge.
func TestEnsureBillingAccount(t *testing.T) {
	var got map[string]any
	var idem string
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/customers/cus_1/billing-accounts" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("environment") == "" || r.URL.Query().Get("merchantAccountId") == "" {
				http.Error(w, "missing query params", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
			return
		}
		posts++
		idem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"billingAccountId":"bacc_42"}}`))
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox"}
	id, err := c.EnsureBillingAccount(context.Background(), "org-1", "cus_1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "bacc_42" {
		t.Fatalf("billingAccountId %q", id)
	}
	if posts != 1 {
		t.Fatalf("posts=%d want 1", posts)
	}
	if idem != "veil-bacc-org-1" {
		t.Fatalf("idempotency-key %q", idem)
	}
	if got["invoiceDeliveryMode"] != "api_only" || got["collectionMode"] != "manual" || got["autoCollectionEnabled"] != false {
		t.Fatalf("sandbox posture wrong: %v", got)
	}
	if got["environment"] != "sandbox" || got["merchantAccountId"] != "ma_7" || got["customerId"] != "cus_1" {
		t.Fatalf("body %v", got)
	}
}

// An existing billing account is reused, never re-created.
func TestEnsureBillingAccountAlreadyExists(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":{"items":[{"billingAccountId":"bacc_9"}]}}`))
			return
		}
		posts++
		http.Error(w, "conflict", http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox"}
	id, err := c.EnsureBillingAccount(context.Background(), "org-1", "cus_1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "bacc_9" {
		t.Fatalf("billingAccountId %q", id)
	}
	if posts != 0 {
		t.Fatalf("posts=%d want 0", posts)
	}
}

// RecordUsage POSTs one delta per call: the flusher's watermark advance.
// Idempotency-Key is deterministic per delta so transport retries dedupe
// upstream.
func TestRecordUsage(t *testing.T) {
	var got map[string]any
	var idem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage-events" || r.Method != http.MethodPost {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		idem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"usageEventId":"ue_1"}}`))
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox", MeterID: "mtr_1", UsageEvent: "credential_use"}
	err := c.RecordUsage(context.Background(), UsageDelta{
		CustomerID:       "cus_1",
		BillingAccountID: "bacc_1",
		Quantity:         5,
		IdempotencyKey:   "veil-usage-org-1-2026-03-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["customerId"] != "cus_1" || got["billingAccountId"] != "bacc_1" || got["meterId"] != "mtr_1" || got["eventName"] != "credential_use" {
		t.Fatalf("body %v", got)
	}
	if got["quantity"] != float64(5) || got["idempotencyKey"] != "veil-usage-org-1-2026-03-5" || got["environment"] != "sandbox" || got["merchantAccountId"] != "ma_7" {
		t.Fatalf("body %v", got)
	}
	if got["occurredAt"] == "" {
		t.Fatal("occurredAt required")
	}
	if idem != "veil-usage-org-1-2026-03-5" {
		t.Fatalf("idempotency-key %q", idem)
	}
}

func TestRecordUsageFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox", MeterID: "mtr_1", UsageEvent: "credential_use"}
	if err := c.RecordUsage(context.Background(), UsageDelta{CustomerID: "c", BillingAccountID: "b", Quantity: 1, IdempotencyKey: "k"}); err == nil {
		t.Fatal("expected error on 500")
	}
}

// VEIL-67 — upgrade checkout. POST /v1/checkout-sessions composes a hosted
// subscription link: mode=subscription + customerId + billingAccountId +
// items[priceId]. Response is {data: {checkoutSession: {checkoutUrl, ...}}}.
// Vortex drops checkoutUrl on replayed responses, so each call sends a fresh
// idempotency key — a second upgrade click must mint a fresh session.
func TestCheckoutSession(t *testing.T) {
	var got map[string]any
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout-sessions" || r.Method != http.MethodPost {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"checkoutSession":{"checkoutSessionId":"plink_1","checkoutUrl":"https://pay.sandbox.vortex.nyc/c/plink_1","status":"open"},"replayed":false}}`))
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox", PriceID: "price_29", SuccessURL: "https://app.veil.nyc/billing?ok=1", CancelURL: "https://app.veil.nyc/billing"}
	url, err := c.CheckoutSession(context.Background(), CheckoutIntent{
		OrgID:            "org-1",
		CustomerID:       "cus_1",
		BillingAccountID: "bacc_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://pay.sandbox.vortex.nyc/c/plink_1" {
		t.Fatalf("checkoutUrl %q", url)
	}
	if got["mode"] != "subscription" || got["customerId"] != "cus_1" || got["billingAccountId"] != "bacc_1" {
		t.Fatalf("body %v", got)
	}
	items, _ := got["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["priceId"] != "price_29" {
		t.Fatalf("items %v", got["items"])
	}
	if got["environment"] != "sandbox" || got["merchantAccountId"] != "ma_7" {
		t.Fatalf("body %v", got)
	}
	if got["successUrl"] != "https://app.veil.nyc/billing?ok=1" || got["cancelUrl"] != "https://app.veil.nyc/billing" {
		t.Fatalf("urls %v", got)
	}
	if len(keys) != 1 || keys[0] == "" {
		t.Fatalf("idempotency keys %v", keys)
	}
}

// Repeat clicks must not replay the same key — a replayed session comes back
// without checkoutUrl and strands the owner mid-upgrade.
func TestCheckoutSessionFreshKeyPerCall(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"checkoutSession":{"checkoutUrl":"https://x/c/1"},"replayed":false}}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Key: "k", MerchantID: "ma_7", Environment: "sandbox", PriceID: "p"}
	for i := 0; i < 2; i++ {
		if _, err := c.CheckoutSession(context.Background(), CheckoutIntent{OrgID: "o", CustomerID: "c", BillingAccountID: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("reused idempotency key %v", keys)
	}
}

func TestCheckoutSessionNoPrice(t *testing.T) {
	c := &Client{BaseURL: "http://x", Key: "k", MerchantID: "m", Environment: "sandbox"}
	if _, err := c.CheckoutSession(context.Background(), CheckoutIntent{OrgID: "o", CustomerID: "c", BillingAccountID: "b"}); err == nil {
		t.Fatal("expected error without PriceID")
	}
}
