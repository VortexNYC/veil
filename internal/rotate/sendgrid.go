package rotate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// sendgrid rotates https://sendgrid.com API keys via their v3 key-management
// API. Like resend the vault credential is also the management credential —
// the live key must carry full-access (or key-management) scope to mint a
// replacement.
type sendgridRotator struct{ base string }

func init() {
	register([]string{"api.sendgrid.com"}, &sendgridRotator{base: "https://api.sendgrid.com"})
}

func (r *sendgridRotator) do(ctx context.Context, key, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

// scopesOf reads the live key's own scope set — the replacement must carry
// the same permissions, never wider (a scoped key must not gain
// full_access by rotating).
func (r *sendgridRotator) scopesOf(ctx context.Context, liveKey []byte) ([]string, error) {
	res, err := r.do(ctx, string(liveKey), http.MethodGet, "/v3/scopes", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sendgrid scopes: %s", res.Status)
	}
	var out struct {
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Scopes, nil
}

func (r *sendgridRotator) Create(ctx context.Context, itemName string, liveKey []byte) ([]byte, string, error) {
	scopes, err := r.scopesOf(ctx, liveKey)
	if err != nil {
		return nil, "", err
	}
	payload, _ := json.Marshal(map[string]any{
		"name":   "veil:" + itemName + ":" + time.Now().UTC().Format("20060102"),
		"scopes": scopes,
	})
	res, err := r.do(ctx, string(liveKey), http.MethodPost, "/v3/api_keys", bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, "", fmt.Errorf("sendgrid create: %s %s", res.Status, raw[:min(len(raw), 200)])
	}
	var out struct {
		ID     string `json:"api_key_id"`
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.APIKey == "" {
		return nil, "", fmt.Errorf("sendgrid create: bad response")
	}
	return []byte(out.APIKey), out.ID, nil
}

func (r *sendgridRotator) Verify(ctx context.Context, secret []byte) error {
	res, err := r.do(ctx, string(secret), http.MethodGet, "/v3/scopes", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("sendgrid verify: %s", res.Status)
	}
	return nil
}

func (r *sendgridRotator) Retire(ctx context.Context, keyID string, liveKey []byte) error {
	if keyID == "" {
		return nil
	}
	res, err := r.do(ctx, string(liveKey), http.MethodDelete, "/v3/api_keys/"+strings.TrimSpace(keyID), nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("sendgrid retire: %s", res.Status)
	}
	return nil
}
