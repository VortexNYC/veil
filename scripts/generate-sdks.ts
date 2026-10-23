#!/usr/bin/env -S pnpm exec tsx
import {
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const repoRoot = resolve(import.meta.dirname, "..");
const openApiPath = resolve(repoRoot, "docs/openapi/veil.openapi.json");
const embedPath = resolve(repoRoot, "internal/publicapi/spec.json");
const typeScriptOutput = resolve(repoRoot, "sdks/typescript");
const pythonOutput = resolve(repoRoot, "sdks/python");
const goOutput = resolve(repoRoot, "sdks/go");
const tempRoot = resolve(repoRoot, ".tmp/sdk-generate");
const GO_MODULE_PATH = "github.com/VortexNYC/veil/sdks/go";

const MIT = `MIT License

Copyright (c) 2026 Veil NYC, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`;

function listFiles(path: string): readonly string[] {
  if (!existsSync(path)) {
    return [];
  }
  return readdirSync(path).flatMap((entry) => {
    const childPath = resolve(path, entry);
    if (statSync(childPath).isDirectory()) {
      return listFiles(childPath);
    }
    return [childPath];
  });
}

function specVersion(): string {
  const parsed: unknown = JSON.parse(readFileSync(openApiPath, "utf8"));
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    !("info" in parsed) ||
    typeof parsed.info !== "object" ||
    parsed.info === null ||
    !("version" in parsed.info) ||
    typeof parsed.info.version !== "string"
  ) {
    throw new Error("OpenAPI spec must define info.version");
  }
  return parsed.info.version;
}

