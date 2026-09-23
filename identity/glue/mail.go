package glue

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

// mailer is the veil-mail Worker: Bearer-authed POST carrying a template
// kind plus its data. The worker owns rendering and delivery.
type mailer struct {
	url    string
	token  string
	client *http.Client
}

func newMailer(url, token string) *mailer {
	url = strings.TrimSpace(url)
	if url == "" || token == "" {
		return nil
	}
	return &mailer{
		url:    url,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *mailer) send(ctx context.Context, to, kind string, data map[string]string) error {
	payload, err := json.Marshal(map[string]any{
		"to":   to,
		"kind": kind,
		"data": data,
	})
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("mail: status %d", res.StatusCode)
	}
	return nil
}
