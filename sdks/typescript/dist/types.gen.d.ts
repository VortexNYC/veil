export type ClientOptions = {
    baseUrl: 'https://veil.nyc' | (string & {});
};
export type Item = {
    id: string;
    org_id: string;
    name: string;
    kind: 'api_key' | 'oauth' | 'ssh' | 'file' | 'passkey' | 'card' | 'identity' | 'secure_note' | 'honey';
    owner: Owner;
    uris: Array<string>;
    tags?: Array<string>;
    archived?: boolean;
    has_totp?: boolean;
    has_file?: boolean;
    /**
     * Fill username. Metadata. Not a secret. Empty if unset.
     */
    login?: string;
    /**
     * Stays visible while the org's Travel Mode is on (VTX-788). Metadata, not ACL.
     */
    travel_safe?: boolean;
};
export type Owner = {
    kind: string;
    id: string;
};
export type ItemsResponse = {
    items: Array<Item>;
};
export type UseRequest = {
    /**
     * Granted item name
     */
    item: string;
    /**
     * Absolute URL. Host must match the item.
     */
    url: string;
    method?: string;
    /**
     * Extra request headers. Never the vault secret.
     */
    headers?: {
        [key: string]: string;
    };
    /**
     * Request body. Never the vault secret. Use --body-file on the CLI.
     */
    body?: string;
    /**
     * Base64 request body for binary payloads. Takes precedence over body. Never the vault secret.
     */
    body_b64?: string;
};
export type UseResponse = {
    decision: 'allow' | 'deny' | 'need_approval';
    reason?: string;
    approval_id?: string;
    /**
     * The filed approval request when decision is need_approval. Owners resolve it via /v1/requests/{id}.
     */
    request_id?: string;
    /**
     * When the filed ask dies unanswered. Agents may keep retrying until then.
     */
    request_expires_at?: string;
    status?: number;
    /**
     * Upstream response headers, including Content-Type and Content-Encoding.
     */
    headers?: {
        [key: string]: Array<string>;
    };
    /**
     * Upstream body with vault secrets scrubbed. Present when the body is valid UTF-8.
     */
    body?: string;
    /**
     * Base64 upstream body with vault secrets scrubbed. Present when the body is not valid UTF-8 (gzip, images, protobuf).
     */
    body_b64?: string;
};
export type AuditEvent = {
    time: string;
    org_id: string;
    agent_id: string;
    item_id: string;
    action: string;
    decision: 'allow' | 'deny' | 'need_approval';
    reason?: string;
    approval_id?: string;
};
export type EventsResponse = {
    events: Array<AuditEvent>;
};
export type CreateItemRequest = {
    name: string;
    uri?: string;
    uris?: Array<string>;
    tags?: Array<string>;
    kind?: 'api_key' | 'oauth' | 'ssh' | 'file' | 'passkey' | 'card' | 'identity' | 'secure_note' | 'honey';
    /**
     * Vault material. Request only. Never returned.
     */
    secret?: string;
    /**
     * TOTP seed. Request only. Never returned.
     */
    totp_seed?: string;
    /**
     * Fill username. Metadata on the item. Also sealed in the envelope. Not a secret.
     */
    login?: string;
    card?: CardFields;
    identity?: IdentityFields;
    wif?: WifItemFields;
    /**
     * Stays visible while the org's Travel Mode is on.
     */
    travel_safe?: boolean;
};
export type UpdateItemRequest = {
    /**
     * Add this autofill host. Does not drop existing hosts.
     */
    uri?: string;
    /**
     * Replace autofill hosts with this list.
     */
    uris?: Array<string>;
    tags?: Array<string>;
    /**
     * Fill username. Metadata on the item. Also sealed in the envelope.
     */
    login?: string;
    /**
     * Rotate the sealed secret — a changed password. TOTP seed, login, and passkey survive. Blank refuses; absent leaves the secret alone.
     */
    secret?: string;
    /**
     * Set the travel_safe flag. Absent leaves it untouched.
     */
    travel_safe?: boolean;
};
export type CreateGrantRequest = {
    /**
     * Agent id. XOR human/group.
     */
    agent?: string;
    /**
     * Kratos identity id. Same Grant.agent_id. Not email. XOR agent/group.
     */
    human?: string;
    item: string;
    level: 'level1' | 'level2';
    /**
     * Go duration. Empty is forever.
     */
    expires?: string;
    /**
     * Org group name — the shared vault. XOR agent/human.
     */
    group?: string;
};
export type Grant = {
    id: string;
    org_id: string;
    agent_id: string;
    item_id: string;
    level: string;
    expires_at?: string;
    /**
     * Subject kind. agent_id holds the subject id — the group id when subject_kind is group.
     */
    subject_kind: 'agent' | 'human' | 'group';
};
export type GrantsResponse = {
    grants: Array<Grant>;
};
export type Agent = {
    kind: string;
    id: string;
    org_id: string;
    owner?: Owner;
    /**
     * Set when the agent is revoked. Record stays for audit.
     */
    revoked_at?: string;
};
export type AgentsResponse = {
    agents: Array<Agent>;
};
export type CreateAgentRequest = {
    name: string;
};
/**
 * Sandbox Use lease metadata. Never the token.
 */
