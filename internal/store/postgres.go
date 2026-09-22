package store

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	kekMu     sync.RWMutex
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
	// Connection budget: VEIL_PG_MAX_CONNS wins (ops resize without a DSN
	// change), then DSN pool_max_conns, then the default. Per-replica total
	// is this pool plus the audit pool — size it against Postgres
	// max_connections (or the PgBouncer pool) times replica count,
	// documented in docs/scale.md.
	if v := poolSizeEnv("VEIL_PG_MAX_CONNS"); v > 0 {
		config.MaxConns = int32(v)
	} else if !strings.Contains(connString, "pool_max_conns") {
		config.MaxConns = 20
	}
	if v := poolSizeEnv("VEIL_PG_MIN_CONNS"); v > 0 {
		config.MinConns = int32(v)
	}
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
	if v := poolSizeEnv("VEIL_PG_AUDIT_CONNS"); v > 0 {
		auditCfg.MaxConns = int32(v)
	}
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

// poolSizeEnv parses a positive-int env override; 0/missing/garbage means
// "not set" so the default or DSN value applies.
func poolSizeEnv(name string) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// resolveOrgKey unwraps an org's master key from org_keys under the KEK.
// Missing rows fail closed with ErrOrgKeyMissing — a wrong-key decrypt attempt
// would be indistinguishable from corruption, so we never try.
func (p *Postgres) resolveOrgKey(ctx context.Context, orgID string) ([]byte, error) {
	row, err := retryOnDeadConn(func() (sqlc.OrgKey, error) {
		return p.sqlc.OrgKey(ctx, orgID)
	})
	if err == pgx.ErrNoRows {
		return nil, ErrOrgKeyMissing
	}
	if err != nil {
		return nil, err
	}
	// AAD binds the wrap to this org: a row copied to another org's row does
	// not open even under the same KEK.
	p.kekMu.RLock()
	defer p.kekMu.RUnlock()
	return crypto.OpenAAD(p.kek, row.Wrapped, []byte(orgID))
}

// EnsureOrgKey seals master under the KEK and inserts the org_keys row if the
// org has none. If a row already exists it is never overwritten — instead the
// committed master is unwrapped and compared: an identical re-seed is a no-op,
// a different master is ErrOrgKeyMismatch. A boot that asserts the wrong master
// fails loud rather than stranding the org's ciphertext under a random key.
// This is the provisioning hook — signup and the one-time VEIL_MASTER_KEY
// migration both land here.
func (p *Postgres) EnsureOrgKey(ctx context.Context, orgID string, master []byte) error {
	if len(master) != crypto.KeySize {
		return fmt.Errorf("store: org master must be %d bytes", crypto.KeySize)
	}
	p.kekMu.RLock()
	wrapped, err := crypto.SealAAD(p.kek, master, []byte(orgID))
	p.kekMu.RUnlock()
	if err != nil {
		return err
	}
	n, err := retryOnDeadConn(func() (int64, error) {
		return p.sqlc.PutOrgKey(ctx, sqlc.PutOrgKeyParams{
			OrgID:      orgID,
			Wrapped:    wrapped,
			KeyVersion: 1,
			CreatedAt:  time.Now().UTC(),
		})
	})
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	row, err := retryOnDeadConn(func() (sqlc.OrgKey, error) {
		return p.sqlc.OrgKey(ctx, orgID)
	})
	if err == pgx.ErrNoRows {
		return fmt.Errorf("store: org_keys insert conflicted but no row for %s", orgID)
	}
	if err != nil {
		return err
	}
	p.kekMu.RLock()
	existing, err := crypto.OpenAAD(p.kek, row.Wrapped, []byte(orgID))
	p.kekMu.RUnlock()
	if err != nil {
		return fmt.Errorf("store: org_keys row for %s does not unwrap under this KEK: %w", orgID, err)
	}
	if !hmac.Equal(existing, master) {
		return ErrOrgKeyMismatch
	}
	return nil
}

