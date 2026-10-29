// Devin-powered StepExecutor for e2e — every agent.act/assert step runs
// through SWE-2 Medium via `devin --print` (headless). The step gets the
// current screen observation + instruction; SWE-2 replies with a single
// JSON action or verdict which the executor performs via ctx.actions.

import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { StepExecutor, StepExecutorContext, StepVerdict } from "e2e";

const exec = promisify(execFile);

interface DevinAction {
  do: "tap" | "type" | "typeSecret" | "press" | "scroll" | "wait" | "assert";
  id?: string;
  text?: string;
  secret?: string;
  key?: string;
  dir?: "up" | "down" | "left" | "right";
  ms?: number;
  verdict?: "pass" | "fail" | "blocked";
  reason?: string;
}

async function askDevin(prompt: string, cwd: string): Promise<string> {
  const { stdout } = await exec(
    "devin",
    ["--print", "--model", "swe-2-medium", "--", prompt],
    { cwd, timeout: 180_000, maxBuffer: 4 * 1024 * 1024 },
  );
  return stdout.trim();
}

const PROTOCOL = `You are driving an automated UI test. Reply with EXACTLY one JSON object, no prose:
{"do":"tap","id":"<node id from screen>"}
{"do":"type","id":"<node id>","text":"<text>"}
{"do":"typeSecret","id":"<node id>","secret":"<param name>"} — for credential fields; never type secrets yourself
{"do":"press","id":"<node id>","key":"Enter|Tab|Escape"}
{"do":"scroll","dir":"down|up"}
{"do":"wait","ms":<number>}
{"do":"assert","verdict":"pass"|"fail"|"blocked","reason":"<why>"}
Use node ids exactly as shown in the screen text. Pick "assert" only when the step instruction asks you to assert/verify, or when the goal is already met.`;

const JSON_BLOCK = /\{[\s\S]*\}/;

function verdict(
  status: "passed" | "failed" | "blocked",
  summary: string,
  errorCode?: StepVerdict["errorCode"],
): StepVerdict {
  return errorCode ? { status, summary, errorCode } : { status, summary };
}
// blocked verdicts must carry a framework errorCode.
// Only executor-allowed codes — runtime-assigned codes like
// STEP_BUDGET_EXHAUSTED/STEP_TIMEOUT are reserved and rejected.
const B = {
  model: "MODEL_UNAVAILABLE",
  refused: "MODEL_REFUSED",
  action: "TEST_SETUP_FAILED",
} as const;

export function createDevinExecutor(opts: { cwd?: string; maxTurns?: number } = {}): StepExecutor {
  const cwd = opts.cwd ?? process.cwd();
  const maxTurns = opts.maxTurns ?? 8;
  return {
    name: "devin-swe2",
    async runStep(ctx: StepExecutorContext): Promise<StepVerdict> {
      let obs = (await ctx.observe()).text;
      let lastAction = "none";
      for (let turn = 0; turn < maxTurns; turn++) {
        const secretNames = ctx.step.secrets?.map((s) => s.name).join(", ") || "none";
        const turnNote =
          turn === 0
            ? `Step instruction: ${ctx.step.instruction}\nLast action: ${lastAction}`
            : `Step instruction: ${ctx.step.instruction}\nLast action: ${lastAction}\n\nYour last action CHANGED the screen. If the step's goal is now met — even if the element you acted on is gone — reply {"do":"assert","verdict":"pass","reason":"goal met"}. Only pick another action if the step genuinely needs more doing.`;
        const raw = await askDevin(
          `${PROTOCOL}\nDeclared secrets you may typeSecret (use these EXACT names): ${secretNames}\n\n${turnNote}\n\nScreen:\n${obs}`,
          cwd,
        );
        const json = raw.match(JSON_BLOCK);
        if (!json) return verdict("blocked", `devin replied without JSON: ${raw.slice(0, 300)}`, B.model);
        let act: DevinAction;
        try {
          act = JSON.parse(json[0]);
        } catch {
          return verdict("blocked", `bad JSON from devin: ${raw.slice(0, 300)}`, B.model);
        }
        if (act.do === "assert") {
          if (act.verdict === "pass") return verdict("passed", act.reason ?? "");
          if (act.verdict === "blocked") return verdict("blocked", act.reason ?? "", B.action);
          return verdict("failed", act.reason ?? "assert failed");
        }
        const id = act.id?.replace(/^#/, "") ?? "";
        lastAction = `${act.do} ${id || act.dir || act.key || act.ms || ""}`.trim();
        try {
          switch (act.do) {
            case "tap":
              await ctx.actions.tap({ id });
              break;
            case "type":
              await ctx.actions.type({ id }, act.text ?? "");
              break;
            case "typeSecret":
              await ctx.actions.typeSecret({ id }, act.secret ?? "");
              break;
            case "press":
              await ctx.actions.press({ id }, act.key ?? "Enter");
              break;
            case "scroll":
              await ctx.actions.scroll(act.dir ?? "down");
              break;
            case "wait":
              await new Promise((r) => setTimeout(r, act.ms ?? 1000));
              break;
            default:
              return verdict("blocked", `unknown devin action: ${JSON.stringify(act)}`, B.model);
          }
        } catch (err) {
          return verdict("blocked", `action ${lastAction} failed: ${String(err).slice(0, 200)}`, B.action);
        }
        const newObs = (await ctx.observe()).text;
        if (newObs === obs && act.do !== "wait") {
          const done = await askDevin(
            `${PROTOCOL}\nStep instruction: ${ctx.step.instruction}\nScreen unchanged after ${lastAction}.\nIf the goal is already satisfied reply {"do":"assert","verdict":"pass","reason":"goal already met"}.\nScreen:\n${newObs}`,
            cwd,
          );
          const j = done.match(JSON_BLOCK);
          if (j) {
            try {
              const a = JSON.parse(j[0]) as DevinAction;
              if (a.do === "assert" && a.verdict === "pass") return verdict("passed", a.reason ?? "");
            } catch {
              /* keep going */
            }
          }
        }
        obs = newObs;
      }
      return verdict("blocked", `devin executor exhausted ${maxTurns} turns without a verdict`, B.action);
    },
  };
}
