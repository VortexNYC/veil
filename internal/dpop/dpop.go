// Package dpop implements RFC 9449 OAuth 2.0 Demonstrating Proof-of-Possession
// (DPoP) verification and proof minting for ES256 (P-256) keys.
//
// When a caller sends a DPoP header (or a token carrying cnf.jkt / previously
// pinned on first DPoP use), VerifyRequest enforces:
//   - header typ="dpop+jwt", alg="ES256", public EC P-256 jwk (no private "d")
//   - valid ES256 signature over header.payload (IEEE P1363 r||s or ASN.1 DER)
//   - htm == r.Method and normalized htu (scheme://host/path without query/fragment)
//   - iat within ±60s of server time and unique jti within the replay window
//   - ath == base64url(sha256(accessToken))
//   - cnf.jkt match (if present in JWT accessToken) + bind-on-first-use pin
//     (tokenHash -> jkt) so a stolen token cannot be used with a different key
//     or downgraded back to plain Bearer.
package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	clockSkewWindow = 60 * time.Second
	pinTTL          = 1 * time.Hour
	maxCacheEntries = 4096
)

type jwkHeader struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d,omitempty"`
}

type dpopHeader struct {
	Typ string    `json:"typ"`
	Alg string    `json:"alg"`
	JWK jwkHeader `json:"jwk"`
}

type dpopClaims struct {
	JTI string `json:"jti"`
	HTM string `json:"htm"`
	HTU string `json:"htu"`
	IAT int64  `json:"iat"`
	ATH string `json:"ath"`
}

type pinEntry struct {
	jkt       string
	expiresAt time.Time
}

type Verifier struct {
	mu   sync.Mutex
	jtis map[string]time.Time
	pins map[string]pinEntry
	Now  func() time.Time
}

var defaultVerifier = NewVerifier()

func NewVerifier() *Verifier {
	return &Verifier{
		jtis: map[string]time.Time{},
		pins: map[string]pinEntry{},
		Now:  time.Now,
	}
}

// VerifyRequest checks the request's DPoP header against accessToken using the
// process-wide verifier.
func VerifyRequest(r *http.Request, accessToken string) error {
	return defaultVerifier.VerifyRequest(r, accessToken)
}

func (v *Verifier) VerifyRequest(r *http.Request, accessToken string) error {
	if r == nil {
		return nil
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil
	}
	now := v.Now().UTC()
	proof := strings.TrimSpace(r.Header.Get("DPoP"))
	tokHash := tokenDigest(accessToken)
	cnfJKT := extractTokenCnfJKT(accessToken)

	if proof == "" {
		if cnfJKT != "" {
			return fmt.Errorf("dpop: token requires DPoP proof (cnf.jkt)")
		}
		if v.pinnedJKT(tokHash, now) != "" {
			return fmt.Errorf("dpop: token is pinned to a DPoP key")
		}
		return nil
	}
	if len(r.Header.Values("DPoP")) > 1 {
		return fmt.Errorf("dpop: multiple DPoP headers")
	}

	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		return fmt.Errorf("dpop: malformed proof JWT")
	}
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("dpop: bad header encoding")
	}
	var hdr dpopHeader
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return fmt.Errorf("dpop: bad header JSON")
	}
	if !strings.EqualFold(hdr.Typ, "dpop+jwt") || hdr.Alg != "ES256" {
		return fmt.Errorf("dpop: invalid typ or alg")
	}
	if hdr.JWK.D != "" {
		return fmt.Errorf("dpop: jwk must not contain private key parameter")
	}
	pub, jkt, err := parseP256JWK(hdr.JWK)
	if err != nil {
		return err
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("dpop: bad signature encoding")
	}
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	if !verifyES256(pub, digest[:], sigBytes) {
		return fmt.Errorf("dpop: signature verification failed")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("dpop: bad payload encoding")
	}
	var claims dpopClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return fmt.Errorf("dpop: bad payload JSON")
	}
	if claims.JTI == "" || len(claims.JTI) > 256 {
		return fmt.Errorf("dpop: invalid jti")
	}
	if !strings.EqualFold(claims.HTM, r.Method) {
		return fmt.Errorf("dpop: htm mismatch")
	}
	if !matchHTU(claims.HTU, r) {
		return fmt.Errorf("dpop: htu mismatch")
	}
	iat := time.Unix(claims.IAT, 0).UTC()
	if now.Sub(iat) > clockSkewWindow || iat.Sub(now) > clockSkewWindow {
		return fmt.Errorf("dpop: iat outside ±60s window")
	}
	wantATH := computeATH(accessToken)
	if claims.ATH == "" || claims.ATH != wantATH {
		return fmt.Errorf("dpop: ath mismatch")
	}
	if cnfJKT != "" && cnfJKT != jkt {
		return fmt.Errorf("dpop: cnf.jkt mismatch")
	}
	return v.recordAndPin(claims.JTI, tokHash, jkt, now)
}

func (v *Verifier) pinnedJKT(tokHash string, now time.Time) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	p, ok := v.pins[tokHash]
	if !ok {
		return ""
	}
	if !now.Before(p.expiresAt) {
		delete(v.pins, tokHash)
		return ""
	}
	return p.jkt
}

