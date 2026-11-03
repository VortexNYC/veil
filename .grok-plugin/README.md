# Grok connector (HTTP MCP)

Do not commit real secrets. Generate your config:

```bash
export VEIL_ORIGIN=https://veil.nyc
export VEIL_AGENT=grok
veil agent hydra grok --secret-file ~/.config/vortex/veil/grok.hydra
veil mcp http
```

Paste the output into [grok.com/connectors](https://grok.com/connectors) → Custom, or merge into Grok Bot MCP settings.

See `docs/grok.md` and `mcp.http.example.json` for shape only.
