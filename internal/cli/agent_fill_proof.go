package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type agentFillPageProof struct {
	State      string `json:"login_state"`
	PageURL    string `json:"page_url,omitempty"`
	PageTitle  string `json:"page_title,omitempty"`
	Screenshot string `json:"screenshot,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

func agentFillCapturePageProof(cmd *cobra.Command, vaultHome, session, startURL string, verify *agentFillVerify) agentFillPageProof {
	proof := agentFillPageProof{State: "no_browser"}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		proof.Summary = "agent-browser not on PATH — cannot capture page proof"
		return proof
	}
	dir := filepath.Join(vaultHome, "proofs")
	_ = os.MkdirAll(dir, 0o755)
	stamp := time.Now().Format("20060102-150405")
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, session)
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.png", safe, stamp))
	env := append(os.Environ(), "AGENT_BROWSER_SESSION="+session)

	shot := exec.CommandContext(cmd.Context(), "agent-browser", "screenshot", path)
	shot.Env = env
	shot.Stderr = cmd.ErrOrStderr()
	if err := shot.Run(); err != nil {
		proof.Summary = "screenshot failed — is the headed session still open?"
		return proof
	}
	proof.Screenshot = path

	pageURL := agentBrowserGet(cmd, env, "url")
	pageTitle := agentBrowserGet(cmd, env, "title")
	signInCTA := agentFillPageHasSignInCTA(cmd, env)
	proof.PageURL = pageURL
	proof.PageTitle = pageTitle
	proof.State, proof.Summary = classifyLoginPage(startURL, pageURL, pageTitle, verify, signInCTA)

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"\n━━ Veil login · proof ━━\nlogin_state=%s\npage_url=%s\nscreenshot=%s\n%s\n",
		proof.State, pageURL, path, proof.Summary,
	)
	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", path).Run()
	}
	return proof
}

func agentBrowserGet(cmd *cobra.Command, env []string, sub string) string {
	c := exec.CommandContext(cmd.Context(), "agent-browser", "get", sub, "--json")
	c.Env = env
	out, err := c.Output()
	if err != nil {
		return ""
	}
	var wrap struct {
		Data struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &wrap) != nil {
		return strings.TrimSpace(string(out))
	}
	if sub == "title" {
		return strings.TrimSpace(wrap.Data.Title)
	}
	return strings.TrimSpace(wrap.Data.URL)
}

func agentFillPageHasSignInCTA(cmd *cobra.Command, env []string) bool {
	c := exec.CommandContext(cmd.Context(), "agent-browser", "snapshot", "-i", "-c")
	c.Env = env
	out, err := c.Output()
	if err != nil {
		return false
	}
	s := string(out)
	return strings.Contains(s, "Sign in") || strings.Contains(s, "link \"Sign in\"")
}

func classifyLoginPage(startURL, nowURL, title string, verify *agentFillVerify, signInCTA bool) (state, summary string) {
	now := strings.ToLower(nowURL + " " + title)
	_ = startURL
	if verify != nil && verify.HasUser && verify.UserLen == 0 {
		return "vault_missing_username",
			"Vault login item has no username/email — edit at app.veil.nyc, then retry."
	}
	if strings.Contains(now, "two-factor") || strings.Contains(now, "/sessions/two-factor") ||
		strings.Contains(now, "verification") && strings.Contains(now, "github") {
		return "pending_2fa", "Password step succeeded — finish 2FA in this browser window."
	}
	if signInCTA && strings.Contains(strings.ToLower(nowURL), "github.com") {
		return "not_logged_in", "GitHub still shows Sign in — not authenticated in this browser. Open screenshot."
	}
	if strings.Contains(strings.ToLower(nowURL), "/login") ||
		(strings.Contains(strings.ToLower(startURL), "github.com") && strings.Contains(strings.ToLower(nowURL), "github.com/login")) {
		return "still_on_login", "Still on the sign-in page — not logged in. Open the screenshot and compare."
	}
	if strings.Contains(strings.ToLower(nowURL), "github.com/session") {
		return "pending_2fa", "GitHub accepted credentials — finish 2FA or device verification in the browser."
	}
	if nowURL != "" && !strings.Contains(strings.ToLower(nowURL), "/login") &&
		!strings.Contains(strings.ToLower(nowURL), "github.com/session") && !signInCTA {
		return "logged_in", "Left login flow without Sign in CTA — confirm in screenshot."
	}
	return "not_logged_in", "Not signed in — inspect proof screenshot and vault password (sync from 1Password if needed)."
}

func loginOutcomeOK(state string) bool {
	switch state {
	case "logged_in", "pending_2fa":
		return true
	default:
		return false
	}
}

func nextStepForLoginState(state, session string) string {
	switch state {
	case "logged_in":
		return "Signed in on this browser session — close when done: veil agent fill close --session " + session
	case "pending_2fa":
		return "Complete 2FA in the headed window, then re-run with --success-url or check screenshot."
	case "vault_missing_username":
		return "Edit the login item in app.veil.nyc (username/email field), then run veil agent fill login again."
	case "still_on_login":
		return "Not logged in — inspect proof screenshot; fix vault username or approve/submit flow."
	default:
		return "Inspect proof screenshot under ~/.veil/proofs/"
	}
}
