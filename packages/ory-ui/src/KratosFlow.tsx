// Kumo-native Kratos renderer — drives flow.ui.nodes for every flow
// (login, registration, recovery, settings, verification). Built on real
// @cloudflare/kumo primitives — one style system for auth + vault.

import { useMemo, useRef, useState } from "react"
import { Input } from "@cloudflare/kumo/components/input"
import { Label } from "@cloudflare/kumo/components/label"
import { Banner } from "@cloudflare/kumo/components/banner"

type UiNodeInput = { attributes?: Record<string, unknown>; messages?: { type: string; text: string }[]; group?: string }
interface FlowLike {
  ui?: {
    action?: string
    method?: string
    nodes?: UiNodeInput[]
    messages?: { type: string; text: string }[]
  }
}

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

const LABELS: Record<string, string> = {
  identifier: "ID",
  "traits.email": "Email",
  password: "Password",
  password_confirmation: "Confirm password",
  totp_code: "Authentication code",
  lookup_secret: "Recovery code",
  method: "",
  email: "Email",
  code: "Code",
}
const PLACEHOLDERS: Record<string, string> = {
  identifier: "Enter your ID",
  "traits.email": "Enter your email",
  email: "Enter your email",
  password: "Enter your password",
  password_confirmation: "Confirm your password",
  totp_code: "123456",
  lookup_secret: "e.g. abcd-efgh",
  code: "Enter the code",
}
const AUTOCOMPLETE: Record<string, string> = {
  identifier: "username",
  "traits.email": "email",
  email: "email",
  password: "current-password",
  password_confirmation: "new-password",
  totp_code: "one-time-code",
}

function groupsOf(nodes: UiNodeInput[]): string[] {
  const seen: string[] = []
  for (const n of nodes) {
    const g = groupOf(n)
    if (!seen.includes(g)) seen.push(g)
  }
  return seen
}

function Field({ n }: { n: UiNodeInput }) {
  const [show, setShow] = useState(false)
  const nm = name(n)
  const type = inputType(n)
  const errors = (n.messages ?? []).filter((m) => m.type === "error")
  if (type === "hidden") return <input type="hidden" name={nm} value={value(n)} />
  if (type === "checkbox") {
    return (
      <label className="flex items-center gap-2">
        <input type="checkbox" name={nm} value={value(n) || "true"} className="h-4 w-4 rounded" />
        <span className="text-sm text-kumo-text">{LABELS[nm] ?? nm}</span>
      </label>
    )
  }
  const isPw = type === "password"
  const realType = isPw ? (show ? "text" : "password") : type
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <Label htmlFor={`veil-${nm}`}>{LABELS[nm] ?? nm}</Label>
        {isPw ? (
          <button type="button" className="text-sm text-kumo-link" onClick={() => setShow((s) => !s)}>
            {show ? "Hide" : "Show"}
          </button>
        ) : null}
      </div>
      <Input
        aria-label={LABELS[nm] ?? nm}
        id={`veil-${nm}`}
        name={nm}
        type={realType}
        autoComplete={AUTOCOMPLETE[nm]}
        placeholder={PLACEHOLDERS[nm]}
        defaultValue={value(n)}
        required={(attrs(n).required as boolean) ?? type !== "checkbox"}
      />
      {errors.length ? <p className="text-sm text-kumo-error">{errors[0].text}</p> : null}
    </div>
  )
}

