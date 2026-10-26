import { createFileRoute } from "@tanstack/react-router"
import { Registration } from "@ory/elements-react/theme"
import { frontend, kratosURL, oryConfig } from "../ory"
import { flowSearch, loadOrCreateFlow } from "../flow"

export const Route = createFileRoute("/registration")({
  validateSearch: flowSearch,
  loaderDeps: ({ search }) => ({ flow: search.flow }),
  loader: ({ deps }) => {
    // Browser flows are navigated to, never fetched: kratos answers
    // /self-service/registration/browser with a 303 back to this page
    // carrying ?flow=, and following that cross-origin redirect chain via
    // fetch dies on CORS. We hand the window to kratos and never resolve.
    const createFlow = async (): Promise<never> => {
      window.location.assign(
        new URL("/self-service/registration/browser", kratosURL),
      )
      return new Promise<never>(() => {})
    }
    return loadOrCreateFlow(
      "/registration",
      deps.flow,
      createFlow,
      // A persisted ?flow= can point at an expired flow — navigate the
      // create path instead of rendering the dead-end.
      async (id) => {
        try {
          return await frontend.getRegistrationFlow({ id })
        } catch {
          return createFlow()
        }
      },
    )
  },
  component: () => <Registration flow={Route.useLoaderData()} config={oryConfig} />,
})
