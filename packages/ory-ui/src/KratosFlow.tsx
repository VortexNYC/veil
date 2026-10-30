// Kratos renderer on shared Vortex auth chrome (@vortex-api/better-auth-ui) +
// Kumo inputs. Ory posts the flow; we do not restyle Kratos semantics.

import { useMemo, useRef, useState, type ReactNode } from "react"
import {
  AuthCard,
  AuthDivider,
  AuthError,
  AuthShell,
} from "@vortex-api/better-auth-ui"
import { Button } from "@cloudflare/kumo/components/button"
import { Input } from "@cloudflare/kumo/components/input"
import { Label } from "@cloudflare/kumo/components/label"

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

const defaultAtmosphere = (
  <div
    className="pointer-events-none absolute inset-0 bg-[radial-gradient(60rem_40rem_at_50%_-10%,color-mix(in_oklch,var(--color-kumo-brand)_8%,transparent),transparent_60%)]"
    aria-hidden
  />
)

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
        <span className="text-sm text-kumo-default">{LABELS[nm] ?? nm}</span>
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
      {errors.length ? <p className="text-sm text-kumo-danger">{errors[0].text}</p> : null}
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
    <Button
      type="submit"
      variant={primary ? "primary" : "secondary"}
      className="w-full"
      name={name(n) || "method"}
      value={value(n)}
      onClick={(e) => {
        const form = (e.currentTarget as HTMLButtonElement).form
        const g = (e.currentTarget as HTMLElement).closest("[data-veil-group]")?.getAttribute("data-veil-group")
        if (form && g) {
          form
            .querySelectorAll("[data-veil-group] input, [data-veil-group] textarea, [data-veil-group] select")
            .forEach((el) => {
              const own = (el as HTMLElement).closest("[data-veil-group]")?.getAttribute("data-veil-group")
              if (own !== g) (el as HTMLInputElement).disabled = true
            })
        }
        onPick(name(n) || "method", value(n))
      }}
    >
      {label}
    </Button>
  )
}

export default function KratosFlow({
  flow,
  title,
  subtitle,
  layout = "page",
  logo,
  atmosphere = defaultAtmosphere,
}: {
  flow: unknown
  title: string
  subtitle?: string
  /** `page` — full AuthShell; `embedded` — card only (e.g. settings with host chrome). */
  layout?: "page" | "embedded"
  logo?: ReactNode
  atmosphere?: ReactNode
}) {
  const nodes = useMemo(() => ((flow as FlowLike).ui?.nodes ?? []) as UiNodeInput[], [flow])
  const pickRef = useRef<{ name: string; value: string } | null>(null)
  const fl = flow as FlowLike & { id?: string; ui?: FlowLike["ui"] }
  const kratosURL =
    (import.meta.env?.VITE_KRATOS_URL as string | undefined) ??
    (import.meta.env?.PROD ? "https://accounts.veil.nyc" : "http://127.0.0.1:4433")
  const action = fl.ui?.action?.startsWith("http")
    ? fl.ui.action
    : `${kratosURL}${fl.ui?.action || `/self-service/login?flow=${fl.id ?? ""}`}`
  const groups = groupsOf(nodes)
  const bannerMsgs = ((flow as FlowLike).ui?.messages ?? []).map((m) => m.text)

  const card = (
    <AuthCard title={title} description={subtitle} className="veil-flow">
      {bannerMsgs.length ? <AuthError message={bannerMsgs.join(" ")} /> : null}

      <form method="post" action={action} className="flex flex-col gap-4">
        {nodes
          .filter((n) => groupOf(n) === "default" && isInput(n))
          .map((n) => (
            <Field key={`d-${name(n)}`} n={n} />
          ))}

        {groups.filter((g) => g !== "default").map((g, gi) => {
          const isField = (n: UiNodeInput) =>
            isInput(n) && !isSubmit(n) && !isButton(n) && name(n) !== "method"
          const fields = nodes.filter((n) => groupOf(n) === g && isField(n))
          const controls = nodes.filter((n) => groupOf(n) === g && (isSubmit(n) || isButton(n)))
          const nonInput = nodes.filter((n) => groupOf(n) === g && !isInput(n))
          return (
            <div key={g} data-veil-group={g} className="flex flex-col gap-4">
              {gi > 0 ? <AuthDivider /> : null}
              {fields.map((n) => (
                <Field key={`${g}-${name(n)}-${inputType(n)}`} n={n} />
              ))}
              {nonInput.map((n, i) => {
                const t = (n.attributes as { text?: { text?: string } } | undefined)?.text?.text
                return t ? (
                  <p
                    key={`${g}-t-${i}`}
                    className="whitespace-pre-wrap rounded-xl border border-kumo-line bg-kumo-base px-4 py-3 font-mono text-sm text-kumo-subtle"
                  >
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
    </AuthCard>
  )

  if (layout === "embedded") {
    return card
  }

  return (
    <AuthShell logo={logo} atmosphere={atmosphere}>
      {card}
    </AuthShell>
  )
}
