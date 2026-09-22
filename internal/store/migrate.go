package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	_ "modernc.org/sqlite"
)

// MigrateReport is the per-table outcome of a sqlite→postgres copy.
type MigrateReport struct {
	Tables []MigrateTableReport
}

type MigrateTableReport struct {
	Table    string
	Read     int
	Inserted int
	Skipped  int
}

// col converts a sqlite driver value (int64, string, []byte, nil) into the
// value the postgres column expects. Secrets and wrapped keys are opaque
// ciphertext under the master key, so bytes always pass through verbatim.
type col func(v any) (any, error)

func keep(v any) (any, error) { return v, nil }

func toBool(v any) (any, error) {
	i, ok := v.(int64)
	if !ok {
		return nil, fmt.Errorf("bool col: got %T", v)
	}
	return i != 0, nil
}

func toEpoch(v any) (any, error) {
	i, ok := v.(int64)
	if !ok {
		return nil, fmt.Errorf("epoch col: got %T", v)
	}
	return time.Unix(i, 0).UTC(), nil
}

func toEpochNull(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	return toEpoch(v)
}

func toTimeText(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("timetext col: got %T", v)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil, fmt.Errorf("timetext col: %w", err)
	}
	return t.UTC(), nil
}

func toTimeTextNull(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	return toTimeText(v)
}

// migrateTable describes one verbatim copy. selectCols read the sqlite row
// (storage encoding); insertCols name the postgres columns in the same order.
type migrateTable struct {
	name       string
	selectCols []string
	insertCols []string
	convs      []col
	// overriding emits OVERRIDING SYSTEM VALUE for GENERATED ALWAYS identity
	// columns so the sqlite autoincrement ids are preserved.
	overriding bool
	orderBy    string
}

var migrateTables = []migrateTable{
	{
		name:       "humans",
		selectCols: []string{"id", "org_id"},
		insertCols: []string{"id", "org_id"},
		convs:      []col{keep, keep},
	},
	{
		name:       "agents",
		selectCols: []string{"id", "org_id", "owner_kind", "owner_id", "revoked_at"},
		insertCols: []string{"id", "org_id", "owner_kind", "owner_id", "revoked_at"},
		convs:      []col{keep, keep, keep, keep, toTimeTextNull},
	},
	{
		name:       "items",
		selectCols: []string{"id", "org_id", "name", "kind", "owner_kind", "owner_id", "uris", "secret", "has_totp", "tags", "archived", "has_file", "login"},
		insertCols: []string{"id", "org_id", "name", "kind", "owner_kind", "owner_id", "uris", "secret", "has_totp", "tags", "archived", "has_file", "login"},
		convs:      []col{keep, keep, keep, keep, keep, keep, keep, keep, toBool, keep, toBool, toBool, keep},
	},
	{
		name:       "grants",
		selectCols: []string{"id", "org_id", "agent_id", "item_id", "level", "actions", "expires_at"},
		insertCols: []string{"id", "org_id", "agent_id", "item_id", "level", "actions", "expires_at"},
		convs:      []col{keep, keep, keep, keep, keep, keep, toEpochNull},
	},
	{
		name:       "approvals",
		selectCols: []string{"grant_id", "id", "human_id", "expires_at"},
		insertCols: []string{"grant_id", "id", "human_id", "expires_at"},
		convs:      []col{keep, keep, keep, toEpoch},
	},
	{
		name:       "audit",
		selectCols: []string{"rowid", "at", "org_id", "agent_id", "item_id", "action", "decision", "reason", "approval_id"},
		insertCols: []string{"id", "at", "org_id", "agent_id", "item_id", "action", "decision", "reason", "approval_id"},
		convs:      []col{keep, toTimeText, keep, keep, keep, keep, keep, keep, keep},
		overriding: true,
		orderBy:    "rowid",
	},
	{
		name:       "workloads",
		selectCols: []string{"issuer", "subject", "agent_id", "audience"},
		insertCols: []string{"issuer", "subject", "agent_id", "audience"},
		convs:      []col{keep, keep, keep, keep},
	},
	{
		name:       "owner_keys",
		selectCols: []string{"org_id", "owner_kind", "owner_id", "wrapped"},
		insertCols: []string{"org_id", "owner_kind", "owner_id", "wrapped"},
		convs:      []col{keep, keep, keep, keep},
	},
	{
		name:       "item_versions",
		selectCols: []string{"id", "item_id", "at", "secret"},
		insertCols: []string{"id", "item_id", "at", "secret"},
		convs:      []col{keep, keep, toTimeText, keep},
		overriding: true,
		orderBy:    "id",
	},
	{
		name:       "sessions",
		selectCols: []string{"id", "org_id", "agent_id", "secret_hash", "expires_at", "created_at", "revoked_at", "renewed_at", "ttl", "max_ttl", "max_uses", "uses"},
		insertCols: []string{"id", "org_id", "agent_id", "secret_hash", "expires_at", "created_at", "revoked_at", "renewed_at", "ttl", "max_ttl", "max_uses", "uses"},
		convs:      []col{keep, keep, keep, keep, toEpoch, toEpoch, toTimeTextNull, toTimeTextNull, keep, keep, keep, keep},
	},
}

