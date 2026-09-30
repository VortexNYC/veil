package app

import (
	"context"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/health"
	"github.com/VortexNYC/veil/internal/protocol"
)

func TestSecurityReportHumanOnly(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	agent := protocol.Principal{Kind: protocol.PrincipalAgent, ID: "claude", OrgID: a.OrgID}
	if _, err := a.SecurityReport(context.Background(), agent, nil); err == nil {
		t.Fatal("agent must not run the vault health report")
	}
}

func TestSecurityReportFlagsWeakReusedSkipsRest(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: DefaultHuman, OrgID: a.OrgID}
	for _, it := range []ItemOpts{
		{Name: "weak-1", Kind: protocol.ItemAPIKey, Token: []byte("password")},
		{Name: "dup-a", Token: []byte("sameToken9!xYz")},
		{Name: "dup-b", Token: []byte("sameToken9!xYz")},
		{Name: "ok-1", Token: []byte("Xk9#mQ2$vLp8!zRw")},
		{Name: "card-1", Kind: protocol.ItemCard, Token: []byte("irrelevant")},
	} {
		if _, err := a.PutItemFor(human, it); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := a.SecurityReport(context.Background(), human, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Items != 4 {
		t.Fatalf("card not scored; items=%d", rep.Items)
	}
	if rep.Weak != 1 || rep.Reused != 2 {
		t.Fatalf("weak=%d reused=%d", rep.Weak, rep.Reused)
	}
	for _, f := range rep.Findings {
		if f.Name == "card-1" || f.Name == "ok-1" {
			t.Fatalf("unflagged item in findings: %+v", f)
		}
	}
}

func TestSecurityReportArchivedExcluded(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: DefaultHuman, OrgID: a.OrgID}
	if _, err := a.PutItemFor(human, ItemOpts{Name: "dead", Token: []byte("password")}); err != nil {
		t.Fatal(err)
	}
	if err := a.ArchiveItem("dead"); err != nil {
		t.Fatal(err)
	}
	rep, err := a.SecurityReport(context.Background(), human, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Items != 0 || len(rep.Findings) != 0 {
		t.Fatalf("archived item scored: %+v", rep)
	}
}

func TestSecurityReportNeverLeaksToken(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: DefaultHuman, OrgID: a.OrgID}
	const tok = "password-leak-canary"
	if _, err := a.PutItemFor(human, ItemOpts{Name: "canary", Token: []byte(tok)}); err != nil {
		t.Fatal(err)
	}
	rep, err := a.SecurityReport(context.Background(), human, func(_ context.Context, _ string) (int, error) {
		return 99, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		s := f.Name + f.ItemID + f.URI + strings.Join(f.Weak, ",")
		if strings.Contains(s, tok) {
			t.Fatalf("finding leaked token: %+v", f)
		}
	}
	if rep.Pwned != 1 {
		t.Fatalf("pwned count %d", rep.Pwned)
	}
}

var _ = health.Finding{} // keep import when API shape shifts
