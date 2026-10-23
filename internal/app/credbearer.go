package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/VortexNYC/veil/internal/id"
)

// agentClientIDPrefix is the Hydra client-id shape provisioned by
// `veil agent hydra` — "agent-" + agent name. Mirrored from
// identity/glue/hydra.AgentClientPrefix; duplicated so the broker never
// imports glue.
const agentClientIDPrefix = "agent-"

// maxCredSources bounds the client_credentials token-source cache so a flood
// of distinct secrets cannot grow memory without limit.
const maxCredSources = 256

// splitCredentialBearer detects the "agent-<name>:<client_secret>" bearer
// form used by hosted HTTP MCP clients that cannot run a local token shim.
// Anything else (JWT, session lease, opaque token) returns ok=false and is
// handled by the normal OIDC/session paths.
func splitCredentialBearer(raw string) (clientID, secret string, ok bool) {
	cid, sec, cut := strings.Cut(strings.TrimSpace(raw), ":")
	if !cut || sec == "" || !strings.HasPrefix(cid, agentClientIDPrefix) {
		return "", "", false
	}
	if !id.Valid(strings.TrimPrefix(cid, agentClientIDPrefix)) {
		return "", "", false
	}
	return cid, sec, true
}

// exchangeAgentCredential trades client_id:client_secret for a Hydra access
// token server-side and returns it. The workload binding (issuer, subject =
// client_id) must already exist — an unbound credential never reaches the
// token endpoint. The binding's recorded audience is what the minted token
// carries. Minted tokens are cached by clientID+audience+secret hash via
// oauth2.ReuseTokenSource, so a hot MCP loop costs zero extra Hydra calls.
func (a *App) exchangeAgentCredential(_ context.Context, clientID, secret string) (string, error) {
	issuer := strings.TrimSpace(a.hydraIssuer)
	if issuer == "" {
		return "", fmt.Errorf("app: unauthorized")
	}
	w, err := a.Store.Workload(issuer, clientID)
	if err != nil {
		return "", err
	}
	if w == nil {
		return "", fmt.Errorf("app: unauthorized")
	}
	audience := strings.TrimSpace(w.Audience)
	if audience == "" {
		return "", fmt.Errorf("app: unauthorized")
	}
	sum := sha256.Sum256([]byte(secret))
	key := clientID + "\x00" + audience + "\x00" + hex.EncodeToString(sum[:])

	a.credMu.Lock()
	if a.credSrcs == nil {
		a.credSrcs = map[string]oauth2.TokenSource{}
	}
	src, ok := a.credSrcs[key]
	if !ok {
		if len(a.credSrcs) >= maxCredSources {
			for k := range a.credSrcs {
				delete(a.credSrcs, k)
				break
			}
		}
		// Bind a background context carrying a bounded client: TokenSource
		// captures the ctx's http.Client for the life of the source, and a
		// per-request ctx would die with the first caller.
		bctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 10 * time.Second})
		base := (&clientcredentials.Config{
			ClientID:       clientID,
			ClientSecret:   secret,
			TokenURL:       strings.TrimRight(issuer, "/") + "/oauth2/token",
			EndpointParams: url.Values{"audience": {audience}},
		}).TokenSource(bctx)
		src = oauth2.ReuseTokenSource(nil, base)
		a.credSrcs[key] = src
	}
	a.credMu.Unlock()

	tok, err := src.Token()
	if err != nil {
		return "", fmt.Errorf("app: unauthorized")
	}
	return tok.AccessToken, nil
}
