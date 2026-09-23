package mcpserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/otelsetup"
	"github.com/VortexNYC/veil/internal/publicapi"
)

const (
	DefaultAddr      = "127.0.0.1:4461"
	DefaultPublicURL = "https://veil.nyc/mcp"
	DefaultIssuer    = "https://id.veil.nyc"
	Path             = "/mcp"
)

// ListenAddr is the TCP address mcp binds. Railway healthchecks $PORT
// (docs/deployments/healthchecks). Distroless has no shell, so CMD cannot
// expand $PORT — the binary must. An explicit --listen still wins.
func ListenAddr(flag string, explicit bool) string {
	if explicit {
		if a := strings.TrimSpace(flag); a != "" {
			return a
		}
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		return "0.0.0.0:" + p
	}
	if a := strings.TrimSpace(flag); a != "" {
		return a
	}
	return DefaultAddr
}

func ResourceURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = DefaultAddr
	}
	return "http://" + listen + Path
}

func Handler(a *app.App, publicURL string) http.Handler {
	server := New(a)
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: publicHost(publicURL),
	})
	opts := &auth.RequireBearerTokenOptions{AllowMissingExpiration: true}
	if publicURL != "" {
		opts.ResourceMetadataURL = wellKnownURL(publicURL)
	}
	return auth.RequireBearerToken(func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		agent, err := a.AgentFromOIDC(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("%w", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{UserID: agent.ID, Scopes: []string{"mcp"}}, nil
	}, opts)(stream)
}

func Mux(a *app.App, publicURL, issuer string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /ready", ready(a, issuer))
	h := Handler(a, publicURL)
	mux.Handle(Path, h)
	mux.Handle(Path+"/", h)
	publicapi.Mount(mux, a)
	if publicURL != "" && issuer != "" {
		meta := &oauthex.ProtectedResourceMetadata{
			Resource:               publicURL,
			AuthorizationServers:   []string{issuer},
			BearerMethodsSupported: []string{"header"},
		}
		wellKnown := auth.ProtectedResourceMetadataHandler(meta)
		mux.Handle("/.well-known/oauth-protected-resource", wellKnown)
		mux.Handle("/.well-known/oauth-protected-resource/", wellKnown)
	}
	return publicapi.CORS(otelsetup.Handler(mux))
}

// ready is origin truth: the process can answer agents only if Hydra discovery
// works and the store can serve a round trip. /health stays process liveness
// so a dead dependency is visible instead of a green lie.
func ready(a *app.App, issuer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		notReady := func() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready\n"))
		}
		if strings.TrimSpace(issuer) == "" {
			notReady()
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			notReady()
			return
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			notReady()
			return
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		if res.StatusCode != http.StatusOK {
			notReady()
			return
		}
		if p, ok := a.Store.(interface {
			Ping(context.Context) error
		}); ok {
			if err := p.Ping(ctx); err != nil {
				notReady()
				return
			}
		}
		_, _ = w.Write([]byte("ok\n"))
	}
}

func publicHost(resourceURL string) bool {
	u, err := url.Parse(resourceURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host != "" && host != "127.0.0.1" && host != "localhost" && host != "::1"
}

func wellKnownURL(resourceURL string) string {
	u, err := url.Parse(resourceURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Path = "/.well-known/oauth-protected-resource"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
