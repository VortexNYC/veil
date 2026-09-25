package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/VortexNYC/veil/internal/billing"
)

// VEIL-65 — usage dual-write. ConsumeUse meters every claim into
// usage_counters; this loop reports the delta (used − reported) to the
// billing provider on a tick, then advances the watermark. The Use path
// never touches the network: a provider outage just grows the backlog, and
// the deterministic idempotency key (org + window + target watermark) means
// a send that lands but fails to mark re-sends as an upstream no-op.

const (
	usageReportInterval = time.Minute
	usageReportBatch    = 200
)

// startUsageReporter launches the flush loop when a meter is configured.
// No meter means reporting is off — the rest of the billing link (webhooks,
// provisioning) does not depend on it.
func (a *App) startUsageReporter() {
	if a.BillingCustomers == nil || a.BillingCustomers.MeterID == "" {
		return
	}
	a.usageStop = make(chan struct{})
	a.usageDone = make(chan struct{})
	go a.usageReporter()
}

func (a *App) usageReporter() {
	defer close(a.usageDone)
	tick := time.NewTicker(usageReportInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.usageStop:
			return
		case <-tick.C:
		}
		a.flushUsageReports(context.Background())
	}
}

// stopUsageReporter parks the flush loop; nil-safe when reporting is off.
func (a *App) stopUsageReporter() {
	if a.usageStop == nil {
		return
	}
	close(a.usageStop)
	<-a.usageDone
}

// flushUsageReports makes one pass over the pending deltas. Each row: ensure
// the billing link (lazy — heals orgs provisioned before billing or during
// an outage), post the delta, mark the watermark. Per-row failures log and
// move on; the pending set is the durable backlog.
func (a *App) flushUsageReports(ctx context.Context) {
	c := a.BillingCustomers
	if c == nil || c.MeterID == "" {
		return
	}
	// The liveness beat precedes the work so a wedged drain still tells the
	// monitor when it was last alive; unmetered callers never mark.
	if err := a.Store.MarkHeartbeat("usage-report"); err != nil {
		slog.Warn("usage report: heartbeat unwritten", "err", err)
	}
	rows, err := a.Store.UsageReportPending(usageReportBatch)
	if err != nil {
		slog.Warn("usage report: pending read failed", "err", err)
		return
	}
	for _, r := range rows {
		if r.CustomerID == "" || r.BillingAccountID == "" {
			ob, err := a.ensureBillingLink(ctx, r.OrgID)
			if err != nil || ob.BillingAccountID == "" || ob.CustomerID == "" {
				slog.Warn("usage report: billing link failed", "org", r.OrgID, "err", err)
				continue
			}
			r.CustomerID, r.BillingAccountID = ob.CustomerID, ob.BillingAccountID
		}
		delta := r.Used - r.Reported
		if delta <= 0 {
			continue
		}
		key := fmt.Sprintf("veil-usage-%s-%d-%d", r.OrgID, r.WindowStart.UTC().Unix(), r.Used)
		if err := c.RecordUsage(ctx, billing.UsageDelta{
			CustomerID:       r.CustomerID,
			BillingAccountID: r.BillingAccountID,
			Quantity:         delta,
			IdempotencyKey:   key,
		}); err != nil {
			slog.Warn("usage report: send failed", "org", r.OrgID, "err", err)
			continue
		}
		if err := a.Store.MarkUsageReported(r.OrgID, r.WindowStart, delta); err != nil {
			slog.Warn("usage report: mark failed", "org", r.OrgID, "err", err)
		}
	}
}
