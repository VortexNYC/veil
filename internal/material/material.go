// Package material is the sealed payload for an item.
//
// Token is the API key / password. TOTP is the base32 *seed*, never a
// 6-digit code. Codes are minted at inject time with pquerna/otp
// (https://pkg.go.dev/github.com/pquerna/otp/totp — GenerateCode).
package material

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/oauth2"
)

const (
	Version    = 1
	HeaderTOTP = "X-TOTP"
	MaxFile    = 8 << 20
)

type Envelope struct {
	V          int    `json:"v"`
	Token      string `json:"token,omitempty"`
	Login      string `json:"login,omitempty"`
	TOTP       string `json:"totp,omitempty"`
	Refresh    string `json:"refresh,omitempty"`
	TokenURL   string `json:"token_url,omitempty"`
	ClientID   string `json:"client_id,omitempty"`
	ClientSec  string `json:"client_secret,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	MIME       string `json:"mime,omitempty"`
	FileB64    string `json:"file_b64,omitempty"`
	PasskeyPEM string `json:"passkey_pem,omitempty"`
	CredID     string `json:"cred_id,omitempty"`
	RpID       string `json:"rp_id,omitempty"`
	UserHandle string `json:"user_handle,omitempty"`
	Number     string `json:"number,omitempty"`
	ExpMonth   string `json:"exp_month,omitempty"`
	ExpYear    string `json:"exp_year,omitempty"`
	CVV        string `json:"cvv,omitempty"`
	GivenName  string `json:"given_name,omitempty"`
	FamilyName string `json:"family_name,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	Postal     string `json:"postal,omitempty"`
	Country    string `json:"country,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
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
	if env.Refresh == "" {
		return env.Token, nil
	}
	if env.TokenURL == "" || env.ClientID == "" {
		return "", errOAuth
	}
	cfg := &oauth2.Config{
		ClientID:     env.ClientID,
		ClientSecret: env.ClientSec,
		Endpoint:     oauth2.Endpoint{TokenURL: env.TokenURL},
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	tok, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: env.Refresh}).Token()
	if err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", errOAuth
	}
	return tok.AccessToken, nil
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
