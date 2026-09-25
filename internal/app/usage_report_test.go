package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/billing"
	"github.com/VortexNYC/veil/internal/store"
)

// VEIL-65 — usage dual-write. The flusher walks usage_counters rows where
// used exceeds the reported watermark, ensures the billing link lazily,
// posts the delta, and marks it — failures leave the watermark so the next
// tick retries. Nothing touches the Use path.

type fakeVortex struct {
	usagePosts  int32
	usageBodies []map[string]any
	usageStatus int32
	customers   int32
	accounts    int32
}

func fakeVortexServer(t *testing.T, f *fakeVortex) *httptest.Server {
	t.Helper()
	if f.usageStatus == 0 {
		f.usageStatus = 201
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/usage-events":
			if atomic.LoadInt32(&f.usageStatus) >= 300 {
				http.Error(w, "down", int(f.usageStatus))
				return
			}
			atomic.AddInt32(&f.usagePosts, 1)
			raw, _ := io.ReadAll(r.Body)
			var b map[string]any
			_ = json.Unmarshal(raw, &b)
			f.usageBodies = append(f.usageBodies, b)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"usageEventId":"ue_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/customers":
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/customers":
			atomic.AddInt32(&f.customers, 1)
			var b map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &b)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"customerId":"` + fmt.Sprint(b["customerId"]) + `"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/billing-accounts"):
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/billing-accounts"):
			atomic.AddInt32(&f.accounts, 1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"billingAccountId":"bacc_1"}}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

func testUsageApp(srv *httptest.Server) *App {
	return &App{
		Store: store.NewMemory(),
		BillingCustomers: &billing.Client{
			BaseURL: srv.URL, Key: "vp_test", MerchantID: "ma_7", Environment: "sandbox",
			MeterID: "mtr_1", UsageEvent: "credential_use",
		},
	}
}

func TestUsageFlushReportsDelta(t *testing.T) {
	f := &fakeVortex{}
	srv := fakeVortexServer(t, f)
	t.Cleanup(srv.Close)
	a := testUsageApp(srv)

	w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if err := a.Store.SetBillingLink("org-1", "cus_1", "bacc_1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := a.Store.ConsumeUse("org-1", w, 0); err != nil {
			t.Fatal(err)
		}
	}
	a.flushUsageReports(context.Background())

	if atomic.LoadInt32(&f.usagePosts) != 1 {
		t.Fatalf("usage posts = %d, want 1", f.usagePosts)
	}
	got := f.usageBodies[0]
	if got["customerId"] != "cus_1" || got["billingAccountId"] != "bacc_1" || got["meterId"] != "mtr_1" || got["quantity"] != float64(5) {
		t.Fatalf("usage body %v", got)
	}
	if got["idempotencyKey"] == "" {
		t.Fatal("idempotencyKey required")
	}
	// Watermark advanced — a second flush sends nothing.
	a.flushUsageReports(context.Background())
	if atomic.LoadInt32(&f.usagePosts) != 1 {
		t.Fatalf("second flush posted: %d", f.usagePosts)
	}
	// New usage posts only the new delta.
	for i := 0; i < 3; i++ {
		if _, _, err := a.Store.ConsumeUse("org-1", w, 0); err != nil {
			t.Fatal(err)
		}
	}
	a.flushUsageReports(context.Background())
	if atomic.LoadInt32(&f.usagePosts) != 2 || f.usageBodies[1]["quantity"] != float64(3) {
		t.Fatalf("second delta = %v", f.usageBodies)
	}
}

// Orgs without a billing link get one provisioned lazily — covers orgs
// created before billing existed or during a provider outage.
func TestUsageFlushProvisionsLink(t *testing.T) {
	f := &fakeVortex{}
	srv := fakeVortexServer(t, f)
	t.Cleanup(srv.Close)
	a := testUsageApp(srv)

	w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := a.Store.ConsumeUse("org-new", w, 0); err != nil {
		t.Fatal(err)
	}
	a.flushUsageReports(context.Background())

	if atomic.LoadInt32(&f.customers) != 1 || atomic.LoadInt32(&f.accounts) != 1 {
		t.Fatalf("link not provisioned: customers=%d accounts=%d", f.customers, f.accounts)
	}
	if atomic.LoadInt32(&f.usagePosts) != 1 {
		t.Fatalf("usage not posted after link: %d", f.usagePosts)
	}
	ob, err := a.Store.Billing("org-new")
	if err != nil {
		t.Fatal(err)
	}
	if ob.CustomerID != "org-new" || ob.BillingAccountID != "bacc_1" {
		t.Fatalf("link = %+v", ob)
	}
}

func TestUsageFlushFailureKeepsDelta(t *testing.T) {
	f := &fakeVortex{}
	f.usageStatus = 500
	srv := fakeVortexServer(t, f)
	t.Cleanup(srv.Close)
	a := testUsageApp(srv)

	w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if err := a.Store.SetBillingLink("org-1", "cus_1", "bacc_1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Store.ConsumeUse("org-1", w, 0); err != nil {
		t.Fatal(err)
	}
	a.flushUsageReports(context.Background())
	rows, _ := a.Store.UsageReportPending(10)
	if len(rows) != 1 {
		t.Fatalf("failed send must stay pending: %+v", rows)
	}
	atomic.StoreInt32(&f.usageStatus, 201)
	a.flushUsageReports(context.Background())
	if atomic.LoadInt32(&f.usagePosts) != 1 {
		t.Fatalf("retry did not send: %d", f.usagePosts)
	}
}

// No meter configured — billing on for webhooks, usage reporting off.
func TestUsageFlushSkipsWithoutMeter(t *testing.T) {
	f := &fakeVortex{}
	srv := fakeVortexServer(t, f)
	t.Cleanup(srv.Close)
	a := testUsageApp(srv)
	a.BillingCustomers.MeterID = ""

	w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := a.Store.ConsumeUse("org-1", w, 0); err != nil {
		t.Fatal(err)
	}
	a.flushUsageReports(context.Background())
	if atomic.LoadInt32(&f.usagePosts) != 0 {
		t.Fatalf("no meter must mean no posts: %d", f.usagePosts)
	}
}
