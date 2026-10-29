import { createRootRoute, Outlet } from "@tanstack/react-router"
import "../styles.css"

export const Route = createRootRoute({
  component: () => (
    <div className="veil-login">
      <Outlet />
    </div>
  ),
})
