# Open-source boundary — the free/hosted split

Status: **DECIDED 2026-10-01** (VEIL-85). Audience: the operator before the
repo flips public, then every agent deciding where new code lives.

The product is Veil. The company is Vortex. The question this doc answers:
which bytes leave this repo under an open license, which under a commercial
one, and what enforces the difference.

## Prior art — what each actually does

### Bitwarden

Two licenses per repo, keyed by directory, plus a license file that gates
paid features even on self-hosted servers.

- `bitwarden/server`: default **AGPL-3.0**; code under `/bitwarden_license`
  is under the **Bitwarden License v1.0** — a source-available license that
  permits internal development and testing only, in non-production
  environments, forbids redistribution and competing services, and reserves
  all trademarks.
  ([LICENSE.txt](https://github.com/bitwarden/server/blob/main/LICENSE.txt),
  [LICENSE_BITWARDEN.txt](https://github.com/bitwarden/server/blob/main/LICENSE_BITWARDEN.txt))
- `bitwarden/clients`: same shape, default **GPL-3.0** for desktop, browser,
  CLI, mobile. ([LICENSE.txt](https://github.com/bitwarden/clients/blob/main/LICENSE.txt))
- Self-hosting is free, but premium and organization features unlock only by
  uploading a license file downloaded from a paid cloud account, bound to an
  installation ID. The license must be refreshed within 60 days of renewal or
  the self-hosted organization is disabled; terms of service permit one
  organization deployment per subscription.
  ([bitwarden.com/help/licensing-on-premise](https://bitwarden.com/help/licensing-on-premise/))
- Trademarks are explicitly not granted; a `TRADEMARK_GUIDELINES.md` sits in
  the server repo.

### Infisical

One repo, MIT core, `ee/` subtrees under a source-available enterprise
license, key-gated.

- Root LICENSE: **MIT Expat** for everything except `ee/` directories, which
  carry their own license. ([LICENSE](https://github.com/Infisical/infisical/blob/main/LICENSE))
- `backend/src/ee/LICENSE.md`: the **Infisical Enterprise License** —
  production use requires a valid enterprise subscription for the correct
  seat count; dev/test copy-and-modify is free; no sublicense, distribution,
  or sale. ([LICENSE.md](https://github.com/Infisical/infisical/blob/main/backend/src/ee/LICENSE.md))
- Enforcement: `LICENSE_KEY` env var validated against Infisical's license
  server, or an offline key format that self-validates. On expiry the
  instance keeps running; EE features disable until renewal.
  ([docs/self-hosting/ee](https://infisical.mintlify.dev/docs/self-hosting/ee))

### 1Password

The counterexample: fully closed. Their own community post states
"1Password isn't open-source" and substitutes third-party audits,
penetration testing, and a bug bounty for source transparency; only
libraries (passkey lib, Electron hardener) are published.
([1Password community](https://www.1password.community/cybersecurity-glossary-67/open-source-security-24725))

### The pattern

All three converge: **open core + directory-scoped license + server-side or
key-based entitlement for paid surfaces, trademarks always reserved.** The
license file mechanism — not the code's visibility — is what makes a
self-hosted feature paid. Nobody relies on secrecy of the check.

## The decision — the Veil split

Founder instinct stands: **broker + CLI are the open core** (the adoption
play — anyone can run Veil locally for their own passwords); **extensions,
mobile, the SPA vault, and hosted identity/billing are the paid surface.**

Two licenses, keyed by directory, Bitwarden/Infisical-shaped:

1. **Apache-2.0** — the community tree. Chosen over the current root MIT
   for two concrete reasons: an express patent grant, and §6's explicit
   non-grant of trademarks, which pairs with the trademark policy below.
   It also matches our pinned ecosystem (Ory is Apache-2.0). At launch the
   root `LICENSE` becomes an Infisical-style preamble: `ee/` directories
   under `ee/LICENSE`, everything else Apache-2.0.
2. **Veil Commercial License v1 (VCL-1)** — `ee/` subtrees. Drafted from the
   Bitwarden License v1.0 / Infisical Enterprise License pattern: internal
   development and testing free; **production use requires a valid
   subscription or license key** for the correct seats; no redistribution,
   no sublicense, no competing service; trademarks reserved. Source stays
   visible — a credential product earns trust by being auditable — while
   the license, not secrecy, makes it paid.

### Per-component table

| Surface | Path(s) | License | Self-hostable | Hosted-only |
|---|---|---|---|---|
| Broker core (origin HTTP, items, grants, Use/Approve, inject, MITM proxy, passkeys, audit, store) | `internal/{broker,store,crypto,grant,inject,proxy,audit,passkey,protocol,material,device,publicapi,human,oidchttp,workload,id,scrub,app}` | Apache-2.0 | yes | — |
| CLI + agent surfaces (MCP server, local socket, SSH agent, TOTP enroll, generate, confirm) | `cmd/veil`, `internal/{cli,mcpserver,socket,sshagent,totpenroll,passgen,confirm}` | Apache-2.0 | yes | — |
| 1Password importer | `internal/oneimport` | Apache-2.0 | yes | — |
| SDKs + API contract | `sdks/{go,python,typescript}`, `docs/openapi`, `openapitools.json` | Apache-2.0 | yes | — |
| Fill native host + test tooling | `cmd/filldist`, `apps/fill/dist.go` plumbing, `internal/livetest`, `cmd/{diag,loadtest}`, `internal/testutil` | Apache-2.0 | yes | — |
| Identity wiring (glue clients, identity-glue, Ory images' compose/config, Ory Elements login UI) | `identity/`, `cmd/identity-glue` | Apache-2.0 | yes — run your own Kratos/Hydra/Keto via `identity/compose.yml` | we run ours at `*.veil.nyc` |
| Backup / DR tooling (pg backup + WAL image, vault replica, drill-restore, R2 ingest worker) | `Dockerfile.{backup,wal}`, `cmd/drillrestore`, `internal/replica`, `apps/backup-ingest`, `docs/{backup-restore,disaster-recovery}.md` | Apache-2.0 | yes — "you can always leave" is a feature | — |
| Mail relay worker (Kratos recovery/invite email) | `apps/mail` | Apache-2.0; reference implementation, self-hosters wire their own SMTP | yes | `mail.veil.nyc` is ours |
| Docs site | `apps/docs`, `docs/` content | Apache-2.0 code, CC BY-4.0 content | yes | — |
| Landing site | `apps/site` | Apache-2.0 code; brand assets/logo all rights reserved | n/a | `veil.nyc` |
| Vortex billing plane (usage counters, webhook, checkout) | `internal/billing` | Apache-2.0 — metering stays inspectable ("we count Uses, nothing else"); dead code without Vortex creds | compiles, inert self-host | enforcement lives in `org_billing.plan` |
| **SPA vault** | `apps/vault` → `ee/vault` | VCL-1 | dev/test only without key | `app.veil.nyc` |
| **Browser extension** (Chrome/Edge/Firefox + Safari web extension) | `apps/fill` → `ee/fill`, `apps/fill-safari` → `ee/fill-safari` | VCL-1 | dev/test only; store builds ship from us | CWS/AMO/App Store listings under Vortex |
| **Mobile app** | `apps/phone` → `ee/phone` | VCL-1 | dev/test only | App Store / Play under Vortex |
| Hosted deployments (Railway services, Cloudflare Worker bindings, Vortex config) | not code — ops state | — | n/a | yes, by definition |

Conventions the table commits us to:

- **Boundary direction.** Apache code never imports `ee/`; `ee/` may import
  Apache code. `go build ./...` on the community tree must succeed with
  `ee/` deleted — CI gets a community-only build job enforcing it (the
  `internal/billing` choice above exists partly so the broker binary keeps
  this property: the meter compiles everywhere and only reports when
  `VEIL_VORTEX_*` is set).
- **Moves at launch.** `apps/{vault,fill,fill-safari,phone}` move under
  `ee/` when the repo goes public — one rename slice, not this doc.
- **New code asks the question.** Anything that only makes sense against
  our hosted plane lands in `ee/`. Anything a laptop user runs lands
  Apache. When unsure: the paid surface is *distribution and hosting*, not
  the protocol.

### What a self-hoster gives up

Self-hosting is welcome and real — broker, CLI, MCP, SDK, importer, the Ory
compose stack, backups — but bounded:

- **Managed identity.** They run Kratos/Hydra/Keto and the login UI
  themselves. `id.veil.nyc` mints nothing for their origin. No SLA.
- **The human surfaces.** SPA vault, extensions, mobile are VCL — free to
  read, modify, and test; production use needs a paid plan or a self-host
  license key.
- **Hosted conveniences.** `veil.nyc/mcp`, `app.veil.nyc`, `login.veil.nyc`,
  the mail relay, managed backups, checkout, support, update cadence.
- **Mail.** Their own SMTP/provider; `apps/mail` is the reference, not a
  service.

## Enforcement

1. **Account entitlement (hosted).** The paid surfaces are thin clients —
   the extension, vault, and phone app all authenticate to an origin. Hosted
   gating is `org_billing.plan` flipped by Vortex webhooks plus the local
   `VEIL_FREE_USE_CAP` claim — already written (`internal/billing`,
   `docs/billing.md`, VEIL-65/67). No new machinery.
2. **License key (self-host).** `VEIL_LICENSE_KEY`, Infisical's offline
   pattern: an ed25519-signed token (org ID, seats, expiry, feature flags)
   verified locally — the `Use` path never depends on a license server.
   Minted by Vortex/billing admin tooling. Expired key = VCL features
   disabled, broker keeps running.
3. **Trademark.** `TRADEMARK.md` at root, modeled on Bitwarden's
   `TRADEMARK_GUIDELINES.md`. The Veil name, `veil.nyc` and subdomains,
   icons, and every store listing (CWS item `cfkdimikgngipjiiklmnoagnanpkapcn`,
   AMO, Apple team `VFWGNKKT4G` — see `docs/store-listing.md`) stay Vortex's.
   Apache-2.0 §6 already withholds trademark rights; forks must rebrand and
   repoint at their own origin.
4. **Distribution.** Store developer accounts are ours; builds are signed
   by us. The extension being a thin client means a rebranded fork still
   can't see anyone's vault — origin auth decides what it can reach.

## Risks

- **AGPL contamination, inbound.** `bitwarden/server` is AGPL-3.0 — prior
  art only, never vendored (AGENTS.md rule 5 stands). But dependencies are
  the real hole: a (A)GPL module linked into the CLI, extension, or broker
  binary creates disclosure obligations at distribution; AGPL in the broker
  puts our own hosted origin under §13 source-offer duty. Add a license
  scan (`go-licenses` for Go, `license-checker` for pnpm) to `make ci`
  before the public flip.
- **AGPL, outbound — rejected for us.** AGPL on the broker would satisfy
  nothing the VCL doesn't already do, scares exactly the adopters the open
  core is for, and obligates *us* to offer source to every `veil.nyc`
  network user. Apache-2.0 for the core.
- **Honor system.** License-key checks are patchable — true for Bitwarden
  and Infisical too; their answer is ToS ("one organization deployment per
  subscription") plus the reality that the money is in hosting and
  distribution, not in a bit-flip. Ours is the same. Do not architect the
  check as if it were a vault boundary.
- **Extension-store friction.** Chrome Web Store + MV3: no remote code, and
  reviewers flag undisclosed account/paywall requirements — the listing
  says "requires a Veil account," entitlement changes happen server-side at
  the origin API, never by remotely toggling shipped code. Apple 3.1.1: no
  purchase link or checkout inside the phone app; accounts are bought on
  the web (multiplatform-service posture), the app just authenticates.
- **Forks.** A rebranded Apache fork of broker+CLI is allowed and fine —
   Vaultwarden exists for Bitwarden too. The moat is the hosted plane, the
   paid surfaces' VCL terms, the trademarks, and the stores — none of which
   a fork gets.
- **Inbound rights.** Apache inbound=outbound keeps contributions simple;
   `CONTRIBUTING.md` gets a DCO sign-off requirement at public flip. If we
   ever need relicensing freedom, that becomes a CLA conversation — don't
   pay for it now.
- **Repo hygiene at flip.** Git history must be scrubbed for secrets
  before public, and internal-only docs (runbooks with live hostnames,
  this file's store IDs) get reviewed — some belong in a private tree.

## Sources

- Bitwarden server license split: <https://github.com/bitwarden/server/blob/main/LICENSE.txt>
- Bitwarden License v1.0 text: <https://github.com/bitwarden/server/blob/main/LICENSE_BITWARDEN.txt>
- Bitwarden clients license split: <https://github.com/bitwarden/clients/blob/main/LICENSE.txt>
- Bitwarden self-host license files: <https://bitwarden.com/help/licensing-on-premise/>
- Infisical root license (MIT except `ee/`): <https://github.com/Infisical/infisical/blob/main/LICENSE>
- Infisical Enterprise License: <https://github.com/Infisical/infisical/blob/main/backend/src/ee/LICENSE.md>
- Infisical EE activation (`LICENSE_KEY`, offline keys): <https://infisical.mintlify.dev/docs/self-hosting/ee>
- 1Password on open source: <https://www.1password.community/cybersecurity-glossary-67/open-source-security-24725>
