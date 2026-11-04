package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func agentFillPhase(cmd *cobra.Command, step, total int, title, detail string) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\n━━ Veil login · %d/%d — %s ━━\n", step, total, title)
	if detail != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", detail)
	}
}

func agentFillOpenApprovalUI(cmd *cobra.Command, approvalURL, requestID string) {
	url := strings.TrimSpace(approvalURL)
	if url == "" {
		url = "https://app.veil.nyc/requests"
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Approve request %s in Veil.app / iOS (same id).\n  %s\n",
		requestID, url,
	)
	if os.Getenv("VEIL_NO_OPEN_APPROVAL") == "1" {
		return
	}
	if runtime.GOOS != "darwin" {
		return
	}
	open := exec.Command("open", url)
	open.Stderr = cmd.ErrOrStderr()
	_ = open.Run()
}

func agentFillDefaultSession(itemRef string) string {
	s := strings.ToLower(strings.TrimSpace(itemRef))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	s = strings.Trim(s, "-")
	if s == "" {
		s = "login"
	}
	return "veil-" + s
}

type agentFillRunResult struct {
	OK            bool             `json:"ok"`
	RequestID     string           `json:"request_id,omitempty"`
	Item          string           `json:"item,omitempty"`
	ItemID        string           `json:"item_id,omitempty"`
	URL           string           `json:"url,omitempty"`
	Session       string           `json:"session,omitempty"`
	Verify        *agentFillVerify `json:"verify,omitempty"`
	SessionClosed bool             `json:"session_closed"`
	KeepOpen      bool             `json:"keep_open,omitempty"`
	NextHumanStep string           `json:"next_human_step,omitempty"`
}

type agentFillVerify struct {
	URL         string `json:"url,omitempty"`
	UserLen     int    `json:"user_len,omitempty"`
	PassLen     int    `json:"pass_len,omitempty"`
	HasUser     bool   `json:"has_user,omitempty"`
	HasPassword bool   `json:"has_password,omitempty"`
}

func parseWorkerVeilResult(stderr string) *agentFillVerify {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "VEIL_RESULT=") {
			continue
		}
		var wrap struct {
			Verify agentFillVerify `json:"verify"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "VEIL_RESULT=")), &wrap); err != nil {
			continue
		}
		v := wrap.Verify
		return &v
	}
	return nil
}

func agentFillEmitResult(cmd *cobra.Command, res agentFillRunResult) error {
	return encode(cmd, res)
}

func agentFillClose(cmd *cobra.Command, session string, all bool) error {
	if _, err := exec.LookPath("agent-browser"); err != nil {
		return fmt.Errorf("agent fill close: agent-browser not on PATH")
	}
	args := []string{"close"}
	if all {
		args = append(args, "--all")
	} else if s := strings.TrimSpace(session); s != "" {
		args = append(args, "--session", s)
	}
	c := exec.CommandContext(cmd.Context(), "agent-browser", args...)
	c.Env = os.Environ()
	c.Stderr = cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return err
	}
	if all {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "closed all agent-browser sessions")
	} else {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "closed session %q\n", session)
	}
	return encode(cmd, map[string]any{"closed": true, "all": all, "session": session})
}
