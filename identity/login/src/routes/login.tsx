import { createFileRoute } from "@tanstack/react-router"
import { Login } from "@ory/elements-react/theme"
import { frontend, oryConfig } from "../ory"
import { loadOrCreateFlow, loginSearch } from "../flow"

export const Route = createFileRoute("/login")({
  validateSearch: loginSearch,
  loaderDeps: ({ search }) => ({
    flow: search.flow,
    return_to: search.return_to,
    login_challenge: search.login_challenge,
  }),
  loader: ({ deps }) => {
    const req: { returnTo?: string; loginChallenge?: string; refresh?: boolean } = {}
    if (deps.return_to) {
      req.returnTo = deps.return_to
    }
    if (deps.login_challenge) {
      req.loginChallenge = deps.login_challenge
    }
    const createFlow = async () => {
      const flow = await frontend.createBrowserLoginFlow(req).catch(async (e: unknown) => {
        // A live session plus prompt=login lands here: kratos refuses a plain
        // flow; recreate it as a refresh flow at the session's AAL (below) or
        // Elements renders a dead-end ("no authentication methods available").
        if (await isActiveSession(e)) {
          return frontend.createBrowserLoginFlow(await refreshAtSessionAal(req))
        }
        throw e
      })
      // With a login_challenge, kratos auto-forces refresh and returns a
      // methodless flow instead of erroring — catch that too.
      if (!hasCredentialNodes(flow)) {
        const session = await frontend.toSession().catch(() => null)
        if (session) {
          return frontend.createBrowserLoginFlow(await refreshAtSessionAal(req))
        }
      }
      return flow
    }
    return loadOrCreateFlow(
      "/login",
      deps.flow,
      createFlow,
      // A persisted ?flow= can also point at a methodless refresh flow —
      // re-run the guarded create path instead of rendering the dead-end.
      async (id) => {
        const flow = await frontend.getLoginFlow({ id })
        return hasCredentialNodes(flow) ? flow : createFlow()
      },
    )
  },
  component: () => <Login flow={Route.useLoaderData()} config={oryConfig} />,
})

// A flow is renderable only when its ui.nodes carry a credential group —
// refresh flows against the wrong AAL come back with csrf_token alone.
type Flow = { ui?: { nodes?: { group?: string }[] } }

function hasCredentialNodes(flow: Flow): boolean {
  return (flow.ui?.nodes ?? []).some((n) => (n.group ?? "default") !== "default")
}

async function refreshAtSessionAal(req: {
  returnTo?: string
  loginChallenge?: string
}): Promise<{ returnTo?: string; loginChallenge?: string; refresh: true; aal?: string }> {
  const session = await frontend.toSession().catch(() => null)
  return {
    ...req,
    refresh: true,
    aal: session?.authenticator_assurance_level === "aal2" ? "aal2" : undefined,
  }
}

async function isActiveSession(e: unknown): Promise<boolean> {
  const res = (e as { response?: Response }).response
  if (!(res instanceof Response)) return false
  try {
    const body = (await res.clone().json()) as { error?: { id?: string } }
    return body.error?.id === "session_already_available"
  } catch {
    return false
  }
}
