package cli

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func agentFillAssertPageURL(cmd *cobra.Command, session, expected string, timeout time.Duration) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		return fmt.Errorf("assert-url: agent-browser not on PATH")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		current, err := agentBrowserCurrentURL(cmd, session)
		if err == nil && urlMatchesSuccess(current, expected) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "assert-url ok: %s\n", current)
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("assert-url: %w (expected %q)", err, expected)
			}
			return fmt.Errorf("assert-url: still on %q, expected prefix of %q", current, expected)
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-tick.C:
		}
	}
}

func agentBrowserCurrentURL(cmd *cobra.Command, session string) (string, error) {
	session = strings.TrimSpace(session)
	args := []string{"get", "url"}
	if session != "" {
		args = append(args, "--session", session)
	}
	c := exec.CommandContext(cmd.Context(), "agent-browser", args...)
	c.Env = os.Environ()
	if session != "" {
		c.Env = append(c.Env, "AGENT_BROWSER_SESSION="+session)
	}
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func urlMatchesSuccess(current, expected string) bool {
	exp, err := url.Parse(strings.TrimSpace(expected))
	if err != nil || exp.Host == "" {
		return false
	}
	got, err := url.Parse(strings.TrimSpace(current))
	if err != nil {
		return false
	}
	expPath := exp.Path
	if expPath == "" {
		expPath = "/"
	}
	return got.Scheme == exp.Scheme &&
		strings.EqualFold(got.Host, exp.Host) &&
		strings.HasPrefix(got.Path, expPath)
}
