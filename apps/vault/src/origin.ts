import {
  addGroupMember,
  approveRequest,
  bindWorkload as sdkBindWorkload,
  createAgent,
  createBillingCheckout,
  createClient,
  createGrant,
  createGroup,
  createItem,
  denyRequest,
  getBilling,
  listAgents,
  listAgentWorkloads as sdkListAgentWorkloads,
  listEvents,
  listGrants,
  listGroupMembers,
  listGroups,
  listItems,
  listRequests,
  listRotateProviders as sdkListRotateProviders,
  removeGroupMember,
  rotateItem as sdkRotateItem,
  unbindWorkload as sdkUnbindWorkload,
  vaultReport,
  verifyIssuer as sdkVerifyIssuer,
  verifyItem as sdkVerifyItem,
} from "@vortex-api/veil"
import type { BindWorkloadRequest, RotateItemRequest } from "@vortex-api/veil"
import { originAPI, token } from "./auth"

function client() {
  const t = token()
  if (!t) {
    throw new Error("signed out")
  }
  return createClient({
    baseUrl: originAPI,
    auth: () => t,
  })
}

export function items() {
  return listItems({ client: client() })
}

export function addItem(body: {
  name: string
  uri?: string
  secret?: string
  login?: string
  kind?: "api_key" | "card" | "identity" | "oauth"
  card?: {
    number?: string
    exp_month?: string
    exp_year?: string
    cvv?: string
    holder?: string
  }
  identity?: {
    given_name?: string
    family_name?: string
    address?: string
    city?: string
    region?: string
    postal?: string
    country?: string
    phone?: string
    email?: string
  }
  wif?: {
    token_url: string
    assertion_token_url: string
    assertion_client_id: string
    assertion_secret: string
    assertion_audience?: string
    federation_rule_id: string
    organization_id?: string
    service_account_id?: string
    workspace_id?: string
  }
}) {
  return createItem({ client: client(), body })
}

export function importItems(file: File) {
  const t = token()
  if (!t) {
    throw new Error("signed out")
  }
  const q = encodeURIComponent(file.name)
  return fetch(`${originAPI}/v1/import?filename=${q}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${t}`,
      "Content-Type": "application/octet-stream",
    },
    body: file,
  })
}

export function grants() {
  return listGrants({ client: client() })
}

export function addGrant(body: {
  item: string
  level: "level1" | "level2"
  agent?: string
  human?: string
  group?: string
}) {
  return createGrant({ client: client(), body })
}

// Groups are the team model — a grant to a group name is the shared vault.
export function groups() {
  return listGroups({ client: client() })
}

export function addGroup(name: string) {
  return createGroup({ client: client(), body: { name } })
}

export function groupMembers(name: string) {
  return listGroupMembers({ client: client(), path: { name } })
}

export function addGroupMemberTo(name: string, memberKind: "agent" | "human", memberId: string) {
  return addGroupMember({
    client: client(),
    path: { name },
    body: { member_kind: memberKind, member_id: memberId },
  })
}

export function removeGroupMemberFrom(name: string, memberKind: "agent" | "human", memberId: string) {
  return removeGroupMember({
    client: client(),
    path: { name, kind: memberKind, id: memberId },
  })
}

export function agents() {
  return listAgents({ client: client() })
}

export function addAgent(name: string) {
  return createAgent({ client: client(), body: { name } })
}

export function bindAgentWorkload(name: string, body: BindWorkloadRequest) {
  return sdkBindWorkload({ client: client(), path: { name }, body })
}

export function verifyWorkloadIssuer(issuer: string) {
  return sdkVerifyIssuer({ client: client(), body: { issuer } })
}

export function agentWorkloads(name: string) {
  return sdkListAgentWorkloads({ client: client(), path: { name } })
}

export function unbindAgentWorkload(name: string, issuer: string, subject: string) {
  return sdkUnbindWorkload({ client: client(), path: { name }, query: { issuer, subject } })
}

export function rotateVaultItem(name: string, body?: RotateItemRequest) {
  return sdkRotateItem({ client: client(), path: { name }, body })
}

export function rotateProviders() {
  return sdkListRotateProviders({ client: client() })
}

export function verifyVaultItem(name: string) {
  return sdkVerifyItem({ client: client(), path: { name } })
}

export function events() {
  return listEvents({ client: client() })
}

// report is the vault health surface (weak/reused/pwned) — metadata only,
// no secrets cross the wire. hibp=1 opts into k-anonymity breach counts.
export function report(hibp?: boolean) {
  return vaultReport({ client: client(), query: hibp ? { hibp: "1" } : {} })
}

// billing reads the owner's plan + window usage for the cap banner.
// Members get 403 — callers render nothing on error.
export function billing() {
  return getBilling({ client: client() })
}

// billingCheckout mints a fresh hosted checkout URL per click — checkout
// tokens are single-use, so the banner never caches one.
export function billingCheckout() {
  return createBillingCheckout({ client: client() })
}

export function requests(status: "open" | "approved" | "denied" | "expired" | "cancelled") {
  return listRequests({ client: client(), query: { status } })
}

export function approveReq(id: string, ttl?: string) {
  return approveRequest({ client: client(), path: { id }, body: ttl ? { ttl } : {} })
}

export function denyReq(id: string) {
  return denyRequest({ client: client(), path: { id } })
}

// watchRequests opens the request-change stream (GET /v1/requests/stream)
// and calls onTick for each server event. EventSource cannot send Bearer
// headers, so this parses SSE frames off a fetch body. Reconnects on drop
// with a short delay; returns an unsubscribe that stops the stream.
export function watchRequests(onTick: () => void): () => void {
  const ctrl = new AbortController()
  void (async () => {
    while (!ctrl.signal.aborted) {
      try {
        const t = token()
        if (!t) {
          return
        }
        const res = await fetch(`${originAPI}/v1/requests/stream`, {
          headers: { Authorization: `Bearer ${t}` },
          signal: ctrl.signal,
        })
        if (!res.ok || !res.body) {
          throw new Error(`stream ${res.status}`)
        }
        const reader = res.body.getReader()
        const dec = new TextDecoder()
        let buf = ""
        for (;;) {
          const { done, value } = await reader.read()
          if (done) {
            throw new Error("eof")
          }
          buf += dec.decode(value, { stream: true })
          let i = buf.indexOf("\n\n")
          while (i >= 0) {
            const frame = buf.slice(0, i)
            buf = buf.slice(i + 2)
            if (frame.startsWith("data:")) {
              onTick()
            }
            i = buf.indexOf("\n\n")
          }
        }
      } catch {
        if (ctrl.signal.aborted) {
          return
        }
        await new Promise((r) => setTimeout(r, 2000))
      }
    }
  })()
  return () => ctrl.abort()
}
