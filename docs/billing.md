# Billing — Vortex integration

Veil is a billing customer of Vortex, not a billing platform. Enforcement
stays local — the `Use` path never depends on Vortex being reachable — while
Vortex owns the customer record, subscriptions, checkout, and (post-VOR-580)
the event stream that flips org plan state.

## Architecture

```
signup ──▶ POST /v1/customers (externalCustomerRef = orgID)  ──▶ org_billing.customer_id
Use    ──▶ local usage_counters claim (VEIL_FREE_USE_CAP)    ──▶ payment_required over cap
Vortex ──▶ POST /v1/billing/webhook (Vortex-Signature HMAC)  ──▶ org_billing.plan flips
owner  ──▶ GET /v1/billing                                   ──▶ vault cap banner + upgrade_url
```

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
| `VEIL_BILLING_UPGRADE_URL` | interim upgrade link until checkout sessions land (VOR-577) |

## Failure posture

- Vortex down at signup → `billing_provision_failed` audit row; the org
  provisions anyway. Orgs with that audit action are the reconcile set.
- Webhook silence (pre-VOR-580) → orgs stay on their last plan; free tier and
  cap enforcement are fully local and unaffected.
- Over-cap attempts still increment `usage_counters` — blocked demand is
  signal.
