import { createRootRoute, Outlet } from "@tanstack/react-router"
import "@ory/elements-react/theme/styles.css"

export const Route = createRootRoute({
  component: () => <Outlet />,
})
