# Billing — Vortex integration

Veil is a billing customer of Vortex, not a billing platform. Enforcement
stays local — the `Use` path never depends on Vortex being reachable — while
Vortex owns the customer record, subscriptions, checkout, and (post-VOR-580)
the event stream that flips org plan state.

## Architecture

```
signup ──▶ POST /v1/customers (externalCustomerRef = orgID)  ──▶ org_billing.customer_id
       ──▶ POST /v1/customers/:id/billing-accounts            ──▶ org_billing.billing_account_id
Use    ──▶ local usage_counters claim (VEIL_FREE_USE_CAP)    ──▶ payment_required over cap
tick   ──▶ POST /v1/usage-events (delta = used − reported)   ──▶ usage_counters.reported
Vortex ──▶ POST /v1/billing/webhook (Vortex-Signature HMAC)  ──▶ org_billing.plan flips
owner  ──▶ GET /v1/billing                                   ──▶ vault cap banner + upgrade_url
owner  ──▶ POST /v1/billing/checkout                         ──▶ {checkout_url} hosted upgrade
```

**Upgrade checkout (VEIL-67).** `POST /v1/billing/checkout` (owner-only)
lazily provisions the billing link — same `ensureBillingLink` path the
flusher uses — then composes a hosted Vortex subscription checkout against
`VEIL_VORTEX_PRICE_ID` and returns `checkout_url`. Each click mints a fresh
session — and must: Vortex stores checkout tokens hash-only, so GET/replay
can never reconstruct `checkoutUrl` (VOR-613, resolved by design). Fresh
keys per call are the correct pattern, not a workaround — sessions are
cheap; a merchant needing a durable link persists the URL or mints anew. Payment lands as
`subscription.created`/`entitlement.granted` webhooks → plan flips → the
cap lifts. `VEIL_VORTEX_PRICE_ID` unset → endpoint 404s (checkout off, the
rest of the billing plane unaffected).

**Usage dual-write (VEIL-65).** `usage_counters.reported` is the watermark of
units already sent to Vortex; a once-a-minute flusher posts each pending
delta as one `usage-events` row and advances the mark only on success. The
idempotency key is deterministic per delta (`veil-usage-<org>-<window>-
<target watermark>`) so a send that lands but fails to mark re-sends as an
upstream no-op — at-least-once without double counting. Orgs missing the
billing link get it provisioned lazily on flush (covers pre-billing orgs and
provision-time outages). Unset `VEIL_VORTEX_METER_ID` = reporting off; the
webhook receiver and provisioning don't depend on it.

Environment semantics: `VEIL_VORTEX_ENV` is `sandbox` or `production`, matching
the Vortex environment the merchant account lives in.

## One-time merchant bootstrap (per VOR-573 — no Finix underwriting for
first-party merchants)

1. **Org + merchant key.** `POST /v1/signup` → org + `vp_` merchant-scoped key
   (`payments:["merchant"]`). Or ops creates the org.
2. **Operator key.** Session-authed `POST /api-key/create` with
   `permissions.payments = ["operator"]` (2FA-gated). Ops mints this for the
   Veil org — it is the only step that needs a human session.
3. **Activate the merchant.** `PUT /v1/merchant-accounts/:id` with
   `status:"active"` (operator key), then
   `POST /v1/merchant-accounts/:id/can-accept-payments`
   `{environment, canAcceptPayments: true}`.
4. **Webhook endpoint.** `POST /v1/webhook-endpoints` with
   `destinationUrl: https://veil.nyc/v1/billing/webhook` and
   `subscribedEventTypes` covering `subscription.*`, `entitlement.*`,
   `billing_state.*`. Response carries the `whsec_` signing secret exactly
   once → `VEIL_BILLING_WEBHOOK_SECRET`.
5. **Catalog.** Product + prices: a `free`-type price (the free tier) and the
   paid price. `metadata.entitlementKeys: ["veil"]` on the product/price —
   subscriptions auto-grant the `veil` entitlement, and
   `entitlement.granted/revoked` webhooks are the primary access signal.

## Env reference

| Var | Source |
|---|---|
| `VEIL_VORTEX_API_URL` | `https://api.sandbox.vortex.nyc` / `https://api.vortex.nyc` |
| `VEIL_VORTEX_API_KEY` | `vp_` merchant key from bootstrap step 1 |
| `VEIL_VORTEX_MERCHANT_ID` | merchant account id from step 3 |
| `VEIL_VORTEX_ENV` | `sandbox` \| `production` |
| `VEIL_BILLING_WEBHOOK_SECRET` | `whsec_` from step 4 — unset = webhook endpoint 404s |
| `VEIL_FREE_USE_CAP` | free-tier monthly use allowance; 0/unset = metering off |
| `VEIL_VORTEX_METER_ID` | meter usage deltas report against (`mtr_…`); unset = dual-write off |
| `VEIL_VORTEX_USAGE_EVENT` | event name on usage rows; default `credential_use` |
| `VEIL_VORTEX_PRICE_ID` | paid-plan catalog price (`veil-pro-monthly`); unset = checkout endpoint 404s |
| `VEIL_CHECKOUT_SUCCESS_URL` | post-payment return target; optional |
| `VEIL_CHECKOUT_CANCEL_URL` | checkout-abandon return target; optional |
| `VEIL_BILLING_UPGRADE_URL` | legacy static link in `GET /v1/billing`; superseded by `POST /v1/billing/checkout` |

## Failure posture

- Vortex down at signup → `billing_provision_failed` audit row; the org
  provisions anyway. Orgs with that audit action are the reconcile set.
- Webhook silence (pre-VOR-580) → orgs stay on their last plan; free tier and
  cap enforcement are fully local and unaffected.
- Over-cap attempts still increment `usage_counters` — blocked demand is
  signal.
- Usage flusher outage → deltas stay pending on `reported`; nothing is lost,
  the next tick resumes where the watermark stopped.
