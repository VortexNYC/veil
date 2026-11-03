// Copy live Alchemy state into committed CI snapshot (strips rpcToken; hashes secret digests for git).
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { digestForGit } from "./digest-for-git.mjs";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const stage = process.env.ALCHEMY_STAGE ?? `live_${process.env.USER ?? "local"}`;
const src = join(root, ".alchemy/state/VeilRailway", stage);
const dst = join(root, "state/ci");

if (!existsSync(src)) {
  console.error(`Missing local state: ${src} — run deploy locally first.`);
  process.exit(1);
}

mkdirSync(dst, { recursive: true });

const scrub = (value) => {
  if (Array.isArray(value)) {
    for (const item of value) scrub(item);
    return;
  }
  if (value && typeof value === "object") {
    if (typeof value.rpcToken === "string" && value.rpcToken.length >= 32) {
      value.rpcToken = "";
    }
    if (typeof value.digest === "string") {
      value.digest = digestForGit(value.digest);
    }
    for (const v of Object.values(value)) scrub(v);
  }
};

const scrubDoc = (doc) => {
  if (doc?.resourceType === "Railway.Variable" && doc.props?.value?.__redacted__) {
    const d = digestForGit(String(doc.props.value.__redacted__));
    doc.props.value = { __redacted__: d };
    if (doc.attr && typeof doc.attr === "object") doc.attr.digest = d;
  }
};

for (const name of readdirSync(src).filter((n) => n.endsWith(".json"))) {
  const doc = JSON.parse(readFileSync(join(src, name), "utf8"));
  if (doc.attr && typeof doc.attr === "object" && "rpcToken" in doc.attr) {
    doc.attr.rpcToken = "";
  }
  scrubDoc(doc);
  scrub(doc.props);
  if (doc.old?.props) {
    scrubDoc(doc.old);
    scrub(doc.old.props);
  }
  writeFileSync(join(dst, name), `${JSON.stringify(doc, null, 2)}\n`);
}

console.log(`Exported ${readdirSync(dst).filter((n) => n.endsWith(".json")).length} files → state/ci/`);
