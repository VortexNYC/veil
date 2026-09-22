// Package human verifies a Hydra ID token. The broker does not issue it.
//
// Verifies with coreos/go-oidc, same check as workload. The subject is the human,
// not a bound agent. golang.org/x/oauth2 exchanges the code. PKCE, public client.
package human

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/VortexNYC/veil/internal/oidchttp"
	"github.com/VortexNYC/veil/internal/protocol"
)

const (
	// Deployed Hydra audience; renaming is a prod migration, not a string sweep.
	DefaultAudience = "password-manager"
	DefaultRedirect = "http://127.0.0.1:4460/oidc/callback"
)

type Config struct {
	Issuer      string
	Audience    string
	RedirectURL string
}

type Verifier struct {
	issuer   string
	audience string
	redirect string

	mu       sync.Mutex
	provider *oidc.Provider
}

func New(cfg Config) (*Verifier, error) {
	iss, err := absURL(cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("human: issuer: %w", err)
	}
	aud := cfg.Audience
	if aud == "" {
		aud = DefaultAudience
	}
	redir := cfg.RedirectURL
	if redir == "" {
		redir = DefaultRedirect
	}
	if _, err := absURL(redir); err != nil {
		return nil, fmt.Errorf("human: redirect: %w", err)
	}
	return &Verifier{issuer: iss, audience: aud, redirect: redir}, nil
}

func (v *Verifier) Issuer() string   { return v.issuer }
func (v *Verifier) Audience() string { return v.audience }
func (v *Verifier) Redirect() string { return v.redirect }

func PKCE() (verifier, state string, err error) {
	verifier = oauth2.GenerateVerifier()
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	return verifier, hex.EncodeToString(b), nil
}

func (v *Verifier) oauth(ctx context.Context) (*oauth2.Config, error) {
	p, err := v.oidc(ctx)
	if err != nil {
		return nil, err
	}
	ep := p.Endpoint()
	ep.AuthStyle = oauth2.AuthStyleInParams
	return &oauth2.Config{
		ClientID:    v.audience,
		RedirectURL: v.redirect,
		Endpoint:    ep,
		Scopes:      []string{oidc.ScopeOpenID},
	}, nil
}

// AuthCodeURL is the Hydra authorize URL. PKCE S256. prompt=login so Hydra
// cannot skip on a remembered session (Kratos would then accept aal1).
func (v *Verifier) AuthCodeURL(ctx context.Context, state, verifier string) (string, error) {
	if state == "" || verifier == "" {
		return "", fmt.Errorf("human: missing pkce")
	}
	cfg, err := v.oauth(ctx)
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "login"),
		oauth2.SetAuthURLParam("max_age", "0"),
	), nil
}

// Human verifies rawToken against the configured Hydra issuer. Subject is the
// human id; org is NOT stamped here — the vault humans row is org of record,
// resolved by the caller after the subject authenticates.
func (v *Verifier) Human(ctx context.Context, rawToken string) (protocol.Principal, error) {
	sub, err := v.Subject(ctx, rawToken)
	if err != nil {
		return protocol.Principal{}, err
	}
	return protocol.Principal{Kind: protocol.PrincipalHuman, ID: sub}, nil
}

func (v *Verifier) Subject(ctx context.Context, rawToken string) (string, error) {
	rawToken = strings.TrimSpace(rawToken)
	iss, err := unverifiedIssuer(rawToken)
	if err != nil {
		return "", err
	}
	if iss != v.issuer {
		return "", fmt.Errorf("human: issuer not trusted")
	}
	p, err := v.oidc(ctx)
	if err != nil {
		return "", err
	}
	tok, err := p.Verifier(&oidc.Config{ClientID: v.audience}).Verify(ctx, rawToken)
	if err != nil {
		return "", fmt.Errorf("human: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := tok.Claims(&claims); err != nil {
		return "", err
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("human: empty subject")
	}
	return claims.Sub, nil
}

// Exchange trades an authorization code for an ID token. Access and refresh
// stay here; the caller only gets the ID token to verify.
func (v *Verifier) Exchange(ctx context.Context, code, verifier string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" || verifier == "" {
		return "", fmt.Errorf("human: missing code")
	}
	cfg, err := v.oauth(ctx)
	if err != nil {
		return "", err
	}
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("human: exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", fmt.Errorf("human: no id_token")
	}
	return raw, nil
}

func (v *Verifier) oidc(ctx context.Context) (*oidc.Provider, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.provider != nil {
		return v.provider, nil
	}
	p, err := oidc.NewProvider(oidchttp.Context(ctx), v.issuer)
	if err != nil {
		return nil, fmt.Errorf("human: %w", err)
	}
	v.provider = p
	return p, nil
}

func unverifiedIssuer(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("human: malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("human: malformed token")
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Iss == "" {
		return "", fmt.Errorf("human: missing issuer")
	}
	return claims.Iss, nil
}

func absURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("need an absolute URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("need an absolute URL")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
