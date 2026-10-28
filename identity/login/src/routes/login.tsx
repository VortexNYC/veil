import { createFileRoute } from "@tanstack/react-router"
import KratosFlow from "ory-ui"
import { frontend, kratosURL } from "../ory"
import { loadOrCreateFlow, loginSearch } from "../flow"

export const Route = createFileRoute("/login")({
  validateSearch: loginSearch,
  loaderDeps: ({ search }) => ({
    flow: search.flow,
    return_to: search.return_to,
    login_challenge: search.login_challenge,
  }),
  loader: ({ deps }) => {
    // Browser flows are navigated to, never fetched: kratos answers
    // /self-service/login/browser with a 303 back to this page carrying
    // ?flow=, and following that cross-origin redirect chain via fetch
    // dies on CORS. We hand the window to kratos and never resolve.
    const createFlow = async (): Promise<never> => {
      // A live session (Android custom tabs share Chrome's cookie jar)
      // makes a plain create bounce — navigate with refresh at the
      // session's AAL instead of erroring into a dead-end.
      const session = await frontend.toSession().catch(() => null)
      const url = new URL("/self-service/login/browser", kratosURL)
      if (deps.return_to) url.searchParams.set("return_to", deps.return_to)
      if (deps.login_challenge) url.searchParams.set("login_challenge", deps.login_challenge)
      if (session) {
        url.searchParams.set("refresh", "true")
        if (session.authenticator_assurance_level === "aal2") {
          url.searchParams.set("aal", "aal2")
        }
      }
      window.location.assign(url)
      return new Promise<never>(() => {})
    }
    return loadOrCreateFlow(
      "/login",
      deps.flow,
      createFlow,
      // A persisted ?flow= can also point at a methodless refresh flow or a
      // dead id from a stale cookie jar — navigate the create path instead
      // of rendering the dead-end.
      async (id) => {
        try {
          const flow = await frontend.getLoginFlow({ id })
          if (hasCredentialNodes(flow)) {
            return flow
          }
        } catch {
          // fall through — a dead flow id self-heals via navigation
        }
        return createFlow()
      },
    )
  },
  component: () => <KratosFlow flow={Route.useLoaderData()} title="Sign in" subtitle="Sign in with your email and password or a security key" />,
})

// A flow is renderable only when its ui.nodes carry a credential group —
// refresh flows against the wrong AAL come back with csrf_token alone.
type Flow = { ui?: { nodes?: { group?: string }[] } }

function hasCredentialNodes(flow: Flow): boolean {
  return (flow.ui?.nodes ?? []).some((n) => (n.group ?? "default") !== "default")
}
