package app

import (
	"context"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

func TestRequestAgentFillRequiresGrant(t *testing.T) {
	a := groupApp(t)
	if _, err := a.AddItem("airbnb", "https://www.airbnb.com", []byte("sekret")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("grok"); err != nil {
		t.Fatal(err)
	}
	_, err := a.RequestAgentFill(context.Background(), "grok", "airbnb", "https://www.airbnb.com/login")
	if err == nil {
		t.Fatal("expected not granted")
	}
	if _, err := a.AddGrant("grok", "airbnb", protocol.Level1); err != nil {
		t.Fatal(err)
	}
	req, err := a.RequestAgentFill(context.Background(), "grok", "airbnb", "https://www.airbnb.com/login")
	if err != nil {
		t.Fatal(err)
	}
	if req.Action != protocol.ActionFill || req.AgentID != "grok" {
		t.Fatalf("%+v", req)
	}
}

func TestRequestAgentFillRejectsBadURL(t *testing.T) {
	a := groupApp(t)
	if _, err := a.AddItem("airbnb", "https://www.airbnb.com", []byte("sekret")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAgent("grok"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("grok", "airbnb", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	_, err := a.RequestAgentFill(context.Background(), "grok", "airbnb", "https://evil.example/login")
	if err == nil {
		t.Fatal("expected url mismatch")
	}
}
