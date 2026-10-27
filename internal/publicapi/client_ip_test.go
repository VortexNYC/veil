package publicapi

import (
	"net/http"
	"testing"
)

func reqWith(headers map[string]string, remote string) *http.Request {
	r, err := http.NewRequest("POST", "https://origin/v1/shares/s/redeem", nil)
	if err != nil {
		panic(err)
	}
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIPPrecedence(t *testing.T) {
	r := reqWith(map[string]string{
		"CF-Connecting-IP": "203.0.113.5",
		"X-Forwarded-For":  "198.51.100.9, 10.0.0.1",
	}, "192.0.2.1:4444")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("cf-connecting-ip should win: %q", got)
	}

	r = reqWith(map[string]string{"X-Forwarded-For": "198.51.100.9, 10.0.0.1"}, "192.0.2.1:4444")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Fatalf("x-forwarded-for left-most: %q", got)
	}

	r = reqWith(nil, "192.0.2.1:4444")
	if got := clientIP(r); got != "192.0.2.1" {
		t.Fatalf("remote addr host: %q", got)
	}
}

func TestClientIPCanonicalizesAndSkipsGarbage(t *testing.T) {
	// Spelling variants of one address share a bucket.
	r := reqWith(map[string]string{"CF-Connecting-IP": " ::FFFF:203.0.113.5 "}, "192.0.2.1:4444")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("canonical form: %q", got)
	}

	// A non-IP header is not a key — it falls through to RemoteAddr.
	r = reqWith(map[string]string{"CF-Connecting-IP": "not-an-ip", "X-Forwarded-For": "also-bad"}, "192.0.2.1:4444")
	if got := clientIP(r); got != "192.0.2.1" {
		t.Fatalf("garbage falls to remote addr: %q", got)
	}
}
