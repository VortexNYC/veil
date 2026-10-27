import {
  addGroupMember,
  approveRequest,
  archiveItem as sdkArchiveItem,
  bindWorkload as sdkBindWorkload,
  completeRecovery as sdkCompleteRecovery,
  createAgent,
  createBillingCheckout,
  createClient,
  createGrant,
  createGroup,
  createItem,
  deleteItem as sdkDeleteItem,
  denyRecovery as sdkDenyRecovery,
  denyRequest,
  enrollRecovery as sdkEnrollRecovery,
  getBilling,
  getRecoveryShare,
  getTravelMode,
  listAgents,
  listAgentWorkloads as sdkListAgentWorkloads,
  listEvents,
  listGrants,
  listGroupMembers,
  listGroups,
  listItems,
  listRecoveries,
  listRequests,
  listRotateProviders as sdkListRotateProviders,
  provision as sdkProvision,
  removeGroupMember,
  createShare as sdkCreateShare,
  listShares as sdkListShares,
  revokeShare as sdkRevokeShare,
  revokeAgent as sdkRevokeAgent,
  rotateItem as sdkRotateItem,
  setTravelMode as sdkSetTravelMode,
  startRecovery as sdkStartRecovery,
  unbindWorkload as sdkUnbindWorkload,
  updateItem as sdkUpdateItem,
  vaultReport,
  verifyIssuer as sdkVerifyIssuer,
  verifyItem as sdkVerifyItem,
} from "@vortex-api/veil";
import type {
  BindWorkloadRequest,
  RedeemShareResponse,
  RotateItemRequest,
  StartRecoveryRequest,
  UpdateItemRequest,
} from "@vortex-api/veil";
import { originAPI, token } from "./auth";

function client() {
  const t = token();
  if (!t) {
    throw new Error("signed out");
  }
  return createClient({
    baseUrl: originAPI,
    auth: () => t,
  });
}

export function items() {
  return listItems({ client: client() });
}

export function addItem(body: {
  name: string;
  uri?: string;
  uris?: string[];
  secret?: string;
  totp_seed?: string;
  tags?: string[];
  login?: string;
  travel_safe?: boolean;
  kind?: "api_key" | "card" | "identity" | "oauth" | "secure_note" | "ssh";
  card?: {
    number?: string;
    exp_month?: string;
    exp_year?: string;
    cvv?: string;
    holder?: string;
  };
  identity?: {
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
  wif?: {
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
}) {
  return createItem({ client: client(), body });
}

export function updateVaultItem(id: string, body: UpdateItemRequest) {
  return sdkUpdateItem({ client: client(), path: { name: id }, body });
}

export function archiveVaultItem(id: string) {
  return sdkArchiveItem({ client: client(), path: { name: id } });
}

export function deleteVaultItem(id: string) {
  return sdkDeleteItem({ client: client(), path: { name: id } });
}

export function importItems(file: File) {
  const t = token();
  if (!t) {
    throw new Error("signed out");
  }
  const q = encodeURIComponent(file.name);
  return fetch(`${originAPI}/v1/import?filename=${q}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${t}`,
      "Content-Type": "application/octet-stream",
    },
    body: file,
  });
}

export function grants() {
  return listGrants({ client: client() });
}

export function addGrant(body: {
  item: string;
  level: "level1" | "level2";
  agent?: string;
  human?: string;
  group?: string;
  expires?: string;
}) {
  return createGrant({ client: client(), body });
}

// Groups are the team model — a grant to a group name is the shared vault.
export function groups() {
  return listGroups({ client: client() });
}

export function addGroup(name: string) {
  return createGroup({ client: client(), body: { name } });
}

export function groupMembers(name: string) {
  return listGroupMembers({ client: client(), path: { name } });
}

export function addGroupMemberTo(name: string, memberKind: "agent" | "human", memberId: string) {
  return addGroupMember({
    client: client(),
    path: { name },
    body: { member_kind: memberKind, member_id: memberId },
  });
}

export function removeGroupMemberFrom(
  name: string,
  memberKind: "agent" | "human",
  memberId: string,
) {
  return removeGroupMember({
    client: client(),
    path: { name, kind: memberKind, id: memberId },
  });
}

export function agents() {
  return listAgents({ client: client() });
}

