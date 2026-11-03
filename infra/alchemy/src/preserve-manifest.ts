/**
 * Service-scoped env var names that `.railway/railway.ts` still lists as preserve().
 * Migration target: `Railway.Variable` per name (or shared + Railway.ref).
 * Do not put values here — adopt reads live Railway vars (digest in state only).
 */
export const preserveManifest = {} as const satisfies Record<string, readonly string[]>;

export type PreserveService = keyof typeof preserveManifest;
