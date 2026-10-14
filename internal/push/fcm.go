// FCM HTTP v1 — the Android leg of the push channel. Same contract as
// the APNs sender: a thin alert payload that wakes the app to fetch,
// never the vault's metadata. Service-account JSON in envs; OAuth2
// JWT-bearer to mint send tokens — no firebase-admin dependency.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/oauth2/google"

	"github.com/VortexNYC/veil/internal/protocol"
)

// FCMConfig is the Android push setup — a Firebase service account's
// JSON plus the project it mints sends for. Zero-value is disabled.
type FCMConfig struct {
	// ServiceAccount is the credential JSON's contents (env-friendly,
	// same shape as the APNs .p8). ServiceAccountPath reads the file.
	ServiceAccount     string
	ServiceAccountPath string
}

// FCMConfigFromEnv reads the FCM wiring.
func FCMConfigFromEnv() FCMConfig {
	return FCMConfig{
		ServiceAccount:     os.Getenv("VEIL_FCM_SERVICE_ACCOUNT"),
		ServiceAccountPath: os.Getenv("VEIL_FCM_SERVICE_ACCOUNT_FILE"),
	}
}

// Enabled reports whether the env carries a service account to send with.
func (c FCMConfig) Enabled() bool {
	return c.ServiceAccount != "" || c.ServiceAccountPath != ""
}

// fcmSender posts FCM HTTP v1 messages. tokens mint the OAuth bearer
// per send — JWT-access is valid for 1h and cached by the source.
type fcmSender struct {
	projectID string
	source    func() (string, error)
	httpc     *http.Client
}

// NewFCMSender builds the Android sender — nil when unconfigured.
func NewFCMSender(c FCMConfig) (Sender, error) {
	if !c.Enabled() {
		return nil, nil
	}
	raw := c.ServiceAccount
	if raw == "" {
		b, err := os.ReadFile(c.ServiceAccountPath)
		if err != nil {
			return nil, fmt.Errorf("fcm service account file: %w", err)
		}
		raw = string(b)
	}
	creds, err := google.CredentialsFromJSON(context.Background(), []byte(raw),
		"https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	return &fcmSender{
		projectID: creds.ProjectID,
		source: func() (string, error) {
			t, err := creds.TokenSource.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		},
		httpc: &http.Client{},
	}, nil
}

// fcmMessage is the HTTP v1 request body — alert notification + the
// thin data map the app deep-links on.
type fcmMessage struct {
	Message struct {
		Token        string            `json:"token"`
		Notification map[string]string `json:"notification"`
		Data         map[string]string `json:"data,omitempty"`
		Android      map[string]any    `json:"android"`
	} `json:"message"`
}

// fcmError mirrors a v1 error response enough to name the dead-token
// reasons the way APNs' reason strings do.
type fcmError struct {
	Error struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// fcmResponseError is a non-200 send — Reason names the failure.
type fcmResponseError struct{ Reason string }

func (e *fcmResponseError) Error() string { return "fcm: " + e.Reason }

// IsUnregisteredFCM reports whether the send error means the token's
// dead — FCM's UNREGISTERED, or a 404 which is the same verdict.
func IsUnregisteredFCM(err error) bool {
	if re, ok := err.(*fcmResponseError); ok {
		return re.Reason == "UNREGISTERED" || re.Reason == "NOT_FOUND"
	}
	return false
}

// Send posts one wake-up to the v1 endpoint — same payload shape as
// APNs: generic title/body, data carries type+ids for the deep link.
func (s *fcmSender) Send(ctx context.Context, d protocol.PushDevice, title, body string, data map[string]string) error {
	if d.Platform != "fcm" {
		return nil
	}
	bearer, err := s.source()
	if err != nil {
		return fmt.Errorf("fcm token: %w", err)
	}
	var m fcmMessage
	m.Message.Token = d.Token
	m.Message.Notification = map[string]string{"title": title, "body": body}
	m.Message.Data = data
	m.Message.Android = map[string]any{
		"priority":     "HIGH",
		"notification": map[string]any{"channel_id": "approvals"},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://fcm.googleapis.com/v1/projects/"+s.projectID+"/messages:send",
		bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("fcm send: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var fe fcmError
		_ = json.NewDecoder(res.Body).Decode(&fe)
		reason := fe.Error.Status
		if reason == "" {
			reason = fmt.Sprintf("status %d", res.StatusCode)
		}
		return &fcmResponseError{Reason: reason}
	}
	return nil
}

// MultiSender routes each device to its platform's sender — apns now,
// fcm when Android registers tokens.
type MultiSender struct {
	APNS Sender
	FCM  Sender
}

func (m *MultiSender) Send(ctx context.Context, d protocol.PushDevice, title, body string, data map[string]string) error {
	switch d.Platform {
	case "fcm":
		if m.FCM == nil {
			return nil
		}
		return m.FCM.Send(ctx, d, title, body, data)
	default:
		if m.APNS == nil {
			return nil
		}
		return m.APNS.Send(ctx, d, title, body, data)
	}
}
