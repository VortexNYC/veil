package push

import (
	"fmt"

	"github.com/VortexNYC/veil/internal/protocol"
)

// ApprovalNotifyCopy returns push title/body aligned with CLI step 3/4 banners.
// Payload stays metadata-only (agent id, item id, request id, action).
func ApprovalNotifyCopy(req protocol.ApprovalRequest) (title, body string) {
	switch req.Action {
	case protocol.ActionFill:
		title = "Veil login · approve (3/4)"
		body = fmt.Sprintf("Agent %q → item %q · %s", req.AgentID, req.ItemID, req.ID)
	default:
		title = "Veil approval · " + req.ID
		body = fmt.Sprintf("Agent %q · item %q · %s", req.AgentID, req.ItemID, req.Action)
	}
	return title, body
}
