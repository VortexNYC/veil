// Package oidchttp is the HTTP client go-oidc uses for discovery and JWKS.
// Cloudflare Bot Fight 1010s the Go default User-Agent. We send ours.
package oidchttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const ua = "veil"

type transport struct {
	base http.RoundTripper
}

var safeTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   rejectPrivateOrMetadataPeer,
	}).DialContext,
	TLSHandshakeTimeout: 10 * time.Second,
}

// rejectPrivateOrMetadataPeer blocks OIDC discovery/JWKS connections that
// resolve (including via DNS rebinding) to RFC1918 private networks,
// link-local / cloud-metadata (169.254.0.0/16, fe80::/10), multicast, or
// unspecified addresses. Loopback remains permitted for local Hydra and tests.
func rejectPrivateOrMetadataPeer(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	ip := ap.Addr().Unmap()
	if ip.IsLoopback() {
		return nil
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("oidchttp: refused non-public peer %s", ip)
	}
	return nil
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", ua)
	base := t.base
	if base == nil {
		base = safeTransport
	}
	return base.RoundTrip(r)
}

func Context(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport{},
	})
}
