// Proxies Kratos under /auth/* so the sign-in dance never leaves app.veil.nyc.
// Redirect targets and flow JSON (ui.action, continue_with) are rewritten so
// nothing points the browser back at accounts.veil.nyc.

const UPSTREAM = "https://accounts.veil.nyc"

export default {
  async fetch(request: Request, env: { ASSETS: { fetch: (r: Request) => Promise<Response> } }): Promise<Response> {
    const url = new URL(request.url)
    if (!url.pathname.startsWith("/auth/")) {
      return env.ASSETS.fetch(request)
    }
    const host = url.origin
    const upstream = UPSTREAM + url.pathname.slice("/auth".length) + url.search
    const res = await fetch(upstream, {
      method: request.method,
      headers: request.headers,
      body: request.body,
      redirect: "manual",
    })
    const headers = new Headers(res.headers)
    const loc = headers.get("location")
    if (loc) {
      headers.set("location", loc.replaceAll(UPSTREAM, `${host}/auth`))
    }
    const ct = headers.get("content-type") ?? ""
    if (ct.includes("application/json")) {
      const body = (await res.text()).replaceAll(UPSTREAM, `${host}/auth`)
      return new Response(body, { status: res.status, headers })
    }
    return new Response(res.body, { status: res.status, headers })
  },
}
