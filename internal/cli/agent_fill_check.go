package cli

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/cli/agenticfill"
	"github.com/VortexNYC/veil/internal/fill"
)

type fillCheckIssue struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

func agentFillCheck(cmd *cobra.Command, agentName string) error {
	if originBase() == "" {
		return fmt.Errorf("agent fill check: VEIL_ORIGIN is required")
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	}
	if agentName == "" {
		return fmt.Errorf("agent fill check: set --agent or VEIL_AGENT")
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	vault := filepath.Join(user, ".veil")
	sock := filepath.Join(vault, "fill.sock")
	ext := filepath.Join(vault, "extension")

	report := map[string]fillCheckIssue{
		"origin": {OK: true, Detail: originBase()},
	}

	if _, err := originAgentCLI(cmd.Context(), agentName); err != nil {
		report["agent_jwt"] = fillCheckIssue{Fix: "veil agent token --secret-file … --out-file ~/.config/vortex/veil/" + agentName + ".jwt"}
	} else {
		tok, _ := originAgentCLI(cmd.Context(), agentName)
		if _, err := originDo(cmd.Context(), http.MethodGet, "/v1/items", tok, nil); err != nil {
			report["agent_jwt"] = fillCheckIssue{
				Detail: "file present but origin rejected bearer (often expired)",
				Fix:    "veil agent token " + agentName + " --secret-file ~/.config/vortex/veil/" + agentName + ".hydra --out-file ~/.config/vortex/veil/" + agentName + ".jwt",
			}
		} else {
			report["agent_jwt"] = fillCheckIssue{OK: true}
		}
	}
	if _, err := originHumanCLI(cmd.Context()); err != nil {
		report["human_jwt"] = fillCheckIssue{
			Detail: "optional for CLI [y/N]; required for request watch SSE",
			Fix:    "veil human login --out-file ~/.config/veil/human.jwt",
		}
	} else {
		report["human_jwt"] = fillCheckIssue{OK: true}
	}
	if st, err := os.Stat(sock); err != nil || st.Mode()&os.ModeSocket == 0 {
		report["fill_bridge"] = fillCheckIssue{Fix: "Veil.app or: veil fill --bridge"}
	} else {
		report["fill_bridge"] = fillCheckIssue{OK: true}
	}
	if st, err := os.Stat(ext); err != nil || !st.IsDir() {
		report["extension"] = fillCheckIssue{Fix: "veil fill install"}
	} else {
		report["extension"] = fillCheckIssue{OK: true, Detail: ext}
	}
	host := fill.HostPath(vault)
	if st, err := os.Stat(host); err != nil || st.IsDir() {
		report["native_host"] = fillCheckIssue{Fix: "veil fill install"}
	} else {
		report["native_host"] = fillCheckIssue{OK: true}
	}
	if cf := fill.DefaultAgentBrowserChrome(user); cf == "" {
		report["chrome_for_testing"] = fillCheckIssue{
			Fix: "install agent-browser / CfT; set AGENT_BROWSER_EXECUTABLE_PATH",
		}
	} else {
		report["chrome_for_testing"] = fillCheckIssue{OK: true, Detail: cf}
	}
	if _, err := exec.LookPath("vb"); err != nil {
		report["vb"] = fillCheckIssue{Fix: "vortex-browser on PATH (agent-browser wrapper)"}
	} else {
		report["vb"] = fillCheckIssue{OK: true}
	}
	script, dir, werr := agentFillWorkerMaterial(vault)
	if werr != nil {
		report["worker_fill"] = fillCheckIssue{Fix: werr.Error()}
	} else if !agenticfill.PlaywrightInstalled(dir) {
		report["worker_fill"] = fillCheckIssue{
			Detail: script,
			Fix:    "pnpm install --dir " + dir + "  (playwright-core for CDP worker)",
		}
	} else {
		report["worker_fill"] = fillCheckIssue{OK: true, Detail: script}
	}

	allOK := true
	for k, v := range report {
		if !v.OK {
			allOK = false
			if v.Fix == "" {
				report[k] = fillCheckIssue{OK: false, Detail: v.Detail, Fix: "see docs/guides/agentic-autofill.md"}
			}
		}
	}
	out := map[string]any{"ready": allOK, "checks": report}
	if err := encode(cmd, out); err != nil {
		return err
	}
	if !allOK {
		return fmt.Errorf("agent fill check: not ready — fix items above (Cursor agents: use this command, not shell scripts)")
	}
	return nil
}

func agentFillWorkerMaterial(vaultHome string) (script, dir string, err error) {
	if s, d, e := agenticfill.WorkerDir(); e == nil {
		return s, d, nil
	}
	dir = filepath.Join(vaultHome, "agentic")
	script = filepath.Join(dir, "worker-fill.mjs")
	if st, e := os.Stat(script); e == nil && !st.IsDir() {
		return script, dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(script, agenticfill.WorkerFillJS, 0o644); err != nil {
		return "", "", err
	}
	if !agenticfill.PlaywrightInstalled(dir) {
		return script, dir, fmt.Errorf("playwright-core missing beside worker — pnpm install --dir %s or set VEIL_ROOT to repo", dir)
	}
	return script, dir, nil
}
