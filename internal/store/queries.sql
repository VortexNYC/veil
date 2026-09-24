-- name: UseAuth :one
SELECT
    a.id AS agent_id,
    a.org_id AS agent_org_id,
    a.owner_kind AS agent_owner_kind,
    a.owner_id AS agent_owner_id,
    a.revoked_at AS agent_revoked_at,
    i.id AS item_id,
    i.org_id AS item_org_id,
    i.name AS item_name,
    i.kind AS item_kind,
    i.owner_kind AS item_owner_kind,
    i.owner_id AS item_owner_id,
    i.uris AS item_uris,
    i.has_totp AS item_has_totp,
    i.tags AS item_tags,
    i.archived AS item_archived,
    i.has_file AS item_has_file,
    i.login AS item_login,
    g.id AS grant_id,
    g.org_id AS grant_org_id,
    g.agent_id AS grant_agent_id,
    g.item_id AS grant_item_id,
    g.level AS grant_level,
    g.actions AS grant_actions,
    g.expires_at AS grant_expires_at,
    ap.id AS approval_id,
    ap.grant_id AS approval_grant_id,
    ap.human_id AS approval_human_id,
    ap.expires_at AS approval_expires_at
FROM (SELECT @agent_id::text AS agent_id, @item_id::text AS item_id, @now::timestamptz AS now) AS v
LEFT JOIN agents a ON a.id = v.agent_id
LEFT JOIN items i ON i.id = v.item_id
LEFT JOIN grants g ON g.agent_id = v.agent_id AND g.item_id = i.id
LEFT JOIN approvals ap ON ap.grant_id = g.id AND ap.expires_at > v.now;

-- name: UseAuthSession :one
SELECT
    s.id AS session_id,
    a.id AS agent_id,
    a.org_id AS agent_org_id,
    a.owner_kind AS agent_owner_kind,
    a.owner_id AS agent_owner_id,
    a.revoked_at AS agent_revoked_at,
    i.id AS item_id,
    i.org_id AS item_org_id,
    i.name AS item_name,
    i.kind AS item_kind,
    i.owner_kind AS item_owner_kind,
    i.owner_id AS item_owner_id,
    i.uris AS item_uris,
    i.has_totp AS item_has_totp,
    i.tags AS item_tags,
    i.archived AS item_archived,
    i.has_file AS item_has_file,
    i.login AS item_login,
    g.id AS grant_id,
    g.org_id AS grant_org_id,
    g.agent_id AS grant_agent_id,
    g.item_id AS grant_item_id,
    g.level AS grant_level,
    g.actions AS grant_actions,
    g.expires_at AS grant_expires_at,
    ap.id AS approval_id,
    ap.grant_id AS approval_grant_id,
    ap.human_id AS approval_human_id,
    ap.expires_at AS approval_expires_at
FROM (SELECT @session_hash::bytea AS session_hash, @item_id::text AS item_id, @now::timestamptz AS now) AS v
LEFT JOIN sessions s ON s.secret_hash = v.session_hash AND s.expires_at > v.now AND s.revoked_at IS NULL AND (s.max_uses = 0 OR s.uses < s.max_uses)
LEFT JOIN agents a ON a.id = s.agent_id
LEFT JOIN items i ON i.id = v.item_id
LEFT JOIN grants g ON g.agent_id = s.agent_id AND g.item_id = i.id
LEFT JOIN approvals ap ON ap.grant_id = g.id AND ap.expires_at > v.now;

-- name: ConsumeSession :one
UPDATE sessions s
SET uses = uses + 1
FROM agents a
WHERE s.secret_hash = @session_hash::bytea
  AND s.agent_id = a.id
  AND a.revoked_at IS NULL
  AND s.expires_at > @now::timestamptz
  AND s.revoked_at IS NULL
  AND (s.max_uses = 0 OR s.uses < s.max_uses)
RETURNING a.id AS agent_id, a.org_id AS agent_org_id, a.owner_kind AS agent_owner_kind, a.owner_id AS agent_owner_id, a.revoked_at AS agent_revoked_at;

-- name: PutSession :exec
INSERT INTO sessions(id, org_id, agent_id, secret_hash, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses)
VALUES(@id::text, @org_id::text, @agent_id::text, @secret_hash::bytea, @expires_at::timestamptz, @created_at::timestamptz, sqlc.narg(revoked_at), sqlc.narg(renewed_at), @ttl::bigint, @max_ttl::bigint, @max_uses::integer, @uses::integer)
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
    uses=excluded.uses;