// HasOrgKey reports whether an org_keys row exists for orgID.
func (p *Postgres) HasOrgKey(ctx context.Context, orgID string) (bool, error) {
	_, err := retryOnDeadConn(func() (sqlc.OrgKey, error) {
		return p.sqlc.OrgKey(ctx, orgID)
	})
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ErrRotationConflict is a concurrent rotation detected by the key_version
// guard: another RotateOrgKey bumped the version first. Retry to converge.
var ErrRotationConflict = errors.New("store: concurrent key rotation")

// RotateOrgKey mints a fresh org master and rewraps every owner DEK under
// it, in one transaction. Item ciphertexts seal under owner DEKs — the DEK
// bytes do not change — so rotation never re-seals items. The key_version
// guard makes concurrent rotations fail one side instead of interleaving
// rewraps under different masters.
func (p *Postgres) RotateOrgKey(ctx context.Context, orgID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	row, err := q.OrgKey(ctx, orgID)
	if err == pgx.ErrNoRows {
		return ErrOrgKeyMissing
	}
	if err != nil {
		return err
	}
	p.kekMu.RLock()
	oldMaster, err := crypto.OpenAAD(p.kek, row.Wrapped, []byte(orgID))
	p.kekMu.RUnlock()
	if err != nil {
		return fmt.Errorf("store: org_keys row for %s does not unwrap under this KEK: %w", orgID, err)
	}
	newMaster, err := crypto.NewKey()
	if err != nil {
		return err
	}
	owners, err := q.ListOwnerKeysForOrg(ctx, orgID)
	if err != nil {
		return err
	}
	for _, ow := range owners {
		dek, err := crypto.Open(oldMaster, ow.Wrapped)
		if err != nil {
			return fmt.Errorf("store: owner_keys row %s/%s does not unwrap: %w", ow.OwnerKind, ow.OwnerID, err)
		}
		resealed, err := crypto.Seal(newMaster, dek)
		if err != nil {
			return err
		}
		n, err := q.RewrapOwnerKey(ctx, sqlc.RewrapOwnerKeyParams{
			OrgID: orgID, OwnerKind: ow.OwnerKind, OwnerID: ow.OwnerID, Wrapped: resealed,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: owner_keys row %s/%s vanished mid-rotation", ow.OwnerKind, ow.OwnerID)
		}
	}
	p.kekMu.RLock()
	wrapped, err := crypto.SealAAD(p.kek, newMaster, []byte(orgID))
	p.kekMu.RUnlock()
	if err != nil {
		return err
	}
	n, err := q.BumpOrgKey(ctx, sqlc.BumpOrgKeyParams{
		OrgID: orgID, Wrapped: wrapped, RotatedAt: time.Now().UTC(), KeyVersion: row.KeyVersion,
	})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRotationConflict
	}
	// Recovery wraps seal the OLD master under owner-held material — after
	// rotation they would open a dead key. Delete them; owners re-mint.
	if err := q.DeleteRecoveryWrapsForOrg(ctx, orgID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.km.InvalidateOrg(orgID)
	return nil
}

// ReseedOrgKey overwrites the org_keys wrap under this store's KEK. It is
// the lost-KEK recovery verb: EnsureOrgKey refuses to touch an existing row,
// so after recovery material yields the master this re-anchors it. Fails
// ErrOrgKeyMissing when the org has no row — provisioning is EnsureOrgKey's
// job. The master itself is unchanged, so key_version stays — this is a
// re-wrap under a new KEK, not a rotation.
func (p *Postgres) ReseedOrgKey(ctx context.Context, orgID string, master []byte) error {
	if len(master) != crypto.KeySize {
		return fmt.Errorf("store: org master must be %d bytes", crypto.KeySize)
	}
	p.kekMu.RLock()
	wrapped, err := crypto.SealAAD(p.kek, master, []byte(orgID))
	p.kekMu.RUnlock()
	if err != nil {
		return err
	}
	n, err := retryOnDeadConn(func() (int64, error) {
		return p.sqlc.RewrapOrgKey(ctx, sqlc.RewrapOrgKeyParams{OrgID: orgID, Wrapped: wrapped})
	})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrOrgKeyMissing
	}
	p.km.InvalidateOrg(orgID)
	return nil
}

// recoveryAAD binds a recovery wrap to its org and owner — a row copied to
// another owner or org does not open.
func recoveryAAD(orgID string, o protocol.Owner) []byte {
	return []byte(orgID + "\x00" + string(o.Kind) + "\x00" + o.ID)
}

// StoreRecoveryWrap seals the org's committed master under owner-held
// recovery material and upserts the wrap. The recovery key never persists —
// losing it strands the wrap, not the vault. Re-minting replaces the row and
// clears used_at.
func (p *Postgres) StoreRecoveryWrap(ctx context.Context, orgID string, o protocol.Owner, recoveryKey []byte, expiresAt time.Time) error {
	if len(recoveryKey) != crypto.KeySize {
		return fmt.Errorf("store: recovery key must be %d bytes", crypto.KeySize)
	}
	if o.Kind == "" || o.ID == "" {
		return fmt.Errorf("store: missing owner")
	}
	master, err := p.resolveOrgKey(ctx, orgID)
	if err != nil {
		return err
	}
	wrapped, err := crypto.SealAAD(recoveryKey, master, recoveryAAD(orgID, o))
	if err != nil {
		return err
	}
	exp := sql.NullTime{Valid: !expiresAt.IsZero(), Time: expiresAt}
	_, err = retryOnDeadConn(func() (struct{}, error) {
		return struct{}{}, p.sqlc.PutRecoveryWrap(ctx, sqlc.PutRecoveryWrapParams{
			OrgID: orgID, OwnerKind: string(o.Kind), OwnerID: o.ID,
			Wrapped: wrapped, CreatedAt: time.Now().UTC(), ExpiresAt: exp,
		})
	})
	return err
}

// OpenRecoveryWrap verifies recoveryKey against the owner's wrap and returns
// the org master. Single-use: the first successful open stamps used_at inside
// the same transaction that locked the row — replays, concurrent opens, and
// expired wraps all fail closed.
func (p *Postgres) OpenRecoveryWrap(ctx context.Context, orgID string, o protocol.Owner, recoveryKey []byte) ([]byte, error) {
	if len(recoveryKey) != crypto.KeySize {
		return nil, fmt.Errorf("store: recovery key must be %d bytes", crypto.KeySize)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	row, err := q.RecoveryWrap(ctx, sqlc.RecoveryWrapParams{
		OrgID: orgID, OwnerKind: string(o.Kind), OwnerID: o.ID,
	})
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.UsedAt.Valid {
		return nil, fmt.Errorf("store: recovery wrap already used")
	}
	if row.ExpiresAt.Valid && time.Now().UTC().After(row.ExpiresAt.Time) {
		return nil, fmt.Errorf("store: recovery wrap expired")
	}
	master, err := crypto.OpenAAD(recoveryKey, row.Wrapped, recoveryAAD(orgID, o))
	if err != nil {
		return nil, fmt.Errorf("store: recovery key does not open this wrap: %w", err)
	}
	n, err := q.ConsumeRecoveryWrap(ctx, sqlc.ConsumeRecoveryWrapParams{
		OrgID: orgID, OwnerKind: string(o.Kind), OwnerID: o.ID, UsedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, fmt.Errorf("store: recovery wrap consumed concurrently")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return master, nil
}

// RotateKEK rewraps every org master under newKEK. Masters do not change —
// owner DEKs and item ciphertexts are untouched; only the org_keys wrap
// moves. cmk-managed rows are skipped (an external CMK owns its own wrap).
// On commit this store adopts newKEK; every cached master is invalidated so
// the next resolve re-unwraps under it.
func (p *Postgres) RotateKEK(ctx context.Context, newKEK []byte) error {
	if len(newKEK) != crypto.KeySize {
		return fmt.Errorf("store: KEK must be %d bytes", crypto.KeySize)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.sqlc.WithTx(tx)
	rows, err := q.ListOrgKeys(ctx)
	if err != nil {
		return err
	}
	p.kekMu.RLock()
	defer p.kekMu.RUnlock()
	for _, row := range rows {
		if row.CmkID.Valid {
			continue
		}
		master, err := crypto.OpenAAD(p.kek, row.Wrapped, []byte(row.OrgID))
		if err != nil {
			return fmt.Errorf("store: org_keys row for %s does not unwrap under the current KEK: %w", row.OrgID, err)
		}
		resealed, err := crypto.SealAAD(newKEK, master, []byte(row.OrgID))
		if err != nil {
			return err
		}
		n, err := q.RewrapOrgKey(ctx, sqlc.RewrapOrgKeyParams{OrgID: row.OrgID, Wrapped: resealed})
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: org_keys row for %s vanished mid-rotation", row.OrgID)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.kek = newKEK
	for _, row := range rows {
		p.km.InvalidateOrg(row.OrgID)
	}
	return nil
}

// EnsurePostgresSchema creates the vault tables/indexes if missing. It is the
// same DDL OpenPostgres runs at boot, exposed so the sqlite→postgres migrator
// can prepare an empty database without a master key.
func EnsurePostgresSchema(ctx context.Context, pool *pgxpool.Pool) error {
	// Legacy audit migration: a plain audit table cannot become partitioned
	// in place. Rename it aside first — the DDL below then creates the
	// partitioned form, the copy step moves rows, and the drop frees the
	// idx_audit_* index names before index creation runs. Detection keys on
	// relkind: 'r' is a plain table, 'p' is already partitioned.
	var auditIsPlain bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'audit' AND c.relkind = 'r')`).Scan(&auditIsPlain); err != nil {
		return err
	}
	if auditIsPlain {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		// Table renames do not rename owned sequences: the legacy identity
		// sequence keeps the name audit_id_seq. It must move aside or
		// CREATE SEQUENCE IF NOT EXISTS binds the partitioned table's
		// default to a sequence owned by the doomed legacy table — and the
		// later DROP fails on the dependency.
		for _, q := range []string{
			`ALTER TABLE audit RENAME TO audit_legacy`,
			`ALTER SEQUENCE IF EXISTS audit_id_seq RENAME TO audit_id_seq_legacy`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
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
		// Audit is range-partitioned by month on `at` (VEIL-4): retention is
		// DETACH PARTITION, not DELETE scans. The partition key must be part
		// of the PK, hence composite (id, at). id comes from a plain sequence
		// — PG16 cannot declare IDENTITY columns on partitioned tables.
		`CREATE SEQUENCE IF NOT EXISTS audit_id_seq`,
		`CREATE TABLE IF NOT EXISTS audit (
			id BIGINT NOT NULL DEFAULT nextval('audit_id_seq'),
			at TIMESTAMPTZ NOT NULL,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			action TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason TEXT NOT NULL,
			approval_id TEXT NOT NULL,
			PRIMARY KEY (id, at)
		) PARTITION BY RANGE (at)`,
		// Catch-all: rows outside every created month land here instead of
		// erroring. Migrated legacy rows live here permanently; retention
		// only detaches named monthly partitions.
		`CREATE TABLE IF NOT EXISTS audit_default PARTITION OF audit DEFAULT`,
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
		`CREATE TABLE IF NOT EXISTS recovery_wraps (
			org_id TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			wrapped BYTEA NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ,
			used_at TIMESTAMPTZ,
			PRIMARY KEY (org_id, owner_kind, owner_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workloads_issuer ON workloads(issuer)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	// Legacy audit copy: move every row from audit_legacy into the
	// partitioned table (the DEFAULT partition catches pre-partition-era
	// months), advance the sequence past copied ids, and drop the legacy
	// table — freeing the idx_audit_* names for the index phase below. One
	// transaction: a crash rolls back and the next boot retries the copy.
	var hasLegacyAudit bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'audit_legacy' AND c.relkind = 'r')`).Scan(&hasLegacyAudit); err != nil {
		return err
	}
	if hasLegacyAudit {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		for _, q := range []string{
			// A legacy table from before org-carrying rows may lack the
			// column entirely; default it so the copy backfills '' →
			// LocalOrgID via the later org backfill pass.
			`ALTER TABLE audit_legacy ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT ''`,
			`INSERT INTO audit(id, at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			 SELECT id, at, org_id, agent_id, item_id, action, decision, reason, approval_id FROM audit_legacy`,
			`SELECT setval('audit_id_seq', COALESCE((SELECT max(id) FROM audit), 1))`,
			`DROP TABLE audit_legacy`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	// Current and next month's partitions exist after every boot — the
	// origin self-maintains its partition horizon on deploy/restart, and
	// `veil sweep` runs the same ensure so a never-restarting process still
	// gets partitions.
	if err := EnsureAuditPartitions(ctx, pool, 2); err != nil {
		return err
	}
	// Index phase: after the legacy drop so the idx_audit_* names bind to
	// the partitioned parent and propagate to every partition.
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS idx_items_org_name ON items(org_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_items_org_archived_name ON items(org_id, archived, name)`,
		`CREATE INDEX IF NOT EXISTS idx_grants_item ON grants(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_agent_at ON audit(agent_id, at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_at ON audit(at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_item_versions_item ON item_versions(item_id, id)`,
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
	// master and move byte-for-byte. The gate is PK membership, not column
	// presence, and the sequence runs in one transaction — a crash mid-way
	// rolls back and the next boot retries cleanly instead of stranding the
	// table without a primary key.
	var pkHasOrg bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(c.conkey)
		WHERE n.nspname = current_schema()
		  AND t.relname = 'owner_keys'
		  AND c.contype = 'p'
		  AND a.attname = 'org_id')`).Scan(&pkHasOrg); err != nil {
		return err
	}
	if !pkHasOrg {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		for _, q := range []string{
			`ALTER TABLE owner_keys ADD COLUMN IF NOT EXISTS org_id TEXT`,
			`UPDATE owner_keys SET org_id = '` + protocol.LocalOrgID + `' WHERE org_id IS NULL`,
			`ALTER TABLE owner_keys DROP CONSTRAINT IF EXISTS owner_keys_pkey`,
			`ALTER TABLE owner_keys ALTER COLUMN org_id SET NOT NULL`,
			`ALTER TABLE owner_keys ADD PRIMARY KEY (org_id, owner_kind, owner_id)`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	// Rows with org_id '' predate multi-tenancy. They are the original
	// single-tenant vault, so they belong to LocalOrgID — the org whose
	// wrapped master still opens their ciphertexts. Any other assignment
	// would strand them undecryptable. The whole loop runs in one
	// transaction: readers never see a half-migrated mix of '' and
	// LocalOrgID rows, and a crash rolls back cleanly for the next boot.
	// The CHECK then makes the invariant durable — no writer can ever
	// reintroduce an empty org, and a stale binary that tries fails loudly
	// instead of reopening the unscoped-row hole.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, table := range []string{"humans", "agents", "items", "grants", "audit", "sessions", "recovery_wraps"} {
		if _, err := tx.Exec(ctx, `ALTER TABLE `+table+` ADD COLUMN IF NOT EXISTS org_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET org_id = $1 WHERE org_id = '' OR org_id IS NULL`, protocol.LocalOrgID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint c
				JOIN pg_class t ON t.oid = c.conrelid
				JOIN pg_namespace n ON n.oid = t.relnamespace
				WHERE n.nspname = current_schema()
				  AND t.relname = '`+table+`'
				  AND c.conname = '`+table+`_org_id_nonempty') THEN
				ALTER TABLE `+table+` ADD CONSTRAINT `+table+`_org_id_nonempty CHECK (org_id <> '');
			END IF;
		END $$`); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (p *Postgres) migrate() error {
	return EnsurePostgresSchema(context.Background(), p.pool)
}

func (p *Postgres) Close() error { p.pool.Close(); p.auditPool.Close(); return nil }

// auditPartitionName names monthly audit partitions deterministically.
func auditPartitionName(t time.Time) string {
	return fmt.Sprintf("audit_%04d_%02d", t.UTC().Year(), int(t.UTC().Month()))
}

// EnsureAuditPartitions creates monthly partitions of audit covering this
// month and ahead-1 following months. Idempotent — reruns create only the
// missing range. Boot calls it with ahead=2; `veil sweep` calls it with a
// wider horizon so a never-restarting origin still partitions ahead.
func EnsureAuditPartitions(ctx context.Context, pool *pgxpool.Pool, ahead int) error {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < ahead; i++ {
		from := start.AddDate(0, i, 0)
		to := from.AddDate(0, 1, 0)
		q := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF audit FOR VALUES FROM ('%s') TO ('%s')`,
			auditPartitionName(from),
			from.Format("2006-01-02"), to.Format("2006-01-02"))
		if _, err := pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// DetachAuditPartitionsBefore detaches every monthly audit partition whose
// range ends before cutoff, returning the detached table names. Detached
// partitions become ordinary tables — archive or drop is an ops call (e.g.
// pg_dump -t then DROP), never an implicit data loss inside the verb.
// audit_default is never detached: it is the catch-all, not a month.
func DetachAuditPartitionsBefore(ctx context.Context, pool *pgxpool.Pool, cutoff time.Time) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class p ON p.oid = i.inhparent
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_namespace n ON n.oid = p.relnamespace
		WHERE n.nspname = current_schema() AND p.relname = 'audit'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	monthName := regexp.MustCompile(`^audit_(\d{4})_(\d{2})$`)
	var detach []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		m := monthName.FindStringSubmatch(name)
		if m == nil {
			continue // audit_default and anything not month-named
		}
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		end := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		if !end.After(cutoff) {
			detach = append(detach, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var detached []string
	for _, name := range detach {
		if _, err := pool.Exec(ctx, `ALTER TABLE audit DETACH PARTITION `+name); err != nil {
			return detached, fmt.Errorf("detach %s: %w", name, err)
		}
		detached = append(detached, name)
	}
	return detached, nil
}

func (p *Postgres) ownerDEK(orgID string, o protocol.Owner) ([]byte, error) {
	return p.km.ownerDEK(context.Background(), p, orgID, o)
}

var (
	_ Store       = (*Postgres)(nil)
	_ ownerSource = (*Postgres)(nil)
)
