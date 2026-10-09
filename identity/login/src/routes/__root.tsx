import { createRootRoute, Outlet } from "@tanstack/react-router"
import "@ory/elements-react/theme/styles.css"

export const Route = createRootRoute({
  component: () => (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: "2rem",
      }}
    >
      <div style={{ width: "100%", maxWidth: "420px" }}>
        <Outlet />
      </div>
    </div>
  ),
})