-- name: SessionByHash :one
SELECT id, org_id, agent_id, secret_hash, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses
FROM sessions WHERE secret_hash = @secret_hash::bytea;

-- name: SessionByID :one
SELECT id, org_id, agent_id, secret_hash, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses
FROM sessions WHERE id = @id::text;

-- name: ListSessions :many
SELECT id, org_id, agent_id, secret_hash, expires_at, created_at, revoked_at, renewed_at, ttl, max_ttl, max_uses, uses
FROM sessions ORDER BY expires_at LIMIT @max_results::bigint;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = COALESCE(revoked_at, @at::timestamptz) WHERE id = @id::text;

-- name: RenewSession :exec
UPDATE sessions SET expires_at = @expires_at::timestamptz, renewed_at = @renewed_at::timestamptz WHERE id = @id::text;

-- name: ItemOwner :one
SELECT org_id, owner_kind, owner_id FROM items WHERE id = @id::text;

-- name: SnapshotItem :exec
INSERT INTO item_versions(item_id, at, secret)
SELECT @item_id::text, @at::timestamptz, secret FROM items WHERE id = @item_id::text;

-- name: PutItem :execrows
INSERT INTO items(id, org_id, name, kind, owner_kind, owner_id, uris, secret, has_totp, tags, archived, has_file, login)
VALUES(@id::text, @org_id::text, @name::text, @kind::text, @owner_kind::text, @owner_id::text, @uris::text, @secret::bytea, @has_totp::bool, @tags::text, @archived::bool, @has_file::bool, @login::text)
ON CONFLICT(id) DO UPDATE SET
    org_id=excluded.org_id, name=excluded.name, kind=excluded.kind,
    owner_kind=excluded.owner_kind, owner_id=excluded.owner_id,
    uris=excluded.uris, secret=excluded.secret, has_totp=excluded.has_totp,
    tags=excluded.tags, archived=excluded.archived, has_file=excluded.has_file,
    login=excluded.login
WHERE items.owner_kind=excluded.owner_kind AND items.owner_id=excluded.owner_id
    AND items.org_id=excluded.org_id;

-- name: ItemByID :one
SELECT id, org_id, name, kind, owner_kind, owner_id, uris, has_totp, tags, archived, has_file, login
FROM items WHERE id = @id::text;

-- name: ItemByName :one
SELECT id, org_id, name, kind, owner_kind, owner_id, uris, has_totp, tags, archived, has_file, login
FROM items WHERE org_id = @org_id::text AND name = @name::text;

-- name: ListItems :many
SELECT id, org_id, name, kind, owner_kind, owner_id, uris, has_totp, tags, archived, has_file, login
FROM items WHERE archived = FALSE ORDER BY name LIMIT @max_results::bigint;

-- name: ArchiveItem :execrows
UPDATE items SET archived = TRUE WHERE id = @id::text;

-- name: DeleteItemVersions :exec
DELETE FROM item_versions WHERE item_id = @item_id::text;

-- name: DeleteItemGrants :exec
DELETE FROM grants WHERE item_id = @item_id::text;

-- name: DeleteItem :execrows
DELETE FROM items WHERE id = @id::text;

-- name: ItemVersions :many
SELECT id, item_id, at FROM item_versions WHERE item_id = @item_id::text ORDER BY id DESC LIMIT @max_results::bigint;

-- name: ItemVersionSecret :one
SELECT secret FROM item_versions WHERE id = @id::bigint AND item_id = @item_id::text;

-- name: RestoreItemSecret :exec
UPDATE items SET secret = @secret::bytea WHERE id = @id::text;

-- name: ItemSecretOwner :one
SELECT secret, org_id, owner_kind, owner_id FROM items WHERE id = @id::text;

-- name: PutGrant :exec
INSERT INTO grants(id, org_id, agent_id, item_id, level, actions, expires_at)
VALUES(@id::text, @org_id::text, @agent_id::text, @item_id::text, @level::text, @actions::text, sqlc.narg(expires_at))
ON CONFLICT(agent_id, item_id) DO UPDATE SET
    id=excluded.id, org_id=excluded.org_id, level=excluded.level,
    actions=excluded.actions, expires_at=excluded.expires_at;

