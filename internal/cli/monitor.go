package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/broker"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/push"
	"github.com/VortexNYC/veil/internal/store"
)

// monitorCmd is the dead-man's switch: a cron-shaped verb that checks the
// beats periodic jobs write (ops_heartbeat), the audit outbox, and the
// public readiness probe — then emails the ops address when something is
// stale. It alerts at most once per --alert-cooldown so a stuck job pages
// once, not every tick.
func monitorCmd() *cobra.Command {
	var dsn, mailURL, mailToken, alertTo, readyURL, ntfyTopic string
	var backupStale, sweepStale, alertCooldown, usageStale, auditExportStale, walStale time.Duration
	var usagePending int64
	c := &cobra.Command{
		Use:   "monitor",
		Short: "Check ops heartbeats and outbox lag; email on failure. Not MCP.",
		Long: "Reads ops_heartbeat for backup/sweep beats, audits the audit_outbox " +
			"backlog, and probes the public /ready endpoint. A missing or stale " +
			"beat and a backed-up outbox are findings; findings email VEIL_ALERT_TO " +
			"through the mail worker at most once per --alert-cooldown (default 6h) " +
			"per incident. Exits 0 even with findings — alerting must not crash-loop " +
			"the cron; the email is the signal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var findings []string

			pool, err := pgxpool.New(ctx, dsn)
			if err != nil {
				findings = append(findings, "postgres unreachable: "+err.Error())
			} else {
				defer pool.Close()
				findings = append(findings, checkBeat(ctx, pool, "backup", backupStale)...)
				findings = append(findings, checkBeat(ctx, pool, "sweep", sweepStale)...)
				findings = append(findings, checkBeat(ctx, pool, "audit-export", auditExportStale)...)
				findings = append(findings, checkBeat(ctx, pool, "wal-archive", walStale)...)
				// The beat proves the shipper loop is alive; the slots prove
				// a receiver is actually attached and WAL is flowing. Primary
				// (wal_archive) and standby (wal_archive_dr) both archive;
				// page only when no receiver is attached or a slot went lost.
				slots, err := store.WalSlots(ctx, pool)
				if err != nil {
					findings = append(findings, "wal-archive slots unreadable: "+err.Error())
				} else {
					anyActive := false
					for _, s := range slots {
						if s.Active {
							anyActive = true
						}
						if s.Status == "lost" {
							findings = append(findings,
								fmt.Sprintf("wal-archive slot %s lost retained WAL (max_slot_wal_keep_size fired)", s.Name))
						}
						// Per-archiver beat: two archivers share the
						// 'wal-archive' row, so a wedged shipper is only
						// visible on its own slot-named beat.
						findings = append(findings, checkBeat(ctx, pool, "wal-archive:"+s.Name, walStale)...)
					}
					if len(slots) == 0 {
						findings = append(findings, "wal-archive slot missing")
					} else if !anyActive {
						findings = append(findings, "wal-archive: no active slot — WAL not streaming")
					}
				}
				// usage-report is opt-in: unmetered deploys never run the
				// flusher, so a missing beat is silence, not a finding. A
				// beat that exists and goes stale means the reporter wedged.
				if age, ok, err := store.HeartbeatAge(ctx, pool, "usage-report"); err != nil {
					findings = append(findings, "usage-report heartbeat unreadable: "+err.Error())
				} else if ok && age > usageStale {
					findings = append(findings,
						fmt.Sprintf("usage-report last reported %s ago", age.Round(time.Minute)))
				}
				if pending, stale, units, err := store.UsageReportLag(ctx, pool, broker.MonthWindow(time.Now())); err != nil {
					findings = append(findings, "usage backlog unreadable: "+err.Error())
				} else {
					if stale > 0 {
						findings = append(findings,
							fmt.Sprintf("usage report stuck: %d deltas from closed windows unreported", stale))
					}
					if pending > usagePending {
						findings = append(findings,
							fmt.Sprintf("usage report backlog: %d org-windows, %d units unreported", pending, units))
					}
				}
				if depth, oldest, err := store.OutboxLag(ctx, pool); err != nil {
					findings = append(findings, "audit_outbox unreadable: "+err.Error())
				} else if depth > 500 || oldest > 2*time.Minute {
					findings = append(findings,
						fmt.Sprintf("audit_outbox backlog: %d rows, oldest %s", depth, oldest.Round(time.Second)))
				}
			}
			if readyURL != "" {
				if err := probeReady(ctx, readyURL); err != nil {
					findings = append(findings, "ready probe: "+err.Error())
				}
			}
			// Every public surface — the same probes deploy-check.sh runs.
			// Expected codes are honest per-surface: POST-only workers
			// answer 405, consent answers 400 on a bare GET. A deploy that
			// kills a route is invisible until a probe lands — that gap is
			// what hid the backup-ingest manifest outage for 11 days.
			for _, s := range surfaces {
				if err := probeSurface(ctx, s); err != nil {
					findings = append(findings, s.name+": "+err.Error())
				}
			}

			// The ops plane lands on the PostHog board too — one
			// ops_finding event per tick carrying the findings so the same
			// dashboard sees dead-beats and route-deaths, not just product
			// bugs. The pager stays email+ntfy; this is the record.
			reportOpsFindings(ctx, findings)
			// Registered phones get the page natively too — the Veil app
			// is the product's own pager once APNs is wired; ntfy remains
			// the fallback while it is unconfigured.
			pushOpsPage(ctx, pool, findings)

			for _, f := range findings {
				fmt.Fprintf(cmd.OutOrStdout(), "FINDING %s\n", f)
			}
			if len(findings) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "ok")
				return nil
			}
			if mailURL == "" || mailToken == "" || alertTo == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "alerts unconfigured (VEIL_MAIL_URL/VEIL_MAIL_TOKEN/VEIL_ALERT_TO)")
				return nil
			}
			if pool == nil {
				// DB is down — dedup state lives there; send unconditionally.
				return alert(ctx, cmd, mailURL, mailToken, alertTo, ntfyTopic, findings)
			}
			age, ok, err := store.HeartbeatAge(ctx, pool, "alert")
			if err == nil && (!ok || age > alertCooldown) {
				if err := alert(ctx, cmd, mailURL, mailToken, alertTo, ntfyTopic, findings); err != nil {
					return err
				}
				if err := store.MarkHeartbeat(ctx, pool, "alert"); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "alert beat unwritten: %v\n", err)
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "suppressed (last alert %s ago)\n", age.Round(time.Minute))
			}
			return nil
		},
	}
	c.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (default env VEIL_POSTGRES_DSN)")
	c.Flags().StringVar(&mailURL, "mail-url", "", "mail worker URL (default env VEIL_MAIL_URL)")
	c.Flags().StringVar(&mailToken, "mail-token", "", "mail worker bearer (default env VEIL_MAIL_TOKEN)")
	c.Flags().StringVar(&alertTo, "alert-to", "", "alert recipient (default env VEIL_ALERT_TO)")
	c.Flags().StringVar(&readyURL, "ready-url", "", "public readiness URL (default env VEIL_READY_URL)")
	c.Flags().StringVar(&ntfyTopic, "ntfy-topic", "", "ntfy.sh topic for a phone push (default env VEIL_NTFY_TOPIC) — email is archival, this is the page")
	c.Flags().DurationVar(&backupStale, "backup-stale", 26*time.Hour, "backup beat older than this is a finding")
	c.Flags().DurationVar(&sweepStale, "sweep-stale", 100*time.Minute, "sweep beat older than this is a finding")
	c.Flags().DurationVar(&usageStale, "usage-stale", 10*time.Minute, "usage-report beat older than this is a finding (absent beat = unmetered deploy, skipped)")
	c.Flags().DurationVar(&auditExportStale, "audit-export-stale", 3*time.Hour, "audit-export beat older than this is a finding")
	c.Flags().DurationVar(&walStale, "wal-stale", 10*time.Minute, "wal-archive beat older than this is a finding — WAL archiving is what bounds backup RPO")
	c.Flags().Int64Var(&usagePending, "usage-pending", 25, "unreported usage org-windows beyond this is a finding")
	c.Flags().DurationVar(&alertCooldown, "alert-cooldown", 6*time.Hour, "minimum time between alert emails")
	c.PreRunE = func(cmd *cobra.Command, args []string) error {
		if dsn == "" {
			dsn = os.Getenv("VEIL_POSTGRES_DSN")
		}
		if mailURL == "" {
			mailURL = os.Getenv("VEIL_MAIL_URL")
		}
		if mailToken == "" {
			mailToken = os.Getenv("VEIL_MAIL_TOKEN")
		}
		if alertTo == "" {
			alertTo = os.Getenv("VEIL_ALERT_TO")
		}
		if readyURL == "" {
			readyURL = os.Getenv("VEIL_READY_URL")
		}
		if ntfyTopic == "" {
			ntfyTopic = os.Getenv("VEIL_NTFY_TOPIC")
		}
		if dsn == "" {
			return fmt.Errorf("monitor: --dsn or VEIL_POSTGRES_DSN required")
		}
		return nil
	}
	return c
}