export function addAgent(name: string) {
  return createAgent({ client: client(), body: { name } });
}

export function revokeVaultAgent(name: string) {
  return sdkRevokeAgent({ client: client(), path: { name } });
}

export function bindAgentWorkload(name: string, body: BindWorkloadRequest) {
  return sdkBindWorkload({ client: client(), path: { name }, body });
}

export function verifyWorkloadIssuer(issuer: string) {
  return sdkVerifyIssuer({ client: client(), body: { issuer } });
}

export function agentWorkloads(name: string) {
  return sdkListAgentWorkloads({ client: client(), path: { name } });
}

export function unbindAgentWorkload(name: string, issuer: string, subject: string) {
  return sdkUnbindWorkload({ client: client(), path: { name }, query: { issuer, subject } });
}

export interface HydraAgentMint {
  agent_id: string;
  client_id: string;
  client_secret?: string;
  issuer: string;
  audience: string;
}

// mintAgentHydra calls POST /v1/agents/{name}/hydra — not yet in the OpenAPI
// spec, so this is a raw fetch like importItems. The client_secret is returned
// once, at mint time; the dialog that shows it must not persist it.
export async function mintAgentHydra(
  name: string,
): Promise<{ data?: HydraAgentMint; error?: string }> {
  const t = token();
  if (!t) {
    throw new Error("signed out");
  }
  const res = await fetch(`${originAPI}/v1/agents/${encodeURIComponent(name)}/hydra`, {
    method: "POST",
    headers: { Authorization: `Bearer ${t}` },
  });
  if (!res.ok) {
    const msg = await res.text().catch(() => "");
    return { error: msg || `mint failed (${res.status})` };
  }
  const body = (await res.json()) as {
    agent_id: string;
    client_id: string;
    secret?: string;
    issuer: string;
    audience: string;
  };
  return {
    data: {
      agent_id: body.agent_id,
      client_id: body.client_id,
      client_secret: body.secret,
      issuer: body.issuer,
      audience: body.audience,
    },
  };
}

export function rotateVaultItem(name: string, body?: RotateItemRequest) {
  return sdkRotateItem({ client: client(), path: { name }, body });
}

export function rotateProviders() {
  return sdkListRotateProviders({ client: client() });
}

export function verifyVaultItem(name: string) {
  return sdkVerifyItem({ client: client(), path: { name } });
}

// reveal is the deliberate human read-back — off-contract like /v1/fill, so
// raw fetch rather than the generated SDK.
export async function revealVaultItem(
  name: string,
): Promise<{ data?: { login: string; name: string; password: string; uuid: string; totp?: string }; error?: string }> {
  const t = token();
  if (!t) {
    throw new Error("signed out");
  }
  const res = await fetch(`${originAPI}/v1/items/${encodeURIComponent(name)}/reveal`, {
    method: "POST",
    headers: { Authorization: `Bearer ${t}`, "Content-Type": "application/json" },
    body: JSON.stringify({ mint_totp: true }),
  });
  if (!res.ok) {
    const msg = await res.text().catch(() => "");
    return { error: msg || `reveal failed (${res.status})` };
  }
  return { data: (await res.json()) as { login: string; name: string; password: string; uuid: string; totp?: string } };
}

export function events() {
  return listEvents({ client: client() });
}

// report is the vault health surface (weak/reused/pwned) — metadata only,
// no secrets cross the wire. hibp=1 opts into k-anonymity breach counts.
export function report(hibp?: boolean) {
  return vaultReport({ client: client(), query: hibp ? { hibp: "1" } : {} });
}

// billing reads the owner's plan + window usage for the cap banner.
// Members get 403 — callers render nothing on error.
export function billing() {
  return getBilling({ client: client() });
}

// billingCheckout mints a fresh hosted checkout URL per click — checkout
// tokens are single-use, so the banner never caches one.
export function billingCheckout() {
  return createBillingCheckout({ client: client() });
}

// provisionVault is first-sign-in provisioning: the org, sealed master key,
// Keto owner/member tuples, and the Vortex billing customer. Idempotent —
// safe to call on every login.
export function provisionVault() {
  return sdkProvision({ client: client() });
}

