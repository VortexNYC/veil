// Package material is the sealed payload for an item.
//
// Token is the API key / password. TOTP is the base32 *seed*, never a
// 6-digit code. Codes are minted at inject time with pquerna/otp
// (https://pkg.go.dev/github.com/pquerna/otp/totp — GenerateCode).
package material

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	Version    = 1
	HeaderTOTP = "X-TOTP"
	MaxFile    = 8 << 20
)

type Envelope struct {
	V                  int    `json:"v"`
	Token              string `json:"token,omitempty"`
	Login              string `json:"login,omitempty"`
	TOTP               string `json:"totp,omitempty"`
	Refresh            string `json:"refresh,omitempty"`
	TokenURL           string `json:"token_url,omitempty"`
	ClientID           string `json:"client_id,omitempty"`
	ClientSec          string `json:"client_secret,omitempty"`
	FileName           string `json:"file_name,omitempty"`
	MIME               string `json:"mime,omitempty"`
	FileB64            string `json:"file_b64,omitempty"`
	PasskeyPEM         string `json:"passkey_pem,omitempty"`
	CredID             string `json:"cred_id,omitempty"`
	RpID               string `json:"rp_id,omitempty"`
	UserHandle         string `json:"user_handle,omitempty"`
	Number             string `json:"number,omitempty"`
	ExpMonth           string `json:"exp_month,omitempty"`
	ExpYear            string `json:"exp_year,omitempty"`
	CVV                string `json:"cvv,omitempty"`
	GivenName          string `json:"given_name,omitempty"`
	FamilyName         string `json:"family_name,omitempty"`
	Address            string `json:"address,omitempty"`
	City               string `json:"city,omitempty"`
	Region             string `json:"region,omitempty"`
	Postal             string `json:"postal,omitempty"`
	Country            string `json:"country,omitempty"`
	Phone              string `json:"phone,omitempty"`
	Email              string `json:"email,omitempty"`
	KeyID              string `json:"key_id,omitempty"`
	PendingKeyID       string `json:"pending_key_id,omitempty"`
	ManagementItemID   string `json:"management_item_id,omitempty"`
	RotateInterval     string `json:"rotate_interval,omitempty"`
	NextRotateAt       int64  `json:"next_rotate_at,omitempty"`
	AssertionTokenURL  string `json:"assertion_token_url,omitempty"`
	AssertionClientID  string `json:"assertion_client_id,omitempty"`
	AssertionClientSec string `json:"assertion_client_secret,omitempty"`
	AssertionAudience  string `json:"assertion_audience,omitempty"`
	FederationRuleID   string `json:"federation_rule_id,omitempty"`
	FederationOrgID    string `json:"federation_org_id,omitempty"`
	ServiceAccountID   string `json:"service_account_id,omitempty"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
}

func PackFile(name, mime string, body []byte) ([]byte, error) {
	if len(body) > MaxFile {
		return nil, fmt.Errorf("material: file too large")
	}
	return pack(Envelope{
		V:        Version,
		FileName: name,
		MIME:     mime,
		FileB64:  base64.StdEncoding.EncodeToString(body),
	})
}

func FileBytes(env Envelope) ([]byte, error) {
	if env.FileB64 == "" {
		return nil, fmt.Errorf("material: not a file")
	}
	return base64.StdEncoding.DecodeString(env.FileB64)
}

func Pack(token, totpSeed []byte) ([]byte, error) {
	return pack(Envelope{
		V:     Version,
		Token: string(trim(token)),
		TOTP:  strings.ToUpper(string(trim(totpSeed))),
	})
}

// WithLogin puts the fill username in the sealed blob. Empty login leaves raw
// unchanged. Item list and MCP never see this field.
func WithLogin(raw []byte, login string) ([]byte, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return raw, nil
	}
	env := Unpack(raw)
	env.V = Version
	env.Login = login
	return pack(env)
}

// WithToken rotates the sealed secret — a changed password keeps its TOTP
// seed, login, and passkey. A raw pre-envelope token upgrades to v1.
// Blank tokens refuse: a rotate must never seal nothing.
func WithToken(raw, token []byte) ([]byte, error) {
	token = trim(token)
	if len(token) == 0 {
		return nil, fmt.Errorf("material: empty token")
	}
	env := Unpack(raw)
	env.V = Version
	env.Token = string(token)
	env.KeyID = ""
	env.PendingKeyID = ""
	env.FederationRuleID = ""
	env.AssertionTokenURL = ""
	env.AssertionClientID = ""
	env.AssertionClientSec = ""
	env.AssertionAudience = ""
	return pack(env)
}

// WithRotatedToken seals a rotated token and the dual-phase provider key IDs
// in one envelope. Item list and MCP never see these fields.
func WithRotatedToken(raw, token []byte, keyID, pendingKeyID string) ([]byte, error) {
	return WithRotatedTokenMeta(raw, token, keyID, pendingKeyID, "", "", 0)
}

// WithRotatedTokenMeta seals a rotated token, dual-phase key IDs, and optional
// management-credential / rotation-schedule metadata inside the envelope.
func WithRotatedTokenMeta(raw, token []byte, keyID, pendingKeyID, mgmtItemID, interval string, nextRotateAt int64) ([]byte, error) {
	token = trim(token)
	if len(token) == 0 {
		return nil, fmt.Errorf("material: empty token")
	}
	env := Unpack(raw)
	env.V = Version
	env.Token = string(token)
	env.KeyID = strings.TrimSpace(keyID)
	env.PendingKeyID = strings.TrimSpace(pendingKeyID)
	if strings.TrimSpace(mgmtItemID) != "" {
		env.ManagementItemID = strings.TrimSpace(mgmtItemID)
	}
	if strings.TrimSpace(interval) != "" {
		env.RotateInterval = strings.TrimSpace(interval)
	}
	if nextRotateAt > 0 {
		env.NextRotateAt = nextRotateAt
	}
	return pack(env)
}

func PackOAuth(refresh, tokenURL, clientID, clientSecret []byte) ([]byte, error) {
	return pack(Envelope{
		V:         Version,
		Refresh:   string(trim(refresh)),
		TokenURL:  string(trim(tokenURL)),
		ClientID:  string(trim(clientID)),
		ClientSec: string(trim(clientSecret)),
	})
}

// WIFOpts configures an outbound RFC 7523 jwt-bearer Workload Identity
// Federation item (such as Anthropic's POST /v1/oauth/token) so Veil mints a
// short-lived upstream access token per use without storing a static API key.
type WIFOpts struct {
	TokenURL          string
	AssertionTokenURL string
	AssertionClientID string
	AssertionSecret   string
	AssertionAudience string
	FederationRuleID  string
	OrganizationID    string
	ServiceAccountID  string
	WorkspaceID       string
}

func PackWIF(opts WIFOpts) ([]byte, error) {
	if strings.TrimSpace(opts.TokenURL) == "" ||
		strings.TrimSpace(opts.AssertionTokenURL) == "" ||
		strings.TrimSpace(opts.AssertionClientID) == "" ||
		strings.TrimSpace(opts.AssertionSecret) == "" ||
		strings.TrimSpace(opts.FederationRuleID) == "" {
		return nil, fmt.Errorf("material: incomplete WIF configuration")
	}
	return pack(Envelope{
		V:                  Version,
		TokenURL:           strings.TrimSpace(opts.TokenURL),
		AssertionTokenURL:  strings.TrimSpace(opts.AssertionTokenURL),
		AssertionClientID:  strings.TrimSpace(opts.AssertionClientID),
		AssertionClientSec: strings.TrimSpace(opts.AssertionSecret),
		AssertionAudience:  strings.TrimSpace(opts.AssertionAudience),
		FederationRuleID:   strings.TrimSpace(opts.FederationRuleID),
		FederationOrgID:    strings.TrimSpace(opts.OrganizationID),
		ServiceAccountID:   strings.TrimSpace(opts.ServiceAccountID),
		WorkspaceID:        strings.TrimSpace(opts.WorkspaceID),
	})
}

// WithRefresh seals a rotated OAuth refresh token into an existing envelope,
// preserving all other fields.
func WithRefresh(raw []byte, refresh string) ([]byte, error) {
	refresh = strings.TrimSpace(refresh)
	if refresh == "" {
		return nil, fmt.Errorf("material: empty refresh token")
	}
	env := Unpack(raw)
	env.V = Version
	env.Refresh = refresh
	return pack(env)
}

func PackCard(number, expMonth, expYear, cvv, name string) ([]byte, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return nil, fmt.Errorf("material: empty card")
	}
	return pack(Envelope{
		V:         Version,
		Number:    number,
		ExpMonth:  strings.TrimSpace(expMonth),
		ExpYear:   strings.TrimSpace(expYear),
		CVV:       strings.TrimSpace(cvv),
		GivenName: strings.TrimSpace(name),
	})
}

func PackIdentity(given, family, address, city, region, postal, country, phone, email string) ([]byte, error) {
	env := Envelope{
		V:          Version,
		GivenName:  strings.TrimSpace(given),
		FamilyName: strings.TrimSpace(family),
		Address:    strings.TrimSpace(address),
		City:       strings.TrimSpace(city),
		Region:     strings.TrimSpace(region),
		Postal:     strings.TrimSpace(postal),
		Country:    strings.TrimSpace(country),
		Phone:      strings.TrimSpace(phone),
		Email:      strings.TrimSpace(email),
	}
	if env.GivenName == "" && env.FamilyName == "" && env.Address == "" && env.Phone == "" && env.Email == "" {
		return nil, fmt.Errorf("material: empty identity")
	}
	return pack(env)
}

func PackPasskey(pem, credID, rpID, userHandle string) ([]byte, error) {
	if strings.TrimSpace(pem) == "" || credID == "" || rpID == "" {
		return nil, fmt.Errorf("material: empty passkey")
	}
	return pack(Envelope{
		V:          Version,
		PasskeyPEM: pem,
		CredID:     credID,
		RpID:       rpID,
		UserHandle: userHandle,
	})
}

func pack(env Envelope) ([]byte, error) {
	return json.Marshal(env)
}

func Unpack(raw []byte) Envelope {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.V == Version {
		return env
	}
	return Envelope{Token: string(raw)}
}

func Mint(seed string, now time.Time) (string, error) {
	seed = strings.ToUpper(strings.TrimSpace(seed))
	seed = strings.ReplaceAll(seed, " ", "")
	if seed == "" {
		return "", nil
	}
	return totp.GenerateCode(seed, now)
}

// AuthorizationValue is the Authorization header for an injected token.
// Stripe/GitHub/OpenAI want Bearer. Linear API keys (lin_api_) reject it.
func AuthorizationValue(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	if strings.HasPrefix(token, "lin_api_") {
		return token
	}
	return "Bearer " + token
}

func Apply(h http.Header, env Envelope, now time.Time) (code string, err error) {
	if env.Token != "" && h.Get("Authorization") == "" && env.Refresh == "" {
		h.Set("Authorization", AuthorizationValue(env.Token))
	}
	if env.TOTP == "" {
		return "", nil
	}
	code, err = Mint(env.TOTP, now)
	if err != nil {
		return "", err
	}
	h.Set(HeaderTOTP, code)
	return code, nil
}

// AccessToken follows Authsome's "log in once" pattern. Refresh stays here.
// golang.org/x/oauth2 performs the refresh. The access token is for inject only.
func AccessToken(ctx context.Context, env Envelope, client *http.Client) (string, error) {
	access, _, err := AccessTokenWithRefresh(ctx, env, client)
	return access, err
}

// AccessTokenWithRefresh performs the OAuth refresh (or RFC 7523 jwt-bearer
// WIF exchange when FederationRuleID is set) and also returns any rotated
// refresh token issued by the provider so the broker can persist it.
func AccessTokenWithRefresh(ctx context.Context, env Envelope, client *http.Client) (access, nextRefresh string, err error) {
	access, nextRefresh, _, err = AccessTokenWithMetadata(ctx, env, client)
	return access, nextRefresh, err
}

// AccessTokenWithMetadata is AccessTokenWithRefresh plus any intermediate
// secret material (such as a minted WIF assertion JWT) that the caller must
// include in response scrubbing.
func AccessTokenWithMetadata(ctx context.Context, env Envelope, client *http.Client) (access, nextRefresh, extraScrub string, err error) {
	if env.FederationRuleID != "" {
		tok, assertion, err := exchangeWIFJWTBearer(ctx, env, client)
		return tok, "", assertion, err
	}
	if env.Refresh == "" {
		return env.Token, "", "", nil
	}
	if env.TokenURL == "" || env.ClientID == "" {
		return "", "", "", errOAuth
	}
	cfg := &oauth2.Config{
		ClientID:     env.ClientID,
		ClientSecret: env.ClientSec,
		Endpoint:     oauth2.Endpoint{TokenURL: env.TokenURL},
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	tok, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: env.Refresh}).Token()
	if err != nil {
		return "", "", "", err
	}
	if tok.AccessToken == "" {
		return "", "", "", errOAuth
	}
	return tok.AccessToken, strings.TrimSpace(tok.RefreshToken), "", nil
}

func exchangeWIFJWTBearer(ctx context.Context, env Envelope, client *http.Client) (access, assertion string, err error) {
	if env.TokenURL == "" || env.AssertionTokenURL == "" || env.AssertionClientID == "" || env.AssertionClientSec == "" {
		return "", "", errOAuth
	}
	if client == nil {
		client = http.DefaultClient
	}
	aud := env.AssertionAudience
	if aud == "" {
		aud = "veil"
	}
	cc := clientcredentials.Config{
		ClientID:     env.AssertionClientID,
		ClientSecret: env.AssertionClientSec,
		TokenURL:     env.AssertionTokenURL,
		AuthStyle:    oauth2.AuthStyleInHeader,
		EndpointParams: url.Values{
			"audience": {aud},
		},
	}
	assertionTok, err := cc.Token(context.WithValue(ctx, oauth2.HTTPClient, client))
	if err != nil || strings.TrimSpace(assertionTok.AccessToken) == "" {
		return "", "", errOAuth
	}
	assertion = strings.TrimSpace(assertionTok.AccessToken)
	body := map[string]string{
		"grant_type":         "urn:ietf:params:oauth:grant-type:jwt-bearer",
		"assertion":          assertion,
		"federation_rule_id": env.FederationRuleID,
	}
	if env.FederationOrgID != "" {
		body["organization_id"] = env.FederationOrgID
	}
	if env.ServiceAccountID != "" {
		body["service_account_id"] = env.ServiceAccountID
	}
	if env.WorkspaceID != "" {
		body["workspace_id"] = env.WorkspaceID
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", assertion, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.TokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", assertion, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return "", assertion, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode != http.StatusOK {
		return "", assertion, errOAuth
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || strings.TrimSpace(out.AccessToken) == "" {
		return "", assertion, errOAuth
	}
	return strings.TrimSpace(out.AccessToken), assertion, nil
}

var errOAuth = errString("material: oauth refresh failed")

type errString string

func (e errString) Error() string { return string(e) }

func ScrubList(env Envelope, extra ...[]byte) [][]byte {
	var out [][]byte
	if env.Token != "" {
		out = append(out, []byte(env.Token))
	}
	if env.Login != "" {
		out = append(out, []byte(env.Login))
	}
	if env.TOTP != "" {
		out = append(out, []byte(env.TOTP))
	}
	if env.Refresh != "" {
		out = append(out, []byte(env.Refresh))
	}
	if env.ClientSec != "" {
		out = append(out, []byte(env.ClientSec))
	}
	if env.AssertionClientSec != "" {
		out = append(out, []byte(env.AssertionClientSec))
	}
	if env.PasskeyPEM != "" {
		out = append(out, []byte(env.PasskeyPEM))
	}
	for _, s := range []string{
		env.Number, env.ExpMonth, env.ExpYear, env.CVV,
		env.GivenName, env.FamilyName, env.Address, env.City,
		env.Region, env.Postal, env.Country, env.Phone, env.Email,
	} {
		if s != "" {
			out = append(out, []byte(s))
		}
	}
	for _, e := range extra {
		if len(e) > 0 {
			out = append(out, e)
		}
	}
	return out
}

func trim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