export type Session = {
    id: string;
    org_id: string;
    agent_id: string;
    expires_at: string;
    created_at: string;
    revoked_at?: string;
    renewed_at?: string;
    /**
     * Initial lease duration in seconds.
     */
    ttl: number;
    /**
     * Maximum cumulative lifetime in seconds.
     */
    max_ttl: number;
    /**
     * Maximum successful Use calls. 0 = unlimited.
     */
    max_uses: number;
    /**
     * Use calls that passed authorization and reached upstream consumption.
     */
    uses: number;
};
export type SessionsResponse = {
    sessions: Array<Session>;
};
export type CreateSessionRequest = {
    agent: string;
    /**
     * Go duration. Default 15m. Max 1h.
     */
    ttl?: string;
    /**
     * Maximum successful Use calls. 0 = unlimited.
     */
    max_uses?: number;
};
/**
 * Token is create-only. Never list. Never MCP.
 */
export type CreateSessionResponse = {
    id: string;
    org_id: string;
    agent_id: string;
    expires_at: string;
    created_at: string;
    revoked_at?: string;
    renewed_at?: string;
    ttl: number;
    max_ttl: number;
    max_uses: number;
    /**
     * Use calls that passed authorization and reached upstream consumption.
     */
    uses: number;
    token: string;
};
/**
 * Request only. Never returned. Never MCP.
 */
export type CardFields = {
    number?: string;
    exp_month?: string;
    exp_year?: string;
    cvv?: string;
    holder?: string;
};
/**
 * Request only. Never returned. Never MCP.
 */
export type IdentityFields = {
    given_name?: string;
    family_name?: string;
    address?: string;
    city?: string;
    region?: string;
    postal?: string;
    country?: string;
    phone?: string;
    email?: string;
};
export type ImportResponse = {
    names: Array<string>;
    count: number;
};
export type ProvisionResponse = {
    subject: string;
    org_id: string;
};
export type InviteRequest = {
    email: string;
};
export type InviteResponse = {
    identity_id: string;
    emailed: boolean;
    /**
     * Only present when mail delivery is not configured (local dev).
     */
    recovery_url?: string;
};
export type ApprovalRequest = {
    id: string;
    agent_id: string;
    item_id: string;
    grant_id: string;
    action: string;
    status: 'open' | 'approved' | 'denied' | 'expired' | 'cancelled';
    created_at: string;
    expires_at: string;
    resolved_at?: string;
    /**
     * The Kratos identity of the owner who answered — first write wins.
     */
    resolved_by?: string;
    /**
     * The grant approval created by an approve resolution.
     */
    approval_id?: string;
};
export type RequestsResponse = {
    requests: Array<ApprovalRequest>;
};
export type ApproveRequestBody = {
    /**
     * Go duration for the grant approval lifetime (default 15m).
     */
    ttl?: string;
};
export type BillingView = {
    /**
     * Plan as enforced by the Use gate
     */
    plan: 'free' | 'active';
    /**
     * Uses consumed in the current window
     */
    used: number;
    /**
     * Free allowance per window; null when unlimited (metering off or paid plan)
     */
    included: number | null;
    window_start: string;
    window_end: string;
    /**
     * Vortex checkout/portal link for capped owners
     */
    upgrade_url?: string;
};
export type AuditFeedEvent = {
    /**
     * Committed row id — monotonically increasing per append; the feed cursor.
     */
    id: number;
    time: string;
    org_id: string;
    agent_id: string;
    item_id: string;
    action: string;
    decision: 'allow' | 'deny' | 'need_approval';
    reason?: string;
    approval_id?: string;
};
export type AuditFeedResponse = {
    events: Array<AuditFeedEvent>;
    /**
     * Cursor for the next page — pass as ?after=. Equals the last event id, or the requested cursor when the page is empty.
     */
    next_after: number;
};
export type BillingCheckout = {
    /**
     * Fresh hosted Vortex checkout URL for this org — navigate immediately, single-use
     */
    checkout_url: string;
};
export type VaultReport = {
    items: number;
    weak: number;
    reused: number;
    pwned: number;
    hibp: 'off' | 'ok' | 'error';
    findings: Array<VaultReportFinding>;
};
export type VaultReportFinding = {
    item_id: string;
    name: string;
    kind: string;
    uri?: string;
    weak?: Array<string>;
    reused?: number;
    pwned: number;
};
export type Group = {
    id: string;
    org_id: string;
    name: string;
};
export type CreateGroupRequest = {
    name: string;
};
export type GroupsResponse = {
    groups: Array<Group>;
};
export type GroupMember = {
    group_id: string;
    member_kind: 'agent' | 'human';
    member_id: string;
};
export type GroupMembersResponse = {
    members: Array<GroupMember>;
};
export type AddGroupMemberRequest = {
    member_kind: 'agent' | 'human';
    /**
     * Agent id, or Kratos identity id for humans.
     */
    member_id: string;
};
export type DeviceRegisterRequest = {
    /**
     * push platform
     */
    platform: 'apns' | 'fcm';
    /**
     * device push token
     */
    token: string;
    /**
     * device label
     */
    label?: string;
};
export type BindWorkloadRequest = {
    /**
     * OIDC issuer URL
     */
    issuer: string;
    /**
     * expected token sub claim
     */
    subject: string;
    /**
     * expected token aud claim
     */
    audience: string;
    /**
     * optional OidcPolicy claim rules (every key must match at least one allowed value)
     */
    claims?: {
        [key: string]: Array<string>;
    };
};
export type WorkloadView = {
    agent_id: string;
    issuer: string;
    subject: string;
    audience: string;
    /**
     * optional OidcPolicy claim rules (every key must match at least one allowed value)
     */
    claims?: {
        [key: string]: Array<string>;
    };
};
export type VerifyIssuerRequest = {
    /**
     * OIDC issuer URL to dry-run
     */
    issuer: string;
};
export type VerifyIssuerResponse = {
    code: 'ok' | 'invalid-issuer' | 'discovery-failed';
    issuer: string;
    message: string;
};
export type WorkloadsResponse = {
    workloads: Array<WorkloadView>;
};
export type WifItemFields = {
    token_url: string;
    assertion_token_url: string;
    assertion_client_id: string;
    assertion_secret: string;
    assertion_audience?: string;
    federation_rule_id: string;
    organization_id?: string;
    service_account_id?: string;
    workspace_id?: string;
};
export type RotateItemRequest = {
    /**
     * separate vault item holding the provider management credential
     */
    management_item?: string;
    /**
     * auto-rotation interval (e.g. 24h)
     */
    interval?: string;
    /**
     * replacement credential for assisted rotation
     */
    secret?: string;
};
export type RotateProvidersResponse = {
    zero_touch_hosts: Array<string>;
    assisted: boolean;
};
export type VerifyItemResponse = {
    item: string;
    verified: boolean;
    mode: 'wif_ok' | 'oauth_ok' | 'provider_ok' | 'sealed_ok';
};
export type RotateHook = {
    /**
     * consumer label for audit, e.g. ci-resecrets
     */
    name: string;
    /**
     * hook type; only webhook exists
     */
    type: 'webhook';
    /**
     * consumer endpoint; a capability token may ride in path/query — sealed, never in audit
     */
    url: string;
};
export type RotateHooksRequest = {
    hooks: Array<RotateHook>;
};
export type RotateHooksResponse = {
    hooks: Array<RotateHook>;
};
export type RotateHookResult = {
    hook?: string;
    ok?: boolean;
    status?: number;
    error?: string;
};
export type RotateHooksFireResponse = {
    results: Array<RotateHookResult>;
};
/**
 * The member's own org-key share state (VEIL-78). pubkey and wrapped_master are ciphertext material only — the 32-byte X25519 public key (base64) and the 80-byte box.SealAnonymous blob (base64). The private key and the plaintext org master never leave the client/store.
 */
