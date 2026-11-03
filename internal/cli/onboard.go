package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func onboardCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "onboard",
		Short: "Human setup checklist for you and your coding agents (no secrets).",
	}
	c.AddCommand(onboardStatusCmd())
	c.AddCommand(onboardGuideCmd())
	return c
}

func onboardStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "JSON checklist — paste to a coding agent or read in the SPA.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if originBase() == "" {
				return fmt.Errorf("onboard status: set VEIL_ORIGIN and VEIL_HUMAN_TOKEN_FILE")
			}
			tok, err := originHumanTokenLive(cmd.Context())
			if err != nil {
				return err
			}
			raw, err := originDo(cmd.Context(), "GET", "/v1/onboarding", tok, nil)
			if err != nil {
				return err
			}
			var pretty json.RawMessage
			if err := json.Unmarshal(raw, &pretty); err != nil {
				return err
			}
			out, err := json.MarshalIndent(pretty, "", "  ")
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
}

func onboardGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Markdown playbook for humans + agents (stdout only).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if originBase() == "" {
				return fmt.Errorf("onboard guide: set VEIL_ORIGIN and VEIL_HUMAN_TOKEN_FILE")
			}
			tok, err := originHumanTokenLive(cmd.Context())
			if err != nil {
				return err
			}
			raw, err := originDo(cmd.Context(), "GET", "/v1/onboarding", tok, nil)
			if err != nil {
				return err
			}
			var view struct {
				Complete bool   `json:"complete"`
				Role     string `json:"role"`
				Steps    []struct {
					Title       string `json:"title"`
					Done        bool   `json:"done"`
					HumanAction string `json:"human_action"`
					AgentHint   string `json:"agent_hint"`
				} `json:"steps"`
				ImportFormats  []string          `json:"import_formats"`
				ImportCaveats  []string          `json:"import_caveats"`
				McpTools       []string          `json:"mcp_tools"`
				McpURL         string            `json:"mcp_url"`
				Docs           map[string]string `json:"docs"`
				AgentPlaybooks []struct {
					Title string   `json:"title"`
					Steps []string `json:"steps"`
				} `json:"agent_playbooks"`
			}
			if err := json.Unmarshal(raw, &view); err != nil {
				return err
			}
			var b strings.Builder
			b.WriteString("# Veil onboarding\n\n")
			if view.Complete {
				b.WriteString("Status: **ready** for agent Use (grants + items in place).\n\n")
			} else {
				b.WriteString("Status: **in progress** — finish the open steps below.\n\n")
			}
			b.WriteString("## Checklist\n\n")
			for _, s := range view.Steps {
				mark := "[ ]"
				if s.Done {
					mark = "[x]"
				}
				fmt.Fprintf(&b, "- %s **%s** — %s\n", mark, s.Title, s.HumanAction)
				if s.AgentHint != "" {
					fmt.Fprintf(&b, "  - Agent: %s\n", s.AgentHint)
				}
			}
			b.WriteString("\n## Import formats (human / owner only)\n\n")
			for _, f := range view.ImportFormats {
				fmt.Fprintf(&b, "- %s\n", f)
			}
			if len(view.ImportCaveats) > 0 {
				b.WriteString("\n## Import caveats\n\n")
				for _, c := range view.ImportCaveats {
					fmt.Fprintf(&b, "- %s\n", c)
				}
			}
			fmt.Fprintf(&b, "\nCloud MCP URL: `%s`\n\n", view.McpURL)
			if len(view.McpTools) > 0 {
				b.WriteString("MCP tools: ")
				b.WriteString(strings.Join(view.McpTools, ", "))
				b.WriteString("\n\n")
			}
			if len(view.Docs) > 0 {
				b.WriteString("## Docs\n\n")
				for k, u := range view.Docs {
					fmt.Fprintf(&b, "- %s: %s\n", k, u)
				}
				b.WriteString("\n")
			}
			b.WriteString("## Agent playbooks\n\n")
			for _, p := range view.AgentPlaybooks {
				fmt.Fprintf(&b, "### %s\n\n", p.Title)
				for i, step := range p.Steps {
					fmt.Fprintf(&b, "%d. %s\n", i+1, step)
				}
				b.WriteString("\n")
			}
			b.WriteString("Agents never receive vault secrets. Import, invite, and agent creation stay off MCP by design.\n")
			_, _ = fmt.Fprint(cmd.OutOrStdout(), b.String())
			return nil
		},
	}
}
