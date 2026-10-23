package oidchttp

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestTransportSetsUserAgent(t *testing.T) {
	var got string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		w.WriteHeader(204)
	}))
	t.Cleanup(s.Close)
	c := &http.Client{Transport: transport{}}
	res, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got != ua {
		t.Fatalf("ua %q", got)
	}
}

func TestRejectPrivateOrMetadataPeer(t *testing.T) {
	for _, blocked := range []string{
		"169.254.169.254:80",
		"10.0.0.1:443",
		"100.64.0.1:443",
		"172.16.0.1:443",
		"192.168.1.1:443",
		"0.0.0.0:443",
		"[::ffff:0.0.0.0]:443",
		"[::ffff:169.254.169.254]:80",
		"[fe80::1]:443",
		"[fc00::1]:443",
	} {
		if err := rejectPrivateOrMetadataPeer("tcp", blocked, nil); err == nil {
			t.Fatalf("rejectPrivateOrMetadataPeer(%q) allowed non-public peer", blocked)
		}
	}
	for _, allowed := range []string{
		"127.0.0.1:4444",
		"[::1]:4444",
		"140.82.112.6:443",
	} {
		if err := rejectPrivateOrMetadataPeer("tcp", allowed, nil); err != nil {
			t.Fatalf("rejectPrivateOrMetadataPeer(%q) rejected valid peer: %v", allowed, err)
		}
	}
	if err := checkResolvedIP(netip.MustParseAddr("127.0.0.1"), false); err == nil {
		t.Fatal("checkResolvedIP allowed loopback for non-loopback hostname")
	}
}
