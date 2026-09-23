package kratos

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfficialMFAMethodsEnabled(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Dir(file)
	for _, name := range []string{"kratos.yml", "kratos.veil.yml"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		for _, needle := range []string{"totp:", "webauthn:", "lookup_secret:", "issuer: Veil"} {
			if !strings.Contains(s, needle) {
				t.Fatalf("%s missing %s", name, needle)
			}
		}
		if strings.Contains(s, "passwordless: true") {
			t.Fatalf("%s webauthn is first-factor; MFA is password then totp/webauthn", name)
		}
		if name == "kratos.veil.yml" {
			if strings.Contains(s, "127.0.0.1:1025") || strings.Contains(s, "smtp://mail:") {
				t.Fatal("origin courier is mailpit")
			}
			if !strings.Contains(s, "delivery_strategy: http") || !strings.Contains(s, "mail.veil.nyc") || !strings.Contains(s, "noreply@veil.nyc") {
				t.Fatal("origin courier is not the veil-mail worker from noreply@veil.nyc")
			}
			if !strings.Contains(s, "type: api_key") || !strings.Contains(s, "name: Authorization") {
				t.Fatal("origin courier auth is not a bearer token header")
			}
			if !strings.Contains(s, "registration:\n      enabled: false") {
				t.Fatal("origin registration is not invite-gated")
			}
			if !strings.Contains(s, "required_aal: highest_available") {
				t.Fatal("origin session does not require TOTP when enrolled")
			}
			if !strings.Contains(s, "https://app.veil.nyc") {
				t.Fatal("origin kratos must allow the vault SPA return_to")
			}
		}
	}
	schema, err := os.ReadFile(filepath.Join(dir, "identity.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{`"totp"`, `"account_name"`, `"webauthn"`} {
		if !strings.Contains(string(schema), needle) {
			t.Fatalf("schema missing %s", needle)
		}
	}
}
