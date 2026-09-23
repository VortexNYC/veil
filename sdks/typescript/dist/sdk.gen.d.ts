import type { Client, ClientMeta, Options as Options2, RequestResult, TDataShape } from './client/index.js';
import type { ArchiveItemData, ArchiveItemErrors, ArchiveItemResponses, CreateAgentData, CreateAgentErrors, CreateAgentResponses, CreateGrantData, CreateGrantErrors, CreateGrantResponses, CreateInviteData, CreateInviteErrors, CreateInviteResponses, CreateItemData, CreateItemErrors, CreateItemResponses, CreateSessionData, CreateSessionErrors, CreateSessionResponses, DeleteItemData, DeleteItemErrors, DeleteItemResponses, DeleteMeData, DeleteMeErrors, DeleteMeResponses, DeleteOrgData, DeleteOrgErrors, DeleteOrgResponses, DemoteOwnerData, DemoteOwnerErrors, DemoteOwnerResponses, GetHealthData, GetHealthResponses, GetOpenApiData, GetOpenApiResponses, ImportItemsData, ImportItemsErrors, ImportItemsResponses, ListAgentsData, ListAgentsErrors, ListAgentsResponses, ListEventsData, ListEventsErrors, ListEventsResponses, ListGrantsData, ListGrantsErrors, ListGrantsResponses, ListItemsData, ListItemsErrors, ListItemsResponses, ListSessionsData, ListSessionsErrors, ListSessionsResponses, PromoteOwnerData, PromoteOwnerErrors, PromoteOwnerResponses, ProvisionData, ProvisionErrors, ProvisionResponses, RemoveMemberData, RemoveMemberErrors, RemoveMemberResponses, RevokeAgentData, RevokeAgentErrors, RevokeAgentResponses, UpdateItemData, UpdateItemErrors, UpdateItemResponses, UseItemData, UseItemErrors, UseItemResponses } from './types.gen.js';
export type Options<TData extends TDataShape = TDataShape, ThrowOnError extends boolean = boolean, TResponse = unknown> = Options2<TData, ThrowOnError, TResponse> & {
    /**
     * You can provide a client instance returned by `createClient()` instead of
     * individual options. This might be also useful if you want to implement a
     * custom client.
     */
    client?: Client;
    /**
     * You can pass arbitrary values through the `meta` object. This can be
     * used to access values that aren't defined as part of the SDK function.
     */
    meta?: keyof ClientMeta extends never ? Record<string, unknown> : ClientMeta;
};
/**
 * Liveness. No secrets.
 */
export declare const getHealth: <ThrowOnError extends boolean = false>(options?: Options<GetHealthData, ThrowOnError>) => RequestResult<GetHealthResponses, unknown, ThrowOnError>;
/**
 * This contract.
 */
export declare const getOpenApi: <ThrowOnError extends boolean = false>(options?: Options<GetOpenApiData, ThrowOnError>) => RequestResult<GetOpenApiResponses, unknown, ThrowOnError>;
/**
 * Items this principal may see. Agent: granted. Human owner: the org. Human member: granted. Names and URIs. Never secrets.
 */
export declare const listItems: <ThrowOnError extends boolean = false>(options?: Options<ListItemsData, ThrowOnError>) => RequestResult<ListItemsResponses, ListItemsErrors, ThrowOnError>;
/**
 * Create an item. Secret is in the request over TLS. Never in the response. Not MCP.
 */
export declare const createItem: <ThrowOnError extends boolean = false>(options: Options<CreateItemData, ThrowOnError>) => RequestResult<CreateItemResponses, CreateItemErrors, ThrowOnError>;
/**
 * One-shot 1Password .1pux or CSV onto origin. Secret in the file, never in the response. Not MCP.
 */
export declare const importItems: <ThrowOnError extends boolean = false>(options: Options<ImportItemsData, ThrowOnError>) => RequestResult<ImportItemsResponses, ImportItemsErrors, ThrowOnError>;
/**
 * Remove the item and its grants. Not MCP.
 */
export declare const deleteItem: <ThrowOnError extends boolean = false>(options: Options<DeleteItemData, ThrowOnError>) => RequestResult<DeleteItemResponses, DeleteItemErrors, ThrowOnError>;
/**
 * Replace URIs, tags, and fill username. No secret.
 */
export declare const updateItem: <ThrowOnError extends boolean = false>(options: Options<UpdateItemData, ThrowOnError>) => RequestResult<UpdateItemResponses, UpdateItemErrors, ThrowOnError>;
/**
 * Hide from Use and list. History stays.
 */
export declare const archiveItem: <ThrowOnError extends boolean = false>(options: Options<ArchiveItemData, ThrowOnError>) => RequestResult<ArchiveItemResponses, ArchiveItemErrors, ThrowOnError>;
/**
 * Grants in this org. No secrets. Not MCP.
 */
export declare const listGrants: <ThrowOnError extends boolean = false>(options?: Options<ListGrantsData, ThrowOnError>) => RequestResult<ListGrantsResponses, ListGrantsErrors, ThrowOnError>;
/**
 * Grant an agent or a Kratos human Use on an item. Same grant object. Not MCP. Not a family vault.
 */
