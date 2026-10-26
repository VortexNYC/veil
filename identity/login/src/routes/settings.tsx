import { createFileRoute } from "@tanstack/react-router"
import { Settings } from "@ory/elements-react/theme"
import { frontend, oryConfig } from "../ory"
import { flowSearch, loadOrCreateFlow } from "../flow"

export const Route = createFileRoute("/settings")({
  validateSearch: flowSearch,
  loaderDeps: ({ search }) => ({ flow: search.flow }),
  loader: ({ deps }) =>
    loadOrCreateFlow("/settings", deps.flow, () => frontend.createBrowserSettingsFlow(), (id) =>
      frontend.getSettingsFlow({ id }),
    ),
  component: () => (
    <main>
      <Settings flow={Route.useLoaderData()} config={oryConfig} />
      <p>
        <a href="https://app.veil.nyc">Continue to Veil Vault →</a>
      </p>
    </main>
  ),
})
