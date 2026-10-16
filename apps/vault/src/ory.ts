import type { OryClientConfiguration } from "@ory/elements-react"
import { Configuration, FrontendApi } from "@ory/client-fetch"

// The vault owns its sign-in — the Elements form renders at /login on
// app.veil.nyc instead of bouncing to a separate login host.
export const kratosURL =
  (import.meta.env.VITE_KRATOS_URL as string | undefined) ??
  (import.meta.env.PROD ? "https://accounts.veil.nyc" : "http://127.0.0.1:4433")

export const oryConfig: OryClientConfiguration = {
  project: {
    name: "Veil",
    hide_ory_branding: true,
    default_redirect_url: "/",
    error_ui_url: "https://login.veil.nyc/error",
    registration_enabled: false,
    verification_enabled: true,
    recovery_enabled: true,
    login_ui_url: "/login",
    registration_ui_url: "https://login.veil.nyc/registration",
    recovery_ui_url: "https://login.veil.nyc/recovery",
    verification_ui_url: "https://login.veil.nyc/verification",
    settings_ui_url: "https://login.veil.nyc/settings",
  },
  sdk: {
    url: kratosURL,
  },
}

export const frontend = new FrontendApi(
  new Configuration({
    basePath: kratosURL,
    credentials: "include",
  }),
)
