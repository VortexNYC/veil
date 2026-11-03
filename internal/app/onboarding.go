package app

import (
	"os"
	"strings"

	"github.com/VortexNYC/veil/internal/protocol"
)

// OnboardingStep is one human-visible milestone. AgentHint tells a coding
// agent what it can do without owner secrets; import stays off MCP by design.
type OnboardingStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Done        bool   `json:"done"`
	Detail      string `json:"detail"`
	HumanAction string `json:"human_action"`
	AgentHint   string `json:"agent_hint"`
	Href        string `json:"href,omitempty"`
}

// AgentPlaybook is a non-secret recipe for wiring a deployed or local agent.
type AgentPlaybook struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

// OnboardingView is the owner/member setup checklist. No secrets.
type OnboardingView struct {
	Complete       bool              `json:"complete"`
	Role           string            `json:"role"`
	ItemCount      int               `json:"item_count"`
	AgentCount     int               `json:"agent_count"`
	GrantCount     int               `json:"grant_count"`
	Steps          []OnboardingStep  `json:"steps"`
	ImportFormats  []string          `json:"import_formats"`
	ImportCaveats  []string          `json:"import_caveats"`
	McpTools       []string          `json:"mcp_tools"`
	McpURL         string            `json:"mcp_url"`
	HydraIssuer    string            `json:"hydra_issuer"`
	Docs           map[string]string `json:"docs"`
	AgentPlaybooks []AgentPlaybook   `json:"agent_playbooks"`
}

var importFormatLabels = []string{
	"1Password (.1pux export or `veil item import --from-op`)",
	"Bitwarden (CSV or unencrypted JSON export)",
	"Apple Passwords CSV",
	"LastPass, Dashlane, Google Passwords CSV",
	"Proton Pass zip export",
	"KeePass XML export",
	"otpauth:// seed file",
}

var importCaveatLabels = []string{
	"1Password Grok integration is separate — migrate with import, then wire Veil MCP (docs/grok.md).",
	"Passkeys and Sign-in-with-Google logins often do not survive .1pux export — re-register or keep human fill.",
	"Repeat import skips matching rows; it does not overwrite passwords rotated in Veil after import.",
	"1Password for Agents (Grok Bot Password Managers) only covers web login autofill — not API MCP inject.",
	"1Password also ships Muse, Instinct, Town today; Anthropic/OpenAI/Browserbase/etc. on waitlist — see docs/1password-for-agents-landscape.md.",
}

var mcpToolNames = []string{
	"list_items",
	"match_items",
	"check",
	"fetch",
	"approval_status",
}

func onboardingDocs() map[string]string {
	return map[string]string{
		"onboarding":          "https://veil.nyc/docs/guides/onboarding",
		"grok":                "https://veil.nyc/docs/guides/grok",
		"migration_1password": "https://veil.nyc/docs/guides/migration-1password",
		"1p_agents_landscape": "https://veil.nyc/docs/guides/1password-for-agents-landscape",
		"agents":              "https://veil.nyc/docs/guides/agents",
	}
}

