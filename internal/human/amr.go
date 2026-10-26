package human

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// AMR reads the Hydra id_token amr claim. ok is false when the token is not a JWT.
func AMR(raw string) ([]string, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 || parts[1] == "" {
		return nil, false
	}
	p := parts[1]
	if n := len(p) % 4; n != 0 {
		p += strings.Repeat("=", 4-n)
	}
	payload, err := base64.URLEncoding.DecodeString(p)
	if err != nil {
		return nil, false
	}
	var claims struct {
		Amr []string `json:"amr"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return nil, false
	}
	return claims.Amr, true
}

// secondFactors are the amr values that satisfy aal2 — the same set the
// vault accepts in apps/vault/src/auth.ts: an authenticator app, a passkey,
// or a recovery code.
var secondFactors = map[string]bool{
	"totp":          true,
	"webauthn":      true,
	"lookup_secret": true,
}

// RequireMFA rejects an id token whose amr skipped a second factor — totp,
// webauthn (passkey), or lookup_secret. Opaque test tokens are not JWTs and
// are left alone.
func RequireMFA(raw string) error {
	amr, ok := AMR(raw)
	if !ok {
		return nil
	}
	for _, m := range amr {
		if secondFactors[m] {
			return nil
		}
	}
	return fmt.Errorf("human: id token amr must include a second factor")
}