func checkBeat(ctx context.Context, pool *pgxpool.Pool, name string, stale time.Duration) []string {
	age, ok, err := store.HeartbeatAge(ctx, pool, name)
	if err != nil {
		return []string{name + " heartbeat unreadable: " + err.Error()}
	}
	if !ok {
		return []string{name + " has never reported"}
	}
	if age > stale {
		return []string{fmt.Sprintf("%s last reported %s ago", name, age.Round(time.Minute))}
	}
	return nil
}

// The public surfaces the monitor checks — name, URL, the HTTP status a
// healthy service returns. POST-only and bare-GET-400 services keep their
// honest codes; changing a surface's contract means updating this list.
var surfaces = []struct {
	name, url string
	want      int
}{
	{"origin", "https://veil.nyc/ready", 200},
	{"backup-ingest", "https://backup-ingest.veil.nyc/healthz", 200},
	{"login", "https://login.veil.nyc/", 200},
	{"vault", "https://app.veil.nyc/", 200},
	{"hydra", "https://id.veil.nyc/.well-known/openid-configuration", 200},
	{"kratos", "https://accounts.veil.nyc/health/ready", 200},
	{"consent", "https://consent.veil.nyc/", 400},
	{"mail", "https://mail.veil.nyc/", 405},
}

func probeSurface(ctx context.Context, s struct {
	name, url string
	want      int
}) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if res.StatusCode != s.want {
		return fmt.Errorf("%s -> %d", s.url, res.StatusCode)
	}
	return nil
}

