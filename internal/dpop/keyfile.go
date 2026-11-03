package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
)

type privJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d"`
}

// LoadOrGenerateKey reads a P-256 ES256 private key from path or creates one.
// The file is written mode 0600 with a JSON JWK including the private scalar.
func LoadOrGenerateKey(path string) (*ecdsa.PrivateKey, error) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return nil, fmt.Errorf("dpop: empty key path")
	}
	if b, err := os.ReadFile(path); err == nil {
		return parsePrivJWK(b)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, err := marshalPrivJWK(key)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func marshalPrivJWK(key *ecdsa.PrivateKey) ([]byte, error) {
	if key == nil || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("dpop: key must be EC P-256")
	}
	xb := key.PublicKey.X.FillBytes(make([]byte, 32))
	yb := key.PublicKey.Y.FillBytes(make([]byte, 32))
	db := key.D.FillBytes(make([]byte, 32))
	j := privJWK{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(xb),
		Y:   base64.RawURLEncoding.EncodeToString(yb),
		D:   base64.RawURLEncoding.EncodeToString(db),
	}
	return json.Marshal(j)
}

func parsePrivJWK(b []byte) (*ecdsa.PrivateKey, error) {
	var j privJWK
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("dpop: bad key file: %w", err)
	}
	if j.Kty != "EC" || j.Crv != "P-256" || j.D == "" || j.X == "" || j.Y == "" {
		return nil, fmt.Errorf("dpop: invalid key file")
	}
	d, err := decodeCoord(j.D)
	if err != nil {
		return nil, err
	}
	x, err := decodeCoord(j.X)
	if err != nil {
		return nil, err
	}
	y, err := decodeCoord(j.Y)
	if err != nil {
		return nil, err
	}
	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D:         d,
	}
	if !priv.PublicKey.Curve.IsOnCurve(x, y) {
		return nil, fmt.Errorf("dpop: key point off curve")
	}
	return priv, nil
}

func decodeCoord(s string) (*big.Int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("dpop: bad coordinate")
	}
	return new(big.Int).SetBytes(raw), nil
}
