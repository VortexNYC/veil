package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// RotateHookPayload is the POST body a webhook receives after a successful
// rotate. Metadata only — item name, rotation time, key version. The secret
// never leaves the broker; consumers that need the new value Use it through
// the grant protocol like everything else.
type RotateHookPayload struct {
	Event     string `json:"event"` // "item_rotated"
	Item      string `json:"item"`
	ItemID    string `json:"item_id"`
	OrgID     string `json:"org_id"`
	RotatedAt int64  `json:"rotated_at"` // unix seconds
	Actor     string `json:"actor"`
	KeyID     string `json:"key_id,omitempty"`
	Replay    bool   `json:"replay,omitempty"`
}

// RotateHookResult is the per-hook outcome — surfaced by the fire endpoint
// and mirrored into the audit reason.
type RotateHookResult struct {
	Hook   string `json:"hook"`
	OK     bool   `json:"ok"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ValidateRotateHooks rejects malformed hook config before it is sealed.
// Only webhooks exist today; the URL must be http(s) because capability
// tokens ride in the path/query — that is also why the URL is sealed and
// why audit rows carry the host, never the full URL.
func ValidateRotateHooks(hooks []protocol.RotateHook) error {
	seen := map[string]bool{}
	for i, h := range hooks {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			return fmt.Errorf("rotate hook %d: name is required", i)
		}
		if seen[name] {
			return fmt.Errorf("rotate hook: duplicate name %q", name)
		}
		seen[name] = true
		if h.Type != protocol.RotateHookTypeWebhook {
			return fmt.Errorf("rotate hook %q: unknown type %q", name, h.Type)
		}
		u, err := url.Parse(strings.TrimSpace(h.URL))
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("rotate hook %q: URL must be http(s)", name)
		}
	}
	return nil
}

// RotateHooks lists an item's configured post-rotation fan-outs. Owner only —
// hook URLs may embed capability tokens, so this is sealed metadata.
func (a *App) RotateHooks(actor protocol.Principal, name string) ([]protocol.RotateHook, error) {
	item, err := a.ownerItem(actor, name)
	if err != nil {
		return nil, err
	}
	raw, err := a.Store.Secret(item.ID)
	if err != nil {
		return nil, err
	}
	hooks := material.Unpack(raw).RotateHooks
	if hooks == nil {
		hooks = []protocol.RotateHook{}
	}
	return hooks, nil
}

// SetRotateHooks replaces an item's post-rotation fan-out list. Owner only;
// the config lives inside the sealed envelope next to RotateInterval, and
// the write commits with an item_updated audit row.
func (a *App) SetRotateHooks(actor protocol.Principal, name string, hooks []protocol.RotateHook) (protocol.Item, error) {
	if err := ValidateRotateHooks(hooks); err != nil {
		return protocol.Item{}, err
	}
	item, err := a.ownerItem(actor, name)
	if err != nil {
		return protocol.Item{}, err
	}
	raw, err := a.Store.Secret(item.ID)
	if err != nil {
		return protocol.Item{}, err
	}
	sealed, err := material.WithRotateHooks(raw, hooks)
	if err != nil {
		return protocol.Item{}, err
	}
	reason := fmt.Sprintf("name=%s rotate_hooks=%d", item.Name, len(hooks))
	if err := a.Store.PutItem(item, store.Secret(sealed), adminEvent(actor.ID, item.OrgID, item.ID, protocol.ActionItemUpdated, reason)); err != nil {
		return protocol.Item{}, err
	}
	return item, nil
}

// FireRotateHooks re-sends the rotation notification without rotating —
// the retry path when a consumer missed a firing. Same payload, replay=true,
// same audit row per firing.
func (a *App) FireRotateHooks(ctx context.Context, actor protocol.Principal, name string) ([]RotateHookResult, error) {
	item, err := a.ownerItem(actor, name)
	if err != nil {
		return nil, err
	}
	raw, err := a.Store.Secret(item.ID)
	if err != nil {
		return nil, err
	}
	env := material.Unpack(raw)
	return a.fireRotateHooks(ctx, actor.ID, item, env, true), nil
}

// fireRotateHooks POSTs rotation metadata to every configured consumer after
// the sealed swap commits. Post-commit best-effort: a consumer failure is
// audited rotation_hook_fired with the outcome and never fails the rotate —
// the retry path is FireRotateHooks, not a second rotation.
func (a *App) fireRotateHooks(ctx context.Context, actorID string, item protocol.Item, env material.Envelope, replay bool) []RotateHookResult {
	results := make([]RotateHookResult, 0, len(env.RotateHooks))
	for _, h := range env.RotateHooks {
		res := RotateHookResult{Hook: h.Name}
		status, err := a.fireRotateHook(ctx, h, RotateHookPayload{
			Event:     "item_rotated",
			Item:      item.Name,
			ItemID:    item.ID,
			OrgID:     item.OrgID,
			RotatedAt: time.Now().UTC().Unix(),
			Actor:     actorID,
			KeyID:     env.KeyID,
			Replay:    replay,
		})
		res.Status = status
		if err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
		}
		host := ""
		if u, uerr := url.Parse(h.URL); uerr == nil {
			host = u.Host
		}
		outcome := "ok"
		if !res.OK {
			outcome = "failed"
			if len(res.Error) > 120 {
				res.Error = res.Error[:120]
			}
		}
		reason := fmt.Sprintf("hook=%s host=%s outcome=%s", h.Name, host, outcome)
		if !res.OK && res.Error != "" {
			reason += " err=" + res.Error
		}
		if replay {
			reason += " replay=1"
		}
		if err := a.auditAdmin(adminEvent(actorID, item.OrgID, item.ID, protocol.ActionRotationHookFired, reason)); err != nil {
			slog.Warn("rotation hook audit append failed", "item", item.Name, "hook", h.Name, "err", err)
		}
		results = append(results, res)
	}
	return results
}

func (a *App) fireRotateHook(ctx context.Context, h protocol.RotateHook, payload RotateHookPayload) (int, error) {
	if h.Type != protocol.RotateHookTypeWebhook {
		return 0, fmt.Errorf("unknown hook type %q", h.Type)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	client := a.HookClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "veil-rotate-hook/1")
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode >= 400 {
		return res.StatusCode, fmt.Errorf("consumer returned %d", res.StatusCode)
	}
	return res.StatusCode, nil
}

// ownerItem resolves an item the caller may administer — the same gate
// rotate itself uses.
func (a *App) ownerItem(actor protocol.Principal, name string) (protocol.Item, error) {
	if ok, err := a.ownsVault(actor); err != nil || !ok {
		if err != nil {
			return protocol.Item{}, err
		}
		return protocol.Item{}, ErrForbidden
	}
	item, err := a.Store.Item(name)
	if err != nil {
		return protocol.Item{}, err
	}
	if item.OrgID != actor.OrgID {
		return protocol.Item{}, ErrForbidden
	}
	return item, nil
}
