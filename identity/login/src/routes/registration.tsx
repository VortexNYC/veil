import { createFileRoute, Link } from "@tanstack/react-router"

export const Route = createFileRoute("/registration")({
  component: InviteOnly,
})

function InviteOnly() {
  return (
    <main>
      <h1>Veil is invite-only</h1>
      <p>
        Veil is in private alpha. Accounts are created by invitation — ask the
        person who invited you for your setup link, which arrives by email.
      </p>
      <p>
        <Link to="/login">Back to log in</Link>
      </p>
    </main>
  )
}
