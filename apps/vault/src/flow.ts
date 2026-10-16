export function loginSearch(search: Record<string, unknown>): {
  flow?: string
  return_to?: string
  login_challenge?: string
} {
  const out: { flow?: string; return_to?: string; login_challenge?: string } = {}
  if (typeof search.flow === "string") {
    out.flow = search.flow
  }
  if (typeof search.return_to === "string") {
    out.return_to = search.return_to
  }
  if (typeof search.login_challenge === "string") {
    out.login_challenge = search.login_challenge
  }
  return out
}