export function requests(status: "open" | "approved" | "denied" | "expired" | "cancelled") {
  return listRequests({ client: client(), query: { status } });
}

export function approveReq(id: string, ttl?: string) {
  return approveRequest({ client: client(), path: { id }, body: ttl ? { ttl } : {} });
}

export function denyReq(id: string) {
  return denyRequest({ client: client(), path: { id } });
}

// watchRequests opens the request-change stream (GET /v1/requests/stream)
// and calls onTick for each server event. EventSource cannot send Bearer
// headers, so this parses SSE frames off a fetch body. Reconnects on drop
// with a short delay; returns an unsubscribe that stops the stream.
export function watchRequests(onTick: () => void): () => void {
  const ctrl = new AbortController();
  void (async () => {
    while (!ctrl.signal.aborted) {
      try {
        const t = token();
        if (!t) {
          return;
        }
        const res = await fetch(`${originAPI}/v1/requests/stream`, {
          headers: { Authorization: `Bearer ${t}` },
          signal: ctrl.signal,
        });
        if (!res.ok || !res.body) {
          throw new Error(`stream ${res.status}`);
        }
        const reader = res.body.getReader();
        const dec = new TextDecoder();
        let buf = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) {
            throw new Error("eof");
          }
          buf += dec.decode(value, { stream: true });
          let i = buf.indexOf("\n\n");
          while (i >= 0) {
            const frame = buf.slice(0, i);
            buf = buf.slice(i + 2);
            if (frame.startsWith("data:")) {
              onTick();
            }
            i = buf.indexOf("\n\n");
          }
        }
      } catch {
        if (ctrl.signal.aborted) {
          return;
        }
        await new Promise((r) => setTimeout(r, 2000));
      }
    }
  })();
  return () => ctrl.abort();
}

// Travel Mode (VTX-788): the org flag — any human reads it so the settings
// card can explain hidden items; the POST toggle is owner-only server-side.
export function travelMode() {
  return getTravelMode({ client: client() });
}

export function setTravelMode(enabled: boolean) {
  return sdkSetTravelMode({ client: client(), body: { enabled } });
}

// VEIL-78 member recovery: the caller's own org-key share. enrolled=false
// drives the kit banner / settings card.
export function recoveryShare() {
  return getRecoveryShare({ client: client() });
}

// pubkey is the 32-byte X25519 public half (base64) of a keypair minted in
// the browser — the private half only ever lives in the downloaded kit.
export function enrollRecovery(pubkey: string) {
  return sdkEnrollRecovery({ client: client(), body: { pubkey } });
}

export function startRecovery(body: StartRecoveryRequest) {
  return sdkStartRecovery({ client: client(), body });
}

// Owner-only list — members get 403 and callers render nothing.
export function recoveries(status?: "open" | "completed" | "denied" | "expired" | "cancelled") {
  return listRecoveries({ client: client(), query: status ? { status } : {} });
}

export function completeRecovery(id: string, proofHmac?: string) {
  return sdkCompleteRecovery({
    client: client(),
    path: { id },
    body: proofHmac ? { proof_hmac: proofHmac } : {},
  });
}

export function denyRecovery(id: string) {
  return sdkDenyRecovery({ client: client(), path: { id } });
}

// VTX-783 share links. The blob POSTs already sealed — these calls never
// see plaintext or the fragment key.
export function createShareLink(body: {
  name: string;
  blob_b64: string;
  meta?: { [key: string]: unknown };
  ttl_seconds?: number;
}) {
  return sdkCreateShare({ client: client(), body });
}

export function shares() {
  return sdkListShares({ client: client() });
}

export function revokeShareLink(id: string) {
  return sdkRevokeShare({ client: client(), path: { id } });
}

// redeemShareLink is PUBLIC: the recipient need not have an account, so
// this raw fetch deliberately carries no Authorization — the link id is
// the capability, and the one-time read is rate-limited per id.
export async function redeemShareLink(
  id: string,
): Promise<{ status: number; data?: RedeemShareResponse }> {
  const res = await fetch(`${originAPI}/v1/shares/${encodeURIComponent(id)}/redeem`, {
    method: "POST",
  });
  if (!res.ok) {
    return { status: res.status };
  }
  return { status: res.status, data: (await res.json()) as RedeemShareResponse };
}
