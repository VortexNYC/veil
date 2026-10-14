import path from "node:path"
import { fileURLToPath } from "node:url"
import { cloudflare } from "@cloudflare/vite-plugin"
import tailwindcss from "@tailwindcss/vite"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

const root = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  // Relative base so the same dist serves app.veil.nyc and the Mac app's
  // bundled vault (loaded via a custom URL scheme, not a web root).
  base: "./",
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
