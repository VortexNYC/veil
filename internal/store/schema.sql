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

CREATE TABLE audit (
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

CREATE INDEX idx_items_org_name ON items(org_id, name);
CREATE INDEX idx_items_org_archived_name ON items(org_id, archived, name);
CREATE INDEX idx_grants_item ON grants(item_id);
CREATE INDEX idx_audit_agent_at ON audit(agent_id, at);
CREATE INDEX idx_audit_at ON audit(at);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
CREATE INDEX idx_item_versions_item ON item_versions(item_id, id);
CREATE INDEX idx_workloads_issuer ON workloads(issuer);
