// Kumo-native Kratos renderer — drives flow.ui.nodes for every flow
// (login, registration, recovery, settings, verification). Replaces Ory
// Elements: inputs, buttons, checkboxes, messages, method groups all map to
// our components; secrets POST back to flow.ui.action untouched.

import { useMemo, useState, type FormEvent } from "react"

type UiNodeInput = { attributes?: Record<string, unknown>; messages?: { type: string; text: string }[]; group?: string }
interface FlowLike {
  ui?: {
    action?: string
    method?: string
    nodes?: UiNodeInput[]
    messages?: { type: string; text: string }[]
  }
}

const inputStyle = {
  background: "var(--veil-field)",
  borderColor: "var(--veil-field-border)",
  borderRadius: "var(--veil-radius-field)",
} as const;
const primaryStyle = { background: "var(--veil-primary)", borderRadius: "var(--veil-radius-field)" } as const;
const secondaryStyle = {
  background: "var(--veil-card)",
  borderColor: "var(--veil-border)",
  borderRadius: "var(--veil-radius-field)",
} as const;
const dangerStyle = {
  background: "var(--veil-danger-bg)",
  borderColor: "var(--veil-danger-border)",
  color: "var(--veil-danger-text)",
  borderRadius: "var(--veil-radius-field)",
} as const;
const linkStyle = { color: "var(--veil-link)" } as const;

const inputCls =
  "w-full rounded-xl border border-zinc-600 px-4 py-2.5 text-[0.9375rem] text-zinc-100 placeholder-zinc-500 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/30"
const btnCls =
  "w-full px-4 py-2.5 text-[0.9375rem] font-semibold text-white transition disabled:opacity-50"
const btnSecondary =
  "w-full border px-4 py-2.5 text-[0.9375rem] font-semibold text-[var(--veil-text)] transition hover:bg-zinc-700 disabled:opacity-50"
const labelCls = "text-sm font-medium text-zinc-300"
const linkCls = "text-sm text-blue-400 hover:text-blue-300"

function attrs(n: UiNodeInput): Record<string, unknown> {
  return n.attributes ?? {}
}
function name(n: UiNodeInput): string {
  return String(attrs(n).name ?? "")
}
function inputType(n: UiNodeInput): string {
  return String(attrs(n).type ?? "text")
}
function value(n: UiNodeInput): string {
  return String(attrs(n).value ?? "")
}
function isInput(n: UiNodeInput): boolean {
  return (n.attributes as { node_type?: string } | undefined)?.node_type === "input"
}
function isSubmit(n: UiNodeInput): boolean {
  return isInput(n) && inputType(n) === "submit"
}
function isButton(n: UiNodeInput): boolean {
  return isInput(n) && inputType(n) === "button"
}
function groupOf(n: UiNodeInput): string {
  return String(n.group ?? "default")
}

// Inputs that render as labeled fields (skip hidden, submit/button — those
// are controls; skip code/lookup/totp handled specially by method screens).
const LABELS: Record<string, string> = {
  identifier: "ID",
  "traits.email": "Email",
  password: "Password",
  "password_confirmation": "Confirm password",
  totp_code: "Authentication code",
  lookup_secret: "Recovery code",
  method: "",
}
const PLACEHOLDERS: Record<string, string> = {
  identifier: "Enter your ID",
  "traits.email": "Enter your email",
  password: "Enter your password",
  password_confirmation: "Confirm your password",
  totp_code: "123456",
  lookup_secret: "e.g. abcd-efgh",
}
const AUTOCOMPLETE: Record<string, string> = {
  identifier: "username",
  "traits.email": "email",
  password: "current-password",
  password_confirmation: "new-password",
  totp_code: "one-time-code",
}

// Nodes grouped by their group tag so we can render method sections in the
// order Kratos sent them — password, totp, lookup, webauthn, passkey, code.
function groupsOf(nodes: UiNodeInput[]): string[] {
  const seen: string[] = []
  for (const n of nodes) {
    const g = groupOf(n)
    if (!seen.includes(g)) seen.push(g)
  }
  return seen
}

// A field inside one group section — everything that's a labeled input.
function Field({ n }: { n: UiNodeInput }) {
  const nm = name(n)
  const type = inputType(n)
  const errors = (n.messages ?? []).filter((m) => m.type === "error")
  if (type === "hidden") return <input type="hidden" name={nm} value={value(n)} />
  if (type === "checkbox") {
    return (
      <label className="flex items-center gap-2 text-sm text-zinc-300">
        <input type="checkbox" name={nm} value={value(n) || "true"} className="h-4 w-4 rounded" />
        <span>{LABELS[nm] ?? nm}</span>
      </label>
    )
  }
  const [show, setShow] = useState(false)
  const realType = type === "password" ? (show ? "text" : "password") : type
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <label className={labelCls} htmlFor={`veil-${nm}`}>{LABELS[nm] ?? nm}</label>
        {type === "password" ? (
          <button type="button" className={linkCls} style={linkStyle} onClick={() => setShow((s) => !s)}>
            {show ? "Hide" : "Show"}
          </button>
        ) : null}
      </div>
      <input
        id={`veil-${nm}`}
        className={inputCls}
        style={inputStyle}
        name={nm}
        type={realType}
        autoComplete={AUTOCOMPLETE[nm]}
        placeholder={PLACEHOLDERS[nm]}
        defaultValue={value(n)}
        required={(attrs(n).required as boolean) ?? type !== "checkbox"}
      />
      {errors.length ? <p className="text-sm text-red-400">{errors[0].text}</p> : null}
    </div>
  )
}

