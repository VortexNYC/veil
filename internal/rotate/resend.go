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

// resend rotates https://resend.com API keys via their key-management API.
// The credential in the vault is also the management credential — the key
// rotates itself. That self-authority is why providers without a separate
// management token can still rotate zero-touch.
type resendRotator struct{ base string }

func init() { register([]string{"api.resend.com"}, &resendRotator{base: "https://api.resend.com"}) }

func (r *resendRotator) do(ctx context.Context, key, method, path string, body io.Reader) (*http.Response, error) {
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

func (r *resendRotator) Create(ctx context.Context, itemName string, liveKey []byte) ([]byte, string, error) {
	payload, _ := json.Marshal(map[string]string{
		"name": "veil:" + itemName + ":" + time.Now().UTC().Format("20060102"),
	})
	res, err := r.do(ctx, string(liveKey), http.MethodPost, "/api-keys", bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, "", fmt.Errorf("resend create: %s %s", res.Status, raw[:min(len(raw), 200)])
	}
	var out struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		return nil, "", fmt.Errorf("resend create: bad response")
	}
	return []byte(out.Token), out.ID, nil
}

func (r *resendRotator) Verify(ctx context.Context, secret []byte) error {
	res, err := r.do(ctx, string(secret), http.MethodGet, "/api-keys", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("resend verify: %s", res.Status)
	}
	return nil
}

func (r *resendRotator) Retire(ctx context.Context, keyID string, liveKey []byte) error {
	if keyID == "" {
		return nil
	}
	res, err := r.do(ctx, string(liveKey), http.MethodDelete, "/api-keys/"+strings.TrimSpace(keyID), nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("resend retire: %s", res.Status)
	}
	return nil
}
