import path from "node:path"
import { fileURLToPath } from "node:url"
import { cloudflare } from "@cloudflare/vite-plugin"
import tailwindcss from "@tailwindcss/vite"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

const root = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  // Absolute base: relative ("./") breaks on nested routes — /oidc/callback
  // resolves ./assets to /oidc/assets and the module never executes, so the
  // sign-in callback renders a blank page forever. The Mac app loads the
  // bundle at veil-app://vault/, where /assets still resolves under the
  // scheme root — VaultScheme maps it to Resources/vault/assets.
  base: "/",
  plugins: [
    tanstackRouter({
      routesDirectory: path.join(root, "src/routes"),
      generatedRouteTree: path.join(root, "src/routeTree.gen.ts"),
    }),
    react(),
    tailwindcss(),
    cloudflare(),
  ],
  server: {
    port: 4470,
    strictPort: true,
  },
  preview: {
    port: 4470,
  },
})
