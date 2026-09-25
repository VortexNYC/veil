package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	// MeterID + UsageEvent configure usage reporting (VEIL-65); empty MeterID
	// disables the flusher — the webhook receiver works without it.
	MeterID    string
	UsageEvent string
	HTTP       *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// EnsureCustomer links the org to a billing customer and returns the Vortex
// customerId. Create collides 409 on an existing customerId — it does not
// upsert — so ensure is lookup-first: GET ?externalCustomerRef=<org>, create
// on miss, re-resolve on 409 (the cross-writer race). Idempotency-Key covers
// transport retries of the same POST, not repeated ensures.
func (c *Client) EnsureCustomer(ctx context.Context, orgID string) (string, error) {
	if id, err := c.findByExternalRef(ctx, orgID); err == nil && id != "" {
		return id, nil
	}
	id, err := c.createCustomer(ctx, orgID)
	if err == nil {
		return id, nil
	}
	if errors.Is(err, errConflict) {
		if found, ferr := c.findByExternalRef(ctx, orgID); ferr == nil && found != "" {
			return found, nil
		}
	}
	return "", err
}

var errConflict = errors.New("vortex customers: conflict")

func (c *Client) do(ctx context.Context, method, path string, body []byte, idemKey string) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, raw, nil
}

func (c *Client) createCustomer(ctx context.Context, orgID string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"environment":         c.Environment,
		"merchantAccountId":   c.MerchantID,
		"customerId":          orgID, // caller-chosen: events then carry the org id as customerExternalId
		"name":                orgID,
		"defaultCurrency":     "USD", // ISO code, case-sensitive — PROCESSABLE_CURRENCIES is uppercase
		"externalCustomerRef": orgID,
		"metadata":            map[string]string{"source": "veil", "orgId": orgID},
	})
	if err != nil {
		return "", err
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/v1/customers", body, "veil-customer-"+orgID)
	if err != nil {
		return "", err
	}
	if status == http.StatusConflict {
		return "", errConflict
	}
	if status >= 300 {
		return "", fmt.Errorf("vortex customers: %d", status)
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

// findByExternalRef resolves the org's billing customer by the join key;
// "" means unlinked.
func (c *Client) findByExternalRef(ctx context.Context, orgID string) (string, error) {
	path := "/v1/customers?environment=" + url.QueryEscape(c.Environment) +
		"&merchantAccountId=" + url.QueryEscape(c.MerchantID) +
		"&externalCustomerRef=" + url.QueryEscape(orgID)
	status, raw, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", fmt.Errorf("vortex customers list: %d", status)
	}
	var out struct {
		Data struct {
			Items []struct {
				CustomerID          string `json:"customerId"`
				ExternalCustomerRef string `json:"externalCustomerRef"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("vortex customers list: bad response: %w", err)
	}
	for _, it := range out.Data.Items {
		if it.ExternalCustomerRef == orgID && it.CustomerID != "" {
			return it.CustomerID, nil
		}
	}
	return "", nil
}

// EnsureBillingAccount returns the org's billing account id — the account
// usage events post to. Lookup-first like EnsureCustomer: list the
// customer's accounts, create on miss. Veil provisions api_only + manual
// collection + no auto-charge: the account binds usage to the customer;
// collection flips to automatic when a payment profile lands.
func (c *Client) EnsureBillingAccount(ctx context.Context, orgID, customerID string) (string, error) {
	path := "/v1/customers/" + url.PathEscape(customerID) + "/billing-accounts?environment=" +
		url.QueryEscape(c.Environment) + "&merchantAccountId=" + url.QueryEscape(c.MerchantID)
	status, raw, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", fmt.Errorf("vortex billing-accounts list: %d", status)
	}
	var list struct {
		Data struct {
			Items []struct {
				BillingAccountID string `json:"billingAccountId"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", fmt.Errorf("vortex billing-accounts list: bad response: %w", err)
	}
	for _, it := range list.Data.Items {
		if it.BillingAccountID != "" {
			return it.BillingAccountID, nil
		}
	}
	body, err := json.Marshal(map[string]any{
		"environment":           c.Environment,
		"merchantAccountId":     c.MerchantID,
		"customerId":            customerID,
		"invoiceDeliveryMode":   "api_only",
		"collectionMode":        "manual",
		"autoCollectionEnabled": false,
		"metadata":              map[string]string{"source": "veil", "orgId": orgID},
	})
	if err != nil {
		return "", err
	}
	status, raw, err = c.do(ctx, http.MethodPost, "/v1/customers/"+url.PathEscape(customerID)+"/billing-accounts", body, "veil-bacc-"+orgID)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", fmt.Errorf("vortex billing-accounts: %d", status)
	}
	var out struct {
		Data struct {
			BillingAccountID string `json:"billingAccountId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("vortex billing-accounts: bad response: %w", err)
	}
	if out.Data.BillingAccountID == "" {
		return "", fmt.Errorf("vortex billing-accounts: empty billingAccountId")
	}
	return out.Data.BillingAccountID, nil
}

// UsageDelta is one flushed usage delta for an org's billing account.
// IdempotencyKey is deterministic per delta (org + window + watermark) so
// upstream dedupes retries of the same send.
type UsageDelta struct {
	CustomerID       string
	BillingAccountID string
	Quantity         int64
	OccurredAt       time.Time
	IdempotencyKey   string
}

// RecordUsage posts one usage event. Contract:
// createBillingUsageEventCommandSchema — environment + merchantAccountId +
// customerId + billingAccountId + meterId + eventName + quantity +
// occurredAt + idempotencyKey, all required.
func (c *Client) RecordUsage(ctx context.Context, d UsageDelta) error {
	occurredAt := d.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	body, err := json.Marshal(map[string]any{
		"environment":       c.Environment,
		"merchantAccountId": c.MerchantID,
		"customerId":        d.CustomerID,
		"billingAccountId":  d.BillingAccountID,
		"meterId":           c.MeterID,
		"eventName":         c.UsageEvent,
		"quantity":          d.Quantity,
		"occurredAt":        occurredAt.UTC().Format(time.RFC3339Nano),
		"idempotencyKey":    d.IdempotencyKey,
		"metadata":          map[string]string{"source": "veil"},
	})
	if err != nil {
		return err
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/v1/usage-events", body, d.IdempotencyKey)
	if err != nil {
		return err
	}
	// 409 on the idempotency key means the delta already landed — a retry
	// that has nothing to do.
	if status == http.StatusConflict {
		return nil
	}
	if status >= 300 {
		return fmt.Errorf("vortex usage-events: %d", status)
	}
	var out struct {
		Data struct {
			UsageEventID string `json:"usageEventId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("vortex usage-events: bad response: %w", err)
	}
	return nil
}
