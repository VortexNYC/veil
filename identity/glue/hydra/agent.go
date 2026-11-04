package hydra

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	ory "github.com/ory/hydra-client-go/v26"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/VortexNYC/veil/identity/glue/internal/absurl"
)

const AgentClientPrefix = "agent-"

type AgentClient struct {
	ID       string
	Audience string
}

type AgentCred struct {
	ID       string
	Secret   string
	Audience string
}

func (ac AgentClient) id() string {
	return strings.TrimSpace(ac.ID)
}

func (ac AgentClient) audience() string {
	if ac.Audience != "" {
		return ac.Audience
	}
	return DefaultClientID
}

func agentBody(ac AgentClient) (*ory.OAuth2Client, error) {
	id := ac.id()
	if id == "" || id == DefaultClientID {
		return nil, fmt.Errorf("hydra: agent client id")
	}
	if !strings.HasPrefix(id, AgentClientPrefix) {
		return nil, fmt.Errorf("hydra: agent client id must start with %s", AgentClientPrefix)
	}
	c := ory.NewOAuth2Client()
	c.SetClientId(id)
	c.SetClientName(id)
	c.SetGrantTypes([]string{"client_credentials"})
	c.SetAudience([]string{ac.audience()})
	c.SetAccessTokenStrategy("jwt")
	c.SetTokenEndpointAuthMethod("client_secret_basic")
	c.SetScope("")
	return c, nil
}

func (c *Client) EnsureAgent(ctx context.Context, ac AgentClient) (AgentCred, error) {
	if c == nil || c.admin == nil {
		return AgentCred{}, fmt.Errorf("hydra: admin not configured")
	}
	body, err := agentBody(ac)
	if err != nil {
		return AgentCred{}, err
	}
	_, resp, err := c.admin.OAuth2API.GetOAuth2Client(ctx, ac.id()).Execute()
	missing := resp != nil && resp.StatusCode == http.StatusNotFound
	if err != nil && !missing {
		return AgentCred{}, fmt.Errorf("hydra: agent client: %w", err)
	}

	var got *ory.OAuth2Client
	if missing {
		got, resp, err = c.admin.OAuth2API.CreateOAuth2Client(ctx).OAuth2Client(*body).Execute()
	} else {
		got, resp, err = c.admin.OAuth2API.SetOAuth2Client(ctx, ac.id()).OAuth2Client(*body).Execute()
	}
	if err != nil {
		return AgentCred{}, fmt.Errorf("hydra: agent client: %w", err)
	}
	_ = resp
	if got == nil || got.GetClientId() == "" {
		return AgentCred{}, fmt.Errorf("hydra: agent client: empty")
	}
	if got.GetAccessTokenStrategy() != "jwt" {
		return AgentCred{}, fmt.Errorf("hydra: agent client: access token is not jwt")
	}
	grants := got.GetGrantTypes()
	if len(grants) != 1 || grants[0] != "client_credentials" {
		return AgentCred{}, fmt.Errorf("hydra: agent client: not client_credentials")
	}
	return AgentCred{
		ID:       got.GetClientId(),
		Secret:   got.GetClientSecret(),
		Audience: ac.audience(),
	}, nil
}

// TokenURL is Hydra's OAuth2 token endpoint for issuer.
func TokenURL(issuer string) (string, error) {
	iss, err := absurl.Parse(issuer)
	if err != nil {
		return "", fmt.Errorf("hydra: issuer: %w", err)
	}
	return strings.TrimRight(iss, "/") + "/oauth2/token", nil
}

func ClientCredentials(ctx context.Context, issuer, clientID, secret, audience string) (string, error) {
	return ClientCredentialsWithHTTPClient(ctx, issuer, clientID, secret, audience, nil)
}

// ClientCredentialsWithHTTPClient mints a JWT access token via client_credentials.
// When httpClient is non-nil it is used for the token request (oauth2.HTTPClient).
func ClientCredentialsWithHTTPClient(ctx context.Context, issuer, clientID, secret, audience string, httpClient *http.Client) (string, error) {
	iss, err := absurl.Parse(issuer)
	if err != nil {
		return "", fmt.Errorf("hydra: issuer: %w", err)
	}
	clientID = strings.TrimSpace(clientID)
	secret = strings.TrimSpace(secret)
	if clientID == "" || secret == "" {
		return "", fmt.Errorf("hydra: missing client")
	}
	if audience == "" {
		audience = DefaultClientID
	}
	if httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	}
	cfg := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		TokenURL:     strings.TrimRight(iss, "/") + "/oauth2/token",
		AuthStyle:    oauth2.AuthStyleInHeader,
		EndpointParams: url.Values{
			"audience": {audience},
		},
	}
	tok, err := cfg.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("hydra: client credentials: %w", err)
	}
	raw := strings.TrimSpace(tok.AccessToken)
	if strings.Count(raw, ".") != 2 {
		return "", fmt.Errorf("hydra: access token is not a jwt")
	}
	return raw, nil
}

func AgentClientID(agentID string) string {
	return AgentClientPrefix + strings.TrimSpace(agentID)
}
