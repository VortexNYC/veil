// Package push sends wake-ups to registered phone devices. Two flows
// share the channel: an approval ask filed at the deny edge (the agent
// waits on the human's phone) and ops pages (monitor findings reaching
// the operator's lock screen instead of an unread inbox).
//
// The payload is deliberately thin — type + ids + a generic title. No
// item names, no org data, no secrets: the message crosses Apple's or
// Google's servers, and a push should wake the app to fetch, not carry
// the vault's metadata through a third party.
//
// APNs is token-auth (.p8 + key id + team id — the Apple-issued key,
// minted in the developer portal, held as envs). Unconfigured envs mean
// a nil sender and silent no-op: pushes are a wake channel, not the
// durable part — the request row and the email path carry the truth.
package push

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
)

// Sender delivers one wake-up to one device. Implementations exist per
// platform (apns now; fcm when an Android build registers tokens).
type Sender interface {
	Send(ctx context.Context, d protocol.PushDevice, title, body string, data map[string]string) error
}

// DeviceStore is the narrow read/write the Pusher needs — store.Store
// satisfies it, and so does a thin pool adapter for the monitor cron.
type DeviceStore interface {
	PushDevices(orgID string) ([]protocol.PushDevice, error)
	DeletePushDevice(orgID, token string) error
}

// Pusher fans one notify out to every registered device of an org and
// prunes dead tokens — APNs reporting Unregistered means the device
// re-registers or is gone, and keeping it only wastes sends.
type Pusher struct {
	store  DeviceStore
	sender Sender
}

// New returns a Pusher over the org's registered devices. Sender nil =
// pushes are no-ops (envs unset, or a store that can't push).
func New(s DeviceStore, sender Sender) *Pusher {
	return &Pusher{store: s, sender: sender}
}

// PoolDevices adapts a pgx pool to DeviceStore — the monitor cron has a
// pool, not a full Store.
type PoolDevices struct {
	Pool *pgxpool.Pool
}

func (d PoolDevices) PushDevices(orgID string) ([]protocol.PushDevice, error) {
	return store.PushDevicesFromPool(context.Background(), d.Pool, orgID)
}

func (d PoolDevices) DeletePushDevice(orgID, token string) error {
	_, err := d.Pool.Exec(context.Background(),
		`DELETE FROM push_devices WHERE org_id = $1 AND token = $2`, orgID, token)
	return err
}

// NotifyRequest implements the app's RequestNotify shape — an approval
// ask wakes every registered device of the org.
func (p *Pusher) NotifyRequest(ctx context.Context, req protocol.ApprovalRequest) error {
	if p == nil || p.sender == nil {
		return nil
	}
	devs, err := p.store.PushDevices(req.OrgID)
	if err != nil {
		return err
	}
	for _, d := range devs {
		data := map[string]string{
			"type": "approve_request",
			"id":   req.ID,
		}
		if err := p.sender.Send(ctx, d, "Veil approval request",
			"An agent is asking to use a credential — open Veil to approve", data); err != nil {
			// A non-Unregistered failure is a real outage (bad key, APNs
			// down, topic mismatch) — silent sends mean silent misses.
			if !IsUnregistered(err) {
				slog.Warn("push send failed", "request", req.ID, "err", err)
			}
			p.pruneOnDead(ctx, d, err)
		}
	}
	return nil
}

// NotifyOps pages an org's devices with monitor findings — the ops
// plane's own channel (findings arrive by name; the device is the page).
func (p *Pusher) NotifyOps(ctx context.Context, orgID string, findings []string) error {
	if p == nil || p.sender == nil || len(findings) == 0 {
		return nil
	}
	devs, err := p.store.PushDevices(orgID)
	if err != nil {
		return err
	}
	body := findings[0]
	if len(findings) > 1 {
		body = fmt.Sprintf("%s (+%d more)", body, len(findings)-1)
	}
	data := map[string]string{"type": "ops_finding", "count": fmt.Sprint(len(findings))}
	for _, d := range devs {
		if err := p.sender.Send(ctx, d, "Veil ops alert", body, data); err != nil {
			if !IsUnregistered(err) {
				slog.Warn("push ops send failed", "org", orgID, "err", err)
			}
			p.pruneOnDead(ctx, d, err)
		}
	}
	return nil
}

