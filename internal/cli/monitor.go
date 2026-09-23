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

	"github.com/VortexNYC/veil/internal/store"
)

// monitorCmd is the dead-man's switch: a cron-shaped verb that checks the
// beats periodic jobs write (ops_heartbeat), the audit outbox, and the
// public readiness probe — then emails the ops address when something is
// stale. It alerts at most once per --alert-cooldown so a stuck job pages
// once, not every tick.
func monitorCmd() *cobra.Command {
	var dsn, mailURL, mailToken, alertTo, readyURL string
	var backupStale, sweepStale, alertCooldown time.Duration
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
				return alert(ctx, cmd, mailURL, mailToken, alertTo, findings)
			}
			age, ok, err := store.HeartbeatAge(ctx, pool, "alert")
			if err == nil && (!ok || age > alertCooldown) {
				if err := alert(ctx, cmd, mailURL, mailToken, alertTo, findings); err != nil {
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
	c.Flags().DurationVar(&backupStale, "backup-stale", 26*time.Hour, "backup beat older than this is a finding")
	c.Flags().DurationVar(&sweepStale, "sweep-stale", 100*time.Minute, "sweep beat older than this is a finding")
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
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s -> %d", u, res.StatusCode)
	}
	return nil
}

func alert(ctx context.Context, cmd *cobra.Command, mailURL, mailToken, to string, findings []string) error {
	text := "veil monitor findings:\n\n" + joinLines(findings)
	body, err := json.Marshal(map[string]any{
		"to":      to,
		"subject": fmt.Sprintf("[veil ops] %d finding(s): %s", len(findings), findings[0]),
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
