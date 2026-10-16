import { createRootRoute, Outlet } from "@tanstack/react-router"
import "@ory/elements-react/theme/styles.css"
import "../styles.css"

export const Route = createRootRoute({
  component: () => (
    <div className="veil-login">
      <Outlet />
    </div>
  ),
})