export type RecoveryShare = {
    /**
     * Whether the member has an org_key_shares row.
     */
    enrolled: boolean;
    /**
     * Member's 32-byte X25519 public key, base64.
     */
    pubkey?: string;
    /**
     * 80-byte box.SealAnonymous ciphertext of the org master, base64.
     */
    wrapped_master?: string;
    /**
     * Share wrap epoch; bumps on every completed recovery.
     */
    wrap_pubkey_epoch: number;
    /**
     * Org master version the share wraps.
     */
    org_key_version: number;
    created_at?: string;
    updated_at?: string;
};
/**
 * One initiator -> confirmer recovery ceremony row (VEIL-78). Owner callers also see human_email, current_epoch, and confirmer_wrapped_master (the caller's own share blob for the proof_hmac fast path); the initiator/target member sees the bare row.
 */
export type RecoveryRequest = {
    id: string;
    org_id: string;
    /**
     * The member being recovered.
     */
    human_id: string;
    /**
     * Who filed the request — the member (self) or an owner.
     */
    initiated_by: string;
    /**
     * Staged 32-byte X25519 public key, base64.
     */
    new_pubkey: string;
    /**
     * On completion, wipe the member's Kratos totp/webauthn and mail a re-enrollment link to their address.
     */
    reset_mfa: boolean;
    status: 'open' | 'completed' | 'denied' | 'expired' | 'cancelled';
    created_at: string;
    /**
     * Immediate for owner-confirmed paths; created_at+24h for sole-owner self-recovery.
     */
    unlock_at: string;
    expires_at: string;
    resolved_at?: string;
    confirmed_by?: string;
    /**
     * The member's new wrap_pubkey_epoch after completion.
     */
    result_epoch?: number;
    /**
     * Owner view only — the member's resolved Kratos address.
     */
    human_email?: string;
    /**
     * Owner view only — the member's current wrap_pubkey_epoch.
     */
    current_epoch: number;
    /**
     * Owner view only — the caller's own wrapped share (80B base64) for computing proof_hmac locally.
     */
    confirmer_wrapped_master?: string;
};
export type RecoveriesResponse = {
    requests: Array<RecoveryRequest>;
};
export type EnrollRequest = {
    /**
     * Member's 32-byte X25519 public key, base64 or hex.
     */
    pubkey: string;
};
export type StartRecoveryRequest = {
    /**
     * Omit for self-recovery; set for owner-initiated recovery of a member.
     */
    human_id?: string;
    /**
     * Staged 32-byte X25519 public key, base64 or hex. Must not match any enrolled share pubkey.
     */
    new_pubkey: string;
    /**
     * Also wipe the member's Kratos second factors on completion.
     */
    reset_mfa?: boolean;
};
export type CompleteRecoveryRequest = {
    /**
     * hex(HMAC-SHA256(org master, request id)) — the sole-owner fast path that bypasses the 24h timelock.
     */
    proof_hmac?: string;
};
export type StartRecoveryResponse = {
    request: RecoveryRequest;
    /**
     * false means an open request already existed — the live row is returned and notifications were suppressed.
     */
    created: boolean;
};
export type ProvisionRequest = {
    /**
     * Optional 32-byte X25519 public key (base64 or hex) to enroll the member's recovery share at signup.
     */
    recovery_pubkey?: string;
};
export type TravelModeRequest = {
    enabled: boolean;
};
export type TravelModeView = {
    enabled: boolean;
};
export type CreateShareRequest = {
    /**
     * Label for the link (metadata only).
     */
    name?: string;
    /**
     * Base64 of the client-sealed envelope (nacl secretbox). The origin never sees plaintext or the key.
     */
    blob_b64: string;
    /**
     * Envelope params JSON (alg/version/passphrase marker). Opaque to the origin.
     */
    meta?: {
        [key: string]: unknown;
    };
    /**
     * Link lifetime; default 24h, max 30d.
     */
    ttl_seconds?: number;
};
/**
 * Owner metadata for a share link. The sealed blob is never returned by list.
 */
