// Package scrub removes known secrets from bytes the agent is allowed to see.
//
// Same idea as Infisical Agent Vault / Passman: inject at the proxy, censor
// the response. Exact match + standard base64. Do not invent a "6 encoding"
// scrubber until a test fails without it.
package scrub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
)

const Redacted = "[redacted]"

func Bytes(in []byte, secrets ...[]byte) []byte {
	out := in
	for _, s := range secrets {
		if len(s) == 0 {
			continue
		}
		out = bytes.ReplaceAll(out, s, []byte(Redacted))
		out = bytes.ReplaceAll(out, []byte(base64.StdEncoding.EncodeToString(s)), []byte(Redacted))
		out = bytes.ReplaceAll(out, []byte(base64.RawStdEncoding.EncodeToString(s)), []byte(Redacted))
		out = bytes.ReplaceAll(out, []byte(base64.URLEncoding.EncodeToString(s)), []byte(Redacted))
		out = bytes.ReplaceAll(out, []byte(base64.RawURLEncoding.EncodeToString(s)), []byte(Redacted))
	}
	return out
}

func Contains(haystack []byte, secret []byte) bool {
	if len(secret) == 0 || len(haystack) == 0 {
		return false
	}
	return bytes.Contains(haystack, secret)
}

// JSON must not include the secret in any encoding of v.
func JSON(v any, secrets ...[]byte) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Bytes(b, secrets...), nil
}

type writer struct {
	dst     io.Writer
	secrets [][]byte
}

// NewWriter wraps dst so any written bytes have known secrets scrubbed before
// reaching the underlying stream (used for non-TTY stdout/stderr in `veil run`).
func NewWriter(dst io.Writer, secrets ...[]byte) io.Writer {
	var clean [][]byte
	for _, s := range secrets {
		if len(s) > 0 {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		return dst
	}
	return &writer{dst: dst, secrets: clean}
}

func (w *writer) Write(p []byte) (int, error) {
	redacted := Bytes(p, w.secrets...)
	if _, err := w.dst.Write(redacted); err != nil {
		return 0, err
	}
	return len(p), nil
}
