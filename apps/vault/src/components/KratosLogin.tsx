// Custom Kratos login renderer — drives flow.ui.nodes directly instead of
// Ory Elements, so every field/button/state is ours (Kumo dark). Handles
// password (identifier+secret), totp, and lookup_secret. The form POSTs to
// flow.ui.action; errors land back as flow.ui.messages.

import { useMemo, useState } from "react"
import type { LoginFlow, UiNode } from "@ory/client-fetch"

const inputCls =
  "w-full rounded-xl border border-zinc-600 bg-zinc-900 px-4 py-2.5 text-[15px] text-zinc-100 placeholder-zinc-500 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/30"
const labelCls = "text-sm font-medium text-zinc-300"
const btnCls =
  "w-full rounded-xl bg-blue-600 px-4 py-2.5 text-[15px] font-semibold text-white transition hover:bg-blue-500 disabled:opacity-50"
const linkCls = "text-sm text-blue-400 hover:text-blue-300"

type InputAttr = { node_type: "input"; name?: string; value?: unknown; type?: string }
const asInput = (n: UiNode): InputAttr | null =>
  n.attributes.node_type === "input" ? (n.attributes as InputAttr) : null

function hiddenInputs(nodes: UiNode[]): [string, string][] {
  const out: [string, string][] = []
  for (const n of nodes) {
    const a = asInput(n)
    if (a?.type === "hidden" && a.name) out.push([a.name, String(a.value ?? "")])
  }
  return out
}

function valueOf(nodes: UiNode[], name: string): string {
  const n = nodes.find((x) => asInput(x)?.name === name && asInput(x)?.value != null)
  const a = n ? asInput(n) : null
  return a ? String(a.value ?? "") : ""
}

