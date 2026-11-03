// Package hydra talks to Ory Hydra only.
//
// Tokens. First-party and agent clients.
// Login challenges are Kratos oauth2_provider, not this package.
// Consent skip still hits urls.consent; AcceptConsent is that hop.
// Not humans (Kratos). Not owner/member (Keto).
package hydra

import (
	"context"
	"errors"
	"fmt"

	ory "github.com/ory/hydra-client-go/v26"

	"github.com/VortexNYC/veil/identity/glue/internal/absurl"
)

var ErrChallenge = errors.New("glue: consent challenge")

var ErrLogoutChallenge = errors.New("glue: logout challenge")

const DefaultClientID = "veil" // first-party Hydra client ID + default audience

type Client struct {
	admin *ory.APIClient
}

func New(adminURL string) (*Client, error) {
	u, err := absurl.Parse(adminURL)
	if err != nil {
		return nil, fmt.Errorf("hydra: admin: %w", err)
	}
	cfg := ory.NewConfiguration()
	cfg.Servers = ory.ServerConfigurations{{URL: u}}
	return &Client{admin: ory.NewAPIClient(cfg)}, nil
}

func (c *Client) AcceptConsent(ctx context.Context, challenge string) (string, error) {
	if c == nil || c.admin == nil {
		return "", fmt.Errorf("hydra: admin not configured")
	}
	if challenge == "" {
		return "", ErrChallenge
	}
	req, resp, err := c.admin.OAuth2API.GetOAuth2ConsentRequest(ctx).ConsentChallenge(challenge).Execute()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrChallenge, err)
	}
	_ = resp
	body := ory.NewAcceptOAuth2ConsentRequest()
	body.SetGrantScope(req.GetRequestedScope())
	body.SetGrantAccessTokenAudience(req.GetRequestedAccessTokenAudience())
	body.SetRemember(true)
	accepted, resp, err := c.admin.OAuth2API.AcceptOAuth2ConsentRequest(ctx).
		ConsentChallenge(challenge).
		AcceptOAuth2ConsentRequest(*body).
		Execute()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrChallenge, err)
	}
	_ = resp
	redirectTo := accepted.GetRedirectTo()
	if redirectTo == "" {
		return "", ErrChallenge
	}
	return redirectTo, nil
}

// AcceptLogout is the skip_logout_consent half of RP-initiated logout.
// /oauth2/sessions/logout still redirects the browser to URLS_LOGOUT
// carrying a logout_challenge — the verifier (this hop, glue) resolves
// it and hands back the registered post_logout_redirect_uri.
func (c *Client) AcceptLogout(ctx context.Context, challenge string) (string, error) {
	if c == nil || c.admin == nil {
		return "", fmt.Errorf("hydra: admin not configured")
	}
	if challenge == "" {
		return "", ErrLogoutChallenge
	}
	if _, resp, err := c.admin.OAuth2API.GetOAuth2LogoutRequest(ctx).LogoutChallenge(challenge).Execute(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrLogoutChallenge, err)
	} else {
		_ = resp
	}
	accepted, resp, err := c.admin.OAuth2API.AcceptOAuth2LogoutRequest(ctx).
		LogoutChallenge(challenge).
		Execute()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrLogoutChallenge, err)
	}
	_ = resp
	redirectTo := accepted.GetRedirectTo()
	if redirectTo == "" {
		return "", ErrLogoutChallenge
	}
	return redirectTo, nil
}
