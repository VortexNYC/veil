import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { createRouter, RouterProvider } from "@tanstack/react-router"
import posthog from "posthog-js"
import { routeTree } from "./routeTree.gen"
import "./styles.css"

// Bug telemetry lands in one place: PostHog error tracking. Exceptions
// only — no autocapture, no session recording, no item data. The vault's
// contents never leave the client.
posthog.init("phc_v5HaYTE3N9siA53wtYmkFHSm28mBATJJsdDjeaobHxC8", {
  api_host: "https://us.i.posthog.com",
  autocapture: false,
  capture_pageview: false,
  disable_session_recording: true,
  capture_exceptions: true,
  persistence: "localStorage",
})

if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
  document.documentElement.dataset.mode = "dark"
}

const router = createRouter({ routeTree })

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

const root = document.getElementById("root")
if (!root) {
  throw new Error("missing root")
}

createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
