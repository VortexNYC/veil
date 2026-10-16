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
      if (deps.login_challenge) {
        const url = new URL("/self-service/login/browser", kratosURL)
        url.searchParams.set("login_challenge", deps.login_challenge)
        window.location.assign(url)
      } else {
        window.location.assign(await beginLogin())
      }
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
