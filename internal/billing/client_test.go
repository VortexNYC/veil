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