// errBudget is the 5xx count over the origin's rolling 5-minute window that
// counts as an outage — blips below this are noise, at-or-above is a bleed.
const errBudget = 10

// spoolBudget is the on-disk audit backlog that turns into a finding. Any
// nonzero depth means store writes are failing; the grace absorbs a short
// burst draining between monitor ticks.
const spoolBudget = 100

func probeReady(ctx context.Context, u string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s -> %d", u, res.StatusCode)
	}
	// /ready reports the rolling 5xx count and audit spool depth:
	// "ok errors_5m=N audit_spool=N". A process can be green and still
	// bleeding — this is how the monitor sees it.
	for _, f := range strings.Fields(string(body)) {
		if n, ok := strings.CutPrefix(f, "errors_5m="); ok {
			var c int
			if _, err := fmt.Sscanf(n, "%d", &c); err == nil && c >= errBudget {
				return fmt.Errorf("origin 5xx: %d in the last 5m", c)
			}
		}
		if n, ok := strings.CutPrefix(f, "audit_spool="); ok {
			var c int
			if _, err := fmt.Sscanf(n, "%d", &c); err == nil && c > spoolBudget {
				return fmt.Errorf("audit spool backlog: %d events awaiting relay", c)
			}
		}
	}
	return nil
}

// reportOpsFindings emits one ops_finding event per tick (zero findings is
// still a row — a flat line on the insight means the monitor itself is
// dead, which is itself a signal). POSTHOG_PROJECT_KEY unset = a no-op.
func reportOpsFindings(ctx context.Context, findings []string) {
	key := os.Getenv("POSTHOG_PROJECT_KEY")
	if key == "" {
		return
	}
	list := findings
	if len(list) > 8 {
		list = list[:8]
	}
	body, _ := json.Marshal(map[string]any{
		"api_key": key,
		"event":   "ops_finding",
		"properties": map[string]any{
			"distinct_id": "veil-monitor",
			"$lib":        "veil-monitor",
			"count":       len(findings),
			"findings":    strings.Join(list, "; "),
		},
	})
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodPost,
		"https://us.i.posthog.com/capture/", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
}

// pushOpsPage fans monitor findings to registered phone devices — the
// product's own pager. APNS envs unset = no-op; ntfy stays the fallback
// until the native channel is proven.
func pushOpsPage(ctx context.Context, pool *pgxpool.Pool, findings []string) {
	if len(findings) == 0 || pool == nil {
		return
	}
	sender, err := push.NewAPNSSender(push.ConfigFromEnv())
	if err != nil || sender == nil {
		return
	}
	p := push.New(push.PoolDevices{Pool: pool}, sender)
	_ = p.NotifyOps(ctx, protocol.LocalOrgID, findings)
}

func alert(ctx context.Context, cmd *cobra.Command, mailURL, mailToken, to, ntfyTopic string, findings []string) error {
	text := "veil monitor findings:\n\n" + joinLines(findings)
	subject := fmt.Sprintf("[veil ops] %d finding(s): %s", len(findings), findings[0])
	if ntfyTopic != "" {
		// The email is the record; this is the page. ntfy is one POST —
		// Priority=high so a do-not-disturb phone still buzzes.
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://ntfy.sh/"+ntfyTopic, strings.NewReader(text))
		req.Header.Set("Title", subject)
		req.Header.Set("Priority", "high")
		req.Header.Set("Tags", "rotating_light")
		if _, err := http.DefaultClient.Do(req); err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "ntfy push failed: %v\n", err)
		}
	}
	body, err := json.Marshal(map[string]any{
		"to":      to,
		"subject": subject,
		"text":    text,
		"html":    "<pre>" + htmlEscape(text) + "</pre>",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mailURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+mailToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("alert send: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("alert send: %d %s", res.StatusCode, string(b))
	}
	fmt.Fprintln(cmd.OutOrStdout(), "alert sent")
	return nil
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n"
		}
		out += "- " + s
	}
	return out
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
