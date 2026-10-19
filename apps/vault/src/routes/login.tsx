import { createFileRoute } from "@tanstack/react-router"
import { useEffect } from "react"
import { Login } from "@ory/elements-react/theme"
import { frontend, kratosURL, oryConfig } from "../ory"
import { beginLogin } from "../auth"
import { loginSearch } from "../flow"

export const Route = createFileRoute("/login")({
  validateSearch: loginSearch,
  loaderDeps: ({ search }) => ({
    flow: search.flow,
    login_challenge: search.login_challenge,
  }),
  loader: async ({ deps }) => {
    // The vault's credential is the OIDC id_token — a bare kratos flow
    // can't mint it, so without ?flow= we restart the authorize hop and
    // Hydra returns here carrying the login_challenge flow. A dead flow id
    // self-heals the same way.
    if (!deps.flow) {
      const url = new URL(`${kratosURL}/self-service/login/browser`)
      url.searchParams.set("_", String(Date.now())) // bust edge-cached SPA fallback under this path
      if (deps.login_challenge) {
        url.searchParams.set("login_challenge", deps.login_challenge)
      } else {
        // No challenge yet: kratos session exists → OIDC hop silently mints
        // the token; no session → browser flow bounces straight back here
        // with ?flow= — skipping the Hydra round-trip cuts a visible hop.
        if (await frontend.toSession().catch(() => null)) {
          window.location.assign(await beginLogin())
        } else {
          url.searchParams.set("return_to", `${window.location.origin}/login`)
          window.location.assign(url)
        }
        return new Promise<never>(() => {})
      }
      window.location.assign(url)
      return new Promise<never>(() => {})
    }
    try {
      const flow = await frontend.getLoginFlow({ id: deps.flow })
      if ((flow.ui?.nodes ?? []).some((n) => (n.group ?? "default") !== "default")) {
        return flow
      }
    } catch {
      // fall through — restart the chain below
    }
    window.location.assign(await beginLogin())
    return new Promise<never>(() => {})
  },
  pendingComponent: () => (
    <div className="veil-login">
      <div style={{ color: "#0f172a", fontWeight: 700, fontSize: "1.25rem", letterSpacing: "0.02em" }}>
        Veil
      </div>
      <div style={{ color: "#64748b", fontSize: "0.9rem" }}>Signing you in…</div>
    </div>
  ),
  component: () => {
    // The Elements card is light-theme only — pin the document scheme while
    // this route is mounted so the OS can't auto-darken the form controls.
    useEffect(() => {
      const root = document.documentElement
      const prev = root.style.colorScheme
      root.style.colorScheme = "light"
      return () => {
        root.style.colorScheme = prev
      }
    }, [])
    return (
      <div className="veil-login">
        <Login flow={Route.useLoaderData()} config={oryConfig} />
      </div>
    )
  },
})
