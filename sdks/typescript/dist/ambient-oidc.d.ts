import { type Config } from "./client/index.js";
export type AmbientOidcOptions = {
    request?: {
        headers?: {
            get(name: string): string | null;
        };
    };
    audience?: string;
    /** When true (or a CryptoKeyPair), signs every outbound request with an RFC 9449 ES256 DPoP proof. */
    dpop?: boolean | CryptoKeyPair;
};
export declare function generateDPoPKeyPair(): Promise<CryptoKeyPair>;
export declare function signDPoPProof(opts: {
    keyPair: CryptoKeyPair;
    method: string;
    url: string;
    accessToken?: string;
}): Promise<string>;
/**
 * Resolves an ambient workload OIDC token in priority order:
 * 1. Request `x-vercel-oidc-token` header (Vercel Functions)
 * 2. `VERCEL_OIDC_TOKEN_FILE` (strict read on every call, no fallback if unreadable/empty)
 * 3. `VERCEL_OIDC_TOKEN` (`vercel env pull` / Vercel builds)
 * 4. GitHub Actions OIDC (`ACTIONS_ID_TOKEN_REQUEST_URL` + `ACTIONS_ID_TOKEN_REQUEST_TOKEN`)
 * 5. `VEIL_OIDC_TOKEN_FILE`
 * 6. `VEIL_OIDC_TOKEN`
 */
export declare function ambientOidcToken(opts?: AmbientOidcOptions): Promise<string | undefined>;
export declare function createAmbientClient(config?: Config & AmbientOidcOptions): import("./client").Client;
