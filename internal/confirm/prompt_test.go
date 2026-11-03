package confirm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptCommandPrefersZenity(t *testing.T) {
	gotPath, gotArgs, err := promptCommand(":0", "fill github.com", func(name string) (string, error) {
		switch name {
		case "zenity":
			return "/usr/bin/zenity", nil
		case "kdialog":
			return "/usr/bin/kdialog", nil
		default:
			return "", errors.New("not found")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/usr/bin/zenity" {
		t.Fatalf("path %q", gotPath)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"--question", "--title=Veil", "--text=fill github.com", "--ok-label=Allow", "--cancel-label=Cancel"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
}

func TestPromptCommandFallsBackToKDialog(t *testing.T) {
	gotPath, gotArgs, err := promptCommand("wayland-0", "approve fill", func(name string) (string, error) {
		if name == "kdialog" {
			return "/usr/bin/kdialog", nil
		}
		return "", errors.New("not found")
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/usr/bin/kdialog" || strings.Join(gotArgs, " ") != "--title Veil --yesno approve fill" {
		t.Fatalf("path=%q args=%q", gotPath, gotArgs)
	}
}

func TestPromptCommandFailsClosedWithoutDesktopOrDialog(t *testing.T) {
	if _, _, err := promptCommand("", "approve", func(string) (string, error) {
		t.Fatal("looked up dialog without a desktop session")
		return "", nil
	}); err == nil {
		t.Fatal("no desktop session accepted")
	}
	if _, _, err := promptCommand(":0", "approve", func(string) (string, error) {
		return "", errors.New("not found")
	}); err == nil {
		t.Fatal("missing dialog accepted")
	}
}

func TestPromptReturnsOnlyOnExplicitAllow(t *testing.T) {
	binDir := t.TempDir()
	zenity := filepath.Join(binDir, "zenity")
	if err := os.WriteFile(zenity, []byte("#!/bin/sh\nexit \"$VEIL_TEST_PROMPT_EXIT\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("VEIL_TEST_PROMPT_EXIT", "0")
	if err := Prompt("fill github.com"); err != nil {
		t.Fatalf("allow prompt: %v", err)
	}

	t.Setenv("VEIL_TEST_PROMPT_EXIT", "1")
	if err := Prompt("fill github.com"); err == nil {
		t.Fatal("cancel must deny")
	}
}

func TestPromptAvailableRequiresDesktopAndDialog(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "zenity"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if promptAvailable() {
		t.Fatal("available without a desktop session")
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if !promptAvailable() {
		t.Fatal("not available with a dialog and Wayland session")
	}
}
