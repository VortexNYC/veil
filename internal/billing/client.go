package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the merchant-scoped Vortex Billing API surface Veil consumes.
// Wire contract verified against vortex packages/worker:
// merchant-schemas.ts createBillingCustomerCommandSchema and
// billingCustomerSnapshotSchema — POST /v1/customers carries environment +
// merchantAccountId in the body, mutations require Idempotency-Key, and the
// Bearer is a vp_ merchant key minted at merchant bootstrap (see docs).
type Client struct {
	BaseURL     string
	Key         string
	MerchantID  string
	Environment string // "sandbox" | "production"
	HTTP        *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// EnsureCustomer creates the billing customer keyed by the Veil org ID —
// externalCustomerRef is the join key. Idempotent by construction: the
// Idempotency-Key is deterministic per org, so retries and reprovisions are
// safe no-ops server-side. Returns the Vortex customerId.
func (c *Client) EnsureCustomer(ctx context.Context, orgID string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"environment":         c.Environment,
		"merchantAccountId":   c.MerchantID,
		"customerId":          orgID, // caller-chosen: events then carry the org id as customerExternalId
		"name":                orgID,
		"defaultCurrency":     "usd",
		"externalCustomerRef": orgID,
		"metadata":            map[string]string{"source": "veil", "orgId": orgID},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/customers", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "veil-customer-"+orgID)
	res, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("vortex customers: %s", res.Status)
	}
	var out struct {
		Data struct {
			CustomerID string `json:"customerId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("vortex customers: bad response: %w", err)
	}
	if out.Data.CustomerID == "" {
		return "", fmt.Errorf("vortex customers: empty customerId")
	}
	return out.Data.CustomerID, nil
}
