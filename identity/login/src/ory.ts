import type { OryClientConfiguration } from "@ory/elements-react"
import { Configuration, FrontendApi } from "@ory/client-fetch"

export const kratosURL =
  (import.meta.env.VITE_KRATOS_URL as string | undefined) ?? "http://127.0.0.1:4433"

export const oryConfig: OryClientConfiguration = {
  project: {
    name: "veil",
    default_redirect_url: "/",
    error_ui_url: "/error",
    registration_enabled: false,
    verification_enabled: true,
    recovery_enabled: true,
    login_ui_url: "/login",
    registration_ui_url: "/registration",
    recovery_ui_url: "/recovery",
    verification_ui_url: "/verification",
    settings_ui_url: "/settings",
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
