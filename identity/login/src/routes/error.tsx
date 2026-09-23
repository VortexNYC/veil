import { createFileRoute, Link } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { frontend } from "../ory"

export const Route = createFileRoute("/error")({
  validateSearch: (search: Record<string, unknown>): { id?: string } => ({
    id: typeof search.id === "string" ? search.id : undefined,
  }),
  component: ErrorPage,
})

function ErrorPage() {
  const { id } = Route.useSearch()
  const [disabled, setDisabled] = useState(false)

  useEffect(() => {
    if (!id) return
    void frontend
      .getFlowError({ id })
      .then((res) => {
        const err = (res as { error?: { code?: number; message?: string } }).error
        if (err?.message?.includes("disabled") || err?.code === 404) {
          setDisabled(true)
        }
      })
      .catch(() => {})
  }, [id])

  if (disabled) {
    return (
      <main>
        <h1>Veil is invite-only</h1>
        <p>
          Veil is in private alpha. Accounts are created by invitation — your
          setup link arrives by email from the person who invited you.
        </p>
        <p>
          <Link to="/login">Back to log in</Link>
        </p>
      </main>
    )
  }

  return (
    <main>
      <p>Something went wrong.</p>
      <p>
        <Link to="/login">Log in</Link>
      </p>
    </main>
  )
}
