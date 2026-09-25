package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	db     *sql.DB
	master []byte
	km     *keyManager
	reqBus *requestBus
}

func OpenSQLite(path string, key []byte) (*SQLite, error) {
	if len(key) != crypto.KeySize {
		return nil, fmt.Errorf("store: key must be %d bytes", crypto.KeySize)
	}

	dsn := sqliteDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// In-memory databases are per-connection; keep the pool to one so a test
	// cannot open a second empty database. File-backed vaults can use a few
	// connections with busy-timeout queuing.
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(4)
	}
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(10 * time.Minute)

	// A local vault is one tenant: the vault key is the org master for every
	// org_id it will ever see. The resolver keeps sqlite symmetric with the
	// Postgres KEK→org_keys path without pretending at per-org custody.
	s := &SQLite{db: db, master: append([]byte(nil), key...), reqBus: newRequestBus()}
	s.km = newKeyManager(func(context.Context, string) ([]byte, error) { return key, nil })
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func sqliteDSN(path string) string {
	// The modernc driver accepts either a bare filename or a file: URI. Build
	// the DSN so connection-level pragmas are set on every pooled connection.
	u, err := url.Parse(path)
	if err == nil && (u.Scheme == "file" || u.Scheme == "") && path != ":memory:" && !strings.HasPrefix(path, "file::memory:") {
		q := u.Query()
		q.Set("_busy_timeout", "5000")
		q.Set("_journal_mode", "wal")
		q.Set("_fk", "1")
		q.Set("_txlock", "immediate")
		u.RawQuery = q.Encode()
		return u.String()
	}

	// For paths that do not parse as a simple URL (including :memory:),
	// append query parameters directly.
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "_busy_timeout=5000&_journal_mode=wal&_fk=1&_txlock=immediate"
}

func (s *SQLite) migrate() error {
	if err := EnsureSQLiteSchema(s.db); err != nil {
		return err
	}
	return s.rewrapLegacy()
}