// pruneOnDead drops tokens APNs says are dead — a send error that is
// Unregistered is durable: the device rotated or uninstalled and the
// next register() will bring a fresh token.
func (p *Pusher) pruneOnDead(ctx context.Context, d protocol.PushDevice, err error) {
	if IsUnregistered(err) {
		if derr := p.store.DeletePushDevice(d.OrgID, d.Token); derr != nil {
			slog.Warn("push: prune failed", "err", derr)
		}
	}
}

// Config is the APNs token-auth setup. Zero-value is a disabled sender.
type Config struct {
	Key      string // .p8 PEM contents — the Railway-able shape
	KeyPath  string // or a path to the .p8 file (local dev)
	KeyID    string
	TeamID   string
	BundleID string // e.g. nyc.veil.phone
	Prod     bool   // production APNs host; false = sandbox
}

// ConfigFromEnv reads the APNS wiring. Key or KeyPath plus the ids set =
// configured.
func ConfigFromEnv() Config {
	return Config{
		Key:      os.Getenv("VEIL_APNS_KEY"),
		KeyPath:  os.Getenv("VEIL_APNS_KEY_PATH"),
		KeyID:    os.Getenv("VEIL_APNS_KEY_ID"),
		TeamID:   os.Getenv("VEIL_APNS_TEAM_ID"),
		BundleID: os.Getenv("VEIL_APNS_BUNDLE"),
		Prod:     os.Getenv("VEIL_APNS_PROD") == "1",
	}
}

// Enabled reports whether the env carries a complete APNs setup.
func (c Config) Enabled() bool {
	return (c.Key != "" || c.KeyPath != "") && c.KeyID != "" && c.TeamID != "" && c.BundleID != ""
}

// authToken resolves the signing key — PEM bytes from the env when set,
// else the .p8 file path.
func authToken(c Config) (*token.Token, error) {
	var key *ecdsa.PrivateKey
	var err error
	if c.Key != "" {
		key, err = token.AuthKeyFromBytes([]byte(c.Key))
	} else {
		key, err = token.AuthKeyFromFile(c.KeyPath)
	}
	if err != nil {
		return nil, fmt.Errorf("apns auth key: %w", err)
	}
	return &token.Token{AuthKey: key, KeyID: c.KeyID, TeamID: c.TeamID}, nil
}

// NewAPNSSender builds the APNs sender from the config — nil when
// unconfigured so callers can hold a nil Sender.
func NewAPNSSender(c Config) (Sender, error) {
	if !c.Enabled() {
		return nil, nil
	}
	tok, err := authToken(c)
	if err != nil {
		return nil, err
	}
	cli := apns2.NewTokenClient(tok)
	if c.Prod {
		cli = cli.Production()
	} else {
		cli = cli.Development()
	}
	return &apnsSender{client: cli, topic: c.BundleID}, nil
}

type apnsSender struct {
	client *apns2.Client
	topic  string
}

// IsUnregistered reports whether a send error means a dead token —
// APNs' BadDeviceToken/Unregistered reasons.
func IsUnregistered(err error) bool {
	if re, ok := err.(*apnsResponse); ok {
		return re.Reason == apns2.ReasonBadDeviceToken ||
			re.Reason == apns2.ReasonUnregistered ||
			re.Reason == apns2.ReasonDeviceTokenNotForTopic ||
			re.Reason == apns2.ReasonExpiredToken
	}
	return false
}

type apnsResponse struct {
	Reason string
}

func (e *apnsResponse) Error() string { return "apns: " + e.Reason }

func (s *apnsSender) Send(ctx context.Context, d protocol.PushDevice, title, body string, data map[string]string) error {
	if d.Platform != "apns" {
		return nil
	}
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]any{
				"title": title,
				"body":  body,
			},
			"sound":              "default",
			"interruption-level": "time-sensitive",
		},
	}
	for k, v := range data {
		payload[k] = v
	}
	n := &apns2.Notification{
		DeviceToken: d.Token,
		Topic:       s.topic,
		Payload:     payload,
		Priority:    apns2.PriorityHigh,
	}
	res, err := s.client.PushWithContext(ctx, n)
	if err != nil {
		return fmt.Errorf("apns send: %w", err)
	}
	if res.StatusCode != 200 {
		return &apnsResponse{Reason: res.Reason}
	}
	return nil
}