-- name: GrantByID :one
SELECT id, org_id, agent_id, item_id, level, actions, expires_at
FROM grants WHERE id = @id::text;

-- name: GrantFor :one
SELECT id, org_id, agent_id, item_id, level, actions, expires_at
FROM grants WHERE agent_id = @agent_id::text AND item_id = @item_id::text;

-- name: ListGrants :many
SELECT id, org_id, agent_id, item_id, level, actions, expires_at
FROM grants ORDER BY id LIMIT @max_results::bigint;

-- name: PutApproval :exec
INSERT INTO approvals(grant_id, id, human_id, expires_at)
VALUES(@grant_id::text, @id::text, @human_id::text, @expires_at::timestamptz)
ON CONFLICT(grant_id) DO UPDATE SET
    id=excluded.id, human_id=excluded.human_id, expires_at=excluded.expires_at;

-- name: ApprovalByGrant :one
SELECT grant_id, id, human_id, expires_at FROM approvals WHERE grant_id = @grant_id::text;

-- name: ExpireOpenRequest :one
UPDATE approval_requests SET status = 'expired', resolved_at = @now::timestamptz
WHERE grant_id = @grant_id::text AND action = @action::text
    AND status = 'open' AND expires_at <= @now::timestamptz
RETURNING id;

-- name: InsertRequest :one
INSERT INTO approval_requests(id, org_id, agent_id, item_id, grant_id, action,
    status, created_at, expires_at)
VALUES(@id::text, @org_id::text, @agent_id::text, @item_id::text, @grant_id::text,
    @action::text, 'open', @created_at::timestamptz, @expires_at::timestamptz)
ON CONFLICT(grant_id, action) WHERE status = 'open' DO NOTHING
RETURNING id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id;

-- name: OpenRequestByGrant :one
SELECT id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id
FROM approval_requests
WHERE grant_id = @grant_id::text AND action = @action::text AND status = 'open';

-- name: RequestByID :one
SELECT id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id
FROM approval_requests WHERE id = @id::text;

-- name: ListOpenRequests :many
SELECT id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id
FROM approval_requests
WHERE org_id = @org_id::text AND status = 'open' AND expires_at > @now::timestamptz
ORDER BY created_at DESC LIMIT @max_results::bigint;

-- name: ListRequestsByStatus :many
SELECT id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id
FROM approval_requests
WHERE org_id = @org_id::text AND status = @status::text
ORDER BY created_at DESC LIMIT @max_results::bigint;

-- name: ResolveOpenRequest :one
UPDATE approval_requests
SET status = @status::text, resolved_at = @at::timestamptz,
    resolved_by = @human_id::text, approval_id = sqlc.narg('approval_id')::text
WHERE id = @id::text AND status = 'open' AND expires_at > @at::timestamptz
RETURNING id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id;

-- name: ApproveOpenRequest :one
-- Request-scoped approve: the ask must be open AND unexpired AND its grant
-- still live — grant unexpired, agent unrevoked, item not archived.
-- Approving a dead edge would mint a useless approval and a misleading
-- 'approved' resolution.
UPDATE approval_requests
SET status = 'approved', resolved_at = @at::timestamptz,
    resolved_by = @human_id::text, approval_id = @approval_id::text
WHERE id = @id::text AND status = 'open' AND expires_at > @at::timestamptz
    AND EXISTS (SELECT 1 FROM grants g
        JOIN agents ag ON ag.id = g.agent_id
        JOIN items i ON i.id = g.item_id
        WHERE g.id = grant_id
        AND (g.expires_at IS NULL OR g.expires_at > @at::timestamptz)
        AND ag.revoked_at IS NULL AND NOT i.archived)
RETURNING id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id;

-- name: LiveGrantForApprove :one
-- Grant-scoped approve (`veil approve GRANT_ID`) must fail on a dead edge —
-- expired grant, revoked agent, or archived item — rather than mint an
-- approval that can never be used.
SELECT g.id FROM grants g
JOIN agents ag ON ag.id = g.agent_id
JOIN items i ON i.id = g.item_id
WHERE g.id = @grant_id::text
    AND (g.expires_at IS NULL OR g.expires_at > @at::timestamptz)
    AND ag.revoked_at IS NULL AND NOT i.archived;

-- name: ApproveRequestsForGrant :many
UPDATE approval_requests
SET status = 'approved', resolved_at = @at::timestamptz,
    resolved_by = @human_id::text, approval_id = @approval_id::text
