import { Configuration, FrontendApi } from "@ory/client-fetch"

// Prod builds must never bake localhost — dev hits local kratos, anything else
// hits the public endpoint unless VITE_KRATOS_URL overrides.
export const kratosURL =
  (import.meta.env.VITE_KRATOS_URL as string | undefined) ??
  (import.meta.env.PROD ? "https://accounts.veil.nyc" : "http://127.0.0.1:4433")

export const frontend = new FrontendApi(
  new Configuration({
    basePath: kratosURL,
    credentials: "include",
  }),
)
