import { createFileRoute } from "@tanstack/react-router"

// Sign-up lives on the dedicated identity surface. The Ory Elements "Sign
// up" link inside the vault's embedded login card resolves
// registration_ui_url against this origin, so the hop routes through this
// router and hands the window to the real registration flow.
export const Route = createFileRoute("/registration")({
  loader: () => {
    window.location.assign("https://login.veil.nyc/registration")
    return new Promise<never>(() => {})
  },
  component: () => null,
})