WHERE grant_id = @grant_id::text AND status = 'open' AND expires_at > @at::timestamptz
RETURNING id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id;

-- name: CancelRequestsForItem :exec
UPDATE approval_requests SET status = 'cancelled', resolved_at = @at::timestamptz
WHERE item_id = @item_id::text AND status = 'open';

-- name: CancelRequestsForAgent :exec
UPDATE approval_requests SET status = 'cancelled', resolved_at = @at::timestamptz
WHERE agent_id = @agent_id::text AND status = 'open';

-- name: ExpireStaleRequests :many
-- Returns the rows it expired so the sweep can audit each request_expired —
-- "nobody answered in time" is a security event, not hygiene.
UPDATE approval_requests SET status = 'expired', resolved_at = @at::timestamptz
WHERE status = 'open' AND expires_at <= @at::timestamptz
RETURNING id, org_id, agent_id, item_id, grant_id, action, status, created_at,
    expires_at, resolved_at, resolved_by, approval_id;

-- name: OwnerWrapped :one
SELECT wrapped FROM owner_keys
WHERE org_id = @org_id::text AND owner_kind = @owner_kind::text AND owner_id = @owner_id::text;

-- name: PutOwnerWrapped :exec
INSERT INTO owner_keys(org_id, owner_kind, owner_id, wrapped)
VALUES(@org_id::text, @owner_kind::text, @owner_id::text, @wrapped::bytea)
ON CONFLICT(org_id, owner_kind, owner_id) DO NOTHING;

-- name: OrgKey :one
SELECT org_id, wrapped, key_version, cmk_id, created_at, rotated_at
FROM org_keys WHERE org_id = @org_id::text;

-- name: OrgKeyForUpdate :one
SELECT org_id, wrapped, key_version, cmk_id, created_at, rotated_at
FROM org_keys WHERE org_id = @org_id::text FOR UPDATE;

-- name: OrgKeyForShare :one
SELECT org_id, wrapped, key_version, cmk_id, created_at, rotated_at
FROM org_keys WHERE org_id = @org_id::text FOR SHARE;

-- name: PutOrgKey :execrows
INSERT INTO org_keys(org_id, wrapped, key_version, cmk_id, created_at)
VALUES(@org_id::text, @wrapped::bytea, @key_version::integer, sqlc.narg(cmk_id), @created_at::timestamptz)
ON CONFLICT(org_id) DO NOTHING;

-- name: BumpOrgKey :execrows
UPDATE org_keys SET wrapped = @wrapped::bytea, key_version = key_version + 1, rotated_at = @rotated_at::timestamptz
WHERE org_id = @org_id::text AND key_version = @key_version::integer;

-- name: ListOrgKeys :many
SELECT org_id, wrapped, key_version, cmk_id, created_at, rotated_at FROM org_keys;

-- name: RewrapOrgKey :execrows
UPDATE org_keys SET wrapped = @wrapped::bytea WHERE org_id = @org_id::text;

-- name: ListOwnerKeysForOrg :many
SELECT org_id, owner_kind, owner_id, wrapped FROM owner_keys WHERE org_id = @org_id::text;

-- name: RewrapOwnerKey :execrows
UPDATE owner_keys SET wrapped = @wrapped::bytea
WHERE org_id = @org_id::text AND owner_kind = @owner_kind::text AND owner_id = @owner_id::text;

-- name: PutRecoveryWrap :exec
INSERT INTO recovery_wraps(org_id, owner_kind, owner_id, wrapped, created_at, expires_at)
VALUES(@org_id::text, @owner_kind::text, @owner_id::text, @wrapped::bytea, @created_at::timestamptz, sqlc.narg(expires_at))
ON CONFLICT(org_id, owner_kind, owner_id) DO UPDATE SET
  wrapped = EXCLUDED.wrapped, created_at = EXCLUDED.created_at,
  expires_at = EXCLUDED.expires_at, used_at = NULL;

-- name: RecoveryWrap :one
SELECT org_id, owner_kind, owner_id, wrapped, created_at, expires_at, used_at
FROM recovery_wraps
WHERE org_id = @org_id::text AND owner_kind = @owner_kind::text AND owner_id = @owner_id::text
FOR UPDATE;