// OnboardingFor builds the setup checklist for a signed-in human.
func (a *App) OnboardingFor(p protocol.Principal) (OnboardingView, error) {
	view := OnboardingView{
		ImportFormats: append([]string(nil), importFormatLabels...),
		ImportCaveats: append([]string(nil), importCaveatLabels...),
		McpTools:      append([]string(nil), mcpToolNames...),
		McpURL:        strings.TrimSpace(a.mcpPublicURL()),
		HydraIssuer:   strings.TrimSpace(onboardingHydraIssuer()),
		Docs:          onboardingDocs(),
		AgentPlaybooks: []AgentPlaybook{
			{
				ID:    "cursor_laptop",
				Title: "Cursor / Codex on your Mac (stdio MCP)",
				Steps: []string{
					"Owner completes this checklist in app.veil.nyc or runs `veil onboard status`.",
					"Owner creates agent + grant in Vault → Agents / Grants (or `veil agent` + `veil grant add`).",
					"Owner runs `veil agent hydra AGENT --secret-file ~/.config/vortex/veil/AGENT.hydra` (never paste secret into chat).",
					"Point MCP at `veil mcp stdio` or `veil mcp laptop` with VEIL_ORIGIN=https://veil.nyc.",
					"Agent MCP tools: list_items, match_items, check, fetch, approval_status — human owns import, invite, agents.",
				},
			},
			{
				ID:    "grok_cloud",
				Title: "Grok (grok.com custom MCP — HTTPS)",
				Steps: []string{
					"Finish import + agent + grants (checklist above). Create agent `grok`, grant level2 on API items.",
					"Run `VEIL_AGENT=grok veil mcp http` — paste JSON into grok.com/connectors → Custom. URL: https://veil.nyc/mcp.",
					"Bearer is agent-grok:<hydra_client_secret> — broker remints at Hydra; never ${file:} in cloud JSON.",
					"Level1 grants: human approves at app.veil.nyc/requests; agent polls approval_status then retries fetch.",
					"Not a 1Password Grok Password Managers link — see docs/grok.md for web login (fill) vs API (MCP).",
				},
			},
			{
				ID:    "grok_bot_desktop",
				Title: "Grok Bot app (desktop / mobile)",
				Steps: []string{
					"Same vault checklist. Prefer custom MCP to https://veil.nyc/mcp when Grok Bot supports HTTP connectors.",
					"If Grok spawns local MCP: `veil mcp laptop` with grok.hydra on PATH — parent must be Grok or set VEIL_AGENT=grok.",
					"Website tasks (Airbnb, …): install Veil fill on the device Grok’s browser uses — MCP fetch does not fill login forms.",
					"Disconnect 1Password from Grok Bot after migrating if Veil is the only store you want agents to use.",
				},
			},
			{
				ID:    "coding_agent_shell",
				Title: "Coding agent + human in the terminal",
				Steps: []string{
					"Human approves `veil onboard status` — agent reads JSON steps and narrates what is left.",
					"Human approves owner commands: `veil item import FILE`, `veil human invite`, `veil grant add` — agent must not receive JWT or export files in chat.",
					"After grants exist, agent may call MCP tools; until then it can only guide.",
				},
			},
		},
	}
	owner, err := a.OwnsVault(p)
	if err != nil {
		return OnboardingView{}, err
	}
	if owner {
		view.Role = "owner"
		return a.ownerOnboarding(p, view)
	}
	view.Role = "member"
	return a.memberOnboarding(p, view)
}

