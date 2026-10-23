import { defineConfig } from "blume";

export default defineConfig({
  title: "Veil",
  description:
    "Agent credential broker. Use injects. The model never holds the secret.",
  logo: {
    image: {
      light: "/logo-light.svg",
      dark: "/logo-dark.svg",
      alt: "Veil",
    },
    text: "Veil",
    href: "/",
  },
  github: {
    owner: "VeilNYC",
    repo: "veil",
    branch: "main",
    dir: "apps/docs",
  },
  content: { root: "content/docs" },
  navigation: {
    tabs: [
      { label: "Docs", path: "/" },
      { label: "API", path: "/reference" },
    ],
    sidebar: [
      {
        label: "Overview",
        items: ["index", "architecture", "changelog"],
      },
      {
        label: "Authentication & Identity",
        items: [
          "guides/principal-separation",
          "guides/oidc",
          "guides/wif",
          "guides/dpop",
        ],
      },
      {
        label: "Broker & Vault Features",
        items: [
          "guides/agents",
          "guides/rotation",
          "guides/passkeys",
          "guides/self-host",
        ],
      },
      {
        label: "SDKs & Clients",
        items: [
          "sdk/quickstart",
          "sdk/typescript",
          "sdk/python",
          "sdk/go",
        ],
      },
    ],
  },
  openapi: {
    enabled: true,
    route: "/reference",
    spec: "../../docs/openapi/veil.openapi.json",
    codeSamples: ["curl", "js", "python"],
  },
  theme: {
    accent: "oklch(0.1448 0 0)",
    background: {
      light: "oklch(0.947 0.0074 80.72)",
      dark: "oklch(0 0 0)",
    },
    radius: "md",
    mode: "system",
    fonts: {
      display: {
        name: "Hedvig Letters Serif",
        fallback: "serif",
        variants: [{ src: "./fonts/hedvig-letters-serif.woff2", weight: 400 }],
      },
      body: {
        name: "Hedvig Letters Sans",
        fallback: "sans",
        variants: [{ src: "./fonts/hedvig-letters-sans.woff2", weight: 400 }],
      },
      mono: "ibm-plex-mono",
    },
  },
  lastModified: true,
  export: true,
  ai: {
    llmsTxt: true,
    ask: { enabled: false },
    mcp: {
      enabled: true,
      route: "/mcp",
      name: "Veil Docs",
      instructions:
        "Documentation for Veil, the agent credential broker. OpenAPI is the contract. listItems and useItem never return vault secrets. MCP tools list_items and fetch are the same operations.",
    },
  },
  seo: {
    og: { enabled: true },
    sitemap: true,
    robots: true,
    structuredData: true,
  },
  deployment: {
    output: "server",
    adapter: "cloudflare",
    site: "https://veil.nyc",
    base: "/docs",
  },
});
