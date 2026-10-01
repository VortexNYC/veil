package hydra

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	ory "github.com/ory/hydra-client-go/v26"

	"github.com/VortexNYC/veil/identity/glue/internal/absurl"
)

type FirstParty struct {
	ID           string
	RedirectURL  string
	RedirectURLs []string
	// NativeRedirectURLs are custom-scheme app callbacks (veil://…) that
	// absurl's http(s) rule would reject. Hydra accepts them for public clients.
	NativeRedirectURLs []string
}

func (fp FirstParty) id() string {
	if fp.ID != "" {
		return fp.ID
	}
	return DefaultClientID
}

func firstPartyClient(fp FirstParty) (*ory.OAuth2Client, error) {
	raw := make([]string, 0, 1+len(fp.RedirectURLs))
	if fp.RedirectURL != "" {
		raw = append(raw, fp.RedirectURL)
	}
	raw = append(raw, fp.RedirectURLs...)
	uris := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, r := range raw {
		redirect, err := absurl.Parse(r)
		if err != nil {
			return nil, fmt.Errorf("hydra: redirect: %w", err)
		}
		if _, ok := seen[redirect]; ok {
			continue
		}
		seen[redirect] = struct{}{}
		uris = append(uris, redirect)
	}
	for _, r := range fp.NativeRedirectURLs {
		u, err := url.Parse(strings.TrimSpace(r))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("hydra: native redirect: %w", err)
		}
		if _, ok := seen[u.String()]; ok {
			continue
		}
		seen[u.String()] = struct{}{}
		uris = append(uris, u.String())
	}
	if len(uris) == 0 {
		return nil, fmt.Errorf("hydra: redirect")
	}
	c := ory.NewOAuth2Client()
	c.SetClientId(fp.id())
	c.SetClientName(fp.id())
	c.SetGrantTypes([]string{"authorization_code", "refresh_token"})
	c.SetResponseTypes([]string{"code"})
	c.SetScope("openid offline_access")
	c.SetRedirectUris(uris)
	c.SetSkipConsent(true)
	c.SetSkipLogoutConsent(true)
	c.SetTokenEndpointAuthMethod("none")
	return c, nil
}

func (c *Client) EnsureFirstParty(ctx context.Context, fp FirstParty) error {
	if c == nil || c.admin == nil {
		return fmt.Errorf("hydra: admin not configured")
	}
	body, err := firstPartyClient(fp)
	if err != nil {
		return err
	}
	_, resp, err := c.admin.OAuth2API.GetOAuth2Client(ctx, fp.id()).Execute()
	missing := resp != nil && resp.StatusCode == http.StatusNotFound
	if err != nil && !missing {
		return fmt.Errorf("hydra: first-party client: %w", err)
	}

	var got *ory.OAuth2Client
	if missing {
		got, resp, err = c.admin.OAuth2API.CreateOAuth2Client(ctx).OAuth2Client(*body).Execute()
	} else {
		got, resp, err = c.admin.OAuth2API.SetOAuth2Client(ctx, fp.id()).OAuth2Client(*body).Execute()
	}
	if err != nil {
		return fmt.Errorf("hydra: first-party client: %w", err)
	}
	_ = resp
	if got == nil || !got.GetSkipConsent() {
		return fmt.Errorf("hydra: first-party client: consent not skipped")
	}
	return nil
}
