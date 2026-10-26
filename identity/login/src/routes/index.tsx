import { createFileRoute, Link } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import type { Session } from "@ory/client-fetch"
import { frontend } from "../ory"

export const Route = createFileRoute("/")({
  component: Home,
})

function Home() {
  // undefined = session probe in flight; render nothing rather than
  // flashing the wrong state and swapping underneath the user.
  const [session, setSession] = useState<Session | null | undefined>(undefined)

  useEffect(() => {
    void frontend.toSession().then(setSession).catch(() => setSession(null))
  }, [])

  if (session === undefined) {
    return <main />
  }

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
        <p>
          New to Veil? <Link to="/registration">Create an account</Link> —
          public beta, 100 free uses a month.
        </p>
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
