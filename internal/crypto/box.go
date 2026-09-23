// Package crypto wraps golang.org/x/crypto. Do not invent AEAD or KDFs.
package crypto

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

var ErrAuth = errors.New("crypto: authentication failed")

const KeySize = chacha20poly1305.KeySize

func NewKey() ([]byte, error) {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, nil
}

// Seal returns nonce||ciphertext. key must be KeySize bytes.
// This is also the owner-key wrap: master seals the per-owner DEK.
func Seal(key, plaintext []byte) ([]byte, error) {
	return SealAAD(key, plaintext, nil)
}

// SealAAD binds ciphertext to aad: OpenAAD with different data fails.
// New formats take context (org, item) as AAD; legacy blobs predate it.
func SealAAD(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

func Open(key, blob []byte) ([]byte, error) {
	return OpenAAD(key, blob, nil)
}

// epochMark is the blob epoch marker: SealEpoch output is mark||nonce||ct.
// Five bytes keeps a random legacy nonce colliding with it near-impossible
// (~2^-40), so a legacy blob is never misread as an AAD-bound one.
var epochMark = []byte("VEIL1")

// SealEpoch is SealAAD stamped with the epoch marker. Store rows written
// through it are bound to aad; OpenEpoch reads both this and legacy
// nil-AAD blobs.
func SealEpoch(key, plaintext, aad []byte) ([]byte, error) {
	blob, err := SealAAD(key, plaintext, aad)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), epochMark...), blob...), nil
}

// OpenEpoch dispatches on the epoch marker: marked blobs must open under aad
// (a wrong context fails closed — no legacy retry), unmarked blobs are legacy
// and open nil-AAD. aad is ignored for legacy blobs.
func OpenEpoch(key, blob, aad []byte) ([]byte, error) {
	if len(blob) >= len(epochMark) && string(blob[:len(epochMark)]) == string(epochMark) {
		return OpenAAD(key, blob[len(epochMark):], aad)
	}
	return Open(key, blob)
}

// OpenAAD fails with ErrAuth unless aad matches what SealAAD bound.
func OpenAAD(key, blob, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < aead.NonceSize() {
		return nil, ErrAuth
	}
	nonce, ct := blob[:aead.NonceSize()], blob[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrAuth
	}
	return plain, nil
}