func (a *App) ownerOnboarding(p protocol.Principal, view OnboardingView) (OnboardingView, error) {
	items, err := a.Store.ListItems()
	if err != nil {
		return view, err
	}
	agents, err := a.Store.ListAgents()
	if err != nil {
		return view, err
	}
	grants, err := a.Store.ListGrants()
	if err != nil {
		return view, err
	}
	itemN := 0
	for _, it := range items {
		if it.OrgID == p.OrgID && !it.Archived {
			itemN++
		}
	}
	agentN := 0
	var agentIDs []string
	for _, ag := range agents {
		if ag.OrgID == p.OrgID {
			agentN++
			agentIDs = append(agentIDs, ag.ID)
		}
	}
	grantN := 0
	grantsByAgent := map[string]int{}
	for _, g := range grants {
		if g.OrgID != p.OrgID || g.Subject() != protocol.SubjectAgent {
			continue
		}
		grantN++
		if g.AgentID != "" {
			grantsByAgent[g.AgentID]++
		}
	}
	view.ItemCount = itemN
	view.AgentCount = agentN
	view.GrantCount = grantN

	hasItems := itemN > 0
	hasAgent := agentN > 0
	allAgentsGranted := hasAgent
	var ungranted []string
	for _, id := range agentIDs {
		if grantsByAgent[id] == 0 {
			allAgentsGranted = false
			ungranted = append(ungranted, id)
		}
	}

	view.Steps = []OnboardingStep{
		{
			ID:          "account",
			Title:       "Account and org",
			Done:        true,
			Detail:      "Signed in with TOTP; org provisioned.",
			HumanAction: "Invite teammates from Invites when ready.",
			AgentHint:   "Agent cannot provision humans — guide them to app.veil.nyc or login.veil.nyc recovery.",
			Href:        "/invites",
		},
		{
			ID:          "import",
			Title:       "Bring your passwords",
			Done:        hasItems,
			Detail:      "One-shot import onto origin — not sync, not MCP.",
			HumanAction: "Items → Import, or `veil item import export.1pux` / `--from-op` with VEIL_HUMAN_TOKEN_FILE.",
			AgentHint:   "Ask the human to export from their old manager and Import in the SPA, or approve `veil item import` locally — you cannot import over MCP.",
			Href:        "/items",
		},
		{
			ID:          "agents",
			Title:       "Create coding agents",
			Done:        hasAgent,
			Detail:      "Each bot (Cursor, Grok, Devin, …) is its own Hydra client.",
			HumanAction: "Agents → Add agent, or `veil agent add NAME` on origin.",
			AgentHint:   "You cannot create agents via MCP. Tell the human to add `cursor` / `grok` in Vault → Agents.",
			Href:        "/agents",
		},
		{
			ID:    "grants",
			Title: "Grant agents access",
			Done:  allAgentsGranted && hasAgent,
			Detail: func() string {
				if len(ungranted) == 0 {
					return "Every agent has at least one grant."
				}
				return "Agents without grants: " + strings.Join(ungranted, ", ")
			}(),
			HumanAction: "Grants → grant each agent items (level 2 for unattended bots).",
			AgentHint:   "list_items empty usually means no grant — human must grant this agent id in Vault → Grants.",
			Href:        "/grants",
		},
		{
			ID:          "mcp",
			Title:       "Wire MCP (Cursor, Grok, Devin, …)",
			Done:        allAgentsGranted && hasAgent && hasItems,
			Detail:      "Five tools: " + strings.Join(mcpToolNames, ", ") + ". Cloud: " + view.McpURL,
			HumanAction: "Grok: `VEIL_AGENT=grok veil mcp http` → grok.com/connectors Custom. Laptop: `veil mcp laptop`. Docs: " + view.Docs["grok"],
			AgentHint:   "After grants, list_items → match_items → fetch. check files level1 asks; approval_status polls Requests.",
			Href:        "/agents",
		},
		{
			ID:          "fill",
			Title:       "Fill + Grok web login (optional)",
			Done:        hasItems,
			Detail:      "MCP does not fill HTML login forms. Grok Bot website tasks need Veil extension + host on that device.",
			HumanAction: "Mac: Veil.app + Chrome/Safari extension. Enable Settings → Agent approval alerts for level1 while on mobile.",
			AgentHint:   "Execute fill: agent moves cursor; host writes after Touch ID. Not 1Password Password Managers OAuth.",
			Href:        "/settings",
		},
	}
	view.Complete = hasItems && hasAgent && allAgentsGranted
	return view, nil
}

func (a *App) memberOnboarding(p protocol.Principal, view OnboardingView) (OnboardingView, error) {
	items, err := a.ItemsForPrincipal(p)
	if err != nil {
		return view, err
	}
	n := len(items)
	view.ItemCount = n
	view.Steps = []OnboardingStep{
		{
			ID:          "access",
			Title:       "Vault access",
			Done:        n > 0,
			Detail:      "You see items you own or that were granted to you.",
			HumanAction: "Save new logins from the extension or ask your owner for a grant.",
			AgentHint:   "Members cannot import org-wide or create agents — escalate to the org owner.",
			Href:        "/items",
		},
	}
	view.Complete = n > 0
	return view, nil
}

func (a *App) mcpPublicURL() string {
	if u := strings.TrimSpace(os.Getenv("VEIL_MCP_URL")); u != "" {
		return u
	}
	return "https://veil.nyc/mcp"
}

func onboardingHydraIssuer() string {
	if u := strings.TrimSpace(os.Getenv("VEIL_HYDRA_ISSUER")); u != "" {
		return u
	}
	return "https://id.veil.nyc"
}