function Control({
  n,
  onPick,
}: {
  n: UiNodeInput
  onPick: (name: string, value: string) => void
}) {
  const primary = inputType(n) === "submit"
  const meta = (n as { meta?: { label?: { text?: string } } }).meta
  const label =
    meta?.label?.text ??
    (n.attributes as { label?: { text?: string } } | undefined)?.label?.text ??
    String(name(n) || "Continue")
  return (
    <button
      type="submit"
      name={name(n) || "method"}
      value={value(n)}
      data-variant={primary ? "primary" : "secondary"}
      className="w-full rounded-md border border-kumo-line bg-kumo-accent px-3 py-2 text-sm font-medium text-kumo-accent-fg hover:opacity-90 disabled:opacity-50"
      onClick={() => onPick(name(n) || "method", value(n))}
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
  const pickRef = useRef<{ name: string; value: string } | null>(null)
  // Some aal2 flows ship no ui.action — the form must post to Kratos's
  // self-service endpoint for the flow's id, not back to the SPA.
  const fl = flow as FlowLike & { id?: string; ui?: FlowLike["ui"] }
  const kratosURL =
    (import.meta.env?.VITE_KRATOS_URL as string | undefined) ??
    (import.meta.env?.PROD ? "https://accounts.veil.nyc" : "http://127.0.0.1:4433")
  // ui.action may be relative (aal2 flows) or absent — resolve to the
  // Kratos host so the form posts to Kratos, not back to the SPA.
  const action = fl.ui?.action?.startsWith("http")
    ? fl.ui.action
    : `${kratosURL}${fl.ui?.action || `/self-service/login?flow=${fl.id ?? ""}`}`
  const groups = groupsOf(nodes)
  const bannerMsgs = ((flow as FlowLike).ui?.messages ?? []).map((m) => m.text)

  const submit = () => {
    // Persist identifier for the aal2 step — Kratos expects it re-sent
    // on the TOTP/lookup POST even though the aal2 flow has no field for it.
    const id = (document.getElementById("veil-identifier") as HTMLInputElement | null)?.value
    if (id) sessionStorage.setItem("veil.identifier", id)
  }

  return (
    <div className="min-h-[100dvh] bg-kumo-base px-4 py-10 flex items-start justify-center">
      <div className="w-full max-w-sm rounded-xl border border-kumo-line bg-kumo-base p-6 shadow-xl">
        <h1 className="text-xl font-semibold text-kumo-text">Veil</h1>
        <h2 className="mt-4 text-lg font-semibold text-kumo-text">{title}</h2>
        {subtitle ? <p className="mt-1 text-sm text-kumo-subtle">{subtitle}</p> : null}

        {bannerMsgs.length ? (
          <Banner variant="error" className="mt-4">{bannerMsgs.join(" ")}</Banner>
        ) : null}

        {/* One form — Kratos splits nodes into method groups but a single
            submit carries identifier+method anyway, so shared fields render
            once and each group's controls carry method=<value>. */}
        <form method="post" action={action} onSubmit={submit} className="mt-5 flex flex-col gap-4">
          {/* default-group fields (identifier, csrf) render once — the single
              form posts them regardless of which method submits. */}
          {nodes
            .filter((n) => groupOf(n) === "default" && isInput(n))
            .map((n) => (
              <Field key={`d-${name(n)}`} n={n} />
            ))}
          {/* aal2 forms carry the aal1 identifier — Kratos needs it even
              though the flow doesn't expose a field for it. */}
          {(() => {
            const stashed =
              typeof sessionStorage !== "undefined"
                ? sessionStorage.getItem("veil.identifier")
                : null
            return stashed ? (
              <input type="hidden" name="identifier" value={stashed} />
            ) : null
          })()}
          {groups.filter((g) => g !== "default").map((g, gi) => {
            const isField = (n: UiNodeInput) =>
              isInput(n) && !isSubmit(n) && !isButton(n) && name(n) !== "method"
            const fields = nodes.filter((n) => groupOf(n) === g && isField(n))
            const controls = nodes.filter(
              (n) => groupOf(n) === g && (isSubmit(n) || isButton(n)),
            )
            const nonInput = nodes.filter((n) => groupOf(n) === g && !isInput(n))
            return (
              <div key={g} className="flex flex-col gap-4">
                {gi > 0 ? (
                  <div className="flex items-center gap-3 text-xs text-kumo-subtle">
                    <div className="h-px flex-1 bg-kumo-line" />or<div className="h-px flex-1 bg-kumo-line" />
                  </div>
                ) : null}
                {fields.map((n) => (
                  <Field key={`${g}-${name(n)}-${inputType(n)}`} n={n} />
                ))}
                {nonInput.map((n, i) => {
                  const t = (n.attributes as { text?: { text?: string } } | undefined)?.text?.text
                  return t ? (
                    <p key={`${g}-t-${i}`} className="whitespace-pre-wrap rounded-xl border border-kumo-line bg-kumo-base px-4 py-3 font-mono text-sm text-kumo-subtle">
                      {t}
                    </p>
                  ) : null
                })}
                <div className="flex flex-col gap-2.5">
                  {controls.map((n, i) => (
                    <Control
                      key={`${g}-c-${i}`}
                      n={n}
                      onPick={(nm, v) => {
                        pickRef.current = { name: nm, value: v }
                      }}
                    />
                  ))}
                </div>
              </div>
            )
          })}
        </form>
      </div>
    </div>
  )
}
