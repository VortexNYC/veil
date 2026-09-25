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
// billingCustomerSnapshotSchema): POST /v1/customers with environment +
// merchantAccountId in the body, Idempotency-Key required on mutations,
// Bearer vp_ merchant key. Response is {data: {customerId, ...}}.
func TestEnsureCustomer(t *testing.T) {
	var got map[string]any
	var auth, idem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/customers" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		auth = r.Header.Get("Authorization")
		idem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
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
	if auth != "Bearer vp_test" {
		t.Fatalf("auth %q", auth)
	}
	if idem != "veil-customer-org-1" {
		t.Fatalf("idempotency-key %q", idem)
	}
	if got["externalCustomerRef"] != "org-1" || got["merchantAccountId"] != "ma_7" || got["environment"] != "sandbox" {
		t.Fatalf("body %v", got)
	}
	if got["name"] == "" || got["defaultCurrency"] == "" {
		t.Fatalf("required fields missing: %v", got)
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
