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

// auditExportCmd ships the audit trail offsite. Postgres is the durable
// store, but it is one blast radius — this verb copies every audit row past
// the export cursor through the backup-ingest worker into R2 as immutable
// JSONL objects. The cursor only advances after the PUT lands, so a failed
// run re-exports the same range under the same name — idempotent.
func auditExportCmd() *cobra.Command {
	var dsn, ingestURL, ingestToken string
	var batch int
	c := &cobra.Command{
		Use:   "audit-export",
		Short: "Export audit rows past the cursor to offsite object storage. Not MCP.",
		Long: "Reads audit rows with id past audit_export_cursor, batches them into " +
			"audit-<ts>-<first>-<last>.jsonl objects, and PUTs each through the ingest " +
			"worker. Stamps the 'audit-export' ops beat only when the backlog is fully " +
			"exported — a wedged exporter goes silent and the monitor pages.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ingestToken == "" {
				return fmt.Errorf("audit-export: --ingest-token or VEIL_AUDIT_INGEST_TOKEN/OFFSITE_TOKEN required")
			}
			pool, err := pgxpool.New(ctx, dsn)
			if err != nil {
				return err
			}
			defer pool.Close()

			n, err := runAuditExport(ctx, pool, ingestPut(ingestURL, ingestToken), batch)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported %d audit rows\n", n)
			if err := store.MarkHeartbeat(ctx, pool, "audit-export"); err != nil {
				return fmt.Errorf("audit-export beat: %w", err)
			}
			return nil
		},
	}
	c.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (default env VEIL_POSTGRES_DSN)")
	c.Flags().StringVar(&ingestURL, "ingest-url", "", "ingest worker base (default env VEIL_AUDIT_INGEST_URL)")
	c.Flags().StringVar(&ingestToken, "ingest-token", "", "worker bearer (default env VEIL_AUDIT_INGEST_TOKEN, then OFFSITE_TOKEN)")
	c.Flags().IntVar(&batch, "batch", 2000, "max audit rows per exported object")
	c.PreRunE = func(cmd *cobra.Command, args []string) error {
		if dsn == "" {
			dsn = os.Getenv("VEIL_POSTGRES_DSN")
		}
		if ingestURL == "" {
			ingestURL = os.Getenv("VEIL_AUDIT_INGEST_URL")
		}
		if ingestURL == "" {
			ingestURL = "https://backup-ingest.veil.nyc"
		}
		if ingestToken == "" {
			ingestToken = os.Getenv("VEIL_AUDIT_INGEST_TOKEN")
		}
		if ingestToken == "" {
			ingestToken = os.Getenv("OFFSITE_TOKEN")
		}
		if dsn == "" {
			return fmt.Errorf("audit-export: --dsn or VEIL_POSTGRES_DSN required")
		}
		return nil
	}
	return c
}

// runAuditExport drains the pending range in batches: export → PUT → advance
// the cursor, repeating until no rows remain past it. Any failure leaves the
// cursor where the last landed batch put it — nothing is skipped, nothing is
// double-counted (a re-PUT of the same name carries the same bytes).
func runAuditExport(ctx context.Context, pool *pgxpool.Pool, put func(name string, r io.Reader) error, batch int) (int64, error) {
	if batch < 1 {
		batch = 1
	}
	after, err := store.AuditExportCursor(ctx, pool)
	if err != nil {
		return 0, err
	}
	var total int64
	for {
		rows, err := store.ExportableAudits(ctx, pool, after, batch)
		if err != nil {
			return total, err
		}
		if len(rows) == 0 {
			return total, nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return total, err
			}
		}
		name := fmt.Sprintf("audit-%s-%d-%d.jsonl",
			rows[0].At.UTC().Format("20060102-150405"), rows[0].ID, rows[len(rows)-1].ID)
		if err := put(name, &buf); err != nil {
			return total, err
		}
		after = rows[len(rows)-1].ID
		if err := store.SetAuditExportCursor(ctx, pool, after); err != nil {
			return total, err
		}
		total += int64(len(rows))
		if len(rows) < batch {
			return total, nil
		}
	}
}

// ingestPut PUTs objects to the backup-ingest worker (R2 front). Errors
// carry the status so cron logs show whether the sink or the token failed.
func ingestPut(base, token string) func(string, io.Reader) error {
	client := &http.Client{Timeout: 60 * time.Second}
	return func(name string, r io.Reader) error {
		req, err := http.NewRequest(http.MethodPut,
			strings.TrimRight(base, "/")+"/v1/"+name, r)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/x-ndjson")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode/100 != 2 {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
			return fmt.Errorf("ingest %s -> %d: %s", name, res.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil
	}
}