-- name: ConsumeRecoveryWrap :execrows
UPDATE recovery_wraps SET used_at = @used_at::timestamptz
WHERE org_id = @org_id::text AND owner_kind = @owner_kind::text AND owner_id = @owner_id::text
  AND used_at IS NULL;

-- name: DeleteRecoveryWrapsForOrg :exec
DELETE FROM recovery_wraps WHERE org_id = @org_id::text;

-- name: PutAgent :exec
INSERT INTO agents(id, org_id, owner_kind, owner_id, revoked_at)
VALUES(@id::text, @org_id::text, @owner_kind::text, @owner_id::text, sqlc.narg(revoked_at))
ON CONFLICT(id) DO UPDATE SET
    revoked_at=COALESCE(agents.revoked_at, excluded.revoked_at);

-- name: AgentByID :one
SELECT id, org_id, owner_kind, owner_id, revoked_at FROM agents WHERE id = @id::text;

-- name: ListAgents :many
SELECT id, org_id, owner_kind, owner_id, revoked_at FROM agents ORDER BY id LIMIT @max_results::bigint;

-- name: RevokeAgent :execrows
UPDATE agents SET revoked_at = COALESCE(revoked_at, @at::timestamptz) WHERE id = @id::text;

-- name: PutHuman :exec
INSERT INTO humans(id, org_id) VALUES(@id::text, @org_id::text)
ON CONFLICT(id) DO UPDATE SET org_id=excluded.org_id;

-- name: PlantHuman :execrows
INSERT INTO humans(id, org_id) VALUES(@id::text, @org_id::text)
ON CONFLICT(id) DO NOTHING;

-- name: HumanByID :one
SELECT id, org_id FROM humans WHERE id = @id::text;

-- name: ListHumans :many
SELECT id, org_id FROM humans ORDER BY id LIMIT @max_results::bigint;

-- name: PutWorkload :exec
INSERT INTO workloads(issuer, subject, agent_id, audience)
VALUES(@issuer::text, @subject::text, @agent_id::text, @audience::text)
ON CONFLICT(issuer, subject) DO UPDATE SET
    agent_id=excluded.agent_id, audience=excluded.audience;

-- name: WorkloadByKey :one
SELECT issuer, subject, agent_id, audience FROM workloads WHERE issuer = @issuer::text AND subject = @subject::text;

-- name: WorkloadsForIssuer :many
SELECT issuer, subject, agent_id, audience FROM workloads WHERE issuer = @issuer::text ORDER BY subject LIMIT @max_results::bigint;

-- name: InsertAudit :exec
INSERT INTO audit(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
VALUES(@at::timestamptz, @org_id::text, @agent_id::text, @item_id::text, @action::text, @decision::text, @reason::text, @approval_id::text);

-- name: ListAudit :many
-- Outbox rows union in: claim-delete and audit-insert commit in one tx, so an
-- event can never appear in both tables at once — queued events are visible
-- immediately without double-counting.
SELECT id, at, org_id, agent_id, item_id, action, decision, reason, approval_id
FROM (
    SELECT id, at, org_id, agent_id, item_id, action, decision, reason, approval_id FROM audit
    UNION ALL
    SELECT id, at, org_id, agent_id, item_id, action, decision, reason, approval_id FROM audit_outbox
) ev ORDER BY at DESC, id DESC LIMIT @max_results::bigint;

-- name: InsertAuditOutbox :exec
INSERT INTO audit_outbox(at, org_id, agent_id, item_id, action, decision, reason, approval_id)
VALUES(@at::timestamptz, @org_id::text, @agent_id::text, @item_id::text, @action::text, @decision::text, @reason::text, @approval_id::text);

-- name: ClaimAuditOutbox :many
DELETE FROM audit_outbox WHERE id IN (
    SELECT id FROM audit_outbox ORDER BY id LIMIT @max_results::bigint FOR UPDATE SKIP LOCKED
) RETURNING id, at, org_id, agent_id, item_id, action, decision, reason, approval_id;

-- name: SweepExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at < @before::timestamptz
   OR (revoked_at IS NOT NULL AND revoked_at < @before::timestamptz);

-- name: SweepExpiredGrants :execrows
DELETE FROM grants WHERE expires_at IS NOT NULL AND expires_at < @before::timestamptz;

-- name: SweepExpiredApprovals :execrows
DELETE FROM approvals WHERE expires_at < @before::timestamptz;