// MigrateSQLiteToPostgres copies every application row from the sqlite vault
// at sqlitePath into the postgres database at dsn. Inserts are ON CONFLICT DO
// NOTHING, so a rerun inserts only rows written since the last pass and never
// overwrites fresher postgres state (e.g. session use counts).
//
// No master key is needed: secrets, session hashes, and wrapped owner keys are
// opaque ciphertext copied byte-for-byte.
func MigrateSQLiteToPostgres(ctx context.Context, sqlitePath, dsn string) (*MigrateReport, error) {
	if _, err := os.Stat(sqlitePath); err != nil {
		return nil, fmt.Errorf("sqlite vault: %w", err)
	}
	src, err := sql.Open("sqlite", "file:"+sqlitePath+"?mode=ro&_pragma=busy_timeout(30000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer src.Close()
	src.SetMaxOpenConns(1)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "veil-migrate"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	defer pool.Close()
	if err := EnsurePostgresSchema(ctx, pool); err != nil {
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	report := &MigrateReport{}
	for _, mt := range migrateTables {
		tr, err := migrateOneTable(ctx, src, pool, mt)
		if err != nil {
			return report, fmt.Errorf("migrate %s: %w", mt.name, err)
		}
		report.Tables = append(report.Tables, tr)
	}
	// Explicit ids copied into GENERATED ALWAYS identity columns do not
	// advance the backing sequence; without this the live service's next
	// audit/item_versions insert can collide with a copied id.
	for _, table := range []string{"audit", "item_versions"} {
		seq := fmt.Sprintf(
			`SELECT setval(pg_get_serial_sequence('%[1]s', 'id'), COALESCE(MAX(id), 1), MAX(id) IS NOT NULL) FROM %[1]s`,
			table,
		)
		if _, err := pool.Exec(ctx, seq); err != nil {
			return report, fmt.Errorf("reset %s sequence: %w", table, err)
		}
	}
	return report, nil
}

func migrateOneTable(ctx context.Context, src *sql.DB, pool *pgxpool.Pool, mt migrateTable) (MigrateTableReport, error) {
	tr := MigrateTableReport{Table: mt.name}
	query := "SELECT " + strings.Join(mt.selectCols, ", ") + " FROM " + mt.name
	if mt.orderBy != "" {
		query += " ORDER BY " + mt.orderBy
	}
	rows, err := src.QueryContext(ctx, query)
	if err != nil {
		return tr, err
	}
	defer rows.Close()

	placeholders := make([]string, len(mt.insertCols))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt := "INSERT INTO " + mt.name + " (" + strings.Join(mt.insertCols, ", ") + ") "
	if mt.overriding {
		stmt += "OVERRIDING SYSTEM VALUE "
	}
	stmt += "VALUES (" + strings.Join(placeholders, ", ") + ") ON CONFLICT DO NOTHING"

	err = func() error {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		for rows.Next() {
			vals := make([]any, len(mt.selectCols))
			ptrs := make([]any, len(vals))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			tr.Read++
			args := make([]any, len(vals))
			for i, conv := range mt.convs {
				args[i], err = conv(vals[i])
				if err != nil {
					return fmt.Errorf("row %d col %s: %w", tr.Read, mt.selectCols[i], err)
				}
			}
			tag, err := tx.Exec(ctx, stmt, args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				tr.Skipped++
			} else {
				tr.Inserted++
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}()
	return tr, err
}
