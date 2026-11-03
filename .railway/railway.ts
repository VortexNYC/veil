// FROZEN — Veil production Railway plane is owned by infra/alchemy/ (Alchemy + Railway).
// Do not `railway config apply` on prod. Historical shape lived here until 2026-11.
import { defineRailway } from "railway/iac";

export default defineRailway(() => {
  throw new Error(
    "Production IaC moved to infra/alchemy — use: cd infra/alchemy && pnpm plan && pnpm run deploy:secrets",
  );
});
