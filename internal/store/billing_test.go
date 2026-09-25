package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// VEIL-59 — usage metering. The counter is the product-side ledger for the
// free-tier cap: ConsumeUse claims a unit atomically (over-cap claims still
// count — blocked demand is signal), Billing/SetBilling carry the plan state
// the Vortex webhook receiver writes.

type billingStore interface {
	Billing(orgID string) (OrgBilling, error)
	SetBilling(OrgBilling) error
	OrgByBillingCustomer(customerID string) (string, error)
	ConsumeUse(orgID string, window time.Time, cap int64) (int64, bool, error)
	Usage(orgID string, window time.Time) (int64, error)
	UsageReportPending(limit int) ([]UsageReportRow, error)
	MarkUsageReported(orgID string, window time.Time, amount int64) error
	SetBillingLink(orgID, customerID, billingAccountID string) error
}

func billingStores(t *testing.T) map[string]billingStore {
	t.Helper()
	out := map[string]billingStore{}
	for _, s := range testStores(t) {
		out[fmt.Sprintf("%T", s)] = s
	}
	return out
}

func TestBillingAbsentIsFree(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			got, err := s.Billing("org-missing")
			if err != nil {
				t.Fatal(err)
			}
			if got.Plan != "free" {
				t.Fatalf("absent billing row plan = %q, want free", got.Plan)
			}
		})
	}
}

func TestSetBillingRoundTrip(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			ob := OrgBilling{OrgID: "org-1", Plan: "active", CustomerID: "cus_abc", UpdatedAt: time.Now().UTC()}
			if err := s.SetBilling(ob); err != nil {
				t.Fatal(err)
			}
			got, err := s.Billing("org-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Plan != "active" || got.CustomerID != "cus_abc" {
				t.Fatalf("billing round trip = %+v", got)
			}
			// Upsert: plan flip lands on the same row.
			ob.Plan = "past_due"
			if err := s.SetBilling(ob); err != nil {
				t.Fatal(err)
			}
			got, err = s.Billing("org-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Plan != "past_due" {
				t.Fatalf("plan update = %q, want past_due", got.Plan)
			}
		})
	}
}

// The billing customer id (Vortex's cus_…) must reverse-map to its org so
// webhook events that only carry the billing id still resolve.
func TestOrgByBillingCustomer(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := s.SetBilling(OrgBilling{OrgID: "org-1", CustomerID: "cus_9"}); err != nil {
				t.Fatal(err)
			}
			org, err := s.OrgByBillingCustomer("cus_9")
			if err != nil {
				t.Fatal(err)
			}
			if org != "org-1" {
				t.Fatalf("org = %q, want org-1", org)
			}
			if _, err := s.OrgByBillingCustomer("cus_absent"); err == nil {
				t.Fatal("unknown customer must not resolve")
			}
		})
	}
}

func TestConsumeUseWithinCap(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			for i := int64(1); i <= 3; i++ {
				used, ok, err := s.ConsumeUse("org-1", w, 3)
				if err != nil {
					t.Fatal(err)
				}
				if !ok || used != i {
					t.Fatalf("claim %d: used=%d ok=%v", i, used, ok)
				}
			}
			// Fourth claim is over cap — denied but still counted.
			used, ok, err := s.ConsumeUse("org-1", w, 3)
			if err != nil {
				t.Fatal(err)
			}
			if ok || used != 4 {
				t.Fatalf("over-cap claim: used=%d ok=%v, want 4/false", used, ok)
			}
		})
	}
}

func TestConsumeUseUnlimitedStillCounts(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 10; i++ {
				_, ok, err := s.ConsumeUse("org-1", w, 0)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Fatal("cap 0 must always allow")
				}
			}
			got, err := s.Usage("org-1", w)
			if err != nil {
				t.Fatal(err)
			}
			if got != 10 {
				t.Fatalf("usage = %d, want 10", got)
			}
		})
	}
}

func TestUsageAbsentIsZero(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			got, err := s.Usage("org-none", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if got != 0 {
				t.Fatalf("absent usage = %d, want 0", got)
			}
		})
	}
}

func TestUsageWindowsIsolated(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 5; i++ {
				if _, _, err := s.ConsumeUse("org-1", march, 0); err != nil {
					t.Fatal(err)
				}
			}
			// April is a fresh counter — the cap resets with the window.
			used, ok, err := s.ConsumeUse("org-1", april, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !ok || used != 1 {
				t.Fatalf("new window claim: used=%d ok=%v", used, ok)
			}
			if got, _ := s.Usage("org-1", march); got != 5 {
				t.Fatalf("march usage = %d, want 5", got)
			}
		})
	}
}

// Concurrent claims cannot bypass the cap materially: exactly `cap` claims
// return ok, and every claim — including the denied ones — lands on the
// counter once.
func TestConsumeUseConcurrentClaims(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			const cap = int64(50)
			const total = 80
			var wg sync.WaitGroup
			var mu sync.Mutex
			var okCount int64
			for i := 0; i < total; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, ok, err := s.ConsumeUse("org-1", w, cap)
					if err != nil {
						t.Error(err)
						return
					}
					if ok {
						mu.Lock()
						okCount++
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			if okCount != cap {
				t.Fatalf("allowed claims = %d, want %d", okCount, cap)
			}
			if got, _ := s.Usage("org-1", w); got != total {
				t.Fatalf("counter = %d, want %d (every claim lands once)", got, total)
			}
		})
	}
}

