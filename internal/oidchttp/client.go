// Package oidchttp is the HTTP client go-oidc uses for discovery and JWKS.
// Cloudflare Bot Fight 1010s the Go default User-Agent. We send ours.
package oidchttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const ua = "veil"

var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

type transport struct {
	base http.RoundTripper
}

var baseDialer = &net.Dialer{
	Timeout:   10 * time.Second,
	KeepAlive: 30 * time.Second,
	Control:   rejectPrivateOrMetadataPeer,
}

var safeTransport = &http.Transport{
	Proxy:               http.ProxyFromEnvironment,
	DialContext:         safeDialContext,
	TLSHandshakeTimeout: 10 * time.Second,
}

func isExplicitLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return true
	}
	if ip, err := netip.ParseAddr(h); err == nil && ip.Unmap().IsLoopback() {
		return true
	}
	return false
}

func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	explicitLoopback := isExplicitLoopbackHost(host)
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, raw := range ips {
		ip := raw.Unmap()
		if !explicitLoopback && ip.IsLoopback() {
			return nil, fmt.Errorf("oidchttp: DNS rebind to loopback %s refused for host %q", ip, host)
		}
		if err := checkResolvedIP(ip, explicitLoopback); err != nil {
			return nil, err
		}
	}
	for _, raw := range ips {
		ip := raw.Unmap()
		conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("oidchttp: no addresses resolved for %q", host)
}

func checkResolvedIP(ip netip.Addr, allowLoopback bool) error {
	ip = ip.Unmap()
	if ip.IsLoopback() {
		if allowLoopback {
			return nil
		}
		return fmt.Errorf("oidchttp: refused loopback peer %s", ip)
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnatPrefix.Contains(ip) {
		return fmt.Errorf("oidchttp: refused non-public peer %s", ip)
	}
	return nil
}

// rejectPrivateOrMetadataPeer blocks OIDC discovery/JWKS connections that
// resolve (including via DNS rebinding) to RFC1918 private networks, CGNAT
// (100.64.0.0/10), link-local / cloud-metadata (169.254.0.0/16, fe80::/10),
// multicast, or unspecified addresses. Loopback remains permitted for local
// Hydra and tests when the requested hostname itself is loopback.
func rejectPrivateOrMetadataPeer(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	return checkResolvedIP(ap.Addr(), true)
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
