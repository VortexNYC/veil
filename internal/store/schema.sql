CREATE TABLE humans (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL
);

CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    owner_kind TEXT NOT NULL DEFAULT '',
    owner_id TEXT NOT NULL DEFAULT '',
    revoked_at TIMESTAMPTZ
);

CREATE TABLE items (
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
);

CREATE TABLE grants (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    level TEXT NOT NULL,
    actions TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    UNIQUE(agent_id, item_id)
);

CREATE TABLE approvals (
    grant_id TEXT PRIMARY KEY,
    id TEXT NOT NULL,
    human_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    grant_id TEXT NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT,
    approval_id TEXT
);

CREATE UNIQUE INDEX approval_requests_one_open
    ON approval_requests(grant_id, action) WHERE status = 'open';

-- Live-feed fan-out: committed request writes NOTIFY the org so origin
-- replicas can wake SSE watchers. Ticks carry no data — clients refetch.
CREATE OR REPLACE FUNCTION approval_requests_notify() RETURNS trigger
    LANGUAGE plpgsql AS $fn$
    BEGIN
        PERFORM pg_notify('approval_requests', COALESCE(NEW.org_id, OLD.org_id));
        RETURN NULL;
    END $fn$;

DROP TRIGGER IF EXISTS approval_requests_notify ON approval_requests;
CREATE TRIGGER approval_requests_notify AFTER INSERT OR UPDATE OF status
    ON approval_requests FOR EACH ROW EXECUTE FUNCTION approval_requests_notify();

CREATE SEQUENCE audit_id_seq;
CREATE TABLE audit (
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
) PARTITION BY RANGE (at);

-- Durable fallback for audit writes that fail against `audit` (VEIL-10):
-- a relay claims rows with FOR UPDATE SKIP LOCKED and re-lands them.
CREATE TABLE audit_outbox (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at TIMESTAMPTZ NOT NULL,
    org_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    action TEXT NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL,
    approval_id TEXT NOT NULL
);

CREATE TABLE workloads (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    audience TEXT NOT NULL,
    PRIMARY KEY (issuer, subject)
);

CREATE TABLE owner_keys (
    org_id TEXT NOT NULL,
    owner_kind TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    wrapped BYTEA NOT NULL,
    PRIMARY KEY (org_id, owner_kind, owner_id)
);

-- One master key per org, sealed by the deployment KEK. cmk_id is the hook for
-- a per-org external CMK later; NULL means the env KEK wrapped this row.
CREATE TABLE org_keys (
    org_id TEXT PRIMARY KEY,
    wrapped BYTEA NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1,
    cmk_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ
);

-- Per-owner wrapped copy of the org master under owner-held recovery
-- material. KEK-independent escrow: a lost KEK or lost devices recover via
-- the owner's recovery secret, never via stored plaintext.
CREATE TABLE recovery_wraps (
    org_id TEXT NOT NULL,
    owner_kind TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    wrapped BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    used_at TIMESTAMPTZ,
    PRIMARY KEY (org_id, owner_kind, owner_id)
);

CREATE TABLE item_versions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id TEXT NOT NULL,
    at TIMESTAMPTZ NOT NULL,
    secret BYTEA NOT NULL
);

CREATE TABLE sessions (
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
);

-- Billing plane: Vortex is the billing system; these rows are the local
-- projection the Use gate enforces. org_billing.plan flips on signed
-- webhooks; usage_counters is the atomic claim ledger — one row per org per
-- window, claimed under INSERT ... ON CONFLICT.
CREATE TABLE org_billing (
    org_id TEXT PRIMARY KEY,
    plan TEXT NOT NULL DEFAULT 'free',
    customer_id TEXT NOT NULL DEFAULT '',
    billing_account_id TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE usage_counters (
    org_id TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    used BIGINT NOT NULL,
    reported BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, window_start)
);

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA pg_catalog;
CREATE INDEX idx_items_name_trgm ON items USING GIN (name gin_trgm_ops);
CREATE INDEX idx_items_uris_trgm ON items USING GIN (uris gin_trgm_ops);
CREATE INDEX idx_items_tags_trgm ON items USING GIN (tags gin_trgm_ops);
CREATE INDEX idx_items_login_trgm ON items USING GIN (login gin_trgm_ops);
CREATE INDEX idx_items_org_name ON items(org_id, name);
CREATE INDEX idx_items_org_archived_name ON items(org_id, archived, name);
CREATE INDEX idx_grants_item ON grants(item_id);
CREATE INDEX idx_audit_agent_at ON audit(agent_id, at);
CREATE INDEX idx_audit_at ON audit(at);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
CREATE INDEX idx_item_versions_item ON item_versions(item_id, id);
CREATE INDEX idx_workloads_issuer ON workloads(issuer);
