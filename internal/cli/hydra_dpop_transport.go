package cli

import (
	"crypto/ecdsa"
	"net/http"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/dpop"
)

// hydraTokenDPoPTransport attaches an RFC 9449 DPoP proof to Hydra's
// POST /oauth2/token request (no ath claim — access token not yet issued).
type hydraTokenDPoPTransport struct {
	base     http.RoundTripper
	key      *ecdsa.PrivateKey
	tokenURL string
}

func hydraTokenDPoPHTTPClient(key *ecdsa.PrivateKey, tokenURL string) *http.Client {
	base := http.DefaultTransport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: &hydraTokenDPoPTransport{
			base:     base,
			key:      key,
			tokenURL: strings.TrimSpace(tokenURL),
		},
	}
}

func (t *hydraTokenDPoPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.key != nil && t.tokenURL != "" && req.URL != nil {
		target := req.URL.String()
		if strings.HasPrefix(target, t.tokenURL) ||
			(req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/oauth2/token")) {
			proof, err := dpop.MintProof(t.key, req.Method, target, "", time.Now())
			if err != nil {
				return nil, err
			}
			req = req.Clone(req.Context())
			req.Header.Set("DPoP", proof)
		}
	}
	return t.base.RoundTrip(req)
}
