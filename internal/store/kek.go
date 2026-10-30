package store

// kekProvider abstracts where KEK wrap/unwrap happens. localKEK is the
// original in-process key; remoteKEK calls the Cloudflare key-service
// Worker, which writes version-0x02 envelopes (AES-256-GCM) so rows can
// be told apart from legacy local-AEAD wraps on read.
//
// Blob layout contract (mirrors apps/key-service):
//   remote: 0x02 || iv(12) || AES-GCM ct
//   local:  nonce(24) || XChaCha20-Poly1305 ct   (never starts 0x02 in
//           practice — the 1/256 collision is handled by trying remote
//           first and falling back, see openSite dispatch below)
//
// SERVICE_TOKEN is a decrypt oracle for service-wrapped rows — that is
// the accepted remote-KEK design, not a bug. Break-glass is the 2-of-3
// Shamir device escrow, not a local VEIL_KEK fallback.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
)

const (
	remoteBlobVersion = 0x02
	// KeyServiceVersion marks blobs sealed by keys.veil.nyc so openSite
	// can dispatch without trying both providers.
	EnvKeysvcURL   = "VEIL_KEYSVC_URL"
	EnvKeysvcToken = "VEIL_KEYSVC_TOKEN"
)

type kekProvider interface {
	seal(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	open(ctx context.Context, blob, aad []byte) ([]byte, error)
	remote() bool
}

// localKEK wraps the in-process deployment key — the pre-migration path.
type localKEK struct{ key []byte }

func (l *localKEK) seal(_ context.Context, pt, aad []byte) ([]byte, error) {
	return crypto.SealAAD(l.key, pt, aad)
}
func (l *localKEK) open(_ context.Context, blob, aad []byte) ([]byte, error) {
	return crypto.OpenAAD(l.key, blob, aad)
}
func (l *localKEK) remote() bool { return false }

// remoteKEK delegates wrap/unwrap to the Cloudflare key-service over HTTPS.
type remoteKEK struct {
	base  string
	token string
	hc    *http.Client
}

func newRemoteKEK(base, token string) *remoteKEK {
	return &remoteKEK{base: base, token: token,
		hc: &http.Client{Timeout: 10 * time.Second}}
}

func (r *remoteKEK) remote() bool { return true }

func (r *remoteKEK) call(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("key-service %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("key-service %s: HTTP %d", path, resp.StatusCode)
	}
	return json.Unmarshal(raw, out)
}

func (r *remoteKEK) seal(ctx context.Context, pt, aad []byte) ([]byte, error) {
	var out struct {
		CiphertextB64 string `json:"ciphertext_b64"`
		Error         string `json:"error"`
	}
	err := r.call(ctx, "/seal", map[string]string{
		"plaintext_b64": base64.StdEncoding.EncodeToString(pt),
		"aad_b64":       base64.StdEncoding.EncodeToString(aad),
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("key-service seal: %s", out.Error)
	}
	return base64.StdEncoding.DecodeString(out.CiphertextB64)
}

func (r *remoteKEK) open(ctx context.Context, blob, aad []byte) ([]byte, error) {
	var out struct {
		PlaintextB64 string `json:"plaintext_b64"`
		Error        string `json:"error"`
	}
	err := r.call(ctx, "/open", map[string]string{
		"ciphertext_b64": base64.StdEncoding.EncodeToString(blob),
		"aad_b64":        base64.StdEncoding.EncodeToString(aad),
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("key-service open: %s", out.Error)
	}
	return base64.StdEncoding.DecodeString(out.PlaintextB64)
}

// kekFromEnv picks the provider: remote when VEIL_KEYSVC_URL+TOKEN are set,
// else the legacy local key. Remote rows carry the 0x02 version byte.
func kekFromEnv(local []byte) kekProvider {
	if url, tok := os.Getenv(EnvKeysvcURL), os.Getenv(EnvKeysvcToken); url != "" && tok != "" {
		return newRemoteKEK(url, tok)
	}
	return &localKEK{key: append([]byte(nil), local...)}
}

// sealKey wraps under the active provider — remote rows are stamped 0x02.
// Local wraps always use p.kek (updated by RotateKEK); kekp holds the
// provider dispatch only. Callers hold kekMu where rotation matters; the
// remote call must never be made while holding a DB lock (statement_timeout is 5s).
func (p *Postgres) sealKey(ctx context.Context, pt, aad []byte) ([]byte, error) {
	if p.kekp.remote() {
		return p.kekp.seal(ctx, pt, aad)
	}
	return crypto.SealAAD(p.kek, pt, aad)
}

// openKey dispatches on the blob version byte: 0x02 rows go to the service,
// everything else is a legacy local wrap opened with the in-process key.
// During the migration window a store may hold a mix of both.
func (p *Postgres) openKey(ctx context.Context, blob, aad []byte) ([]byte, error) {
	if len(blob) > 0 && blob[0] == remoteBlobVersion && p.kekp.remote() {
		return p.kekp.open(ctx, blob, aad)
	}
	return crypto.OpenAAD(p.kek, blob, aad)
}

// rewrapItems sends ciphertexts through the service's /rewrap — opened
// under the currently bound key(s), re-sealed under VEIL_KEK_NEXT.
// Rotation only: errors clearly when no NEXT key is bound.
func (r *remoteKEK) rewrap(ctx context.Context, items [][2][]byte) ([][]byte, error) {
	in := make([]map[string]string, len(items))
	for i, it := range items {
		in[i] = map[string]string{
			"aad_b64":        base64.StdEncoding.EncodeToString(it[0]),
			"ciphertext_b64": base64.StdEncoding.EncodeToString(it[1]),
		}
	}
	var out struct {
		CiphertextsB64 []string `json:"ciphertexts_b64"`
		Error          string   `json:"error"`
	}
	if err := r.call(ctx, "/rewrap", map[string]any{"items": in}, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("key-service rewrap: %s", out.Error)
	}
	res := make([][]byte, len(out.CiphertextsB64))
	for i, c := range out.CiphertextsB64 {
		if c == "" {
			res[i] = nil
			continue
		}
		b, err := base64.StdEncoding.DecodeString(c)
		if err != nil {
			return nil, err
		}
		res[i] = b
	}
	return res, nil
}
