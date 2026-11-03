package app

import (
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

func TestOwnerOnboardingIncompleteWithoutItems(t *testing.T) {
	a := groupApp(t)
	owner := self(a)
	view, err := a.OnboardingFor(owner)
	if err != nil {
		t.Fatal(err)
	}
	if view.Complete {
		t.Fatal("expected incomplete without items")
	}
	if view.Steps[1].ID != "import" || view.Steps[1].Done {
		t.Fatalf("import step: %+v", view.Steps[1])
	}
}

func TestOwnerOnboardingCompleteWithAgentGrant(t *testing.T) {
	a := groupApp(t)
	owner := self(a)
	if _, err := a.AddItem("github", "https://api.github.com", []byte("sekret")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("cursor"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("cursor", "github", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	view, err := a.OnboardingFor(owner)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Complete {
		t.Fatalf("expected complete, steps=%+v", view.Steps)
	}
}

func TestOnboardingMetadata(t *testing.T) {
	a := groupApp(t)
	owner := self(a)
	view, err := a.OnboardingFor(owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.McpTools) != 5 {
		t.Fatalf("mcp tools: %v", view.McpTools)
	}
	if view.Docs["grok"] == "" {
		t.Fatal("missing grok doc link")
	}
	if len(view.ImportCaveats) < 2 {
		t.Fatalf("import caveats: %v", view.ImportCaveats)
	}
}
