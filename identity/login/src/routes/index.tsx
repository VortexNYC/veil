import { createFileRoute, Link } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import type { Session } from "@ory/client-fetch"
import { frontend } from "../ory"

export const Route = createFileRoute("/")({
  component: Home,
})

function Home() {
  const [session, setSession] = useState<Session | null>(null)

  useEffect(() => {
    void frontend.toSession().then(setSession).catch(() => setSession(null))
  }, [])

  const traits = session?.identity?.traits
  const email =
    traits && typeof traits === "object" && "email" in traits && typeof traits.email === "string"
      ? traits.email
      : null

  if (!session) {
    return (
      <main>
        <p>
          <Link to="/login">Log in</Link>
        </p>
        <p>Veil is in private alpha — access is by invitation.</p>
      </main>
    )
  }

  return (
    <main>
      {email ? <p>{email}</p> : null}
      <p>
        <Link to="/settings">Settings</Link>
      </p>
    </main>
  )
}
