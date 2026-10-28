import { createFileRoute } from "@tanstack/react-router"
import KratosFlow from "ory-ui"
import { frontend, kratosURL } from "../ory"
import { flowSearch, loadOrCreateFlow } from "../flow"

export const Route = createFileRoute("/verification")({
  validateSearch: flowSearch,
  loaderDeps: ({ search }) => ({ flow: search.flow }),
  loader: ({ deps }) => {
    // Browser flows are navigated to, never fetched: kratos answers
    // /self-service/verification/browser with a 303 back to this page
    // carrying ?flow=, and following that cross-origin redirect chain via
    // fetch dies on CORS. We hand the window to kratos and never resolve.
    const createFlow = async (): Promise<never> => {
      window.location.assign(
        new URL("/self-service/verification/browser", kratosURL),
      )
      return new Promise<never>(() => {})
    }
    return loadOrCreateFlow(
      "/verification",
      deps.flow,
      createFlow,
      async (id) => {
        // A persisted ?flow= can point at an expired flow — navigate the
        // create path instead of rendering the dead-end.
        try {
          return await frontend.getVerificationFlow({ id })
        } catch {
          return createFlow()
        }
      },
    )
  },
  component: () => <KratosFlow flow={Route.useLoaderData()} title="Verify your email" subtitle="Enter the code we sent you" />,
})