// EnsureSQLiteSchema creates any missing tables and applies the sqlite
// schema migrations. It touches no secrets, so maintenance commands can call
// it on a raw handle without a master key.
func EnsureSQLiteSchema(db *sql.DB) error {
	s := &SQLite{db: db}
	for _, q := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS humans (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS items (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			uris TEXT NOT NULL,
			secret BLOB NOT NULL,
			has_totp INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS grants (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			level TEXT NOT NULL,
			actions TEXT NOT NULL,
			expires_at INTEGER,
			UNIQUE(agent_id, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS approvals (
			grant_id TEXT PRIMARY KEY,
			id TEXT NOT NULL,
			human_id TEXT NOT NULL,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS approval_requests (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			grant_id TEXT NOT NULL,
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			resolved_at INTEGER,
			resolved_by TEXT,
			approval_id TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS approval_requests_one_open
			ON approval_requests(grant_id, action) WHERE status = 'open'`,
		`CREATE TABLE IF NOT EXISTS audit (
			rowid INTEGER PRIMARY KEY AUTOINCREMENT,
			at TEXT NOT NULL,
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
			wrapped BLOB NOT NULL,
			PRIMARY KEY (org_id, owner_kind, owner_id)
		)`,
		`CREATE TABLE IF NOT EXISTS item_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			item_id TEXT NOT NULL,
			at TEXT NOT NULL,
			secret BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			secret_hash BLOB NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			revoked_at TEXT,
			renewed_at TEXT,
			ttl INTEGER NOT NULL,
			max_ttl INTEGER NOT NULL,
			max_uses INTEGER NOT NULL,
			uses INTEGER NOT NULL,
			UNIQUE(secret_hash)
		)`,
		`CREATE TABLE IF NOT EXISTS org_billing (
			org_id TEXT PRIMARY KEY,
			plan TEXT NOT NULL DEFAULT 'free',
			customer_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS usage_counters (
			org_id TEXT NOT NULL,
			window_start TEXT NOT NULL,
			used INTEGER NOT NULL,
			PRIMARY KEY (org_id, window_start)
		)`,
		`CREATE TABLE IF NOT EXISTS schema_version (
			name TEXT PRIMARY KEY,
			version INTEGER NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	_, _ = s.db.Exec(`ALTER TABLE items ADD COLUMN has_totp INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE agents ADD COLUMN owner_kind TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE agents ADD COLUMN owner_id TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE agents ADD COLUMN revoked_at TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE items ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'`)
	_, _ = s.db.Exec(`ALTER TABLE items ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE items ADD COLUMN has_file INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE items ADD COLUMN login TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN revoked_at TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN renewed_at TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN ttl INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN max_ttl INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN max_uses INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN uses INTEGER NOT NULL DEFAULT 0`)
	// Legacy rows predate the lifecycle columns. Give them usable
	// created_at/ttl/max_ttl so RenewSession can extend them.
	_, _ = s.db.Exec(`UPDATE sessions SET created_at = expires_at WHERE created_at = 0`)
	_, _ = s.db.Exec(`UPDATE sessions SET ttl = 900 WHERE ttl = 0`)
	_, _ = s.db.Exec(`UPDATE sessions SET max_ttl = 3600 WHERE max_ttl = 0`)
	// Rows with org_id '' predate multi-tenancy. They are the original
	// single-tenant vault, so they belong to LocalOrgID — the org whose
	// wrapped master still opens their ciphertexts. Writers always stamp a
	// non-empty org, so after this backfill strict org equality is
	// fail-closed. One transaction: no half-migrated mix is ever visible.
	// sqlite cannot ALTER ADD a CHECK; the single-process store plus the
	// app-level empty-org denials cover what pg enforces by constraint.
	for _, table := range []string{"humans", "agents", "items", "grants", "audit", "sessions"} {
		// Duplicate-column errors mean the column already exists — ignored.
		_, _ = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN org_id TEXT NOT NULL DEFAULT ''`)
	}
	btx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = btx.Rollback() }()
	for _, table := range []string{"humans", "agents", "items", "grants", "audit", "sessions"} {
		if _, err := btx.Exec(`UPDATE `+table+` SET org_id = ? WHERE org_id = ''`, protocol.LocalOrgID); err != nil {
			return err
		}
	}
	if err := btx.Commit(); err != nil {
		return err
	}
	if err := s.dropItemsNameUnique(); err != nil {
		return err
	}
	if err := s.rebuildOwnerKeysOrg(); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS idx_items_org_name ON items(org_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_items_org_archived_name ON items(org_id, archived, name)`,
		`CREATE INDEX IF NOT EXISTS idx_grants_item ON grants(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_at ON audit(at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_agent_at ON audit(agent_id, at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_item_versions_item ON item_versions(item_id, id)`,
		`CREATE INDEX IF NOT EXISTS idx_workloads_issuer ON workloads(issuer)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) dropItemsNameUnique() error {
	var schema string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='items'`).Scan(&schema); err != nil {
		return err
	}
	compact := strings.ReplaceAll(schema, " ", "")
	if !strings.Contains(compact, "UNIQUE(org_id,name)") {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE items_noidx (
			id TEXT PRIMARY KEY,
			org_id TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			uris TEXT NOT NULL,
			secret BLOB NOT NULL,
			has_totp INTEGER NOT NULL DEFAULT 0,
			tags TEXT NOT NULL DEFAULT '[]',
			archived INTEGER NOT NULL DEFAULT 0,
			has_file INTEGER NOT NULL DEFAULT 0,
			login TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO items_noidx(id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp, tags, archived, has_file, login)
		SELECT id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp, tags, archived, has_file, login FROM items`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE items`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE items_noidx RENAME TO items`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) Close() error { return s.db.Close() }

func revokedAtString(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}

func parseRevokedAt(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *SQLite) PutAgent(p protocol.Principal) error {
	rv := revokedAtString(p.RevokedAt)
	// Conflict keeps the existing org/owner — an agent id must never be
	// reassigned across orgs by an upsert.
	_, err := s.db.Exec(`INSERT INTO agents(id, org_id, owner_kind, owner_id, revoked_at) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET revoked_at=COALESCE(agents.revoked_at, excluded.revoked_at)`,
		p.ID, p.OrgID, p.Owner.Kind, p.Owner.ID, rv)
	return err
}

func (s *SQLite) Agent(id string) (protocol.Principal, error) {
	var p protocol.Principal
	p.Kind = protocol.PrincipalAgent
	var ownerKind, ownerID, revokedAt sql.NullString
	err := s.db.QueryRow(`SELECT id, org_id, owner_kind, owner_id, revoked_at FROM agents WHERE id=?`, id).Scan(&p.ID, &p.OrgID, &ownerKind, &ownerID, &revokedAt)
	if err == sql.ErrNoRows {
		return protocol.Principal{}, ErrNotFound
	}
	if err != nil {
		return protocol.Principal{}, err
	}
	p.Owner.Kind = protocol.OwnerKind(ownerKind.String)
	p.Owner.ID = ownerID.String
	rv, err := parseRevokedAt(revokedAt)
	if err != nil {
		return protocol.Principal{}, err
	}
	p.RevokedAt = rv
	return p, nil
}

func (s *SQLite) ListAgents() ([]protocol.Principal, error) {
	rows, err := s.db.Query(`SELECT id, org_id, owner_kind, owner_id, revoked_at FROM agents ORDER BY id LIMIT ?`, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Principal
	for rows.Next() {
		var p protocol.Principal
		p.Kind = protocol.PrincipalAgent
		var ownerKind, ownerID, revokedAt sql.NullString
		if err := rows.Scan(&p.ID, &p.OrgID, &ownerKind, &ownerID, &revokedAt); err != nil {
			return nil, err
		}
		p.Owner.Kind = protocol.OwnerKind(ownerKind.String)
		p.Owner.ID = ownerID.String
		rv, err := parseRevokedAt(revokedAt)
		if err != nil {
			return nil, err
		}
		p.RevokedAt = rv
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) RevokeAgent(id string, at time.Time, audit ...protocol.AuditEvent) error {
	if len(audit) == 0 {
		rv := at.UTC().Format(time.RFC3339)
		res, err := s.db.Exec(`UPDATE agents SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, rv, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rv := at.UTC().Format(time.RFC3339)
	res, err := tx.Exec(`UPDATE agents SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, rv, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}

	for _, e := range audit {
		if _, err := tx.Exec(`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			VALUES(?,?,?,?,?,?,?,?)`,
			e.Time.UTC().Format(time.RFC3339Nano), e.OrgID, e.AgentID, e.ItemID, e.Action, e.Decision, e.Reason, e.ApprovalID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLite) PutHuman(p protocol.Principal) error {
	_, err := s.db.Exec(`INSERT INTO humans(id, org_id) VALUES(?, ?)
		ON CONFLICT(id) DO UPDATE SET org_id=excluded.org_id`, p.ID, p.OrgID)
	return err
}

// PlantHuman is the provisioning anchor — insert-if-absent, never overwrite.
func (s *SQLite) PlantHuman(p protocol.Principal) (bool, error) {
	res, err := s.db.Exec(`INSERT INTO humans(id, org_id) VALUES(?, ?)
		ON CONFLICT(id) DO NOTHING`, p.ID, p.OrgID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// A sqlite vault is one tenant: the single vault key covers every org, so
// org-key provisioning is a no-op here. Per-org masters are a Postgres shape.
func (s *SQLite) EnsureOrgKey(context.Context, string, []byte) error { return nil }
func (s *SQLite) HasOrgKey(context.Context, string) (bool, error)    { return true, nil }
func (s *SQLite) RotateOrgKey(context.Context, string) error         { return ErrUnsupported }
func (s *SQLite) RotateKEK(context.Context, []byte) error            { return ErrUnsupported }
func (s *SQLite) StoreRecoveryWrap(context.Context, string, protocol.Owner, []byte, time.Time) error {
	return ErrUnsupported
}
func (s *SQLite) OpenRecoveryWrap(context.Context, string, protocol.Owner, []byte) ([]byte, error) {
	return nil, ErrUnsupported
}
func (s *SQLite) ReseedOrgKey(context.Context, string, []byte) error { return ErrUnsupported }
func (s *SQLite) RecoverOrgKey(context.Context, string, protocol.Owner, []byte) error {
	return ErrUnsupported
}

func (s *SQLite) Human(id string) (protocol.Principal, error) {
	var p protocol.Principal
	p.Kind = protocol.PrincipalHuman
	err := s.db.QueryRow(`SELECT id, org_id FROM humans WHERE id=?`, id).Scan(&p.ID, &p.OrgID)
	if err == sql.ErrNoRows {
		return protocol.Principal{}, ErrNotFound
	}
	return p, err
}

func (s *SQLite) ListHumans() ([]protocol.Principal, error) {
	rows, err := s.db.Query(`SELECT id, org_id FROM humans ORDER BY id LIMIT ?`, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Principal
	for rows.Next() {
		var p protocol.Principal
		p.Kind = protocol.PrincipalHuman
		if err := rows.Scan(&p.ID, &p.OrgID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type sqlExecer interface {
	QueryRow(string, ...interface{}) *sql.Row
	Exec(string, ...interface{}) (sql.Result, error)
}

func (s *SQLite) snapshot(c sqlExecer, id string) error {
	var blob []byte
	err := c.QueryRow(`SELECT secret FROM items WHERE id=?`, id).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = c.Exec(`INSERT INTO item_versions(item_id, at, secret) VALUES(?,?,?)`,
		id, time.Now().UTC().Format(time.RFC3339Nano), blob)
	return err
}

func (s *SQLite) PutItem(item protocol.Item, secret Secret) error {
	uris, err := json.Marshal(item.URIs)
	if err != nil {
		return err
	}
	if item.URIs == nil {
		uris = []byte("[]")
	}
	tags, err := json.Marshal(item.Tags)
	if err != nil {
		return err
	}
	if item.Tags == nil {
		tags = []byte("[]")
	}
	dek, err := s.ownerDEK(item.OrgID, item.Owner)
	if err != nil {
		return err
	}
	blob, err := crypto.SealEpoch(dek, secret, itemAAD(item.OrgID, item.ID))
	if err != nil {
		return err
	}
	has := 0
	if item.HasTOTP {
		has = 1
	}
	arch := 0
	if item.Archived {
		arch = 1
	}
	hf := 0
	if item.HasFile {
		hf = 1
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var existingOwner protocol.Owner
	var existingOrg string
	err = tx.QueryRow(`SELECT owner_kind, owner_id, org_id FROM items WHERE id=?`, item.ID).Scan(&existingOwner.Kind, &existingOwner.ID, &existingOrg)
	if err == nil {
		if existingOwner != item.Owner || existingOrg != item.OrgID {
			return fmt.Errorf("store: cannot change item owner")
		}
	} else if err != sql.ErrNoRows {
		return err
	}

	if err := s.snapshot(tx, item.ID); err != nil {
		return err
	}
	res, err := tx.Exec(`INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp, tags, archived, has_file, login)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			org_id=excluded.org_id, name=excluded.name, kind=excluded.kind,
			owner_kind=excluded.owner_kind, owner_id=excluded.owner_id,
			uris=excluded.uris, secret=excluded.secret, has_totp=excluded.has_totp,
			tags=excluded.tags, archived=excluded.archived, has_file=excluded.has_file,
			login=excluded.login
		WHERE items.owner_kind=excluded.owner_kind AND items.owner_id=excluded.owner_id
			AND items.org_id=excluded.org_id`,
		item.ID, item.OrgID, item.Name, item.Kind, item.Owner.Kind, item.Owner.ID, uris, blob, has, tags, arch, hf, item.Login)
	if err != nil {
		return err
	}
	// Zero rows means the row was created under another owner or org between
	// the pre-check and this upsert.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("store: cannot change item owner")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLite) scanItem(scan func(dest ...any) error) (protocol.Item, error) {
	var item protocol.Item
	var uris, tags []byte
	var has, arch, hf int
	err := scan(&item.ID, &item.OrgID, &item.Name, &item.Kind, &item.Owner.Kind, &item.Owner.ID, &uris, &has, &tags, &arch, &hf, &item.Login)
	if err == sql.ErrNoRows {
		return protocol.Item{}, ErrNotFound
	}
	if err != nil {
		return protocol.Item{}, err
	}
	if len(uris) > 0 {
		_ = json.Unmarshal(uris, &item.URIs)
	}
	if len(tags) > 0 {
		_ = json.Unmarshal(tags, &item.Tags)
	}
	item.HasTOTP = has != 0
	item.Archived = arch != 0
	item.HasFile = hf != 0
	return item, nil
}

func (s *SQLite) Item(id string) (protocol.Item, error) {
	row := s.db.QueryRow(`SELECT id, org_id, name, kind, owner_kind, owner_id, uris, has_totp, tags, archived, has_file, login FROM items WHERE id=?`, id)
	return s.scanItem(row.Scan)
}

func (s *SQLite) ItemByName(orgID, name string) (protocol.Item, error) {
	row := s.db.QueryRow(`SELECT id, org_id, name, kind, owner_kind, owner_id, uris, has_totp, tags, archived, has_file, login FROM items WHERE org_id=? AND name=?`, orgID, name)
	return s.scanItem(row.Scan)
}

func (s *SQLite) ListItems() ([]protocol.Item, error) {
	rows, err := s.db.Query(`SELECT id, org_id, name, kind, owner_kind, owner_id, uris, has_totp, tags, archived, has_file, login FROM items WHERE archived=0 ORDER BY name LIMIT ?`, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Item
	for rows.Next() {
		item, err := s.scanItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLite) ArchiveItem(id string) error {
	res, err := s.db.Exec(`UPDATE items SET archived=1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) DeleteItem(id string) error {
	if _, err := s.db.Exec(`DELETE FROM item_versions WHERE item_id=?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM grants WHERE item_id=?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM items WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteHuman drops the humans row — token resolution fails closed after this.
func (s *SQLite) DeleteHuman(_ context.Context, id string) error {
	_, err := s.db.Exec(`DELETE FROM humans WHERE id=?`, id)
	return err
}

// PurgeOrg deletes every vault row owned by orgID in one transaction — the
// sqlite schema has no org_keys/recovery_wraps (encryption is file-local).
// Audit rows survive deliberately: teardown must not erase the record.
func (s *SQLite) PurgeOrg(ctx context.Context, orgID string) (PurgeReport, error) {
	var rep PurgeReport
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	n := func(q string, dst *int64) error {
		res, err := tx.ExecContext(ctx, q, orgID)
		if err != nil {
			return err
		}
		*dst, err = res.RowsAffected()
		return err
	}
	steps := []struct {
		q   string
		dst *int64
	}{
		{`DELETE FROM item_versions WHERE item_id IN (SELECT id FROM items WHERE org_id = ?)`, new(int64)},
		{`DELETE FROM approvals WHERE grant_id IN (SELECT id FROM grants WHERE org_id = ?)`, new(int64)},
		{`DELETE FROM sessions WHERE org_id = ?`, &rep.Sessions},
		{`DELETE FROM grants WHERE org_id = ?`, &rep.Grants},
		{`DELETE FROM workloads WHERE agent_id IN (SELECT id FROM agents WHERE org_id = ?)`, new(int64)},
		{`DELETE FROM owner_keys WHERE org_id = ?`, new(int64)},
		{`DELETE FROM items WHERE org_id = ?`, &rep.Items},
		{`DELETE FROM agents WHERE org_id = ?`, &rep.Agents},
		{`DELETE FROM humans WHERE org_id = ?`, &rep.Humans},
	}
	for _, s := range steps {
		if err := n(s.q, s.dst); err != nil {
			return rep, fmt.Errorf("purge org: %w", err)
		}
	}
	return rep, tx.Commit()
}

func (s *SQLite) Versions(itemID string) ([]protocol.ItemVersion, error) {
	// Keep the newest maxListResults snapshots, then return them in
	// chronological order (oldest first) so callers can restore by index.
	rows, err := s.db.Query(`SELECT id, item_id, at FROM item_versions WHERE item_id=? ORDER BY id DESC LIMIT ?`, itemID, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.ItemVersion
	for rows.Next() {
		var v protocol.ItemVersion
		var at string
		if err := rows.Scan(&v.ID, &v.ItemID, &at); err != nil {
			return nil, err
		}
		v.Time, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *SQLite) RestoreVersion(itemID string, versionID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := s.snapshot(tx, itemID); err != nil {
		return err
	}
	var blob []byte
	err = tx.QueryRow(`SELECT secret FROM item_versions WHERE id=? AND item_id=?`, versionID, itemID).Scan(&blob)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE items SET secret=? WHERE id=?`, blob, itemID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLite) Secret(id string) (Secret, error) {
	var blob []byte
	var owner protocol.Owner
	var orgID string
	err := s.db.QueryRow(`SELECT secret, org_id, owner_kind, owner_id FROM items WHERE id=?`, id).Scan(&blob, &orgID, &owner.Kind, &owner.ID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	dek, err := s.ownerDEK(orgID, owner)
	if err != nil {
		return nil, err
	}
	plain, err := crypto.OpenEpoch(dek, blob, itemAAD(orgID, id))
	if err != nil {
		return nil, err
	}
	return Secret(plain), nil
}

func (s *SQLite) UseAuthSession(sessionHash []byte, itemID string, now time.Time) (UseAuth, error) {
	row := s.db.QueryRow(`SELECT
		s.id,
		a.id, a.org_id, a.owner_kind, a.owner_id, a.revoked_at,
		i.id, i.org_id, i.name, i.kind, i.owner_kind, i.owner_id, i.uris, i.has_totp, i.tags, i.archived, i.has_file, i.login,
		g.id, g.org_id, g.agent_id, g.item_id, g.level, g.actions, g.expires_at,
		ap.id, ap.grant_id, ap.human_id, ap.expires_at
	FROM (SELECT ? AS session_hash, ? AS item_id, ? AS now) AS v
	LEFT JOIN sessions s ON s.secret_hash = v.session_hash AND s.expires_at > v.now AND s.revoked_at IS NULL AND (s.max_uses = 0 OR s.uses < s.max_uses)
	LEFT JOIN agents a ON a.id = s.agent_id
	LEFT JOIN items i ON i.id = v.item_id
	LEFT JOIN grants g ON g.agent_id = s.agent_id AND g.item_id = i.id
	LEFT JOIN approvals ap ON ap.grant_id = g.id AND ap.expires_at > v.now`,
		sessionHash, itemID, now.Unix())

	var (
		sID                                                     sql.NullString
		aID, aOrgID, aOwnerKind, aOwnerID, aRevoked             sql.NullString
		iID, iOrgID, iName, iKind, iOwnerKind, iOwnerID, iLogin sql.NullString
		iURIs, iTags                                            []byte
		iHasTOTP, iArchived, iHasFile                           sql.NullInt64
		gID, gOrgID, gAgentID, gItemID, gLevel                  sql.NullString
		gActions                                                []byte
		gExpires, apExpires                                     sql.NullInt64
		apID, apGrantID, apHumanID                              sql.NullString
	)
	if err := row.Scan(
		&sID,
		&aID, &aOrgID, &aOwnerKind, &aOwnerID, &aRevoked,
		&iID, &iOrgID, &iName, &iKind, &iOwnerKind, &iOwnerID, &iURIs, &iHasTOTP, &iTags, &iArchived, &iHasFile, &iLogin,
		&gID, &gOrgID, &gAgentID, &gItemID, &gLevel, &gActions, &gExpires,
		&apID, &apGrantID, &apHumanID, &apExpires,
	); err != nil {
		return UseAuth{}, err
	}
	if !sID.Valid || sID.String == "" || !aID.Valid || aID.String == "" {
		return UseAuth{}, ErrNotFound
	}

	var r UseAuth
	if aID.Valid && aID.String != "" {
		r.Agent = protocol.Principal{Kind: protocol.PrincipalAgent, ID: aID.String, OrgID: aOrgID.String}
		r.Agent.Owner.Kind = protocol.OwnerKind(aOwnerKind.String)
		r.Agent.Owner.ID = aOwnerID.String
		if aRevoked.Valid && aRevoked.String != "" {
			t, err := time.Parse(time.RFC3339, aRevoked.String)
			if err != nil {
				return UseAuth{}, err
			}
			tr := t.UTC()
			r.Agent.RevokedAt = &tr
		}
	}
	if iID.Valid && iID.String != "" {
		r.Item = protocol.Item{ID: iID.String, OrgID: iOrgID.String, Name: iName.String, Kind: protocol.ItemKind(iKind.String)}
		r.Item.Owner.Kind = protocol.OwnerKind(iOwnerKind.String)
		r.Item.Owner.ID = iOwnerID.String
		if len(iURIs) > 0 {
			_ = json.Unmarshal(iURIs, &r.Item.URIs)
		}
		if len(iTags) > 0 {
			_ = json.Unmarshal(iTags, &r.Item.Tags)
		}
		r.Item.HasTOTP = iHasTOTP.Int64 != 0
		r.Item.Archived = iArchived.Int64 != 0
		r.Item.HasFile = iHasFile.Int64 != 0
		r.Item.Login = iLogin.String
	}
	if gID.Valid && gID.String != "" {
		g := &protocol.Grant{ID: gID.String, OrgID: gOrgID.String, AgentID: gAgentID.String, ItemID: gItemID.String, Level: protocol.GrantLevel(gLevel.String)}
		if len(gActions) > 0 {
			_ = json.Unmarshal(gActions, &g.Actions)
		}
		if gExpires.Valid {
			t := time.Unix(gExpires.Int64, 0).UTC()
			g.ExpiresAt = &t
		}
		r.Grant = g
	}
	if apID.Valid && apID.String != "" {
		if apExpires.Valid {
			r.Approval = &protocol.Approval{ID: apID.String, GrantID: apGrantID.String, HumanID: apHumanID.String, ExpiresAt: time.Unix(apExpires.Int64, 0).UTC()}
		}
	}
	return r, nil
}

func (s *SQLite) ConsumeSession(sessionHash []byte, now time.Time) (protocol.Principal, error) {
	return s.consumeSession(sessionHash, now, nil)
}

func (s *SQLite) ConsumeSessionAudited(sessionHash []byte, now time.Time, e protocol.AuditEvent) (protocol.Principal, error) {
	return s.consumeSession(sessionHash, now, &e)
}

// consumeSession runs the atomic consume UPDATE — and, when e is set, the
// audit INSERT — in one transaction. A failed audit insert rolls the consume
// back, so an allow can never leave the origin unaudited.
func (s *SQLite) consumeSession(sessionHash []byte, now time.Time, e *protocol.AuditEvent) (protocol.Principal, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.Principal{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var agentID string
	err = tx.QueryRow(`UPDATE sessions SET uses = uses + 1
		WHERE secret_hash = ? AND expires_at > ? AND revoked_at IS NULL AND (max_uses = 0 OR uses < max_uses)
		RETURNING agent_id`,
		sessionHash, now.Unix()).Scan(&agentID)
	if err == sql.ErrNoRows {
		return protocol.Principal{}, ErrNotFound
	}
	if err != nil {
		return protocol.Principal{}, err
	}

	var (
		aID, aOrgID, aOwnerKind, aOwnerID sql.NullString
		aRevoked                          sql.NullString
	)
	err = tx.QueryRow(`SELECT id, org_id, owner_kind, owner_id, revoked_at FROM agents WHERE id = ? AND revoked_at IS NULL`, agentID).
		Scan(&aID, &aOrgID, &aOwnerKind, &aOwnerID, &aRevoked)
	if err == sql.ErrNoRows {
		return protocol.Principal{}, ErrNotFound
	}
	if err != nil {
		return protocol.Principal{}, err
	}
	if e != nil {
		e.OrgID = aOrgID.String
		e.AgentID = aID.String
		if _, err := tx.Exec(`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			VALUES(?,?,?,?,?,?,?,?)`,
			e.Time.UTC().Format(time.RFC3339Nano), e.OrgID, e.AgentID, e.ItemID, e.Action, e.Decision, e.Reason, e.ApprovalID); err != nil {
			return protocol.Principal{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return protocol.Principal{}, err
	}

	a := protocol.Principal{Kind: protocol.PrincipalAgent, ID: aID.String, OrgID: aOrgID.String}
	a.Owner.Kind = protocol.OwnerKind(aOwnerKind.String)
	a.Owner.ID = aOwnerID.String
	if aRevoked.Valid && aRevoked.String != "" {
		t, err := time.Parse(time.RFC3339, aRevoked.String)
		if err != nil {
			return protocol.Principal{}, err
		}
		tr := t.UTC()
		a.RevokedAt = &tr
	}
	return a, nil
}

func (s *SQLite) UseAuth(agentID, itemID string, now time.Time) (UseAuth, error) {
	row := s.db.QueryRow(`SELECT
		a.id, a.org_id, a.owner_kind, a.owner_id, a.revoked_at,
		i.id, i.org_id, i.name, i.kind, i.owner_kind, i.owner_id, i.uris, i.has_totp, i.tags, i.archived, i.has_file, i.login,
		g.id, g.org_id, g.agent_id, g.item_id, g.level, g.actions, g.expires_at,
		ap.id, ap.grant_id, ap.human_id, ap.expires_at
	FROM (SELECT ? AS agent_id, ? AS item_id, ? AS now) AS v
	LEFT JOIN agents a ON a.id = v.agent_id
	LEFT JOIN items i ON i.id = v.item_id
	LEFT JOIN grants g ON g.agent_id = v.agent_id AND g.item_id = i.id
	LEFT JOIN approvals ap ON ap.grant_id = g.id AND ap.expires_at > v.now`,
		agentID, itemID, now.Unix())

	var (
		aID, aOrgID, aOwnerKind, aOwnerID, aRevoked             sql.NullString
		iID, iOrgID, iName, iKind, iOwnerKind, iOwnerID, iLogin sql.NullString
		iURIs, iTags                                            []byte
		iHasTOTP, iArchived, iHasFile                           sql.NullInt64
		gID, gOrgID, gAgentID, gItemID, gLevel                  sql.NullString
		gActions                                                []byte
		gExpires, apExpires                                     sql.NullInt64
		apID, apGrantID, apHumanID                              sql.NullString
	)
	if err := row.Scan(
		&aID, &aOrgID, &aOwnerKind, &aOwnerID, &aRevoked,
		&iID, &iOrgID, &iName, &iKind, &iOwnerKind, &iOwnerID, &iURIs, &iHasTOTP, &iTags, &iArchived, &iHasFile, &iLogin,
		&gID, &gOrgID, &gAgentID, &gItemID, &gLevel, &gActions, &gExpires,
		&apID, &apGrantID, &apHumanID, &apExpires,
	); err != nil {
		return UseAuth{}, err
	}

	var r UseAuth
	if aID.Valid && aID.String != "" {
		r.Agent = protocol.Principal{Kind: protocol.PrincipalAgent, ID: aID.String, OrgID: aOrgID.String}
		r.Agent.Owner.Kind = protocol.OwnerKind(aOwnerKind.String)
		r.Agent.Owner.ID = aOwnerID.String
		if aRevoked.Valid && aRevoked.String != "" {
			t, err := time.Parse(time.RFC3339, aRevoked.String)
			if err != nil {
				return UseAuth{}, err
			}
			tr := t.UTC()
			r.Agent.RevokedAt = &tr
		}
	}
	if iID.Valid && iID.String != "" {
		r.Item = protocol.Item{ID: iID.String, OrgID: iOrgID.String, Name: iName.String, Kind: protocol.ItemKind(iKind.String)}
		r.Item.Owner.Kind = protocol.OwnerKind(iOwnerKind.String)
		r.Item.Owner.ID = iOwnerID.String
		if len(iURIs) > 0 {
			_ = json.Unmarshal(iURIs, &r.Item.URIs)
		}
		if len(iTags) > 0 {
			_ = json.Unmarshal(iTags, &r.Item.Tags)
		}
		r.Item.HasTOTP = iHasTOTP.Int64 != 0
		r.Item.Archived = iArchived.Int64 != 0
		r.Item.HasFile = iHasFile.Int64 != 0
		r.Item.Login = iLogin.String
	}
	if gID.Valid && gID.String != "" {
		g := &protocol.Grant{ID: gID.String, OrgID: gOrgID.String, AgentID: gAgentID.String, ItemID: gItemID.String, Level: protocol.GrantLevel(gLevel.String)}
		if len(gActions) > 0 {
			_ = json.Unmarshal(gActions, &g.Actions)
		}
		if gExpires.Valid {
			t := time.Unix(gExpires.Int64, 0).UTC()
			g.ExpiresAt = &t
		}
		r.Grant = g
	}
	if apID.Valid && apID.String != "" {
		if apExpires.Valid {
			r.Approval = &protocol.Approval{ID: apID.String, GrantID: apGrantID.String, HumanID: apHumanID.String, ExpiresAt: time.Unix(apExpires.Int64, 0).UTC()}
		}
	}
	return r, nil
}

func (s *SQLite) PutGrant(g protocol.Grant) error {
	actions, err := json.Marshal(g.Actions)
	if err != nil {
		return err
	}
	var exp any
	if g.ExpiresAt != nil {
		exp = g.ExpiresAt.Unix()
	}
	_, err = s.db.Exec(`INSERT INTO grants(id, org_id, agent_id, item_id, level, actions, expires_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(agent_id, item_id) DO UPDATE SET
			id=excluded.id, org_id=excluded.org_id, level=excluded.level,
			actions=excluded.actions, expires_at=excluded.expires_at`,
		g.ID, g.OrgID, g.AgentID, g.ItemID, g.Level, actions, exp)
	return err
}

func scanGrant(scan func(dest ...any) error) (*protocol.Grant, error) {
	var g protocol.Grant
	var actions []byte
	var exp sql.NullInt64
	err := scan(&g.ID, &g.OrgID, &g.AgentID, &g.ItemID, &g.Level, &actions, &exp)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(actions) > 0 {
		_ = json.Unmarshal(actions, &g.Actions)
	}
	if exp.Valid {
		t := time.Unix(exp.Int64, 0).UTC()
		g.ExpiresAt = &t
	}
	return &g, nil
}

func (s *SQLite) Grant(id string) (*protocol.Grant, error) {
	row := s.db.QueryRow(`SELECT id, org_id, agent_id, item_id, level, actions, expires_at FROM grants WHERE id=?`, id)
	return scanGrant(row.Scan)
}

func (s *SQLite) GrantFor(agentID, itemID string) (*protocol.Grant, error) {
	row := s.db.QueryRow(`SELECT id, org_id, agent_id, item_id, level, actions, expires_at FROM grants WHERE agent_id=? AND item_id=?`, agentID, itemID)
	g, err := scanGrant(row.Scan)
	if err == ErrNotFound {
		return nil, nil
	}
	return g, err
}

func (s *SQLite) ListGrants() ([]protocol.Grant, error) {
	rows, err := s.db.Query(`SELECT id, org_id, agent_id, item_id, level, actions, expires_at FROM grants ORDER BY id LIMIT ?`, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Grant
	for rows.Next() {
		g, err := scanGrant(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (s *SQLite) PutApproval(a protocol.Approval) error {
	_, err := s.db.Exec(`INSERT INTO approvals(grant_id, id, human_id, expires_at) VALUES(?,?,?,?)
		ON CONFLICT(grant_id) DO UPDATE SET id=excluded.id, human_id=excluded.human_id, expires_at=excluded.expires_at`,
		a.GrantID, a.ID, a.HumanID, a.ExpiresAt.Unix())
	return err
}

func (s *SQLite) LiveApproval(grantID string, now time.Time) (*protocol.Approval, error) {
	var a protocol.Approval
	var exp int64
	err := s.db.QueryRow(`SELECT grant_id, id, human_id, expires_at FROM approvals WHERE grant_id=?`, grantID).
		Scan(&a.GrantID, &a.ID, &a.HumanID, &exp)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.ExpiresAt = time.Unix(exp, 0).UTC()
	if !now.Before(a.ExpiresAt) {
		return nil, nil
	}
	return &a, nil
}

func (s *SQLite) scanRequest(row *sql.Row) (protocol.ApprovalRequest, error) {
	var r protocol.ApprovalRequest
	var created, exp int64
	var resolvedAt sql.NullInt64
	var resolvedBy, approvalID sql.NullString
	err := row.Scan(&r.ID, &r.OrgID, &r.AgentID, &r.ItemID, &r.GrantID, &r.Action,
		&r.Status, &created, &exp, &resolvedAt, &resolvedBy, &approvalID)
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	r.CreatedAt, r.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(exp, 0).UTC()
	r.ResolvedBy, r.ApprovalID = resolvedBy.String, approvalID.String
	if resolvedAt.Valid {
		at := time.Unix(resolvedAt.Int64, 0).UTC()
		r.ResolvedAt = &at
	}
	return r, nil
}

const requestCols = `id, org_id, agent_id, item_id, grant_id, action, status,
	created_at, expires_at, resolved_at, resolved_by, approval_id`

func (s *SQLite) FileRequest(req protocol.ApprovalRequest) (FileOutcome, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return FileOutcome{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var expiredID string
	err = tx.QueryRow(`UPDATE approval_requests SET status='expired', resolved_at=?
		WHERE grant_id=? AND action=? AND status='open' AND expires_at <= ? RETURNING id`,
		req.CreatedAt.Unix(), req.GrantID, string(req.Action), req.CreatedAt.Unix()).Scan(&expiredID)
	if err == sql.ErrNoRows {
		expiredID = ""
	} else if err != nil {
		return FileOutcome{}, err
	}
	res, err := tx.Exec(`INSERT INTO approval_requests(id, org_id, agent_id, item_id,
		grant_id, action, status, created_at, expires_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(grant_id, action) WHERE status='open' DO NOTHING`,
		req.ID, req.OrgID, req.AgentID, req.ItemID, req.GrantID, string(req.Action),
		string(protocol.RequestOpen), req.CreatedAt.Unix(), req.ExpiresAt.Unix())
	if err != nil {
		return FileOutcome{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return FileOutcome{}, err
	}
	var out protocol.ApprovalRequest
	if n == 0 {
		out, err = s.scanRequest(tx.QueryRow(`SELECT `+requestCols+` FROM approval_requests
			WHERE grant_id=? AND action=? AND status='open'`, req.GrantID, string(req.Action)))
		if err != nil {
			return FileOutcome{}, err
		}
	} else {
		out = req
		out.Status = protocol.RequestOpen
	}
	// Audit inside the same commit — the expired predecessor and the fresh
	// file land with the rows they describe.
	if expiredID != "" {
		old := req
		old.ID = expiredID
		if err := auditRequestEventTx(tx, protocol.ActionRequestExpired, old, req.CreatedAt, ""); err != nil {
			return FileOutcome{}, err
		}
	}
	if n > 0 {
		if err := auditRequestEventTx(tx, protocol.ActionRequestFiled, req, req.CreatedAt, ""); err != nil {
			return FileOutcome{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FileOutcome{}, err
	}
	if n > 0 || expiredID != "" {
		s.reqBus.notify(req.OrgID)
	}
	return FileOutcome{Request: out, Created: n > 0, ExpiredID: expiredID}, nil
}

// auditRequestEventTx writes one request-lifecycle audit row inside tx.
// Reason names the request so the event joins back to the authoritative
// row; decision follows the action.
func auditRequestEventTx(tx *sql.Tx, action protocol.ActionKind, r protocol.ApprovalRequest, at time.Time, approvalID string) error {
	decision := protocol.DecisionAllow
	switch action {
	case protocol.ActionRequestDenied, protocol.ActionRequestCancelled:
		decision = protocol.DecisionDeny
	case protocol.ActionRequestFiled, protocol.ActionRequestExpired:
		decision = protocol.DecisionNeedApproval
	}
	_, err := tx.Exec(`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES(?,?,?,?,?,?,?,?)`,
		at.UTC().Format(time.RFC3339Nano), r.OrgID, r.AgentID, r.ItemID, string(action), string(decision), r.ID, approvalID)
	return err
}

// requestActionForStatus maps a resolution status to its audit action —
// the store emits the event in the same commit that writes the state.
func requestActionForStatus(status protocol.RequestStatus) protocol.ActionKind {
	switch status {
	case protocol.RequestDenied:
		return protocol.ActionRequestDenied
	case protocol.RequestCancelled:
		return protocol.ActionRequestCancelled
	case protocol.RequestExpired:
		return protocol.ActionRequestExpired
	default:
		return protocol.ActionRequestApproved
	}
}

func (s *SQLite) Request(id string) (protocol.ApprovalRequest, error) {
	r, err := s.scanRequest(s.db.QueryRow(`SELECT `+requestCols+` FROM approval_requests WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return protocol.ApprovalRequest{}, ErrNotFound
	}
	return r, err
}

func (s *SQLite) ListRequests(orgID string, status protocol.RequestStatus, now time.Time) ([]protocol.ApprovalRequest, error) {
	q := `SELECT ` + requestCols + ` FROM approval_requests WHERE org_id=? AND status=?`
	args := []any{orgID, string(status)}
	if status == protocol.RequestOpen {
		q += ` AND expires_at > ?`
		args = append(args, now.Unix())
	}
	rows, err := s.db.Query(q+` ORDER BY created_at DESC LIMIT ?`, append(args, maxListResults)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.ApprovalRequest
	for rows.Next() {
		var r protocol.ApprovalRequest
		var created, exp int64
		var resolvedAt sql.NullInt64
		var resolvedBy, approvalID sql.NullString
		if err := rows.Scan(&r.ID, &r.OrgID, &r.AgentID, &r.ItemID, &r.GrantID, &r.Action,
			&r.Status, &created, &exp, &resolvedAt, &resolvedBy, &approvalID); err != nil {
			return nil, err
		}
		r.CreatedAt, r.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(exp, 0).UTC()
		r.ResolvedBy, r.ApprovalID = resolvedBy.String, approvalID.String
		if resolvedAt.Valid {
			at := time.Unix(resolvedAt.Int64, 0).UTC()
			r.ResolvedAt = &at
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) ResolveRequest(id string, status protocol.RequestStatus, humanID, approvalID string, at time.Time) (protocol.ApprovalRequest, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE approval_requests SET status=?, resolved_at=?, resolved_by=?, approval_id=?
		WHERE id=? AND status='open' AND expires_at > ?`,
		string(status), at.Unix(), humanID, sql.NullString{String: approvalID, Valid: approvalID != ""}, id, at.Unix())
	if err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	cur, err := s.scanRequest(tx.QueryRow(`SELECT `+requestCols+` FROM approval_requests WHERE id=?`, id))
	if err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	if n > 0 {
		// Audit in the same commit — the denial never exists without its line.
		if err := auditRequestEventTx(tx, requestActionForStatus(cur.Status), cur, at, approvalID); err != nil {
			return protocol.ApprovalRequest{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return protocol.ApprovalRequest{}, false, err
	}
	if n > 0 {
		s.reqBus.notify(cur.OrgID)
	}
	return cur, n > 0, nil
}

func scanRequestRows(rows *sql.Rows) ([]protocol.ApprovalRequest, error) {
	defer rows.Close()
	var out []protocol.ApprovalRequest
	for rows.Next() {
		var r protocol.ApprovalRequest
		var created, exp int64
		var resolvedAt sql.NullInt64
		var resolvedBy, approvalID sql.NullString
		if err := rows.Scan(&r.ID, &r.OrgID, &r.AgentID, &r.ItemID, &r.GrantID, &r.Action,
			&r.Status, &created, &exp, &resolvedAt, &resolvedBy, &approvalID); err != nil {
			return nil, err
		}
		r.CreatedAt, r.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(exp, 0).UTC()
		r.ResolvedBy, r.ApprovalID = resolvedBy.String, approvalID.String
		if resolvedAt.Valid {
			atv := time.Unix(resolvedAt.Int64, 0).UTC()
			r.ResolvedAt = &atv
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// approveRequestsForGrantTx resolves every open, unexpired ask on a grant —
// a grant-level approval answers all pending asks on it. Runs inside the
// caller's transaction.
func approveRequestsForGrantTx(tx *sql.Tx, grantID, humanID, approvalID string, at time.Time) ([]protocol.ApprovalRequest, error) {
	rows, err := tx.Query(`UPDATE approval_requests SET status='approved', resolved_at=?,
		resolved_by=?, approval_id=? WHERE grant_id=? AND status='open' AND expires_at > ?
		RETURNING `+requestCols, at.Unix(), humanID, approvalID, grantID, at.Unix())
	if err != nil {
		return nil, err
	}
	return scanRequestRows(rows)
}

func putApprovalTx(tx *sql.Tx, a protocol.Approval) error {
	_, err := tx.Exec(`INSERT INTO approvals(grant_id, id, human_id, expires_at) VALUES(?,?,?,?)
		ON CONFLICT(grant_id) DO UPDATE SET id=excluded.id, human_id=excluded.human_id, expires_at=excluded.expires_at`,
		a.GrantID, a.ID, a.HumanID, a.ExpiresAt.Unix())
	return err
}

func (s *SQLite) ApproveRequest(id string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	// The ask must be open AND unexpired AND its grant's edge still live —
	// grant unexpired, agent unrevoked, item not archived. Approving a dead
	// edge would mint a useless approval and a misleading resolution.
	target, err := s.scanRequest(tx.QueryRow(`UPDATE approval_requests SET status='approved',
		resolved_at=?, resolved_by=?, approval_id=? WHERE id=? AND status='open' AND expires_at > ?
		AND EXISTS (SELECT 1 FROM grants g
			JOIN agents ag ON ag.id = g.agent_id
			JOIN items i ON i.id = g.item_id
			WHERE g.id=grant_id AND (g.expires_at IS NULL OR g.expires_at > ?)
			AND ag.revoked_at IS NULL AND NOT i.archived)
		RETURNING `+requestCols, at.Unix(), appr.HumanID, appr.ID, id, at.Unix(), at.Unix()))
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	appr.GrantID = target.GrantID
	if err := putApprovalTx(tx, appr); err != nil {
		return nil, false, err
	}
	sibs, err := approveRequestsForGrantTx(tx, target.GrantID, appr.HumanID, appr.ID, at)
	if err != nil {
		return nil, false, err
	}
	resolved := append([]protocol.ApprovalRequest{target}, sibs...)
	for i := range resolved {
		if err := auditRequestEventTx(tx, protocol.ActionRequestApproved, resolved[i], at, appr.ID); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	s.reqBus.notify(target.OrgID)
	return resolved, true, nil
}

// grantLiveForApprove reports whether a grant's edge is still usable:
// grant unexpired, agent unrevoked, item not archived. Runs in the
// caller's transaction.
func grantLiveForApproveTx(tx *sql.Tx, grantID string, at time.Time) (bool, error) {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM grants g
		JOIN agents ag ON ag.id = g.agent_id
		JOIN items i ON i.id = g.item_id
		WHERE g.id=? AND (g.expires_at IS NULL OR g.expires_at > ?)
		AND ag.revoked_at IS NULL AND NOT i.archived`, grantID, at.Unix()).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLite) ApproveGrant(grantID string, appr protocol.Approval, at time.Time) ([]protocol.ApprovalRequest, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	live, err := grantLiveForApproveTx(tx, grantID, at)
	if err != nil {
		return nil, err
	}
	if !live {
		return nil, ErrGrantNotLive
	}
	appr.GrantID = grantID
	if err := putApprovalTx(tx, appr); err != nil {
		return nil, err
	}
	out, err := approveRequestsForGrantTx(tx, grantID, appr.HumanID, appr.ID, at)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := auditRequestEventTx(tx, protocol.ActionRequestApproved, out[i], at, appr.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		s.reqBus.notify(out[0].OrgID)
	}
	return out, nil
}

// cancelRequestsTx resolves every open ask on the dead edge as cancelled and
// writes each request_cancelled event in the same commit — an ask must never
// vanish without an audit line saying why.
func (s *SQLite) cancelRequests(where string, id string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	raw, err := tx.Query(`UPDATE approval_requests SET status='cancelled', resolved_at=?
		WHERE `+where+`=? AND status='open' RETURNING `+requestCols, at.Unix(), id)
	if err != nil {
		return err
	}
	rows, err := scanRequestRows(raw)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := auditRequestEventTx(tx, protocol.ActionRequestCancelled, rows[i], at, ""); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	orgs := map[string]struct{}{}
	for _, r := range rows {
		orgs[r.OrgID] = struct{}{}
	}
	s.reqBus.notifyOrgs(orgs)
	return nil
}

func (s *SQLite) CancelRequestsForItem(itemID string, at time.Time) error {
	return s.cancelRequests("item_id", itemID, at)
}

func (s *SQLite) CancelRequestsForAgent(agentID string, at time.Time) error {
	return s.cancelRequests("agent_id", agentID, at)
}

func (s *SQLite) ExpireStaleRequests(now time.Time) ([]protocol.ApprovalRequest, error) {
	rows, err := s.db.Query(`UPDATE approval_requests SET status='expired', resolved_at=?
		WHERE status='open' AND expires_at <= ? RETURNING `+requestCols,
		now.Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	out, err := scanRequestRows(rows)
	if err == nil && len(out) > 0 {
		orgs := map[string]struct{}{}
		for _, r := range out {
			orgs[r.OrgID] = struct{}{}
		}
		s.reqBus.notifyOrgs(orgs)
	}
	return out, err
}

func (s *SQLite) AppendAudit(e protocol.AuditEvent) error {
	_, err := s.db.Exec(`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES(?,?,?,?,?,?,?,?)`,
		e.Time.UTC().Format(time.RFC3339Nano), e.OrgID, e.AgentID, e.ItemID, e.Action, e.Decision, e.Reason, e.ApprovalID)
	return err
}

func (s *SQLite) AppendAudits(events []protocol.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	const q = `INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
		VALUES(?,?,?,?,?,?,?,?)`
	for _, e := range events {
		if _, err := tx.Exec(q, e.Time.UTC().Format(time.RFC3339Nano), e.OrgID, e.AgentID, e.ItemID, e.Action, e.Decision, e.Reason, e.ApprovalID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Billing(orgID string) (OrgBilling, error) {
	var ob OrgBilling
	var updated string
	err := s.db.QueryRow(`SELECT org_id, plan, customer_id, updated_at FROM org_billing WHERE org_id = ?`, orgID).
		Scan(&ob.OrgID, &ob.Plan, &ob.CustomerID, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return OrgBilling{OrgID: orgID, Plan: "free"}, nil
	}
	if err != nil {
		return OrgBilling{}, err
	}
	ob.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return OrgBilling{}, err
	}
	return ob, nil
}

func (s *SQLite) SetBilling(ob OrgBilling) error {
	_, err := s.db.Exec(`INSERT INTO org_billing(org_id, plan, customer_id, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(org_id) DO UPDATE SET plan = excluded.plan, customer_id = excluded.customer_id, updated_at = excluded.updated_at`,
		ob.OrgID, ob.Plan, ob.CustomerID, ob.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) OrgByBillingCustomer(customerID string) (string, error) {
	var orgID string
	err := s.db.QueryRow(`SELECT org_id FROM org_billing WHERE customer_id = ?`, customerID).Scan(&orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return orgID, nil
}

func (s *SQLite) ConsumeUse(orgID string, window time.Time, cap int64) (int64, bool, error) {
	var used int64
	err := s.db.QueryRow(`INSERT INTO usage_counters(org_id, window_start, used) VALUES(?,?,1)
		ON CONFLICT(org_id, window_start) DO UPDATE SET used = usage_counters.used + 1
		RETURNING used`,
		orgID, window.UTC().Format(time.RFC3339Nano)).Scan(&used)
	if err != nil {
		return 0, false, err
	}
	return used, cap <= 0 || used <= cap, nil
}

func (s *SQLite) Usage(orgID string, window time.Time) (int64, error) {
	var used int64
	err := s.db.QueryRow(`SELECT used FROM usage_counters WHERE org_id = ? AND window_start = ?`,
		orgID, window.UTC().Format(time.RFC3339Nano)).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

// FlushAuditOutbox: sqlite writes audit rows directly — there is no outbox.
func (s *SQLite) FlushAuditOutbox(int) (int, error) { return 0, nil }

func (s *SQLite) Audit() ([]protocol.AuditEvent, error) {
	rows, err := s.db.Query(`SELECT at, org_id, agent_id, item_id, action, decision, reason, approval_id FROM audit ORDER BY rowid DESC LIMIT ?`, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rev []protocol.AuditEvent
	for rows.Next() {
		var e protocol.AuditEvent
		var at string
		if err := rows.Scan(&at, &e.OrgID, &e.AgentID, &e.ItemID, &e.Action, &e.Decision, &e.Reason, &e.ApprovalID); err != nil {
			return nil, err
		}
		e.Time, _ = time.Parse(time.RFC3339Nano, at)
		rev = append(rev, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]protocol.AuditEvent, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out, nil
}

func (s *SQLite) PutWorkload(w protocol.Workload) error {
	_, err := s.db.Exec(`INSERT INTO workloads(issuer, subject, agent_id, audience)
		VALUES(?,?,?,?)
		ON CONFLICT(issuer, subject) DO UPDATE SET
			agent_id=excluded.agent_id, audience=excluded.audience`,
		w.Issuer, w.Subject, w.AgentID, w.Audience)
	return err
}

func (s *SQLite) Workload(issuer, subject string) (*protocol.Workload, error) {
	var w protocol.Workload
	err := s.db.QueryRow(`SELECT agent_id, issuer, subject, audience FROM workloads WHERE issuer=? AND subject=?`, issuer, subject).
		Scan(&w.AgentID, &w.Issuer, &w.Subject, &w.Audience)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *SQLite) WorkloadsForIssuer(issuer string) ([]protocol.Workload, error) {
	rows, err := s.db.Query(`SELECT agent_id, issuer, subject, audience FROM workloads WHERE issuer=? ORDER BY subject LIMIT ?`, issuer, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Workload
	for rows.Next() {
		var w protocol.Workload
		if err := rows.Scan(&w.AgentID, &w.Issuer, &w.Subject, &w.Audience); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *SQLite) putSessionSQL(sess protocol.Session, secretHash []byte) (string, []any) {
	return `INSERT INTO sessions(id, org_id, agent_id, secret_hash, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			org_id=excluded.org_id,
			agent_id=excluded.agent_id,
			secret_hash=excluded.secret_hash,
			expires_at=excluded.expires_at,
			created_at=excluded.created_at,
			revoked_at=excluded.revoked_at,
			renewed_at=excluded.renewed_at,
			ttl=excluded.ttl,
			max_ttl=excluded.max_ttl,
			max_uses=excluded.max_uses,
			uses=excluded.uses`,
		[]any{
			sess.ID, sess.OrgID, sess.AgentID, secretHash, sess.ExpiresAt.UTC().Unix(),
			sess.CreatedAt.UTC().Unix(), revokedAtString(sess.RevokedAt), revokedAtString(sess.RenewedAt),
			sess.TTL, sess.MaxTTL, sess.MaxUses, sess.Uses,
		}
}

func (s *SQLite) PutSession(sess protocol.Session, secretHash []byte) error {
	q, args := s.putSessionSQL(sess, secretHash)
	_, err := s.db.Exec(q, args...)
	return err
}

func (s *SQLite) scanSession(rows *sql.Rows) (protocol.Session, error) {
	var sess protocol.Session
	var exp, created, ttl, maxttl, maxUses, uses int64
	var rv, rn sql.NullString
	err := rows.Scan(&sess.ID, &sess.OrgID, &sess.AgentID, &exp, &created, &rv, &rn, &ttl, &maxttl, &maxUses, &uses)
	if err != nil {
		return protocol.Session{}, err
	}
	sess.ExpiresAt = time.Unix(exp, 0).UTC()
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.TTL = ttl
	sess.MaxTTL = maxttl
	sess.MaxUses = int(maxUses)
	sess.Uses = int(uses)
	if t, err := parseRevokedAt(rv); err != nil {
		return protocol.Session{}, err
	} else {
		sess.RevokedAt = t
	}
	if t, err := parseRevokedAt(rn); err != nil {
		return protocol.Session{}, err
	} else {
		sess.RenewedAt = t
	}
	return sess, nil
}

func (s *SQLite) SessionByHash(secretHash []byte) (protocol.Session, error) {
	var sess protocol.Session
	var exp, created, ttl, maxttl, maxUses, uses int64
	var rv, rn sql.NullString
	err := s.db.QueryRow(`SELECT id, org_id, agent_id, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses
		FROM sessions WHERE secret_hash=?`, secretHash).
		Scan(&sess.ID, &sess.OrgID, &sess.AgentID, &exp, &created, &rv, &rn, &ttl, &maxttl, &maxUses, &uses)
	if err == sql.ErrNoRows {
		return protocol.Session{}, ErrNotFound
	}
	if err != nil {
		return protocol.Session{}, err
	}
	sess.ExpiresAt = time.Unix(exp, 0).UTC()
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.TTL = ttl
	sess.MaxTTL = maxttl
	sess.MaxUses = int(maxUses)
	sess.Uses = int(uses)
	if t, err := parseRevokedAt(rv); err != nil {
		return protocol.Session{}, err
	} else {
		sess.RevokedAt = t
	}
	if t, err := parseRevokedAt(rn); err != nil {
		return protocol.Session{}, err
	} else {
		sess.RenewedAt = t
	}
	return sess, nil
}

func (s *SQLite) ListSessions() ([]protocol.Session, error) {
	rows, err := s.db.Query(`SELECT id, org_id, agent_id, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses
		FROM sessions ORDER BY expires_at LIMIT ?`, maxListResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Session
	for rows.Next() {
		sess, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *SQLite) SessionByID(id string) (protocol.Session, error) {
	var sess protocol.Session
	var exp, created, ttl, maxttl, maxUses, uses int64
	var rv, rn sql.NullString
	err := s.db.QueryRow(`SELECT id, org_id, agent_id, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses
		FROM sessions WHERE id=?`, id).
		Scan(&sess.ID, &sess.OrgID, &sess.AgentID, &exp, &created, &rv, &rn, &ttl, &maxttl, &maxUses, &uses)
	if err == sql.ErrNoRows {
		return protocol.Session{}, ErrNotFound
	}
	if err != nil {
		return protocol.Session{}, err
	}
	sess.ExpiresAt = time.Unix(exp, 0).UTC()
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.TTL = ttl
	sess.MaxTTL = maxttl
	sess.MaxUses = int(maxUses)
	sess.Uses = int(uses)
	if t, err := parseRevokedAt(rv); err != nil {
		return protocol.Session{}, err
	} else {
		sess.RevokedAt = t
	}
	if t, err := parseRevokedAt(rn); err != nil {
		return protocol.Session{}, err
	} else {
		sess.RenewedAt = t
	}
	return sess, nil
}

func (s *SQLite) RevokeSession(id string, at time.Time) error {
	rv := at.UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`UPDATE sessions SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, rv, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) RenewSession(id string, at time.Time) (protocol.Session, error) {
	sess, err := s.SessionByID(id)
	if err != nil {
		return protocol.Session{}, err
	}
	if sess.RevokedAt != nil {
		return protocol.Session{}, ErrSessionRevoked
	}
	if !sess.ExpiresAt.After(at) {
		return protocol.Session{}, ErrSessionExpired
	}
	maxExpires := sess.CreatedAt.Add(time.Duration(sess.MaxTTL) * time.Second)
	newExpires := sess.ExpiresAt.Add(time.Duration(sess.TTL) * time.Second)
	if newExpires.After(maxExpires) {
		newExpires = maxExpires
	}
	if !newExpires.After(sess.ExpiresAt) {
		newExpires = sess.ExpiresAt
	}
	rn := at.UTC()
	sess.ExpiresAt = newExpires.UTC()
	sess.RenewedAt = &rn
	_, err = s.db.Exec(`UPDATE sessions SET expires_at = ?, renewed_at = ? WHERE id = ?`,
		sess.ExpiresAt.UTC().Unix(), sess.RenewedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return protocol.Session{}, err
	}
	return sess, nil
}

func (s *SQLite) ownerDEK(orgID string, o protocol.Owner) ([]byte, error) {
	return s.km.ownerDEK(context.Background(), s, orgID, o)
}

func (s *SQLite) loadOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner) ([]byte, error) {
	var wrapped []byte
	err := s.db.QueryRow(`SELECT wrapped FROM owner_keys WHERE org_id=? AND owner_kind=? AND owner_id=?`, orgID, o.Kind, o.ID).Scan(&wrapped)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return wrapped, err
}

func (s *SQLite) mintOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner, dek []byte) error {
	sealed, err := crypto.SealEpoch(s.master, dek, ownerWrapAAD(orgID, o))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO owner_keys(org_id, owner_kind, owner_id, wrapped) VALUES(?,?,?,?)
		ON CONFLICT(org_id, owner_kind, owner_id) DO NOTHING`, orgID, o.Kind, o.ID, sealed)
	return err
}

type sqliteTxSource struct {
	tx     *sql.Tx
	master []byte
}

func (ts sqliteTxSource) loadOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner) ([]byte, error) {
	var wrapped []byte
	err := ts.tx.QueryRowContext(ctx, `SELECT wrapped FROM owner_keys WHERE org_id=? AND owner_kind=? AND owner_id=?`, orgID, o.Kind, o.ID).Scan(&wrapped)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return wrapped, err
}

func (ts sqliteTxSource) mintOwnerWrapped(ctx context.Context, orgID string, o protocol.Owner, dek []byte) error {
	sealed, err := crypto.SealEpoch(ts.master, dek, ownerWrapAAD(orgID, o))
	if err != nil {
		return err
	}
	_, err = ts.tx.ExecContext(ctx, `INSERT INTO owner_keys(org_id, owner_kind, owner_id, wrapped) VALUES(?,?,?,?)
		ON CONFLICT(org_id, owner_kind, owner_id) DO NOTHING`, orgID, o.Kind, o.ID, sealed)
	return err
}

// rebuildOwnerKeysOrg adds org_id to owner_keys for vaults written before
// multi-org keys. sqlite cannot ALTER a primary key, so the table is rebuilt;
// every pre-existing row belongs to the one pre-multi-tenant org.
func (s *SQLite) rebuildOwnerKeysOrg() error {
	var schema string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='owner_keys'`).Scan(&schema); err != nil {
		return err
	}
	if strings.Contains(strings.ReplaceAll(schema, " ", ""), `org_id`) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`CREATE TABLE owner_keys_org (
			org_id TEXT NOT NULL,
			owner_kind TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			wrapped BLOB NOT NULL,
			PRIMARY KEY (org_id, owner_kind, owner_id)
		)`,
		`INSERT INTO owner_keys_org(org_id, owner_kind, owner_id, wrapped)
			SELECT '` + protocol.LocalOrgID + `', owner_kind, owner_id, wrapped FROM owner_keys`,
		`DROP TABLE owner_keys`,
		`ALTER TABLE owner_keys_org RENAME TO owner_keys`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// rewrapLegacy moves secrets sealed with master onto the owner DEK.
// Existing vaults stay readable. The grant still does not get a key.
// It is idempotent and tracks completion in schema_version.
func (s *SQLite) rewrapLegacy() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	var committed bool
	defer func() {
		if !committed {
			_ = tx.Rollback()
			s.km.mu.Lock()
			s.km.deks = map[string][]byte{}
			s.km.mu.Unlock()
		}
	}()

	var version int
	err = tx.QueryRow(`SELECT version FROM schema_version WHERE name='rewrap_legacy'`).Scan(&version)
	if err == nil && version >= 1 {
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}

	ts := sqliteTxSource{tx: tx, master: s.master}

	// Rewrap current item secrets. The trailing column is the id bound in the
	// AAD — for items it is the row id itself.
	items, err := tx.Query(`SELECT id, org_id, owner_kind, owner_id, secret, id FROM items`)
	if err != nil {
		return err
	}
	_, resolvedItems, totalItems, err := s.rewrapRows(items, ts, func(id string, blob []byte) error {
		_, err := tx.Exec(`UPDATE items SET secret=? WHERE id=?`, blob, id)
		return err
	})
	if err != nil {
		return err
	}

	// Rewrap historical item versions with their item's owner.
	vers, err := tx.Query(`SELECT v.id, i.org_id, i.owner_kind, i.owner_id, v.secret, v.item_id FROM item_versions v JOIN items i ON v.item_id = i.id`)
	if err != nil {
		return err
	}
	_, resolvedVersions, totalVersions, err := s.rewrapRows(vers, ts, func(id string, blob []byte) error {
		_, err := tx.Exec(`UPDATE item_versions SET secret=? WHERE id=?`, blob, id)
		return err
	})
	if err != nil {
		return err
	}

	// Mark complete only once the vault has at least one item/version and every
	// row in this transaction has been verified owner-sealed or rewrapped. Empty
	// vaults are not marked so a legacy secret copied into the file before the
	// next open will still be rewrapped.
	if totalItems+totalVersions > 0 && resolvedItems == totalItems && resolvedVersions == totalVersions {
		if _, err := tx.Exec(`INSERT INTO schema_version(name, version) VALUES('rewrap_legacy', 1) ON CONFLICT(name) DO UPDATE SET version=excluded.version`); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLite) rewrapRows(rows *sql.Rows, ts sqliteTxSource, update func(id string, blob []byte) error) (bool, int, int, error) {
	defer rows.Close()
	type row struct {
		id     string
		org    string
		owner  protocol.Owner
		blob   []byte
		itemID string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.org, &r.owner.Kind, &r.owner.ID, &r.blob, &r.itemID); err != nil {
			return false, 0, 0, err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return false, 0, 0, err
	}
	if err := rows.Close(); err != nil {
		return false, 0, 0, err
	}

	rewrapped := 0
	resolved := 0
	for _, r := range list {
		plain, err := crypto.Open(s.master, r.blob)
		if err == nil {
			// Master-sealed and key is correct. Create or load the owner DEK and
			// rewrap. ownerDEK only creates when the master key can decrypt,
			// which we just proved.
			dek, err := s.km.ownerDEK(context.Background(), ts, r.org, r.owner)
			if err != nil {
				return false, 0, 0, err
			}
			blob, err := crypto.SealEpoch(dek, plain, itemAAD(r.org, r.itemID))
			if err != nil {
				return false, 0, 0, err
			}
			if err := update(r.id, blob); err != nil {
				return false, 0, 0, err
			}
			rewrapped++
			resolved++
			continue
		}
		if err != crypto.ErrAuth {
			return false, 0, 0, err
		}

		// Master failed. It may be owner-sealed, or the master key may be wrong.
		// Load the wrapped owner key without creating one, and only trust the
		// row if the master key can unwrap it.
		wrapped, err := ts.loadOwnerWrapped(context.Background(), r.org, r.owner)
		if err == ErrNotFound {
			// No owner key and master cannot open; the key is likely wrong or the
			// row is corrupt. Skip without failing so opening with a wrong key
			// still succeeds.
			continue
		}
		if err != nil {
			return false, 0, 0, err
		}
		dek, err := crypto.OpenEpoch(s.master, wrapped, ownerWrapAAD(r.org, r.owner))
		if err == crypto.ErrAuth {
			// Wrong master key. Skip this row.
			continue
		}
		if err != nil {
			return false, 0, 0, err
		}
		if _, err := crypto.OpenEpoch(dek, r.blob, itemAAD(r.org, r.itemID)); err != nil {
			return false, 0, 0, fmt.Errorf("store: %s secret is neither master nor owner sealed: %w", r.id, err)
		}
		resolved++
	}
	return rewrapped > 0, resolved, len(list), nil
}

// SweepSQLite deletes terminally-expired rows older than before: sessions past
// expiry or revoked, and grants/approvals past expiry. Session expires_at is a
// unix epoch integer while revoked_at is RFC3339 text — the encodings match
// the write paths. It takes a bare *sql.DB so callers that never touch
// ciphertext (e.g. `veil sweep`) do not need the master key.
func SweepSQLite(db *sql.DB, before time.Time) (SweepReport, error) {
	var rep SweepReport
	cut := before.UTC()
	// One transaction: request expirations and their request_expired audit
	// rows commit together or not at all — a failed audit insert rolls the
	// expiry back and the next sweep retries, so an expired ask can never
	// exist without its audit line.
	tx, err := db.Begin()
	if err != nil {
		return rep, fmt.Errorf("sweep begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`DELETE FROM sessions WHERE expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)`,
		cut.Unix(), cut.Format(time.RFC3339))
	if err != nil {
		return rep, fmt.Errorf("sweep sessions: %w", err)
	}
	if rep.Sessions, err = res.RowsAffected(); err != nil {
		return rep, err
	}
	if res, err = tx.Exec(`DELETE FROM grants WHERE expires_at IS NOT NULL AND expires_at < ?`, cut.Unix()); err != nil {
		return rep, fmt.Errorf("sweep grants: %w", err)
	}
	if rep.Grants, err = res.RowsAffected(); err != nil {
		return rep, err
	}
	if res, err = tx.Exec(`DELETE FROM approvals WHERE expires_at < ?`, cut.Unix()); err != nil {
		return rep, fmt.Errorf("sweep approvals: %w", err)
	}
	if rep.Approvals, err = res.RowsAffected(); err != nil {
		return rep, err
	}
	// Open asks mark expired at expiry, audited like the postgres sweep —
	// "nobody answered in time" is a security event on every backend.
	now := time.Now().UTC()
	expired, err := tx.Query(`UPDATE approval_requests SET status='expired', resolved_at=?
		WHERE status='open' AND expires_at <= ? RETURNING `+requestCols, now.Unix(), now.Unix())
	if err != nil {
		return rep, fmt.Errorf("sweep requests: %w", err)
	}
	rows, err := scanRequestRows(expired)
	if err != nil {
		return rep, fmt.Errorf("sweep requests: %w", err)
	}
	rep.Requests = int64(len(rows))
	for i := range rows {
		if _, err := tx.Exec(`INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
			VALUES(?,?,?,?,?,?,?,?)`, now.Format(time.RFC3339Nano), rows[i].OrgID, rows[i].AgentID,
			rows[i].ItemID, protocol.ActionRequestExpired, protocol.DecisionNeedApproval, rows[i].ID, ""); err != nil {
			return rep, fmt.Errorf("sweep request audit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return rep, fmt.Errorf("sweep commit: %w", err)
	}
	return rep, nil
}

func (s *SQLite) Sweep(olderThan time.Time) (SweepReport, error) {
	orgs := s.requestOrgs(`status='open' AND expires_at <= ?`, time.Now().Unix())
	rep, err := SweepSQLite(s.db, olderThan)
	if err == nil && rep.Requests > 0 {
		s.reqBus.notifyOrgs(orgs)
	}
	return rep, err
}

// WatchRequests ticks on approval-request changes for orgID — see Store.
func (s *SQLite) WatchRequests(ctx context.Context, orgID string) <-chan struct{} {
	return s.reqBus.watch(ctx, orgID)
}

// requestOrgs lists orgs holding requests matching where — the sweep uses it
// to fan out watch ticks after bulk expiry.
func (s *SQLite) requestOrgs(where string, args ...any) map[string]struct{} {
	orgs := map[string]struct{}{}
	rows, err := s.db.Query(`SELECT DISTINCT org_id FROM approval_requests WHERE `+where, args...)
	if err != nil {
		return orgs
	}
	defer rows.Close()
	for rows.Next() {
		var o string
		if rows.Scan(&o) == nil {
			orgs[o] = struct{}{}
		}
	}
	return orgs
}
