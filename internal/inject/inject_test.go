package inject

import (
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/scrub"
)

func TestExpandGrantedNameDoesNotLeakOnUnknown(t *testing.T) {
	secret := "sk_live_INJECT_SECRET"
	got, err := Expand([]byte("token=${STRIPE}\n"), Map([]string{"STRIPE=" + secret}))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "token="+secret+"\n" {
		t.Fatalf("%q", got)
	}
	_, err = Expand([]byte("token=${MISSING}\n"), Map([]string{"STRIPE=" + secret}))
	if err == nil {
		t.Fatal("expected unknown ref")
	}
	if scrub.Contains([]byte(err.Error()), []byte(secret)) {
		t.Fatalf("error leaked secret: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal(err)
	}
}

func TestExpandPWMURIAndHyphenName(t *testing.T) {
	secret := "sk_hyphen"
	got, err := Expand([]byte("veil://my-key"), Map([]string{"MY_KEY=" + secret}))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != secret {
		t.Fatalf("%q", got)
	}
}

func TestExpandEnvSchemaSupportsVarlockDSLAndVeilRefs(t *testing.T) {
	schema := `# @defaultSensitive=false @defaultRequired=infer
# ---
# @type=port
API_PORT=8080 # default port
# @required @sensitive
STRIPE_SECRET_KEY=veil://stripe
OPENAI_API_KEY=veil(openai-prod)
`
	granted := Map([]string{
		"STRIPE=sk_live_123",
		"OPENAI_PROD=sk-proj-456",
	})
	pairs, err := ExpandEnvSchema([]byte(schema), granted)
	if err != nil {
		t.Fatal(err)
	}
	got := Map(pairs)
	if got["API_PORT"] != "8080" {
		t.Fatalf("API_PORT=%q", got["API_PORT"])
	}
	if got["STRIPE_SECRET_KEY"] != "sk_live_123" {
		t.Fatalf("STRIPE_SECRET_KEY=%q", got["STRIPE_SECRET_KEY"])
	}
	if got["OPENAI_API_KEY"] != "sk-proj-456" {
		t.Fatalf("OPENAI_API_KEY=%q", got["OPENAI_API_KEY"])
	}
}