function run(name: string, args: string[]): void {
  console.log(`\n${name}`);
  console.log(args.join(" "));
  const result = spawnSync(args[0], args.slice(1), {
    cwd: repoRoot,
    stdio: "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`${name} failed`);
  }
}

function javaOk(): boolean {
  return spawnSync("java", ["-version"], { stdio: "ignore" }).status === 0;
}

function dockerOk(): boolean {
  return spawnSync("docker", ["info"], { stdio: "ignore" }).status === 0;
}

function generatorArgs(kind: "python" | "go", input: string, output: string, version: string): string[] {
  const extra =
    kind === "python"
      ? [
          "--additional-properties",
          `packageName=veil,projectName=vortex-api-veil,packageVersion=${version},generateSourceCodeOnly=true,hideGenerationTimestamp=true`,
        ]
      : [
          "--git-user-id",
          "VortexNYC",
          "--git-repo-id",
          "veil/sdks/go",
          "--additional-properties",
          `packageName=veil,packageVersion=${version},hideGenerationTimestamp=true,structPrefix=true,withGoMod=true`,
        ];
  return [
    "generate",
    "-i",
    input,
    "-g",
    kind,
    "-o",
    output,
    ...extra,
    "--global-property",
    "apiTests=false,modelTests=false",
  ];
}

function generateLang(kind: "python" | "go", version: string): void {
  const temp = resolve(tempRoot, kind);
  mkdirSync(temp, { recursive: true });
  if (javaOk()) {
    run(`Generate ${kind} SDK with OpenAPI Generator`, [
      "pnpm",
      "exec",
      "openapi-generator-cli",
      ...generatorArgs(kind, openApiPath, temp, version),
    ]);
  } else if (dockerOk()) {
    const uid = String(process.getuid?.() ?? 0);
    const gid = String(process.getgid?.() ?? 0);
    run(`Generate ${kind} SDK with OpenAPI Generator (docker)`, [
      "docker",
      "run",
      "--rm",
      "--user",
      `${uid}:${gid}`,
      "-v",
      `${repoRoot}:/local`,
      "openapitools/openapi-generator-cli:v7.23.0",
      ...generatorArgs(
        kind,
        "/local/docs/openapi/veil.openapi.json",
        `/local/.tmp/sdk-generate/${kind}`,
        version
      ),
    ]);
  } else {
    throw new Error(`generate ${kind} SDK: need java or docker. This is not optional.`);
  }
  rmSync(kind === "python" ? pythonOutput : goOutput, { force: true, recursive: true });
  mkdirSync(dirname(kind === "python" ? pythonOutput : goOutput), { recursive: true });
  cpSync(temp, kind === "python" ? pythonOutput : goOutput, { recursive: true });
}

function pruneGeneratedSdkOutputs(): void {
  const noise = [
    resolve(pythonOutput, ".openapi-generator"),
    resolve(pythonOutput, ".openapi-generator-ignore"),
    resolve(pythonOutput, "veil/sdks/go", "docs"),
    resolve(pythonOutput, "veil/sdks/go", "test"),
    resolve(pythonOutput, "veil_README.md"),
    resolve(goOutput, ".openapi-generator"),
    resolve(goOutput, ".openapi-generator-ignore"),
    resolve(goOutput, ".gitignore"),
    resolve(goOutput, ".travis.yml"),
    resolve(goOutput, "docs"),
    resolve(goOutput, "git_push.sh"),
  ];
  for (const path of noise) {
    rmSync(path, { force: true, recursive: true });
  }
}

function rewriteGeneratedTypescriptWithoutEslintDisable(): void {
  for (const path of listFiles(typeScriptOutput)) {
    if (!path.endsWith(".ts")) {
      continue;
    }
    const content = readFileSync(path, "utf8");
    const next = content
      .replace(/^[ \t]*\/\/ eslint-disable-next-line.*\n/gm, "")
      .replace(/^[ \t]*\/\* eslint-disable.*\*\/\n/gm, "")
      .replace("export interface ClientMeta {}", "export type ClientMeta = Record<never, never>;");
    if (next !== content) {
      writeFileSync(path, next);
    }
  }
}

function appendTypeScriptRootExports(): void {
  const ambientPath = resolve(typeScriptOutput, "ambient-oidc.ts");
  writeFileSync(
    ambientPath,
    `import { createClient, type Config } from "./client";

export type AmbientOidcOptions = {
  request?: { headers?: { get(name: string): string | null } };
  audience?: string;
  /** When true (or a CryptoKeyPair), signs every outbound request with an RFC 9449 ES256 DPoP proof. */
  dpop?: boolean | CryptoKeyPair;
};

function b64url(bytes: Uint8Array): string {
  let bin = "";
  for (const b of bytes) {
    bin += String.fromCharCode(b);
  }
  return btoa(bin).replace(/\\+/g, "-").replace(/\\//g, "_").replace(/=+$/, "");
}

function b64urlJson(obj: unknown): string {
  return b64url(new TextEncoder().encode(JSON.stringify(obj)));
}

export async function generateDPoPKeyPair(): Promise<CryptoKeyPair> {
  return (await crypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" },
    false,
    ["sign", "verify"]
  )) as CryptoKeyPair;
}

export async function signDPoPProof(opts: {
  keyPair: CryptoKeyPair;
  method: string;
  url: string;
  accessToken?: string;
}): Promise<string> {
  const pubJwk = await crypto.subtle.exportKey("jwk", opts.keyPair.publicKey);
  const parsed = new URL(opts.url);
  const htu = \`\${parsed.protocol}//\${parsed.host}\${parsed.pathname}\`;
  const payload: Record<string, unknown> = {
    htm: opts.method.toUpperCase(),
    htu,
    iat: Math.floor(Date.now() / 1000),
    jti: crypto.randomUUID(),
  };
  if (opts.accessToken) {
    const hash = await crypto.subtle.digest(
      "SHA-256",
      new TextEncoder().encode(opts.accessToken)
    );
    payload.ath = b64url(new Uint8Array(hash));
  }
  const header = {
    typ: "dpop+jwt",
    alg: "ES256",
    jwk: { kty: "EC", crv: "P-256", x: pubJwk.x, y: pubJwk.y },
  };
  const signingInput = \`\${b64urlJson(header)}.\${b64urlJson(payload)}\`;
  const sig = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    opts.keyPair.privateKey,
    new TextEncoder().encode(signingInput)
  );
  return \`\${signingInput}.\${b64url(new Uint8Array(sig))}\`;
}

type NodeProcess = {
  env?: Record<string, string | undefined>;
  getBuiltinModule?: (id: string) => { readFileSync?: (path: string, enc: string) => string } | undefined;
};

function getProc(): NodeProcess | undefined {
  const g = globalThis as { process?: NodeProcess };
  return g.process;
}

function readTokenFile(path: string): string {
  const fs = getProc()?.getBuiltinModule?.("node:fs");
  if (!fs?.readFileSync) {
    throw new Error("ambientOidcToken: file reading requires a Node.js runtime");
  }
  const value = fs.readFileSync(path, "utf8").trim();
  if (!value) {
    throw new Error(\`ambientOidcToken: empty token file \${path}\`);
  }
  return value;
}

/**
 * Resolves an ambient workload OIDC token in priority order:
 * 1. Request \`x-vercel-oidc-token\` header (Vercel Functions)
 * 2. \`VERCEL_OIDC_TOKEN_FILE\` (strict read on every call, no fallback if unreadable/empty)
 * 3. \`VERCEL_OIDC_TOKEN\` (\`vercel env pull\` / Vercel builds)
 * 4. GitHub Actions OIDC (\`ACTIONS_ID_TOKEN_REQUEST_URL\` + \`ACTIONS_ID_TOKEN_REQUEST_TOKEN\`)
 * 5. \`VEIL_OIDC_TOKEN_FILE\`
 * 6. \`VEIL_OIDC_TOKEN\`
 */
export async function ambientOidcToken(opts?: AmbientOidcOptions): Promise<string | undefined> {
  const headerToken = opts?.request?.headers?.get("x-vercel-oidc-token")?.trim();
  if (headerToken) {
    return headerToken;
  }
  const env = getProc()?.env ?? {};
  const vercelFile = env.VERCEL_OIDC_TOKEN_FILE?.trim();
  if (vercelFile) {
    return readTokenFile(vercelFile);
  }
  const vercelToken = env.VERCEL_OIDC_TOKEN?.trim();
  if (vercelToken) {
    return vercelToken;
  }
  const actionsUrl = env.ACTIONS_ID_TOKEN_REQUEST_URL?.trim();
  const actionsBearer = env.ACTIONS_ID_TOKEN_REQUEST_TOKEN?.trim();
  if (actionsUrl && actionsBearer) {
    const aud = encodeURIComponent(opts?.audience ?? "veil");
    const sep = actionsUrl.includes("?") ? "&" : "?";
    const res = await fetch(\`\${actionsUrl}\${sep}audience=\${aud}\`, {
      headers: { Authorization: \`bearer \${actionsBearer}\` },
    });
    if (!res.ok) {
      throw new Error(\`ambientOidcToken: GitHub Actions OIDC request failed (\${res.status})\`);
    }
    const data = (await res.json()) as { value?: string };
    const tok = data.value?.trim();
    if (!tok) {
      throw new Error("ambientOidcToken: empty GitHub Actions OIDC token");
    }
    return tok;
  }
  const veilFile = env.VEIL_OIDC_TOKEN_FILE?.trim();
  if (veilFile) {
    return readTokenFile(veilFile);
  }
  const veilToken = env.VEIL_OIDC_TOKEN?.trim();
  if (veilToken) {
    return veilToken;
  }
  return undefined;
}

export function createAmbientClient(config?: Config & AmbientOidcOptions) {
  let keyPairPromise: Promise<CryptoKeyPair> | undefined;
  const resolveKeyPair = (): Promise<CryptoKeyPair> => {
    if (config?.dpop && typeof config.dpop === "object") {
      return Promise.resolve(config.dpop);
    }
    keyPairPromise ??= generateDPoPKeyPair();
    return keyPairPromise;
  };
  const baseFetch = config?.fetch ?? globalThis.fetch;
  const dpopFetch: typeof fetch | undefined = config?.dpop
    ? async (input, init) => {
        const req = new Request(input, init);
        const auth = req.headers.get("Authorization") ?? "";
        const tok = auth.replace(/^(Bearer|DPoP)\\s+/i, "").trim();
        const kp = await resolveKeyPair();
        const proof = await signDPoPProof({
          keyPair: kp,
          method: req.method,
          url: req.url,
          accessToken: tok || undefined,
        });
        const headers = new Headers(req.headers);
        if (tok) {
          headers.set("Authorization", \`DPoP \${tok}\`);
        }
        headers.set("DPoP", proof);
        return baseFetch(new Request(req, { headers }));
      }
    : config?.fetch;
  return createClient({
    baseUrl: "https://veil.nyc",
    ...config,
    ...(dpopFetch ? { fetch: dpopFetch } : {}),
    auth: async () => (await ambientOidcToken(config)) ?? "",
  });
}
`
  );
  const indexPath = resolve(typeScriptOutput, "index.ts");
  const content = readFileSync(indexPath, "utf8");
  const extra =
    "export { createClient } from './client';\n" +
    "export type { Config } from './client';\n" +
    "export { ambientOidcToken, createAmbientClient, generateDPoPKeyPair, signDPoPProof } from './ambient-oidc';\n" +
    "export type { AmbientOidcOptions } from './ambient-oidc';\n";
  if (content.includes("export { createClient } from './client';")) {
    return;
  }
  writeFileSync(indexPath, `${content.replace(/\n+$/u, "")}\n${extra}`);
}

function buildTypeScriptDist(): void {
  const dist = resolve(typeScriptOutput, "dist");
  rmSync(dist, { force: true, recursive: true });
  run("Bundle TypeScript SDK runtime", [
    "pnpm",
    "exec",
    "esbuild",
    resolve(typeScriptOutput, "index.ts"),
    "--bundle",
    "--format=esm",
    "--platform=neutral",
    `--outfile=${resolve(dist, "index.js")}`,
  ]);
  run("Emit TypeScript SDK declarations", [
    "pnpm",
    "exec",
    "tsc",
    "--emitDeclarationOnly",
    "--declaration",
    "--skipLibCheck",
    "--module",
    "esnext",
    "--moduleResolution",
    "bundler",
    "--target",
    "esnext",
    "--outDir",
    dist,
    resolve(typeScriptOutput, "index.ts"),
  ]);
  for (const path of listFiles(dist)) {
    if (!path.endsWith(".d.ts")) {
      continue;
    }
    const content = readFileSync(path, "utf8");
    const next = content.replace(
      /from\s+(['"])(\.{1,2}\/[^'"]+)\1/g,
      (match, quote: string, specifier: string) => {
        if (specifier.endsWith(".js") || specifier.endsWith(".json")) {
          return match;
        }
        const base = resolve(dirname(path), specifier);
        const target = existsSync(`${base}.d.ts`)
          ? `${specifier}.js`
          : `${specifier}/index.js`;
        return `from ${quote}${target}${quote}`;
      }
    );
    if (next !== content) {
      writeFileSync(path, next);
    }
  }
}

function writePackageMetadata(version: string): void {
  writeFileSync(resolve(typeScriptOutput, "LICENSE"), MIT);
  writeFileSync(
    resolve(typeScriptOutput, "package.json"),
    `${JSON.stringify(
      {
        name: "@vortex-api/veil",
        version,
        description: "Generated TypeScript SDK for Veil. Use injects. Never GetSecret.",
        license: "MIT",
        type: "module",
        sideEffects: false,
        main: "./dist/index.js",
        types: "./dist/index.d.ts",
        exports: {
          ".": {
            types: "./dist/index.d.ts",
            import: "./dist/index.js",
            default: "./dist/index.js",
          },
        },
        files: ["dist"],
        publishConfig: {
          access: "public",
          registry: "https://registry.npmjs.org",
        },
        repository: {
          type: "git",
          url: "git+https://github.com/VortexNYC/veil.git",
          directory: "sdks/typescript",
        },
      },
      null,
      2
    )}\n`
  );
  writeFileSync(
    resolve(typeScriptOutput, "README.md"),
    `# @vortex-api/veil

Generated TypeScript SDK for the Veil public API.

This package is generated from \`docs/openapi/veil.openapi.json\` by \`pnpm run sdk:generate\`.
It is MIT-licensed and published to the public npm registry.

\`\`\`bash
pnpm add @vortex-api/veil
\`\`\`

\`\`\`ts
import { createClient, listItems, useItem } from "@vortex-api/veil";

const client = createClient({
  baseUrl: "https://veil.nyc",
  headers: { Authorization: \`Bearer \${process.env.VEIL_OIDC_TOKEN}\` },
});

const { data } = await listItems({ client });
await useItem({
  client,
  body: { item: "stripe", url: "https://api.stripe.com/v1/customers", method: "POST" },
});
\`\`\`

The vault secret is never in the response.
`
  );

  writeFileSync(resolve(pythonOutput, "LICENSE"), MIT);
  writeFileSync(
    resolve(pythonOutput, "pyproject.toml"),
    `[project]
name = "vortex-api-veil"
version = "${version}"
description = "Generated Python SDK for Veil. Use injects. Never GetSecret."
readme = "README.md"
license = { text = "MIT" }
requires-python = ">=3.9"
dependencies = [
  "urllib3 >= 2.1.0, < 3.0.0",
  "python-dateutil >= 2.8.2",
  "pydantic >= 2",
  "typing-extensions >= 4.7.1",
]

[build-system]
requires = ["setuptools>=61.0"]
build-backend = "setuptools.build_meta"

[tool.setuptools.packages.find]
include = ["veil*"]
`
  );
  writeFileSync(
    resolve(pythonOutput, "README.md"),
    `# vortex-api-veil

Generated from \`docs/openapi/veil.openapi.json\`. Do not handwrite clients.

\`\`\`bash
pip install vortex-api-veil
\`\`\`

\`\`\`py
import os
import veil

configuration = veil.Configuration(host="https://veil.nyc")
configuration.access_token = os.environ["VEIL_OIDC_TOKEN"]
client = veil.ApiClient(configuration)
api = veil.AgentApi(client)
items = api.list_items()
api.use_item(veil.UseRequest(item="stripe", url="https://api.stripe.com/v1/customers"))
\`\`\`

The vault secret is never in the response.
`
  );

  writeFileSync(resolve(goOutput, "LICENSE"), MIT);
  writeFileSync(
    resolve(goOutput, "README.md"),
    `# Veil Go SDK

Generated from \`docs/openapi/veil.openapi.json\`. Do not handwrite clients.

The public module is \`${GO_MODULE_PATH}\`.

\`\`\`bash
go get ${GO_MODULE_PATH}
\`\`\`

\`\`\`go
package main

import (
	"context"
	"os"

	veil "${GO_MODULE_PATH}"
)

func main() {
	cfg := veil.NewConfiguration()
	cfg.Host = "veil.nyc"
	cfg.Scheme = "https"
	cfg.AddDefaultHeader("Authorization", "Bearer "+os.Getenv("VEIL_OIDC_TOKEN"))
	client := veil.NewAPIClient(cfg)
	_, _, _ = client.AgentAPI.ListItems(context.Background()).Execute()
}
\`\`\`

The vault secret is never in the response.
`
  );
}

function assertNoGetSecret(): void {
  // Ban a GetSecret *operation*. OpenAPI Generator names CreateItemRequest
  // field accessors GetSecret / get_secret; those are not the product.
  const symbol = /\b(GetSecret|getSecret|get_secret)\s*\(/;
  const api = /(?:^|\/)(?:api_[^/]+\.go|sdk\.gen\.ts|[^/]+_api\.py)$/;
  for (const root of [typeScriptOutput, pythonOutput, goOutput]) {
    for (const path of listFiles(root)) {
      if (!api.test(path.replaceAll("\\", "/"))) {
        continue;
      }
      if (symbol.test(readFileSync(path, "utf8"))) {
        throw new Error(`${relative(repoRoot, path)} grew GetSecret`);
      }
    }
  }
}

rmSync(tempRoot, { force: true, recursive: true });
mkdirSync(tempRoot, { recursive: true });
mkdirSync(dirname(embedPath), { recursive: true });
cpSync(openApiPath, embedPath);

const version = specVersion();
const tsTemp = resolve(tempRoot, "typescript");

run("Generate TypeScript SDK with Hey API", [
  "pnpm",
  "exec",
  "openapi-ts",
  "--input",
  openApiPath,
  "--output",
  tsTemp,
  "--client",
  "@hey-api/client-fetch",
  "--plugins",
  "@hey-api/sdk",
]);

rmSync(typeScriptOutput, { force: true, recursive: true });
mkdirSync(typeScriptOutput, { recursive: true });
cpSync(tsTemp, typeScriptOutput, { recursive: true });

generateLang("python", version);
generateLang("go", version);
pruneGeneratedSdkOutputs();
rewriteGeneratedTypescriptWithoutEslintDisable();
appendTypeScriptRootExports();
writePackageMetadata(version);
assertNoGetSecret();

run("Format generated SDK outputs", [
  "pnpm",
  "exec",
  "vp",
  "fmt",
  "--no-error-on-unmatched-pattern",
  typeScriptOutput,
  pythonOutput,
  goOutput,
]);

buildTypeScriptDist();

rmSync(tempRoot, { force: true, recursive: true });
console.log("sdk:generate ok");
