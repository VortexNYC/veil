export type ClientOptions = {
    baseUrl: 'https://veil.nyc' | (string & {});
};
export type Item = {
    id: string;
    org_id: string;
    name: string;
    kind: 'api_key' | 'oauth' | 'ssh' | 'file' | 'passkey' | 'card' | 'identity';
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
    kind?: 'api_key' | 'oauth' | 'ssh' | 'file' | 'passkey' | 'card' | 'identity';
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
     * Fill username. Metadata on the item. Also sealed in the envelope. Does not rotate the secret.
     */
    login?: string;
};
export type CreateGrantRequest = {
    /**
     * Agent id. XOR human.
     */
    agent?: string;
    /**
     * Kratos identity id. Same Grant.agent_id. Not email. XOR agent.
     */
    human?: string;
    item: string;
    level: 'level1' | 'level2';
    /**
     * Go duration. Empty is forever.
     */
    expires?: string;
};
export type Grant = {
    id: string;
    org_id: string;
    agent_id: string;
    item_id: string;
    level: string;
    expires_at?: string;
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
export type ProvisionData = {
    body?: never;
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