// VEIL-65 — usage dual-write. usage_counters.reported is the watermark of
// units already sent to the billing provider; UsageReportPending returns the
// deltas the flusher still owes, joined to the org's billing link.

func TestBillingAccountIDRoundTrip(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			ob := OrgBilling{OrgID: "org-1", Plan: "active", CustomerID: "cus_1", BillingAccountID: "bacc_1", UpdatedAt: time.Now().UTC()}
			if err := s.SetBilling(ob); err != nil {
				t.Fatal(err)
			}
			got, err := s.Billing("org-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.BillingAccountID != "bacc_1" {
				t.Fatalf("billing account = %q, want bacc_1", got.BillingAccountID)
			}
		})
	}
}

func TestUsageReportPending(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			if err := s.SetBilling(OrgBilling{OrgID: "org-linked", CustomerID: "cus_1", BillingAccountID: "bacc_1", UpdatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 5; i++ {
				if _, _, err := s.ConsumeUse("org-linked", w, 0); err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.ConsumeUse("org-unlinked", w, 0); err != nil {
					t.Fatal(err)
				}
			}
			// Org with no usage never appears.
			if err := s.SetBilling(OrgBilling{OrgID: "org-quiet", BillingAccountID: "bacc_2", UpdatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			rows, err := s.UsageReportPending(100)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 {
				t.Fatalf("pending rows = %d, want 2: %+v", len(rows), rows)
			}
			byOrg := map[string]UsageReportRow{}
			for _, r := range rows {
				byOrg[r.OrgID] = r
			}
			linked, ok := byOrg["org-linked"]
			if !ok {
				t.Fatalf("org-linked absent from pending: %+v", rows)
			}
			if linked.Used != 5 || linked.Reported != 0 || linked.CustomerID != "cus_1" || linked.BillingAccountID != "bacc_1" {
				t.Fatalf("linked row = %+v", linked)
			}
			unlinked, ok := byOrg["org-unlinked"]
			if !ok {
				t.Fatalf("org-unlinked absent from pending: %+v", rows)
			}
			if unlinked.Used != 5 || unlinked.CustomerID != "" || unlinked.BillingAccountID != "" {
				t.Fatalf("unlinked row = %+v", unlinked)
			}
		})
	}
}

func TestMarkUsageReported(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 5; i++ {
				if _, _, err := s.ConsumeUse("org-1", w, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.MarkUsageReported("org-1", w, 5); err != nil {
				t.Fatal(err)
			}
			rows, err := s.UsageReportPending(100)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 {
				t.Fatalf("pending after full mark = %+v", rows)
			}
			// New claims re-open the delta from the watermark.
			for i := 0; i < 2; i++ {
				if _, _, err := s.ConsumeUse("org-1", w, 0); err != nil {
					t.Fatal(err)
				}
			}
			rows, err = s.UsageReportPending(100)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Used != 7 || rows[0].Reported != 5 {
				t.Fatalf("delta after mark = %+v", rows)
			}
		})
	}
}

// A mark larger than the real delta (double-flush, racing claims) must not
// push reported past used — the watermark only ever trails the counter.
func TestMarkUsageReportedClampsToUsed(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			w := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 3; i++ {
				if _, _, err := s.ConsumeUse("org-1", w, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.MarkUsageReported("org-1", w, 99); err != nil {
				t.Fatal(err)
			}
			rows, err := s.UsageReportPending(100)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 {
				t.Fatalf("over-marked row still pending: %+v", rows)
			}
			if _, _, err := s.ConsumeUse("org-1", w, 0); err != nil {
				t.Fatal(err)
			}
			rows, err = s.UsageReportPending(100)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Used != 4 || rows[0].Reported != 3 {
				t.Fatalf("post-clamp delta = %+v, want used=4 reported=3", rows)
			}
		})
	}
}

// SetBillingLink is the provisioning seam: it writes only the billing link
// fields and never touches plan — a link upsert racing a webhook plan write
// can't clobber the plan (or vice versa).
func TestSetBillingLinkPreservesPlan(t *testing.T) {
	for name, s := range billingStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := s.SetBilling(OrgBilling{OrgID: "org-1", Plan: "active", CustomerID: "cus_1", BillingAccountID: "bacc_1", UpdatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetBillingLink("org-1", "cus_1", "bacc_2"); err != nil {
				t.Fatal(err)
			}
			got, err := s.Billing("org-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Plan != "active" || got.BillingAccountID != "bacc_2" {
				t.Fatalf("link write clobbered plan: %+v", got)
			}
			// Link-only insert creates a free row.
			if err := s.SetBillingLink("org-2", "cus_2", "bacc_3"); err != nil {
				t.Fatal(err)
			}
			got, err = s.Billing("org-2")
			if err != nil {
				t.Fatal(err)
			}
			if got.Plan != "free" || got.CustomerID != "cus_2" || got.BillingAccountID != "bacc_3" {
				t.Fatalf("link insert = %+v", got)
			}
		})
	}
}
