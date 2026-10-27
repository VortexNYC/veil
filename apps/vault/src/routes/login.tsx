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
    // The bundled Mac app vault (veil-app:// WKWebView) must never walk a
    // Kratos browser flow — return_to would point at a scheme Kratos can't
    // serve. Bounce to the app's lock route; the native gate relocks.
    if (window.location.protocol === "veil-app:") {
      window.location.assign("veil-app://vault/lock")
      return new Promise<never>(() => {})
    }
    // The vault's credential is the OIDC id_token — a bare kratos flow
    // can't mint it, so without ?flow= we restart the authorize hop and
    // Hydra returns here carrying the login_challenge flow. A dead flow id
    // self-heals the same way.
    if (!deps.flow) {
      const whoami = await fetch(new URL("/sessions/whoami", kratosURL), {
        credentials: "include",
      }).catch(() => null)
      if (deps.login_challenge) {
        const url = new URL("/self-service/login/browser", kratosURL)
        url.searchParams.set("login_challenge", deps.login_challenge)
        if (whoami?.ok) {
          const session = (await whoami.json().catch(() => null)) as {
            authenticator_assurance_level?: string
          } | null
          url.searchParams.set("refresh", "true")
          if (session?.authenticator_assurance_level === "aal2") {
            url.searchParams.set("aal", "aal2")
          }
        } else if (whoami?.status === 403) {
          // 403 on whoami is session_aal2_required — an aal1 cookie is
          // already present, so ask Kratos for the aal2 second factor.
          url.searchParams.set("aal", "aal2")
        }
        window.location.assign(url)
        return new Promise<never>(() => {})
      }
      // Always start through OIDC authorize so the very first login flow
      // carries Hydra's login_challenge. A bare /self-service/login/browser
      // flow without a challenge cannot mint an id_token and bounces an
      // aal1 session in a 303 loop between /login and Kratos.
      window.location.assign(await beginLogin(whoami?.status === 403))
      return new Promise<never>(() => {})
    }
    try {
      const flow = await frontend.getLoginFlow({ id: deps.flow })
      if ((flow.ui?.nodes ?? []).some((n) => (n.group ?? "default") !== "default")) {
        sessionStorage.removeItem("veil.aal2_flow")
        // Kratos ships both totp and lookup_secret nodes on every aal2 flow,
        // which makes Ory Elements stop on the "Choose a way" picker first.
        // Default active to totp so the 6-digit input renders immediately
        // ("Choose another method" stays available below).
        if (
          flow.requested_aal === "aal2" &&
          !flow.active &&
          (flow.ui?.nodes ?? []).some((n) => n.group === "totp")
        ) {
          flow.active = "totp"
        }
        return flow
      }
      // A csrf-only flow is Kratos answering a refresh login/browser request
      // the session already satisfies — aal1 asked while the session sits at
      // aal2. There is nothing to render, and restarting the OIDC hop mints
      // the same empty flow forever. Upgrade the refresh to aal2 once; the
      // veil.aal2_flow marker makes a second empty flow terminal instead of
      // another redirect (Kratos mints a fresh flow id every hop, so keying
      // the retry on "have we already upgraded" — not the id — is what keeps
      // this loop-free even if aal=aal2 also comes back empty).
      const upgraded = sessionStorage.getItem("veil.aal2_flow")
      if (
        upgraded === null &&
        flow.requested_aal !== "aal2" &&
        (flow.refresh === true || flow.requested_aal === "aal1")
      ) {
        sessionStorage.setItem("veil.aal2_flow", deps.flow)
        const challenge = flow.oauth2_login_request?.challenge ?? deps.login_challenge
        const url = new URL("/self-service/login/browser", kratosURL)
        url.searchParams.set("refresh", "true")
        url.searchParams.set("aal", "aal2")
        if (challenge) {
          url.searchParams.set("login_challenge", challenge)
        } else {
          url.searchParams.set("return_to", `${window.location.origin}/login`)
        }
        window.location.assign(url)
        return new Promise<never>(() => {})
      }
      if (upgraded !== null || flow.requested_aal === "aal2") {
        sessionStorage.removeItem("veil.aal2_flow")
        return flow
      }
    } catch {
      // fall through — restart the chain below
    }
    window.location.assign(await beginLogin())
    return new Promise<never>(() => {})
  },
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