export type Share = {
    id: string;
    name?: string;
    meta?: {
        [key: string]: unknown;
    };
    created_by?: string;
    expires_at: string;
    created_at: string;
    consumed_at?: string;
    revoked_at?: string;
};
export type SharesResponse = {
    shares: Array<Share>;
};
export type RedeemShareResponse = {
    name?: string;
    /**
     * Base64 of the sealed envelope — returned exactly once.
     */
    blob_b64: string;
    meta?: {
        [key: string]: unknown;
    };
};
export type GetHealthData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/health';
};
export type GetHealthResponses = {
    /**
     * ok
     */
    200: string;
};
export type GetHealthResponse = GetHealthResponses[keyof GetHealthResponses];
export type GetOpenApiData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/openapi.json';
};
export type GetOpenApiResponses = {
    /**
     * OpenAPI document
     */
    200: {
        [key: string]: unknown;
    };
};
export type GetOpenApiResponse = GetOpenApiResponses[keyof GetOpenApiResponses];
export type ListItemsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/items';
};
export type ListItemsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
};
export type ListItemsResponses = {
    /**
     * Items
     */
    200: ItemsResponse;
};
export type ListItemsResponse = ListItemsResponses[keyof ListItemsResponses];
export type CreateItemData = {
    body: CreateItemRequest;
    path?: never;
    query?: never;
    url: '/v1/items';
};
export type CreateItemErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * agent cannot create items
     */
    403: unknown;
};
export type CreateItemResponses = {
    /**
     * Item metadata. No secret.
     */
    200: Item;
};
export type CreateItemResponse = CreateItemResponses[keyof CreateItemResponses];
export type ImportItemsData = {
    body: Blob | File;
    path?: never;
    query?: {
        /**
         * export.1pux or chrome.csv. Sniffed from bytes when empty.
         */
        filename?: string;
    };
    url: '/v1/import';
};
export type ImportItemsErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * agent cannot import
     */
    403: unknown;
};
export type ImportItemsResponses = {
    /**
     * Created item names. No secrets.
     */
    200: ImportResponse;
};
export type ImportItemsResponse = ImportItemsResponses[keyof ImportItemsResponses];
export type DeleteItemData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}';
};
export type DeleteItemErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * agent cannot delete items
     */
    403: unknown;
};
export type DeleteItemResponses = {
    /**
     * deleted
     */
    200: unknown;
};
export type UpdateItemData = {
    body: UpdateItemRequest;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}';
};
export type UpdateItemErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * agent cannot update items
     */
    403: unknown;
};
export type UpdateItemResponses = {
    /**
     * Item metadata
     */
    200: Item;
};
export type UpdateItemResponse = UpdateItemResponses[keyof UpdateItemResponses];
export type ArchiveItemData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}/archive';
};
export type ArchiveItemErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * agent cannot archive items
     */
    403: unknown;
};
export type ArchiveItemResponses = {
    /**
     * archived
     */
    200: unknown;
};
export type ListGrantsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/grants';
};
export type ListGrantsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type ListGrantsResponses = {
    /**
     * Grants
     */
    200: GrantsResponse;
};
export type ListGrantsResponse = ListGrantsResponses[keyof ListGrantsResponses];
export type CreateGrantData = {
    body: CreateGrantRequest;
    path?: never;
    query?: never;
    url: '/v1/grants';
};
export type CreateGrantErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type CreateGrantResponses = {
    /**
     * Grant
     */
    200: Grant;
};
export type CreateGrantResponse = CreateGrantResponses[keyof CreateGrantResponses];
export type ListGroupsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/groups';
};
export type ListGroupsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type ListGroupsResponses = {
    /**
     * Groups
     */
    200: GroupsResponse;
};
export type ListGroupsResponse = ListGroupsResponses[keyof ListGroupsResponses];
export type CreateGroupData = {
    body: CreateGroupRequest;
    path?: never;
    query?: never;
    url: '/v1/groups';
};
export type CreateGroupErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type CreateGroupResponses = {
    /**
     * Group
     */
    200: Group;
};
export type CreateGroupResponse = CreateGroupResponses[keyof CreateGroupResponses];
export type ListGroupMembersData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/groups/{name}/members';
};
export type ListGroupMembersErrors = {
    /**
     * unknown group
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type ListGroupMembersResponses = {
    /**
     * Members
     */
    200: GroupMembersResponse;
};
export type ListGroupMembersResponse = ListGroupMembersResponses[keyof ListGroupMembersResponses];
export type AddGroupMemberData = {
    body: AddGroupMemberRequest;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/groups/{name}/members';
};
export type AddGroupMemberErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type AddGroupMemberResponses = {
    /**
     * added
     */
    200: unknown;
};
export type RemoveGroupMemberData = {
    body?: never;
    path: {
        name: string;
        kind: 'agent' | 'human';
        id: string;
    };
    query?: never;
    url: '/v1/groups/{name}/members/{kind}/{id}';
};
export type RemoveGroupMemberErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type RemoveGroupMemberResponses = {
    /**
     * removed
     */
    200: unknown;
};
export type ListAgentsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/agents';
};
export type ListAgentsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type ListAgentsResponses = {
    /**
     * Agents
     */
    200: AgentsResponse;
};
export type ListAgentsResponse = ListAgentsResponses[keyof ListAgentsResponses];
export type CreateAgentData = {
    body: CreateAgentRequest;
    path?: never;
    query?: never;
    url: '/v1/agents';
};
export type CreateAgentErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type CreateAgentResponses = {
    /**
     * Agent principal. No secret.
     */
    200: Agent;
};
export type CreateAgentResponse = CreateAgentResponses[keyof CreateAgentResponses];
export type RevokeAgentData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/agents/{name}/revoke';
};
export type RevokeAgentErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type RevokeAgentResponses = {
    /**
     * Revoked agent. No token or secret.
     */
    200: Agent;
};
export type RevokeAgentResponse = RevokeAgentResponses[keyof RevokeAgentResponses];
export type ListSessionsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/sessions';
};
export type ListSessionsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type ListSessionsResponses = {
    /**
     * Sessions
     */
    200: SessionsResponse;
};
export type ListSessionsResponse = ListSessionsResponses[keyof ListSessionsResponses];
export type CreateSessionData = {
    body: CreateSessionRequest;
    path?: never;
    query?: never;
    url: '/v1/sessions';
};
export type CreateSessionErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type CreateSessionResponses = {
    /**
     * Session plus token once.
     */
    200: CreateSessionResponse;
};
export type CreateSessionResponse2 = CreateSessionResponses[keyof CreateSessionResponses];
export type UseItemData = {
    body: UseRequest;
    path?: never;
    query?: never;
    url: '/v1/use';
};
export type UseItemErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
};
export type UseItemResponses = {
    /**
     * Decision plus upstream result, scrubbed
     */
    200: UseResponse;
};
export type UseItemResponse = UseItemResponses[keyof UseItemResponses];
export type ListRequestsData = {
    body?: never;
    path?: never;
    query?: {
        status?: 'open' | 'approved' | 'denied' | 'expired' | 'cancelled';
    };
    url: '/v1/requests';
};
export type ListRequestsErrors = {
    /**
     * bad status
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type ListRequestsResponses = {
    /**
     * Requests
     */
    200: RequestsResponse;
};
export type ListRequestsResponse = ListRequestsResponses[keyof ListRequestsResponses];
export type StreamRequestsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/requests/stream';
};
export type StreamRequestsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type StreamRequestsResponses = {
    /**
     * text/event-stream; stays open until the client disconnects
     */
    200: string;
};
export type StreamRequestsResponse = StreamRequestsResponses[keyof StreamRequestsResponses];
export type ApproveRequestData = {
    body?: ApproveRequestBody;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/requests/{id}/approve';
};
export type ApproveRequestErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
    /**
     * no such request in this org
     */
    404: unknown;
    /**
     * already resolved or expired
     */
    409: unknown;
};
export type ApproveRequestResponses = {
    /**
     * The approved request
     */
    200: ApprovalRequest;
};
export type ApproveRequestResponse = ApproveRequestResponses[keyof ApproveRequestResponses];
export type DenyRequestData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/requests/{id}/deny';
};
export type DenyRequestErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
    /**
     * no such request in this org
     */
    404: unknown;
    /**
     * already resolved or expired
     */
    409: unknown;
};
export type DenyRequestResponses = {
    /**
     * The denied request
     */
    200: ApprovalRequest;
};
export type DenyRequestResponse = DenyRequestResponses[keyof DenyRequestResponses];
export type ListEventsData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/events';
};
export type ListEventsErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
};
export type ListEventsResponses = {
    /**
     * Recent events for the Bearer agent
     */
    200: EventsResponse;
};
export type ListEventsResponse = ListEventsResponses[keyof ListEventsResponses];
export type VaultReportData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Opt into HaveIBeenPwned k-anonymity breach counts (5-char SHA-1 prefix only).
         */
        hibp?: '1' | 'true';
    };
    url: '/v1/report';
};
export type VaultReportErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not a human principal
     */
    403: unknown;
};
export type VaultReportResponses = {
    /**
     * Health report for the Bearer human's visible items
     */
    200: VaultReport;
};
export type VaultReportResponse = VaultReportResponses[keyof VaultReportResponses];
export type ProvisionData = {
    body?: ProvisionRequest;
    path?: never;
    query?: never;
    url: '/v1/provision';
};
export type ProvisionErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
};
export type ProvisionResponses = {
    /**
     * Provisioned principal
     */
    200: ProvisionResponse;
};
export type ProvisionResponse2 = ProvisionResponses[keyof ProvisionResponses];
export type CreateInviteData = {
    body: InviteRequest;
    path?: never;
    query?: never;
    url: '/v1/invites';
};
export type CreateInviteErrors = {
    /**
     * missing email
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
};
export type CreateInviteResponses = {
    /**
     * Invite result
     */
    200: InviteResponse;
};
export type CreateInviteResponse = CreateInviteResponses[keyof CreateInviteResponses];
export type ListSharesData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/shares';
};
export type ListSharesErrors = {
    /**
     * missing or invalid token
     */
    401: unknown;
    /**
     * not an org owner
     */
    403: unknown;
};
export type ListSharesResponses = {
    /**
     * Share metadata
     */
    200: SharesResponse;
};
export type ListSharesResponse = ListSharesResponses[keyof ListSharesResponses];
export type CreateShareData = {
    body: CreateShareRequest;
    path?: never;
    query?: never;
    url: '/v1/shares';
};
export type CreateShareErrors = {
    /**
     * malformed body, bad blob_b64, or invalid ttl
     */
    400: unknown;
    /**
     * missing or invalid token
     */
    401: unknown;
    /**
     * not an org owner
     */
    403: unknown;
};
export type CreateShareResponses = {
    /**
     * The share metadata
     */
    200: Share;
};
export type CreateShareResponse = CreateShareResponses[keyof CreateShareResponses];
export type RevokeShareData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/shares/{id}';
};
export type RevokeShareErrors = {
    /**
     * missing or invalid token
     */
    401: unknown;
    /**
     * not an org owner
     */
    403: unknown;
    /**
     * unknown share
     */
    404: unknown;
};
export type RevokeShareResponses = {
    /**
     * revoked
     */
    200: unknown;
};
export type RedeemShareData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/shares/{id}/redeem';
};
export type RedeemShareErrors = {
    /**
     * missing, consumed, revoked, or expired
     */
    404: unknown;
    /**
     * redeem rate limit
     */
    429: unknown;
};
export type RedeemShareResponses = {
    /**
     * The sealed envelope
     */
    200: RedeemShareResponse;
};
export type RedeemShareResponse2 = RedeemShareResponses[keyof RedeemShareResponses];
export type RemoveMemberData = {
    body?: never;
    path: {
        /**
         * Kratos identity id of the member
         */
        id: string;
    };
    query?: never;
    url: '/v1/members/{id}';
};
export type RemoveMemberErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * caller is a member, not an owner
     */
    403: unknown;
    /**
     * not a member of this org
     */
    404: unknown;
};
export type RemoveMemberResponses = {
    /**
     * Member removed
     */
    200: {
        removed?: boolean;
    };
};
export type RemoveMemberResponse = RemoveMemberResponses[keyof RemoveMemberResponses];
export type DemoteOwnerData = {
    body?: never;
    path: {
        /**
         * Kratos identity id of the owner
         */
        id: string;
    };
    query?: never;
    url: '/v1/members/{id}/owner';
};
export type DemoteOwnerErrors = {
    /**
     * last-owner demotion refused
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * caller is a member, not an owner
     */
    403: unknown;
    /**
     * not an owner of this org
     */
    404: unknown;
};
export type DemoteOwnerResponses = {
    /**
     * Owner demoted
     */
    200: {
        owner?: boolean;
    };
};
export type DemoteOwnerResponse = DemoteOwnerResponses[keyof DemoteOwnerResponses];
export type PromoteOwnerData = {
    body?: never;
    path: {
        /**
         * Kratos identity id of the member
         */
        id: string;
    };
    query?: never;
    url: '/v1/members/{id}/owner';
};
export type PromoteOwnerErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * caller is a member, not an owner
     */
    403: unknown;
    /**
     * not a member of this org
     */
    404: unknown;
};
export type PromoteOwnerResponses = {
    /**
     * Member promoted
     */
    200: {
        owner?: boolean;
    };
};
export type PromoteOwnerResponse = PromoteOwnerResponses[keyof PromoteOwnerResponses];
export type DeleteMeData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/me';
};
export type DeleteMeErrors = {
    /**
     * sole-owner leave refused
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * not a provisioned human
     */
    403: unknown;
};
export type DeleteMeResponses = {
    /**
     * Account removed from org
     */
    200: {
        deleted?: boolean;
    };
};
export type DeleteMeResponse = DeleteMeResponses[keyof DeleteMeResponses];
export type DeleteOrgData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/org';
};
export type DeleteOrgErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * caller is a member, not an owner
     */
    403: unknown;
};
export type DeleteOrgResponses = {
    /**
     * Org purged
     */
    200: {
        items?: number;
        grants?: number;
        sessions?: number;
        agents?: number;
        humans?: number;
        keys?: number;
    };
};
export type DeleteOrgResponse = DeleteOrgResponses[keyof DeleteOrgResponses];
export type GetBillingData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/billing';
};
export type GetBillingErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
};
export type GetBillingResponses = {
    /**
     * Billing state
     */
    200: BillingView;
};
export type GetBillingResponse = GetBillingResponses[keyof GetBillingResponses];
export type BillingWebhookData = {
    body: {
        [key: string]: unknown;
    };
    headers: {
        /**
         * t=<ms>,v1=<hex-hmac>
         */
        'Vortex-Signature': string;
    };
    path?: never;
    query?: never;
    url: '/v1/billing/webhook';
};
export type BillingWebhookErrors = {
    /**
     * malformed event body
     */
    400: unknown;
    /**
     * missing or invalid signature
     */
    401: unknown;
    /**
     * billing webhook not configured
     */
    404: unknown;
    /**
     * body too large
     */
    413: unknown;
};
export type BillingWebhookResponses = {
    /**
     * accepted (or durable no-op)
     */
    204: void;
};
export type BillingWebhookResponse = BillingWebhookResponses[keyof BillingWebhookResponses];
export type AuditFeedData = {
    body?: never;
    path?: never;
    query?: {
        /**
         * Resume cursor — return committed events with id > after.
         */
        after?: number;
        /**
         * Page size.
         */
        limit?: number;
    };
    url: '/v1/audit/events';
};
export type AuditFeedErrors = {
    /**
     * bad after/limit
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not the org owner
     */
    403: unknown;
};
export type AuditFeedResponses = {
    /**
     * One page of committed org audit events
     */
    200: AuditFeedResponse;
};
export type AuditFeedResponse2 = AuditFeedResponses[keyof AuditFeedResponses];
export type CreateBillingCheckoutData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/billing/checkout';
};
export type CreateBillingCheckoutErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * not owner
     */
    403: unknown;
    /**
     * billing not configured
     */
    404: unknown;
    /**
     * checkout failed
     */
    502: unknown;
};
export type CreateBillingCheckoutResponses = {
    /**
     * Checkout session
     */
    200: BillingCheckout;
};
export type CreateBillingCheckoutResponse = CreateBillingCheckoutResponses[keyof CreateBillingCheckoutResponses];
export type UnregisterDeviceData = {
    body?: never;
    path?: never;
    query: {
        token: string;
    };
    url: '/v1/devices';
};
export type UnregisterDeviceErrors = {
    /**
     * token required
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * principal is not a human
     */
    403: unknown;
};
export type UnregisterDeviceResponses = {
    /**
     * Device unregistered
     */
    204: void;
};
export type UnregisterDeviceResponse = UnregisterDeviceResponses[keyof UnregisterDeviceResponses];
export type RegisterDeviceData = {
    body: DeviceRegisterRequest;
    path?: never;
    query?: never;
    url: '/v1/devices';
};
export type RegisterDeviceErrors = {
    /**
     * platform must be apns or fcm and token is required
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * principal is not a human
     */
    403: unknown;
};
export type RegisterDeviceResponses = {
    /**
     * Device registered
     */
    204: void;
};
export type RegisterDeviceResponse = RegisterDeviceResponses[keyof RegisterDeviceResponses];
export type RotateItemData = {
    body?: RotateItemRequest;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}/rotate';
};
export type RotateItemErrors = {
    /**
     * rotate failed
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
    /**
     * provider has no key-management API
     */
    422: unknown;
};
export type RotateItemResponses = {
    /**
     * Rotated
     */
    200: {
        item?: string;
        rotated?: boolean;
    };
};
export type RotateItemResponse = RotateItemResponses[keyof RotateItemResponses];
export type BindWorkloadData = {
    body: BindWorkloadRequest;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/agents/{name}/bind';
};
export type BindWorkloadErrors = {
    /**
     * bind failed
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type BindWorkloadResponses = {
    /**
     * Workload binding
     */
    200: WorkloadView;
};
export type BindWorkloadResponse = BindWorkloadResponses[keyof BindWorkloadResponses];
export type VerifyIssuerData = {
    body: VerifyIssuerRequest;
    path?: never;
    query?: never;
    url: '/v1/workloads/verify-issuer';
};
export type VerifyIssuerErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type VerifyIssuerResponses = {
    /**
     * Verification result
     */
    200: VerifyIssuerResponse;
};
export type VerifyIssuerResponse2 = VerifyIssuerResponses[keyof VerifyIssuerResponses];
export type UnbindWorkloadData = {
    body?: never;
    path: {
        name: string;
    };
    query: {
        issuer: string;
        subject: string;
    };
    url: '/v1/agents/{name}/workloads';
};
export type UnbindWorkloadErrors = {
    /**
     * unbind failed
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type UnbindWorkloadResponses = {
    /**
     * Unbound
     */
    204: void;
};
export type UnbindWorkloadResponse = UnbindWorkloadResponses[keyof UnbindWorkloadResponses];
export type ListAgentWorkloadsData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/agents/{name}/workloads';
};
export type ListAgentWorkloadsErrors = {
    /**
     * list workloads failed
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type ListAgentWorkloadsResponses = {
    /**
     * Workload bindings
     */
    200: WorkloadsResponse;
};
export type ListAgentWorkloadsResponse = ListAgentWorkloadsResponses[keyof ListAgentWorkloadsResponses];
export type ListRotateProvidersData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/rotate/providers';
};
export type ListRotateProvidersErrors = {
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type ListRotateProvidersResponses = {
    /**
     * Rotation provider coverage
     */
    200: RotateProvidersResponse;
};
export type ListRotateProvidersResponse = ListRotateProvidersResponses[keyof ListRotateProvidersResponses];
export type VerifyItemData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}/verify';
};
export type VerifyItemErrors = {
    /**
     * verify failed
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type VerifyItemResponses = {
    /**
     * Item verified
     */
    200: VerifyItemResponse;
};
export type VerifyItemResponse2 = VerifyItemResponses[keyof VerifyItemResponses];
export type ListRotateHooksData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}/rotate-hooks';
};
export type ListRotateHooksErrors = {
    /**
     * no such item
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type ListRotateHooksResponses = {
    /**
     * Configured hooks
     */
    200: RotateHooksResponse;
};
export type ListRotateHooksResponse = ListRotateHooksResponses[keyof ListRotateHooksResponses];
export type SetRotateHooksData = {
    body: RotateHooksRequest;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}/rotate-hooks';
};
export type SetRotateHooksErrors = {
    /**
     * invalid hook config
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type SetRotateHooksResponses = {
    /**
     * Hooks stored
     */
    200: RotateHooksResponse;
};
export type SetRotateHooksResponse = SetRotateHooksResponses[keyof SetRotateHooksResponses];
export type FireRotateHooksData = {
    body?: never;
    path: {
        name: string;
    };
    query?: never;
    url: '/v1/items/{name}/rotate-hooks/fire';
};
export type FireRotateHooksErrors = {
    /**
     * no such item
     */
    400: unknown;
    /**
     * missing or invalid bearer
     */
    401: unknown;
    /**
     * not the owner
     */
    403: unknown;
};
export type FireRotateHooksResponses = {
    /**
     * Per-hook outcomes
     */
    200: RotateHooksFireResponse;
};
export type FireRotateHooksResponse = FireRotateHooksResponses[keyof FireRotateHooksResponses];
export type GetRecoveryShareData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/recovery/share';
};
export type GetRecoveryShareErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
};
export type GetRecoveryShareResponses = {
    /**
     * Share state
     */
    200: RecoveryShare;
};
export type GetRecoveryShareResponse = GetRecoveryShareResponses[keyof GetRecoveryShareResponses];
export type EnrollRecoveryData = {
    body: EnrollRequest;
    path?: never;
    query?: never;
    url: '/v1/recovery/enroll';
};
export type EnrollRecoveryErrors = {
    /**
     * missing or malformed pubkey
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * aal1 token — enrollment requires a second factor
     */
    403: unknown;
    /**
     * already enrolled
     */
    409: unknown;
};
export type EnrollRecoveryResponses = {
    /**
     * The planted share
     */
    200: RecoveryShare;
};
export type EnrollRecoveryResponse = EnrollRecoveryResponses[keyof EnrollRecoveryResponses];
export type RealignRecoveryCustodianData = {
    body: EnrollRequest;
    path?: never;
    query?: never;
    url: '/v1/recovery/realign-custodian';
};
export type RealignRecoveryCustodianErrors = {
    /**
     * missing or malformed pubkey, or pubkey already enrolled on another share
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * aal1 token — realign requires a second factor
     */
    403: unknown;
    /**
     * member never enrolled a recovery share
     */
    404: unknown;
};
export type RealignRecoveryCustodianResponses = {
    /**
     * The updated share
     */
    200: RecoveryShare;
};
export type RealignRecoveryCustodianResponse = RealignRecoveryCustodianResponses[keyof RealignRecoveryCustodianResponses];
export type StartRecoveryData = {
    body: StartRecoveryRequest;
    path?: never;
    query?: never;
    url: '/v1/recovery/start';
};
export type StartRecoveryErrors = {
    /**
     * malformed new_pubkey or a pubkey already enrolled on a share
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * non-owner naming another human, or aal1 owner-initiated start
     */
    403: unknown;
    /**
     * named human is not an org member
     */
    404: unknown;
    /**
     * recovery start rate limit
     */
    429: unknown;
    /**
     * identity plane unavailable
     */
    502: unknown;
};
export type StartRecoveryResponses = {
    /**
     * The request (created=false when deduped)
     */
    200: StartRecoveryResponse;
};
export type StartRecoveryResponse2 = StartRecoveryResponses[keyof StartRecoveryResponses];
export type ListRecoveriesData = {
    body?: never;
    path?: never;
    query?: {
        status?: 'open' | 'completed' | 'denied' | 'expired' | 'cancelled';
    };
    url: '/v1/recovery';
};
export type ListRecoveriesErrors = {
    /**
     * bad status
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * not owner or aal1
     */
    403: unknown;
};
export type ListRecoveriesResponses = {
    /**
     * The org's requests
     */
    200: RecoveriesResponse;
};
export type ListRecoveriesResponse = ListRecoveriesResponses[keyof ListRecoveriesResponses];
export type GetRecoveryData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/recovery/{id}';
};
export type GetRecoveryErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * not owner, initiator, or target — or aal1 owner
     */
    403: unknown;
    /**
     * no such request in this org
     */
    404: unknown;
};
export type GetRecoveryResponses = {
    /**
     * The request
     */
    200: RecoveryRequest;
};
export type GetRecoveryResponse = GetRecoveryResponses[keyof GetRecoveryResponses];
export type CompleteRecoveryData = {
    body?: CompleteRecoveryRequest;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/recovery/{id}/complete';
};
export type CompleteRecoveryErrors = {
    /**
     * malformed proof_hmac
     */
    400: unknown;
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * not owner, aal1, or two-person integrity violation
     */
    403: unknown;
    /**
     * no such request, or target was offboarded
     */
    404: unknown;
    /**
     * already resolved or expired
     */
    409: unknown;
    /**
     * still inside the unlock_at timelock
     */
    423: unknown;
};
export type CompleteRecoveryResponses = {
    /**
     * The completed request
     */
    200: RecoveryRequest;
};
export type CompleteRecoveryResponse = CompleteRecoveryResponses[keyof CompleteRecoveryResponses];
export type DenyRecoveryData = {
    body?: never;
    path: {
        id: string;
    };
    query?: never;
    url: '/v1/recovery/{id}/deny';
};
export type DenyRecoveryErrors = {
    /**
     * missing or invalid human token
     */
    401: unknown;
    /**
     * not owner, initiator, or target — or aal1 owner
     */
    403: unknown;
    /**
     * no such request in this org
     */
    404: unknown;
    /**
     * already resolved
     */
    409: unknown;
};
export type DenyRecoveryResponses = {
    /**
     * The resolved request
     */
    200: RecoveryRequest;
};
export type DenyRecoveryResponse = DenyRecoveryResponses[keyof DenyRecoveryResponses];
export type GetTravelModeData = {
    body?: never;
    path?: never;
    query?: never;
    url: '/v1/org/travel-mode';
};
export type GetTravelModeErrors = {
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * agents cannot read org settings
     */
    403: unknown;
};
export type GetTravelModeResponses = {
    /**
     * flag state
     */
    200: TravelModeView;
};
export type GetTravelModeResponse = GetTravelModeResponses[keyof GetTravelModeResponses];
export type SetTravelModeData = {
    body: TravelModeRequest;
    path?: never;
    query?: never;
    url: '/v1/org/travel-mode';
};
export type SetTravelModeErrors = {
    /**
     * bad request
     */
    400: unknown;
    /**
     * missing or invalid Bearer
     */
    401: unknown;
    /**
     * owner only
     */
    403: unknown;
};
export type SetTravelModeResponses = {
    /**
     * flag state
     */
    200: TravelModeView;
};
export type SetTravelModeResponse = SetTravelModeResponses[keyof SetTravelModeResponses];
