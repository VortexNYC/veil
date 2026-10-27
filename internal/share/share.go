// Package share seals share-link envelopes client-side (VTX-783).
//
// The origin stores only the sealed blob. The link key lives in the URL
// fragment (#k=) and the optional passphrase in the sender's head — the
// server never sees either. Format, implemented identically by the SPA:
//
//	key   = 32 random bytes          → fragment, base64url
//	keff  = key XOR SHA-256("veil-share-pw\x00"+passphrase)   (key alone when no passphrase)
//	blob  = nonce(24) || secretbox(plaintext, nonce, keff)
//	meta  = {"v":1,"alg":"nacl.secretbox","pw":true}
//
// Do not invent AEAD or KDFs — secretbox is golang.org/x/crypto, the
// passphrase mix is crypto/sha256.
package share

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

const (
	Version  = 1
	alg      = "nacl.secretbox"
	keyLen   = 32
	nonceLen = 24
)

// Meta is the envelope params JSON the origin stores verbatim — it marks
// the algorithm and whether a passphrase mixed into the key. No secret.
type Meta struct {
	V   int    `json:"v"`
	Alg string `json:"alg"`
	PW  bool   `json:"pw,omitempty"`
}

// Seal encrypts plaintext under a fresh link key, optionally mixed with a
// passphrase. Returns the blob to POST, the meta JSON, and the base64url
// key for the link fragment.
func Seal(plaintext []byte, passphrase string) (blob []byte, meta json.RawMessage, keyB64 string, err error) {
	if len(plaintext) == 0 {
		return nil, nil, "", fmt.Errorf("share: empty plaintext")
	}
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, "", err
	}
	keff := mix(key, passphrase)
	var nonce [nonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, "", err
	}
	var nk [keyLen]byte
	var nn [nonceLen]byte
	copy(nk[:], keff)
	copy(nn[:], nonce[:])
	out := make([]byte, 0, nonceLen+len(plaintext)+secretbox.Overhead)
	out = append(out, nonce[:]...)
	out = secretbox.Seal(out, plaintext, &nn, &nk)
	m, err := json.Marshal(Meta{V: Version, Alg: alg, PW: passphrase != ""})
	if err != nil {
		return nil, nil, "", err
	}
	return out, m, base64.RawURLEncoding.EncodeToString(key), nil
}

// Open reverses Seal. The base64url key comes from the link fragment; the
// passphrase from the recipient. Any mismatch is a generic auth failure —
// never distinguish "bad key" from "bad passphrase".
func Open(blob []byte, meta json.RawMessage, keyB64, passphrase string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(keyB64)
	if err != nil || len(key) != keyLen {
		return nil, fmt.Errorf("share: bad key")
	}
	var m Meta
	if len(meta) > 0 {
		if err := json.Unmarshal(meta, &m); err != nil {
			return nil, fmt.Errorf("share: bad meta")
		}
	}
	if m.Alg != alg || m.V != Version {
		return nil, fmt.Errorf("share: unsupported envelope")
	}
	if m.PW && passphrase == "" {
		return nil, fmt.Errorf("share: passphrase required")
	}
	if len(blob) < nonceLen+secretbox.Overhead {
		return nil, fmt.Errorf("share: short blob")
	}
	keff := mix(key, passphrase)
	var nk [keyLen]byte
	var nn [nonceLen]byte
	copy(nk[:], keff)
	copy(nn[:], blob[:nonceLen])
	plain, ok := secretbox.Open(nil, blob[nonceLen:], &nn, &nk)
	if !ok {
		return nil, fmt.Errorf("share: cannot open")
	}
	return plain, nil
}

// mix returns key, XORed with the passphrase key when pass is non-empty.
func mix(key []byte, pass string) []byte {
	out := make([]byte, keyLen)
	copy(out, key)
	if pass == "" {
		return out
	}
	sum := sha256.Sum256(append([]byte("veil-share-pw\x00"), pass...))
	for i := range out {
		out[i] ^= sum[i]
	}
	return out
}