export default function KratosLogin({ flow }: { flow: LoginFlow }) {
  const nodes = (flow.ui?.nodes ?? []) as UiNode[]
  const [values, setValues] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const csrf = hiddenInputs(nodes).filter(([k]) => k === "csrf_token")
  const identifier = valueOf(nodes, "identifier") || valueOf(nodes, "traits.email")
  const hasPassword = nodes.some((n) => n.attributes.node_type === "input" && n.attributes.name === "password")
  const hasTotp = nodes.some((n) => n.attributes.node_type === "input" && n.attributes.name === "totp_code")
  const hasLookup = nodes.some((n) => n.attributes.node_type === "input" && n.attributes.name === "lookup_secret")
  const action = flow.ui?.action ?? ""

  const messages = useMemo(
    () => (flow.ui?.messages ?? []).map((m) => m.text),
    [flow],
  )
  const nodeErrors = useMemo(() => {
    const out: Record<string, string> = {}
    for (const n of nodes) {
      if (n.attributes.node_type !== "input") continue
      const msgs = n.messages?.filter((m) => m.type === "error") ?? []
      if (msgs.length) out[n.attributes.name] = msgs[0].text
    }
    return out
  }, [nodes])

  const set = (k: string) => (e: { target: { value: string } }) =>
    setValues((v) => ({ ...v, [k]: e.target.value }))

  async function submit(e: { preventDefault(): void; target?: unknown }) {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      const form = new FormData(e.target as HTMLFormElement)
      const res = await fetch(action, { method: "POST", body: form, credentials: "include" })
      // Kratos returns a flow JSON on validation errors via browser Accept,
      // or redirects on success — follow where it tells us.
      if (res.redirected) {
        window.location.assign(res.url)
        return
      }
      const ct = res.headers.get("content-type") ?? ""
      if (ct.includes("application/json")) {
        const body = (await res.json().catch(() => null)) as { redirect_browser_to?: string; ui?: unknown; error?: { message?: string } } | null
        if (body?.redirect_browser_to) {
          window.location.assign(body.redirect_browser_to)
          return
        }
        setErr(body?.error?.message ?? "sign-in failed — check your credentials")
      } else {
        // HTML response usually means a redirect Kratos wants us to take.
        window.location.reload()
      }
    } catch {
      setErr("network error — try again")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-[100dvh] bg-[#0e0e10] px-4 py-10 flex items-start justify-center">
      <div className="w-full max-w-sm rounded-2xl border border-zinc-800 bg-[#18181b] p-6 shadow-xl">
        <h1 className="text-xl font-semibold text-zinc-100">Veil</h1>
        <h2 className="mt-4 text-lg font-semibold text-zinc-100">
          {hasTotp || hasLookup ? "Reauthenticate" : "Sign in"}
        </h2>
        <p className="mt-1 text-sm text-zinc-400">
          {hasTotp || hasLookup
            ? "Confirm your identity with your authenticator app or a recovery code"
            : "Sign in with your email and password or a security key"}
        </p>

        {messages.length ? (
          <div className="mt-4 rounded-xl border border-red-900/50 bg-red-950/40 px-4 py-3 text-sm text-red-300">
            {messages.join(" ")}
          </div>
        ) : null}
        {err ? (
          <div className="mt-4 rounded-xl border border-red-900/50 bg-red-950/40 px-4 py-3 text-sm text-red-300">
            {err}
          </div>
        ) : null}

        <form onSubmit={submit} className="mt-6 flex flex-col gap-4" method="post">
          {csrf.map(([k, v]) => (
            <input key={k} type="hidden" name={k} value={v} />
          ))}
          {/* Identifier — prefilled on the password step */}
          {identifier ? (
            <input type="hidden" name="identifier" value={identifier} />
          ) : hasPassword ? (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between">
                <label className={labelCls} htmlFor="veil-identifier">ID</label>
                <a className={linkCls} href="/recovery">Recover Account</a>
              </div>
              <input
                id="veil-identifier"
                className={inputCls}
                name="identifier"
                type="text"
                autoComplete="username"
                placeholder="Enter your ID"
                value={values.identifier ?? ""}
                onChange={set("identifier")}
                required
              />
            </div>
          ) : null}
          {hasPassword ? (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between">
                <label className={labelCls} htmlFor="veil-password">Password</label>
                <a className={linkCls} href="/recovery">Forgot Password?</a>
              </div>
              <input
                id="veil-password"
                className={inputCls}
                name="password"
                type="password"
                autoComplete="current-password"
                placeholder="Enter your Password"
                required
              />
            </div>
          ) : null}
          {hasTotp ? (
            <div className="flex flex-col gap-1.5">
              <label className={labelCls} htmlFor="veil-totp">Authentication code</label>
              <input
                id="veil-totp"
                className={inputCls}
                name="totp_code"
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="123456"
                required
              />
            </div>
          ) : null}
          {hasLookup ? (
            <div className="flex flex-col gap-1.5">
              <label className={labelCls} htmlFor="veil-lookup">Recovery code</label>
              <input
                id="veil-lookup"
                className={inputCls}
                name="lookup_secret"
                placeholder="e.g. abcd-efgh"
              />
            </div>
          ) : null}
          {/* method + provider carry the flow's submit semantics */}
          <input type="hidden" name="method" value={hasTotp ? "totp" : hasLookup ? "lookup_secret" : "password"} />
          <button className={btnCls} type="submit" disabled={busy}>
            {busy ? "Signing in…" : hasTotp || hasLookup ? "Continue" : "Sign in with password"}
          </button>
          {nodeErrors.password ? <p className="text-sm text-red-400">{nodeErrors.password}</p> : null}
        </form>

        {!hasTotp && !hasLookup ? (
          <>
            <div className="my-5 flex items-center gap-3 text-xs text-zinc-600">
              <div className="h-px flex-1 bg-zinc-800" />or<div className="h-px flex-1 bg-zinc-800" />
            </div>
            <form onSubmit={submit} method="post">
              {csrf.map(([k, v]) => (
                <input key={k} type="hidden" name={k} value={v} />
              ))}
              {identifier ? <input type="hidden" name="identifier" value={identifier} /> : null}
              <input type="hidden" name="method" value="webauthn" />
              <button className={btnCls} type="submit">Continue with hardware key</button>
            </form>
          </>
        ) : null}

        <p className="mt-6 text-center text-sm text-zinc-500">
          Don&apos;t have an account?{" "}
          <a className={linkCls} href="/registration">Sign up</a>
        </p>
      </div>
    </div>
  )
}
