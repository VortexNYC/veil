import { Configuration, FrontendApi } from "@ory/client-fetch"

// The vault owns its sign-in — the Elements form renders at /login on
// app.veil.nyc instead of bouncing to a separate login host.
export const kratosURL =
  (import.meta.env.VITE_KRATOS_URL as string | undefined) ??
  (import.meta.env.PROD ? "https://accounts.veil.nyc" : "http://127.0.0.1:4433")

export const frontend = new FrontendApi(
  new Configuration({
    basePath: kratosURL,
    credentials: "include",
  }),
)
