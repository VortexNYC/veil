import { createFileRoute } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { Banner } from "@cloudflare/kumo/components/banner"
import { Link } from "@cloudflare/kumo/components/link"
import KratosFlow from "ory-ui"
import { frontend, kratosURL } from "../ory"
import { flowSearch, loadOrCreateFlow } from "../flow"

export const Route = createFileRoute("/settings")({
  validateSearch: flowSearch,
  loaderDeps: ({ search }) => ({ flow: search.flow }),
  loader: async ({ deps }) => {
    // Browser flows are navigated to, never fetched: kratos answers
    // /self-service/settings/browser with a 303 back to this page carrying
    // ?flow=, and following that cross-origin redirect chain via fetch dies
    // on CORS. We hand the window to kratos and never resolve — a missing
    // session bounces to the login UI the same way.
    const createFlow = async (): Promise<never> => {
      window.location.assign(
        new URL("/self-service/settings/browser", kratosURL),
      )
      return new Promise<never>(() => {})
    }
    const [flow, aal2] = await Promise.all([
      loadOrCreateFlow("/settings", deps.flow, createFlow, async (id) => {
        // A persisted ?flow= can point at an expired flow — navigate the
        // create path instead of rendering the dead-end.
        try {
          return await frontend.getSettingsFlow({ id })
        } catch {
          return createFlow()
        }
      }),
      secondFactorEnrolled(),
    ])
    return { flow, aal2 }
  },
  component: SettingsPage,
})

// A second factor is a totp, webauthn (passkey), or lookup_secret credential
// on the identity — matching the vault's aal2 gate in apps/vault/src/auth.ts.
// whoami demands the highest available AAL, so a session_aal2_required
// rejection is itself proof a second factor is paired but unused this login.
async function secondFactorEnrolled(): Promise<boolean> {
  try {
    const session = await frontend.toSession()
    const creds = session.identity?.credentials ?? {}
    return (
      session.authenticator_assurance_level === "aal2" ||
      "totp" in creds ||
      "webauthn" in creds ||
      "lookup_secret" in creds
    )
  } catch {
    try {
      const res = await fetch(new URL("/sessions/whoami", kratosURL), {
        credentials: "include",
      })
      if (res.status === 403) {
        const body = (await res.json().catch(() => null)) as {
          error?: { id?: string; redirect_browser_to?: string }
        } | null
        const err = body?.error
        return (
          err?.id === "session_aal2_required" ||
          (err?.redirect_browser_to ?? "").includes("aal=aal2")
        )
      }
    } catch {
      // whoami unreachable — leave the banner up
    }
    return false
  }
}

function SettingsPage() {
  const { flow, aal2: enrolled } = Route.useLoaderData()
  const [aal2, setAal2] = useState(enrolled)
  useEffect(() => setAal2(enrolled), [enrolled])
  // The Elements card saves a factor in place without reloading — poll the
  // session until the second factor lands so the banner flips live.
  useEffect(() => {
    if (aal2) {
      return
    }
    let live = true
    const timer = window.setInterval(() => {
      void secondFactorEnrolled().then((yes) => {
        if (live && yes) {
          setAal2(true)
        }
      })
    }, 2500)
    return () => {
      live = false
      window.clearInterval(timer)
    }
  }, [aal2])
  return (
    <main className="bg-kumo-canvas mx-auto flex min-h-dvh w-full max-w-md flex-col gap-4 px-4 py-10">
      <Banner
        variant={aal2 ? "secondary" : "alert"}
        title={aal2 ? "Vault unlocked" : "Pair a second factor"}
        description={
          aal2
            ? "Save your recovery codes offline. You can also add a passkey as another authenticator."
            : "Pair TOTP or a passkey below, then continue to the vault."
        }
      />
      <KratosFlow
        flow={flow}
        title="Settings"
        subtitle="Manage your credentials"
        layout="embedded"
      />
      <p className="veil-settings-continue">
        <Link href="https://app.veil.nyc" variant="inline">
          Continue to Veil Vault →
        </Link>
      </p>
    </main>
  )
}