// A submit/button control inside a group — posts name+value like Kratos
// expects (method=<value> or provider=<value>).
function Control({ n }: { n: UiNodeInput }) {
  const primary = inputType(n) === "submit"
  const label = (n.attributes as { label?: string } | undefined)?.label ?? String(name(n) || "Continue")
  return (
    <button
      type="submit"
      name={name(n) || "method"}
      value={value(n)}
      className={primary ? btnCls : btnSecondary} style={primary ? primaryStyle : secondaryStyle}
    >
      {label}
    </button>
  )
}

export default function KratosFlow({
  flow,
  title,
  subtitle,
}: {
  flow: unknown
  title: string
  subtitle?: string
}) {
  const nodes = useMemo(() => ((flow as FlowLike).ui?.nodes ?? []) as UiNodeInput[], [flow])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const action = (flow as FlowLike).ui?.action ?? ""

  const groups = groupsOf(nodes)
  const bannerMsgs = ((flow as FlowLike).ui?.messages ?? []).map((m) => m.text)

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setErr(null)
    try {
      const form = new FormData(e.currentTarget)
      const res = await fetch(action, { method: "POST", body: form, credentials: "include" })
      if (res.redirected) {
        window.location.assign(res.url)
        return
      }
      const ct = res.headers.get("content-type") ?? ""
      if (ct.includes("json")) {
        const body = (await res.json().catch(() => null)) as
          | { redirect_browser_to?: string; error?: { message?: string } }
          | null
        if (body?.redirect_browser_to) window.location.assign(body.redirect_browser_to)
        else setErr(body?.error?.message ?? "something went wrong — try again")
      } else {
        window.location.reload()
      }
    } catch {
      setErr("network error — try again")
    } finally {
      setBusy(false)
    }
  }

  // One <form> per group section so each method's submit posts its own
  // context — Elements does the same per-section.
  return (
    <div className="veil-flow min-h-[100dvh] px-4 py-10 flex items-start justify-center" style={{ background: "var(--veil-bg)" }}>
      <div className="w-full border shadow-xl" style={{ maxWidth: "var(--veil-w-card)", borderRadius: "var(--veil-radius-card)", borderColor: "var(--veil-border)", background: "var(--veil-card)", padding: "var(--veil-pad-card)" }}>
        <h1 className="text-xl font-semibold text-zinc-100">Veil</h1>
        <h2 className="mt-4 text-lg font-semibold text-zinc-100">{title}</h2>
        {subtitle ? <p className="mt-1 text-sm text-zinc-400">{subtitle}</p> : null}

        {bannerMsgs.length ? (
          <div className="mt-4 border px-4 py-3 text-sm" style={dangerStyle}>
            {bannerMsgs.join(" ")}
          </div>
        ) : null}
        {err ? (
          <div className="mt-4 border px-4 py-3 text-sm" style={dangerStyle}>
            {err}
          </div>
        ) : null}

        {groups.map((g, gi) => {
          const gnodes = nodes.filter((n) => groupOf(n) === g)
          const fields = gnodes.filter((n) => isInput(n) && !isSubmit(n) && !isButton(n))
          const controls = gnodes.filter((n) => isSubmit(n) || isButton(n))
          const nonInput = gnodes.filter((n) => !isInput(n))
          return (
            <form key={g} onSubmit={submit} method="post" className="mt-5 flex flex-col gap-4">
              {gi > 0 ? (
                <div className="flex items-center gap-3 text-xs text-zinc-600">
                  <div className="h-px flex-1 bg-zinc-800" />or<div className="h-px flex-1 bg-zinc-800" />
                </div>
              ) : null}
              {fields.map((n) => (
                <Field key={`${g}-${name(n)}-${inputType(n)}`} n={n} />
              ))}
              {nonInput.map((n, i) => {
                const t = (n.attributes as { text?: { text?: string } } | undefined)?.text?.text
                return t ? (
                  <p key={`${g}-t-${i}`} className="whitespace-pre-wrap rounded-xl border border-zinc-800 bg-zinc-900 px-4 py-3 font-mono text-sm text-zinc-300">
                    {t}
                  </p>
                ) : null
              })}
              <div className="flex flex-col gap-2.5">
                {controls.map((n, i) => (
                  <Control key={`${g}-c-${i}`} n={n} />
                ))}
              </div>
            </form>
          )
        })}
      </div>
    </div>
  )
}
