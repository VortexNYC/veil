package rotate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WellKnownPath is the discovery path for providers implementing the open
// key-rotation contract.
const WellKnownPath = "/.well-known/key-rotation"

// Spec is the declarative 3-operation rotation contract (create, verify,
// retire). All paths are relative to BaseURL's host and HTTP redirects are
// refused so a live credential is never forwarded off-host.
type Spec struct {
	Host            string         `json:"host"`
	Scheme          string         `json:"scheme,omitempty"`
	CreatePath      string         `json:"create_path"`
	CreateBody      map[string]any `json:"create_body,omitempty"`
	TokenField      string         `json:"token_field"`
	KeyIDField      string         `json:"key_id_field"`
	VerifyPath      string         `json:"verify_path"`
	RetirePath      string         `json:"retire_path"` // "{id}" is replaced with the key ID
	RetireMethod    string         `json:"retire_method,omitempty"`
	ManagementPaths []string       `json:"management_paths"`
}

type specRotator struct {
	spec   Spec
	base   string
	client *http.Client
}

// NewSpecRotator validates a declarative Spec and returns a Rotator pinned
// to spec.Host that refuses all HTTP redirects.
func NewSpecRotator(spec Spec) (Rotator, error) {
	host := strings.ToLower(strings.TrimSpace(spec.Host))
	if host == "" || strings.ContainsAny(host, "/?#@\\") {
		return nil, fmt.Errorf("rotate spec: invalid host %q", spec.Host)
	}
	for _, p := range []string{spec.CreatePath, spec.VerifyPath, spec.RetirePath} {
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
			return nil, fmt.Errorf("rotate spec: path %q must be host-relative", p)
		}
	}
	if strings.TrimSpace(spec.TokenField) == "" || strings.TrimSpace(spec.KeyIDField) == "" {
		return nil, fmt.Errorf("rotate spec: token_field and key_id_field are required")
	}
	if !strings.Contains(spec.RetirePath, "{id}") {
		return nil, fmt.Errorf("rotate spec: retire_path must contain {id} placeholder")
	}
	if len(spec.ManagementPaths) == 0 {
		return nil, fmt.Errorf("rotate spec: management_paths required so agent fetch is blocked")
	}
	scheme := strings.ToLower(strings.TrimSpace(spec.Scheme))
	if scheme == "" {
		scheme = "https"
	}
	if scheme != "https" && scheme != "http" {
		return nil, fmt.Errorf("rotate spec: unsupported scheme %q", scheme)
	}
	if strings.TrimSpace(spec.RetireMethod) == "" {
		spec.RetireMethod = http.MethodDelete
	}
	spec.Host = host
	return &specRotator{
		spec: spec,
		base: scheme + "://" + host,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// RegisterSpec validates and registers a declarative provider rotator for
// spec.Host.
func RegisterSpec(spec Spec) error {
	r, err := NewSpecRotator(spec)
	if err != nil {
		return err
	}
	Register(spec.Host, r)
	return nil
}

func (r *specRotator) do(ctx context.Context, key, method, relPath string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "//") {
		return nil, fmt.Errorf("rotate spec: invalid relative path")
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+relPath, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		_ = res.Body.Close()
		return nil, fmt.Errorf("rotate spec: redirect refused (%s)", res.Status)
	}
	return res, nil
}

func (r *specRotator) Create(ctx context.Context, itemName string, liveKey []byte) ([]byte, string, error) {
	bodyMap := map[string]any{
		"name": "veil:" + itemName + ":" + time.Now().UTC().Format("20060102"),
	}
	for k, v := range r.spec.CreateBody {
		bodyMap[k] = v
	}
	payload, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, "", err
	}
	res, err := r.do(ctx, string(liveKey), http.MethodPost, r.spec.CreatePath, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, "", fmt.Errorf("rotate spec create: %s", res.Status)
	}
	tok, err := extractField(raw, r.spec.TokenField)
	if err != nil || tok == "" {
		return nil, "", fmt.Errorf("rotate spec create: missing token field")
	}
	keyID, err := extractField(raw, r.spec.KeyIDField)
	if err != nil || keyID == "" {
		return nil, "", fmt.Errorf("rotate spec create: missing key_id field")
	}
	return []byte(tok), keyID, nil
}

func (r *specRotator) Verify(ctx context.Context, secret []byte) error {
	res, err := r.do(ctx, string(secret), http.MethodGet, r.spec.VerifyPath, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("rotate spec verify: %s", res.Status)
	}
	return nil
}

func (r *specRotator) Retire(ctx context.Context, keyID string, liveKey []byte) error {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return nil
	}
	p := strings.ReplaceAll(r.spec.RetirePath, "{id}", url.PathEscape(keyID))
	res, err := r.do(ctx, string(liveKey), r.spec.RetireMethod, p, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("rotate spec retire: %s", res.Status)
	}
	return nil
}

func (r *specRotator) ManagementPaths() []string {
	return r.spec.ManagementPaths
}

// extractField walks a dot-separated path (max depth 4) in a JSON object and
// returns its string value. No eval, no templating.
func extractField(raw []byte, dotPath string) (string, error) {
	parts := strings.Split(strings.TrimSpace(dotPath), ".")
	if len(parts) == 0 || len(parts) > 4 {
		return "", fmt.Errorf("invalid field path")
	}
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return "", err
	}
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("not an object at %q", p)
		}
		cur, ok = m[p]
		if !ok {
			return "", fmt.Errorf("missing %q", p)
		}
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("field not a string")
	}
	return s, nil
}
