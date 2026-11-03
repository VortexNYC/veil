package confirm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const promptDeadline = 75 * time.Second

type lookPathFunc func(string) (string, error)

func promptCommand(display, reason string, lookPath lookPathFunc) (string, []string, error) {
	if strings.TrimSpace(display) == "" {
		return "", nil, fmt.Errorf("fill: no desktop session for confirmation")
	}
	if path, err := lookPath("zenity"); err == nil {
		return path, []string{
			"--question",
			"--title=Veil",
			"--text=" + reason,
			"--ok-label=Allow",
			"--cancel-label=Cancel",
		}, nil
	}
	if path, err := lookPath("kdialog"); err == nil {
		return path, []string{"--title", "Veil", "--yesno", reason}, nil
	}
	return "", nil, fmt.Errorf("fill: no supported desktop confirmation dialog (install zenity or kdialog)")
}

func desktopDisplay() string {
	if display := strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")); display != "" {
		return display
	}
	return strings.TrimSpace(os.Getenv("DISPLAY"))
}

func promptAvailable() bool {
	_, _, err := promptCommand(desktopDisplay(), "test", exec.LookPath)
	return err == nil
}

// Prompt asks the current desktop session to approve or deny the action.
// It is deliberately per-request: no remembered allow state is created here.
func Prompt(reason string) error {
	path, args, err := promptCommand(desktopDisplay(), reason, exec.LookPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), promptDeadline)
	defer cancel()
	if err := exec.CommandContext(ctx, path, args...).Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("fill: confirmation timed out")
		}
		return fmt.Errorf("fill: confirmation denied")
	}
	return nil
}