export declare const createGrant: <ThrowOnError extends boolean = false>(options: Options<CreateGrantData, ThrowOnError>) => RequestResult<CreateGrantResponses, CreateGrantErrors, ThrowOnError>;
/**
 * Agents in this org. Ids only. Never secrets. Not MCP.
 */
export declare const listAgents: <ThrowOnError extends boolean = false>(options?: Options<ListAgentsData, ThrowOnError>) => RequestResult<ListAgentsResponses, ListAgentsErrors, ThrowOnError>;
/**
 * Register an agent principal. Not a Hydra secret. Not MCP.
 */
export declare const createAgent: <ThrowOnError extends boolean = false>(options: Options<CreateAgentData, ThrowOnError>) => RequestResult<CreateAgentResponses, CreateAgentErrors, ThrowOnError>;
/**
 * Revoke an agent. Idempotent. Kills grants, sessions, and in-flight Use. Record stays for audit.
 */
export declare const revokeAgent: <ThrowOnError extends boolean = false>(options: Options<RevokeAgentData, ThrowOnError>) => RequestResult<RevokeAgentResponses, RevokeAgentErrors, ThrowOnError>;
/**
 * Active sandbox sessions. Metadata only. Never the token. Not MCP.
 */
export declare const listSessions: <ThrowOnError extends boolean = false>(options?: Options<ListSessionsData, ThrowOnError>) => RequestResult<ListSessionsResponses, ListSessionsErrors, ThrowOnError>;
/**
 * Mint a short-lived Use lease onto an existing agent. Token is in this response once. Sandbox gets the session file, not the agent JWT. Default 15m, max 1h. Not MCP.
 */
export declare const createSession: <ThrowOnError extends boolean = false>(options: Options<CreateSessionData, ThrowOnError>) => RequestResult<CreateSessionResponses, CreateSessionErrors, ThrowOnError>;
/**
 * Call a URL as this agent. The broker injects the credential. The vault secret is never in the response.
 */
export declare const useItem: <ThrowOnError extends boolean = false>(options: Options<UseItemData, ThrowOnError>) => RequestResult<UseItemResponses, UseItemErrors, ThrowOnError>;
/**
 * This agent's grant events. Decision, item, action. Never secrets. Not MCP.
 */
export declare const listEvents: <ThrowOnError extends boolean = false>(options?: Options<ListEventsData, ThrowOnError>) => RequestResult<ListEventsResponses, ListEventsErrors, ThrowOnError>;
/**
 * Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.
 */
export declare const provision: <ThrowOnError extends boolean = false>(options?: Options<ProvisionData, ThrowOnError>) => RequestResult<ProvisionResponses, ProvisionErrors, ThrowOnError>;
/**
 * Private-alpha invite. Owner-authenticated — the bearer is a verified human ID token of a provisioned human. Creates the Kratos identity plus recovery link under the inviter's org and emails the setup link via the mail worker. recovery_url only appears when mail is not configured (local dev). Idempotent for re-invites. Not MCP.
 */
export declare const createInvite: <ThrowOnError extends boolean = false>(options: Options<CreateInviteData, ThrowOnError>) => RequestResult<CreateInviteResponses, CreateInviteErrors, ThrowOnError>;
/**
 * Offboard a member. Owner-only — drops the Keto member tuple and the humans row; the removed member's token resolves nothing from that call on. Owners cannot be removed this way (demote first). Not MCP.
 */
export declare const removeMember: <ThrowOnError extends boolean = false>(options: Options<RemoveMemberData, ThrowOnError>) => RequestResult<RemoveMemberResponses, RemoveMemberErrors, ThrowOnError>;
/**
 * Strip the owners tuple from a co-owner. Owner-only. The last owner cannot be demoted — the org would have no administrator. Not MCP.
 */
export declare const demoteOwner: <ThrowOnError extends boolean = false>(options: Options<DemoteOwnerData, ThrowOnError>) => RequestResult<DemoteOwnerResponses, DemoteOwnerErrors, ThrowOnError>;
/**
 * Grant an existing member the owners tuple. Owner-only. Not MCP.
 */
export declare const promoteOwner: <ThrowOnError extends boolean = false>(options: Options<PromoteOwnerData, ThrowOnError>) => RequestResult<PromoteOwnerResponses, PromoteOwnerErrors, ThrowOnError>;
/**
 * Leave the org: drops the member (and owner, when present) tuples plus the humans row — the token resolves nothing afterward. A sole owner is refused: promote a member or delete the org. Not MCP.
 */
export declare const deleteMe: <ThrowOnError extends boolean = false>(options?: Options<DeleteMeData, ThrowOnError>) => RequestResult<DeleteMeResponses, DeleteMeErrors, ThrowOnError>;
/**
 * Teardown. Owner-only. Deletes every Keto tuple for the org, then purges every vault row (items, grants, sessions, agents, humans, keys) in one transaction. Audit rows are kept — teardown must not erase the forensic record. Not MCP.
 */
export declare const deleteOrg: <ThrowOnError extends boolean = false>(options?: Options<DeleteOrgData, ThrowOnError>) => RequestResult<DeleteOrgResponses, DeleteOrgErrors, ThrowOnError>;
