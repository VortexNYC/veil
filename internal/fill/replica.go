package fill

import (
	"encoding/json"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

func (h *Host) replicaWarm() bool {
	return h.Replica != nil && h.Replica.Len() > 0
}

func (h *Host) PullReplica() error {
	if h.Replica == nil || h.Origin == "" {
		return nil
	}
	raw, err := h.originPOST("/v1/fill/sync", []byte("{}"))
	if err != nil {
		return err
	}
	var out publicapi.FillSyncResponse
	if json.Unmarshal(raw, &out) != nil {
		return errGenerateCreate
	}
	for _, row := range out.Items {
		if row.Item.ID == "" || row.Material == "" {
			continue
		}
		if err := h.Replica.Put(row.Item, []byte(row.Material)); err != nil {
			return err
		}
	}
	h.invalidateIndex()
	return nil
}

func (h *Host) replicaFill(uuid string, mintTotp bool) (app.FillEntry, bool) {
	if h.Replica == nil {
		return app.FillEntry{}, false
	}
	var item protocol.Item
	found := false
	for _, it := range h.Replica.Items() {
		if it.ID == uuid {
			item = it
			found = true
			break
		}
	}
	if !found || item.Kind != protocol.ItemAPIKey {
		return app.FillEntry{}, false
	}
	raw := h.Replica.Material(uuid)
	if raw == "" {
		return app.FillEntry{}, false
	}
	env := material.Unpack([]byte(raw))
	if env.PasskeyPEM != "" || env.Number != "" || env.CVV != "" || env.GivenName != "" || env.FamilyName != "" || env.Address != "" || env.Phone != "" {
		return app.FillEntry{}, false
	}
	itemName := item.Name
	if env.Login == "" {
		env.Login = item.Login
	}
	pass := env.Token
	if pass == "" {
		pass = raw
	}
	totp := ""
	if env.TOTP != "" {
		totp = totpPresent
		if mintTotp {
			code, err := material.Mint(env.TOTP, time.Now())
			if err != nil || code == "" {
				return app.FillEntry{}, false
			}
			totp = code
		}
	}
	return app.FillEntry{Login: env.Login, Name: itemName, Password: pass, UUID: uuid, TOTP: totp}, true
}

func (h *Host) fillEnvelope(uuid string) (protocol.Item, material.Envelope, bool) {
	if h.Replica != nil {
		raw := h.Replica.Material(uuid)
		if raw != "" {
			item := protocol.Item{ID: uuid, Name: uuid}
			for _, it := range h.Replica.Items() {
				if it.ID == uuid {
					item = it
					break
				}
			}
			env := material.Unpack([]byte(raw))
			switch item.Kind {
			case protocol.ItemCard:
				if env.Number != "" {
					return item, env, true
				}
			case protocol.ItemIdentity:
				if env.GivenName != "" || env.FamilyName != "" || env.Address != "" || env.Phone != "" || env.Email != "" {
					return item, env, true
				}
			default:
				return item, env, true
			}
		}
	}
	if h.App != nil && h.App.Store != nil {
		item, err := h.App.Store.Item(uuid)
		if err != nil {
			return protocol.Item{}, material.Envelope{}, false
		}
		sec, err := h.App.Store.Secret(uuid)
		if err != nil {
			return protocol.Item{}, material.Envelope{}, false
		}
		return item, material.Unpack([]byte(sec)), true
	}
	return h.originEnvelope(uuid)
}

func (h *Host) originEnvelope(uuid string) (protocol.Item, material.Envelope, bool) {
	if h.Origin == "" || uuid == "" {
		return protocol.Item{}, material.Envelope{}, false
	}
	raw, err := h.originPOST("/v1/fill/sync", []byte("{}"))
	if err != nil {
		fillDebug("origin envelope " + err.Error())
		return protocol.Item{}, material.Envelope{}, false
	}
	var out publicapi.FillSyncResponse
	if json.Unmarshal(raw, &out) != nil {
		fillDebug("origin envelope unmarshal")
		return protocol.Item{}, material.Envelope{}, false
	}
	for _, row := range out.Items {
		if row.Item.ID != uuid {
			continue
		}
		env := material.Unpack([]byte(row.Material))
		if env.Number == "" && env.GivenName == "" && env.FamilyName == "" && env.Address == "" {
			fillDebug("origin envelope empty")
		}
		return row.Item, env, true
	}
	fillDebug("origin envelope miss")
	return protocol.Item{}, material.Envelope{}, false
}
