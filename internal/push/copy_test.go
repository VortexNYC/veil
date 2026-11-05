package push

import (
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

func TestApprovalNotifyCopyFill(t *testing.T) {
	title, body := ApprovalNotifyCopy(protocol.ApprovalRequest{
		ID:      "req-abc123",
		AgentID: "cursor",
		ItemID:  "github",
		Action:  protocol.ActionFill,
	})
	if title != "Veil login · approve (3/4)" {
		t.Fatalf("title %q", title)
	}
	if body != `Agent "cursor" → item "github" · req-abc123` {
		t.Fatalf("body %q", body)
	}
}