func (v *Verifier) recordAndPin(jti, tokHash, jkt string, now time.Time) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	jtiKey := tokHash + "\x00" + jti
	if exp, seen := v.jtis[jtiKey]; seen && now.Before(exp) {
		return fmt.Errorf("dpop: replayed jti")
	}
	if p, ok := v.pins[tokHash]; ok && now.Before(p.expiresAt) && p.jkt != jkt {
		return fmt.Errorf("dpop: jkt mismatch with pinned token key")
	}

	if len(v.jtis) >= maxCacheEntries {
		for k, exp := range v.jtis {
			if !now.Before(exp) {
				delete(v.jtis, k)
			}
		}
		if len(v.jtis) >= maxCacheEntries {
			for k := range v.jtis {
				delete(v.jtis, k)
				break
			}
		}
	}
	v.jtis[jtiKey] = now.Add(2 * clockSkewWindow)

	if len(v.pins) >= maxCacheEntries {
		for k, p := range v.pins {
			if !now.Before(p.expiresAt) {
				delete(v.pins, k)
			}
		}
		if len(v.pins) >= maxCacheEntries {
			for k := range v.pins {
				delete(v.pins, k)
				break
			}
		}
	}
	v.pins[tokHash] = pinEntry{
		jkt:       jkt,
		expiresAt: now.Add(pinTTL),
	}
	return nil
}

func parseP256JWK(j jwkHeader) (*ecdsa.PublicKey, string, error) {
	if j.Kty != "EC" || j.Crv != "P-256" || j.X == "" || j.Y == "" {
		return nil, "", fmt.Errorf("dpop: jwk must be EC P-256")
	}
	xb, err := base64.RawURLEncoding.DecodeString(j.X)
	if err != nil || len(xb) != 32 {
		return nil, "", fmt.Errorf("dpop: invalid jwk x coordinate")
	}
	yb, err := base64.RawURLEncoding.DecodeString(j.Y)
	if err != nil || len(yb) != 32 {
		return nil, "", fmt.Errorf("dpop: invalid jwk y coordinate")
	}
	curve := elliptic.P256()
	x := new(big.Int).SetBytes(xb)
	y := new(big.Int).SetBytes(yb)
	if !curve.IsOnCurve(x, y) {
		return nil, "", fmt.Errorf("dpop: point not on P-256 curve")
	}
	// RFC 7638 canonical JWK thumbprint: lexicographical member order crv, kty, x, y.
	canonical := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":%q,"y":%q}`, j.X, j.Y)
	sum := sha256.Sum256([]byte(canonical))
	jkt := base64.RawURLEncoding.EncodeToString(sum[:])
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, jkt, nil
}

func verifyES256(pub *ecdsa.PublicKey, digest, sig []byte) bool {
	if len(sig) == 64 {
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		return ecdsa.Verify(pub, digest, r, s)
	}
	return ecdsa.VerifyASN1(pub, digest, sig)
}

func matchHTU(htu string, r *http.Request) bool {
	u, err := url.Parse(strings.TrimSpace(htu))
	if err != nil || u == nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return false
	}
	reqHost := r.Host
	if fwdHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fwdHost != "" {
		reqHost = strings.TrimSpace(strings.Split(fwdHost, ",")[0])
	}
	if reqHost != "" && !strings.EqualFold(u.Host, reqHost) {
		return false
	}
	uPath := u.Path
	if uPath == "" {
		uPath = "/"
	}
	rPath := r.URL.Path
	if rPath == "" {
		rPath = "/"
	}
	return uPath == rPath
}

func computeATH(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func tokenDigest(accessToken string) string {
	sum := sha256.Sum256([]byte(accessToken))
	return hex.EncodeToString(sum[:])
}

func extractTokenCnfJKT(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload struct {
		Cnf struct {
			JKT string `json:"jkt"`
		} `json:"cnf"`
	}
	if json.Unmarshal(b, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Cnf.JKT)
}

// MintProof creates an RFC 9449 ES256 DPoP proof JWT for (method, targetURL,
// accessToken).
func MintProof(priv *ecdsa.PrivateKey, method, targetURL, accessToken string, now time.Time) (string, error) {
	u, err := url.Parse(targetURL)
	if err != nil || u == nil || u.Host == "" {
		return "", fmt.Errorf("dpop: invalid target URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	xb := priv.PublicKey.X.FillBytes(make([]byte, 32))
	yb := priv.PublicKey.Y.FillBytes(make([]byte, 32))
	hdr := dpopHeader{
		Typ: "dpop+jwt",
		Alg: "ES256",
		JWK: jwkHeader{
			Kty: "EC",
			Crv: "P-256",
			X:   base64.RawURLEncoding.EncodeToString(xb),
			Y:   base64.RawURLEncoding.EncodeToString(yb),
		},
	}
	var jtiBuf [16]byte
	if _, err := rand.Read(jtiBuf[:]); err != nil {
		return "", err
	}
	claims := dpopClaims{
		JTI: hex.EncodeToString(jtiBuf[:]),
		HTM: strings.ToUpper(method),
		HTU: u.String(),
		IAT: now.UTC().Unix(),
		ATH: computeATH(accessToken),
	}
	hb, err := json.Marshal(hdr)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
