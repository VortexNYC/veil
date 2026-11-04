package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/fill"
)

const agentFillStepTotal = 4

func agentFillAutoApproveAllowed() bool {
	return os.Getenv("VEIL_CI") == "1" || os.Getenv("VEIL_ALLOW_AUTO_APPROVE") == "1"
}

func agentFillInteractiveConfirm(opts agentFillRunOpts) bool {
	if !opts.Confirm {
		return false
	}
	if os.Getenv("VEIL_AGENTIC_INTERACTIVE") == "1" {
		return stdinIsTTY()
	}
	// Cursor / agent CLI: no blocking [y/N] — approve in Veil.app; watch unblocks fill run.
	return false
}

func agentFillRun(cmd *cobra.Command, opts agentFillRunOpts) error {
	if originBase() == "" {
		return fmt.Errorf("agent fill run: VEIL_ORIGIN is required")
	}
	itemRef := strings.TrimSpace(opts.Item)
	pageURL := strings.TrimSpace(opts.URL)
	if itemRef == "" || pageURL == "" {
		return fmt.Errorf("agent fill run: --item and --url are required")
	}
	agentName := strings.TrimSpace(opts.Agent)
	if agentName == "" {
		agentName = strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	}
	if agentName == "" {
		return fmt.Errorf("agent fill run: set --agent or VEIL_AGENT")
	}

	user, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	vaultHome := strings.TrimSpace(opts.VaultHome)
	if vaultHome == "" {
		vaultHome = filepath.Join(user, ".veil")
	}
	session := strings.TrimSpace(opts.Session)
	if session == "" {
		session = agentFillDefaultSession(itemRef)
	}
	sessionClosed := false
	didCloseSession := false
	closeWhenDone := !opts.KeepOpen
	closeSession := func() {
		if opts.KeepOpen || didCloseSession {
			return
		}
		c := exec.CommandContext(context.Background(), "agent-browser", "close", "--session", session)
		c.Env = os.Environ()
		if err := c.Run(); err == nil {
			didCloseSession = true
			sessionClosed = true
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "closed agent-browser session %q\n", session)
		}
	}
	if closeWhenDone {
		defer closeSession()
	}
	profile := strings.TrimSpace(opts.Profile)
	if profile == "" {
		safeSession := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return '-'
		}, session)
		profile = filepath.Join(vaultHome, "profiles", safeSession)
	}
	if err := fill.InstallNativeMessagingProfile(vaultHome, profile); err != nil {
		return err
	}

	sock := filepath.Join(vaultHome, "fill.sock")
	if st, err := os.Stat(sock); err != nil || st.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("fill bridge not running — start Veil.app or: veil fill --bridge")
	}

	agentFillPhase(cmd, 1, agentFillStepTotal, "Prepare",
		"Extension profile, native host, and fill bridge for this session.")

	ext := strings.TrimSpace(opts.Extension)
	if ext == "" {
		ext = filepath.Join(vaultHome, "extension")
	}

	if exe := strings.TrimSpace(os.Getenv("AGENT_BROWSER_EXECUTABLE_PATH")); exe == "" {
		if cf := fill.DefaultAgentBrowserChrome(user); cf != "" {
			_ = os.Setenv("AGENT_BROWSER_EXECUTABLE_PATH", cf)
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "AGENT_BROWSER_EXECUTABLE_PATH=%s\n", cf)
		}
	}

	itemID, err := originGrantResolveItemID(cmd, agentName, itemRef, true)
	if err != nil {
		return err
	}

	if err := originAgentEnsureFresh(cmd, agentName); err != nil {
		return err
	}

	if opts.OpenBrowser {
		agentFillPhase(cmd, 2, agentFillStepTotal, "Open login page",
			fmt.Sprintf("Headed Chrome session %q — approve fills in this window when prompted.", session))
		vb, err := exec.LookPath("vb")
		if err != nil {
			return fmt.Errorf("agent fill run: vb not on PATH — open the browser yourself or install vortex-browser")
		}
		open := exec.CommandContext(cmd.Context(), vb, session, "--headed", "--extension", ext, "--profile", profile, "open", pageURL)
		open.Stdout = cmd.ErrOrStderr()
		open.Stderr = cmd.ErrOrStderr()
		open.Env = append(os.Environ(),
			"AGENT_BROWSER_SESSION="+session,
			"VEIL_ORIGIN="+originBase(),
		)
		if err := open.Run(); err != nil {
			return fmt.Errorf("vb open: %w", err)
		}
		time.Sleep(2 * time.Second)
	}

	out, err := originAgentFillRequestJSON(cmd, agentName, itemRef, pageURL)
	if err != nil {
		return err
	}
	requestID := out.RequestID
	approvalURL := out.ApprovalURL

	agentFillPhase(cmd, 3, agentFillStepTotal, "Human approval",
		"Same request everywhere — Veil.app, iOS, or veil request confirm.")
	agentFillOpenApprovalUI(cmd, approvalURL, requestID)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "request_id=%s\n", requestID)

	if strings.TrimSpace(os.Getenv("VEIL_AUTO_APPROVE")) == "1" {
		if !agentFillAutoApproveAllowed() {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: ignoring VEIL_AUTO_APPROVE — approve in Veil.app (step 3/4), or set VEIL_CI=1 for automation\n",
			)
		} else if err := originRequestResolveQuiet(cmd, requestID, "approve", 15*time.Minute); err != nil {
			return err
		} else {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "auto-approved (VEIL_AUTO_APPROVE + VEIL_CI) — running fill…\n")
		}
	} else if opts.WaitApproved || opts.Confirm {
		if err := originRequestAwaitApproval(cmd, agentName, requestID, opts.ApprovalTimeout, agentFillInteractiveConfirm(opts)); err != nil {
			return err
		}
	}

	pageMatch := pageURLMatchSubstring(pageURL)
	agentFillPhase(cmd, 4, agentFillStepTotal, "Autofill",
		"CDP drives the Veil extension — credentials never leave the fill host.")
	workerScript, workerDir, err := agentFillWorkerMaterial(vaultHome)
	if err != nil {
		return err
	}
	var workerLog bytes.Buffer
	run := exec.CommandContext(cmd.Context(), "node", filepath.Base(workerScript))
	run.Dir = workerDir
	run.Stdout = cmd.ErrOrStderr()
	run.Stderr = io.MultiWriter(cmd.ErrOrStderr(), &workerLog)
	run.Env = append(os.Environ(),
		"REQUEST_ID="+requestID,
		"VEIL_ITEM_ID="+itemID,
		"PAGE_URL_SUBSTR="+pageMatch,
		"AGENT_BROWSER_SESSION="+session,
		"VEIL_AGENTIC_PROFILE="+profile,
	)
	if opts.VerifyFields {
		run.Env = append(run.Env, "VEIL_VERIFY_FIELDS=1")
	}
	if err := run.Run(); err != nil {
		return fmt.Errorf("worker fill: %w", err)
	}
	verify := parseWorkerVeilResult(workerLog.String())
	if opts.VerifyFields {
		if verify == nil {
			return fmt.Errorf("worker fill: --verify-fields set but worker did not emit VEIL_RESULT")
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "fill proof passed (user_len=%d pass_len=%d)\n", verify.UserLen, verify.PassLen)
	}

	if strings.TrimSpace(opts.AssertURL) != "" {
		timeout := opts.AssertTimeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		if err := agentFillAssertPageURL(cmd, session, opts.AssertURL, timeout); err != nil {
			return err
		}
	}
	if opts.Hold > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "holding browser %s — finish 2FA/passkey in the headed window\n", opts.Hold.Round(time.Second))
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(opts.Hold):
		}
	}
	if opts.Snapshot {
		if _, err := exec.LookPath("agent-browser"); err != nil {
			return fmt.Errorf("snapshot: agent-browser not on PATH")
		}
		snap := exec.CommandContext(cmd.Context(), "agent-browser", "snapshot", "-i", "-c")
		snap.Env = append(os.Environ(), "AGENT_BROWSER_SESSION="+session)
		snap.Stdout = cmd.OutOrStdout()
		snap.Stderr = cmd.ErrOrStderr()
		_ = snap.Run()
	}

	res := agentFillRunResult{
		OK:            true,
		RequestID:     requestID,
		Item:          itemRef,
		ItemID:        itemID,
		URL:           pageURL,
		Session:       session,
		Verify:        verify,
		KeepOpen:      opts.KeepOpen,
		SessionClosed: sessionClosed,
	}
	switch {
	case strings.TrimSpace(opts.AssertURL) != "":
		res.NextHumanStep = "Login URL matched --success-url; session complete for automation."
	case opts.KeepOpen:
		res.NextHumanStep = "Finish 2FA or passkey in the headed window, then: veil agent fill close --session " + session
	default:
		res.NextHumanStep = "Fields filled on the login page; submit or complete 2FA if the site still requires it."
	}
	closeSession()
	if opts.EmitJSON {
		res.SessionClosed = sessionClosed
		return agentFillEmitResult(cmd, res)
	}
	return nil
}

type agentFillRunOpts struct {
	Agent, Item, URL, Session, Profile, Extension, VaultHome, AssertURL            string
	OpenBrowser, Confirm, WaitApproved, Snapshot, VerifyFields, KeepOpen, EmitJSON bool
	ApprovalTimeout, AssertTimeout, Hold                                           time.Duration
}

func pageURLMatchSubstring(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if path == "" {
		return u.Host
	}
	return u.Host + path
}

// agentFillRunMCP runs the laptop fill journey and returns structured output for MCP.
func agentFillRunMCP(ctx context.Context, item, pageURL, successURL string) (agentFillRunResult, error) {
	var zero agentFillRunResult
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(os.Stderr)
	agent := strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	opts := agentFillRunOpts{
		Agent:        agent,
		Item:         item,
		URL:          pageURL,
		AssertURL:    successURL,
		EmitJSON:     true,
		OpenBrowser:  true,
		WaitApproved: true,
		VerifyFields: true,
	}
	if err := agentFillRun(cmd, opts); err != nil {
		return zero, err
	}
	var res agentFillRunResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		return zero, fmt.Errorf("agent fill login: decode result: %w", err)
	}
	return res, nil
}
