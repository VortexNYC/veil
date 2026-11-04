package fill

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/protocol"
)

// Remote approval — the 1Password "approve this sign-in on another
// device" posture. A fill gated this way files an approval request at the
// origin and waits; an owner resolves it on another device (the vault's
// requests card, `veil` CLI, or the phone) and the poll returns the
// decision. Local Touch ID is not needed — the remote human was the gate.

// remoteGate reports whether this item's fills need remote approval:
// "all" covers every release, "tagged" covers `veil:remote-approve`
// items, anything else is off.
func (h *Host) remoteGate(item protocol.Item) bool {
	h.mu.Lock()
	mode := h.RemoteApproveMode
	h.mu.Unlock()
	switch mode {
	case "all":
		return true
	case "tagged":
		return item.RemoteApprove()
	default:
		return false
	}
}

// remoteApprove files the ask and polls until a human resolves it. The
// request itself is deduped per item on the origin, so repeated asks in
// the same window share one open row.
func (h *Host) remoteApprove(item protocol.Item, scope string, peer *Peer) error {
	h.mu.Lock()
	device := h.Device
	h.mu.Unlock()
	if device == "" {
		device = "laptop"
	}
	raw, err := h.originPOST("/v1/fill/request", []byte(fmt.Sprintf(
		`{"item_id":%q,"device":%q}`, item.ID, device)))
	if err != nil {
		return fmt.Errorf("fill: remote approve file: %w", err)
	}
	var filed struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(raw, &filed); err != nil || filed.RequestID == "" {
		return fmt.Errorf("fill: remote approve file: bad reply")
	}
	fillDebug("remote approve asked " + filed.RequestID)

	deadline := time.Now().Add(110 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := h.originGET("/v1/fill/request/" + filed.RequestID)
		if err != nil {
			// Origin blink — keep waiting, the ask is still open.
			time.Sleep(2 * time.Second)
			continue
		}
		var st struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(raw, &st)
		switch st.Status {
		case "approved":
			// The remote human was the confirm — arm the peer's reuse
			// window so near-term fills for this peer don't re-ask.
			h.mu.Lock()
			ttl := h.ConfirmTTL
			if ttl <= 0 {
				ttl = confirmReuse
			}
			h.confirmUntil = time.Now().Add(ttl)
			h.confirmScope = scope
			h.confirmPeer = peer.key()
			h.mu.Unlock()
			return nil
		case "denied", "expired", "cancelled":
			return fmt.Errorf("fill: remote approve %s", st.Status)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("fill: remote approve timeout")
}

// agenticApprove is the browser-automation gate: an agent filed the ask
// (MCP or POST /v1/agent/fill/request), a human approved it in the vault,
// and the extension host polls until the row is approved — then arms the
// same reuse window remoteApprove uses so jsonFill does not re-prompt Touch ID
// for the immediate write.
func (h *Host) agenticApprove(item protocol.Item, requestID, pageURL string, peer *Peer) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("fill: agentic approve: missing request")
	}
	pageURL = strings.TrimSpace(pageURL)
	if pageURL != "" && !grant.HostAllowed(item, pageURL) {
		return fmt.Errorf("fill: agentic approve: url mismatch")
	}
	scope := grant.Registrable(pageURL)
	if scope == "" {
		scope = "veil.nyc"
	}
	deadline := time.Now().Add(125 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := h.originGET("/v1/fill/agent-request/" + requestID)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var st struct {
			Status string `json:"status"`
			ItemID string `json:"item_id"`
		}
		if err := json.Unmarshal(raw, &st); err != nil {
			return fmt.Errorf("fill: agentic approve: bad reply")
		}
		if st.ItemID != "" && st.ItemID != item.ID {
			return fmt.Errorf("fill: agentic approve: item mismatch")
		}
		switch st.Status {
		case "approved":
			h.mu.Lock()
			ttl := h.ConfirmTTL
			if ttl <= 0 {
				ttl = confirmReuse
			}
			h.confirmUntil = time.Now().Add(ttl)
			h.confirmScope = scope
			h.confirmPeer = peer.key()
			h.mu.Unlock()
			fillDebug("agentic approve ok " + requestID)
			return nil
		case "denied", "expired", "cancelled":
			return fmt.Errorf("fill: agentic approve %s", st.Status)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("fill: agentic approve timeout")
}
