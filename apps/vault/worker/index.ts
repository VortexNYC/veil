const UPSTREAM = "https://accounts.veil.nyc"
const PREFIX = "/auth"

interface Env {
  ASSETS: { fetch(req: Request): Promise<Response> }
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url)
    if (!url.pathname.startsWith(`${PREFIX}/`)) {
      return env.ASSETS.fetch(request)
    }

    const upstream = new URL(
      url.pathname.slice(PREFIX.length) + url.search,
      UPSTREAM,
    )
    const fwd = new Headers()
    for (const key of [
      "cookie",
      "content-type",
      "accept",
      "accept-language",
      "user-agent",
      "origin",
      "referer",
    ]) {
      const v = request.headers.get(key)
      if (v) fwd.set(key, v.replaceAll(url.origin, UPSTREAM))
    }

    const res = await fetch(upstream, {
      method: request.method,
      headers: fwd,
      body:
        request.method === "GET" || request.method === "HEAD"
          ? undefined
          : request.body,
      redirect: "manual",
      cf: { cacheTtl: 0 },
    })

    const headers = new Headers()
    for (const [k, v] of res.headers) {
      const lk = k.toLowerCase()
      if (lk === "location") {
        headers.set(k, v.replaceAll(UPSTREAM, url.origin + PREFIX))
      } else if (lk === "content-type" || lk === "vary") {
        headers.set(k, v)
      }
    }
    for (const sc of res.headers.getSetCookie()) {
      // Kratos emits Domain=accounts.veil.nyc — the browser never sends it
      // back through app.veil.nyc, so pin cookies to the parent zone.
      headers.append("set-cookie", sc.replace(/Domain=accounts\.veil\.nyc/gi, "Domain=veil.nyc"))
    }
    headers.set("cache-control", "private, no-store")

    if (res.status >= 300 && res.status < 400) {
      return new Response(null, { status: res.status, headers })
    }
    const ct = res.headers.get("content-type") ?? ""
    if (ct.includes("json")) {
      const body = (await res.text()).replaceAll(
        UPSTREAM,
        url.origin + PREFIX,
      )
      return new Response(body, { status: res.status, headers })
    }
    return new Response(res.body, { status: res.status, headers })
  },
}
