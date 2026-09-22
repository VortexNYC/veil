package store

import (
	"context"
	"fmt"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is a pgx-backed Store for the origin. Secrets are encrypted with
// per-owner data keys before they are written, same as the SQLite store.
// Owner DEKs sit under a per-org master resolved from org_keys; the store
// holds the deployment KEK, not any org's plaintext master at rest.
type Postgres struct {
	pool      *pgxpool.Pool
	auditPool *pgxpool.Pool
	kek       []byte
	km        *keyManager
	sqlc      *sqlc.Queries
}

// OpenPostgres opens a Postgres-backed store. The supplied key is the
// deployment KEK that seals org_keys rows — it never seals item secrets
// directly. Org masters live wrapped in the org_keys table.
func OpenPostgres(connString string, kek []byte) (*Postgres, error) {
	if len(kek) != crypto.KeySize {
		return nil, fmt.Errorf("store: KEK must be %d bytes", crypto.KeySize)
	}
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}
	config.ConnConfig.RuntimeParams["application_name"] = "veil"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	config.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "30000"
	config.MaxConns = 20
	if config.MinConns == 0 {
		config.MinConns = 2
	}
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = time.Minute * 30

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, err
	}
	// Audit writes get their own small pool so the async auditor's COPY does
	// not queue behind request-path queries when the main pool is saturated.
	auditCfg := config.Copy()
	auditCfg.MaxConns = 2
	auditCfg.MinConns = 1
	auditCfg.ConnConfig.RuntimeParams["application_name"] = "veil-audit"
	auditPool, err := pgxpool.NewWithConfig(context.Background(), auditCfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	p := &Postgres{pool: pool, auditPool: auditPool, kek: append([]byte(nil), kek...), sqlc: sqlc.New(pool)}
	p.km = newKeyManager(p.resolveOrgKey)
	if err := p.migrate(); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// resolveOrgKey unwraps an org's master key from org_keys under the KEK.
// Missing rows fail closed with ErrNotFound — a wrong-key decrypt attempt
// would be indistinguishable from corruption, so we never try.
func (p *Postgres) resolveOrgKey(ctx context.Context, orgID string) ([]byte, error) {
	row, err := retryOnDeadConn(func() (sqlc.OrgKey, error) {
		return p.sqlc.OrgKey(ctx, orgID)
	})
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return crypto.Open(p.kek, row.Wrapped)
}

// EnsureOrgKey seals master under the KEK and inserts the org_keys row if the
// org has none. Idempotent: an existing row is never overwritten (rotation is
// BumpOrgKey, a different verb). This is the provisioning hook — signup and
// the one-time VEIL_MASTER_KEY migration both land here.
func (p *Postgres) EnsureOrgKey(ctx context.Context, orgID string, master []byte) error {
	if len(master) != crypto.KeySize {
		return fmt.Errorf("store: org master must be %d bytes", crypto.KeySize)
	}
	wrapped, err := crypto.Seal(p.kek, master)
	if err != nil {
		return err
	}
	_, err = retryOnDeadConn(func() (struct{}, error) {
		return struct{}{}, p.sqlc.PutOrgKey(ctx, sqlc.PutOrgKeyParams{
			OrgID:      orgID,
			Wrapped:    wrapped,
			KeyVersion: 1,
			CreatedAt:  time.Now().UTC(),
		})
	})
	return err
}

// EnsurePostgresSchema creates the vault tables/indexes if missing. It is the
// same DDL OpenPostgres runs at boot, exposed so the sqlite→postgres migrator
// can prepare an empty database without a master key.
func EnsurePostgresSchema(ctx context.Context, pool *pgxpool.Pool) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS humans (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			owner_kind TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL DEFAULT '',
			revoked_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS items (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			uris TEXT NOT NULL,
			secret BYTEA NOT NULL,
			has_totp BOOLEAN NOT NULL DEFAULT FALSE,
			tags TEXT NOT NULL DEFAULT '[]',
			archived BOOLEAN NOT NULL DEFAULT FALSE,
			has_file BOOLEAN NOT NULL DEFAULT FALSE,
			login TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS grants (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			level TEXT NOT NULL,
			actions TEXT NOT NULL,
			expires_at TIMESTAMPTZ,
			UNIQUE(agent_id, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS approvals (
			grant_id TEXT PRIMARY KEY,
			id TEXT NOT NULL,
			human_id TEXT NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			at TIMESTAMPTZ NOT NULL,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			action TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason TEXT NOT NULL,
			approval_id TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS workloads (
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			audience TEXT NOT NULL,
			PRIMARY KEY (issuer, subject)
		)`,
		`CREATE TABLE IF NOT EXISTS owner_keys (
			org_id TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			wrapped BYTEA NOT NULL,
			PRIMARY KEY (org_id, owner_kind, owner_id)
		)`,
		`CREATE TABLE IF NOT EXISTS org_keys (
			org_id TEXT PRIMARY KEY,
			wrapped BYTEA NOT NULL,
			key_version INTEGER NOT NULL DEFAULT 1,
			cmk_id TEXT,
			created_at TIMESTAMPTZ NOT NULL,
			rotated_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS item_versions (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			item_id TEXT NOT NULL,
			at TIMESTAMPTZ NOT NULL,
			secret BYTEA NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			secret_hash BYTEA NOT NULL UNIQUE,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			revoked_at TIMESTAMPTZ,
			renewed_at TIMESTAMPTZ,
			ttl BIGINT NOT NULL,
			max_ttl BIGINT NOT NULL,
			max_uses INTEGER NOT NULL,
			uses INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_items_org_name ON items(org_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_items_org_archived_name ON items(org_id, archived, name)`,
		`CREATE INDEX IF NOT EXISTS idx_grants_item ON grants(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_agent_at ON audit(agent_id, at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_at ON audit(at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_item_versions_item ON item_versions(item_id, id)`,
		`CREATE INDEX IF NOT EXISTS idx_workloads_issuer ON workloads(issuer)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z'`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS renewed_at TIMESTAMPTZ`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS ttl BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS max_ttl BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS max_uses INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS uses INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	// Legacy rows predate the lifecycle columns. Give them usable
	// created_at/ttl/max_ttl so RenewSession can extend them.
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = expires_at WHERE created_at = '1970-01-01T00:00:00Z'`); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET ttl = 900 WHERE ttl = 0`); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET max_ttl = 3600 WHERE max_ttl = 0`); err != nil {
		return err
	}
	// owner_keys predates org_id. Backfill every row to the one pre-multi-tenant
	// org and rebuild the PK; wrapped blobs are ciphertext under that org's
	// master and move byte-for-byte.
	var hasOrgID bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name='owner_keys' AND column_name='org_id')`).Scan(&hasOrgID); err != nil {
		return err
	}
	if !hasOrgID {
		for _, q := range []string{
			`ALTER TABLE owner_keys ADD COLUMN org_id TEXT`,
			`UPDATE owner_keys SET org_id = '` + protocol.LocalOrgID + `' WHERE org_id IS NULL`,
			`ALTER TABLE owner_keys DROP CONSTRAINT owner_keys_pkey`,
			`ALTER TABLE owner_keys ALTER COLUMN org_id SET NOT NULL`,
			`ALTER TABLE owner_keys ADD PRIMARY KEY (org_id, owner_kind, owner_id)`,
		} {
			if _, err := pool.Exec(ctx, q); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Postgres) migrate() error {
	return EnsurePostgresSchema(context.Background(), p.pool)
}

func (p *Postgres) Close() error { p.pool.Close(); p.auditPool.Close(); return nil }

func (p *Postgres) ownerDEK(orgID string, o protocol.Owner) ([]byte, error) {
	return p.km.ownerDEK(context.Background(), p, orgID, o)
}

var (
	_ Store       = (*Postgres)(nil)
	_ ownerSource = (*Postgres)(nil)
)
