package material

import (
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

const seed = "JBSWY3DPEHPK3PXP"

func TestPackFileRoundTrip(t *testing.T) {
	raw, err := PackFile("note.txt", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := FileBytes(Unpack(raw))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("%q", got)
	}
	if _, err := PackFile("x", "", make([]byte, MaxFile+1)); err == nil {
		t.Fatal("oversize")
	}
}

func TestUnpackRawTokenCompat(t *testing.T) {
	env := Unpack([]byte("sk_live_plain"))
	if env.Token != "sk_live_plain" || env.TOTP != "" {
		t.Fatalf("%+v", env)
	}
}

func TestWithLoginUpgradesPlainToken(t *testing.T) {
	raw, err := WithLogin([]byte("sk_live_plain"), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	if env.V != 1 || env.Token != "sk_live_plain" || env.Login != "user@example.com" {
		t.Fatalf("%+v", env)
	}
	unchanged, err := WithLogin([]byte("sk_live_plain"), "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "sk_live_plain" {
		t.Fatalf("%q", unchanged)
	}
}

func TestPackRoundTrip(t *testing.T) {
	raw, err := Pack([]byte("sk_live"), []byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	if env.V != 1 || env.Token != "sk_live" || env.TOTP != seed {
		t.Fatalf("%+v", env)
	}
}

func TestWithTokenRotatesKeepsRest(t *testing.T) {
	raw, err := Pack([]byte("old_pass"), []byte(seed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = WithLogin(raw, "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = WithToken(raw, []byte("new_pass"))
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	if env.Token != "new_pass" || env.TOTP != seed || env.Login != "user@example.com" {
		t.Fatalf("%+v", env)
	}
}

func TestWithTokenUpgradesPlainToken(t *testing.T) {
	raw, err := WithToken([]byte("sk_live_plain"), []byte("rotated"))
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	if env.V != 1 || env.Token != "rotated" {
		t.Fatalf("%+v", env)
	}
	if _, err := WithToken(raw, nil); err == nil {
		t.Fatal("empty token rotated")
	}
	if _, err := WithToken(raw, []byte("  ")); err == nil {
		t.Fatal("blank token rotated")
	}
}

func TestMintMatchesPquerna(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	got, err := Mint(seed, now)
	if err != nil {
		t.Fatal(err)
	}
	want, err := totp.GenerateCode(seed, now)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || len(got) != 6 {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApplySetsBearerAndTOTP(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	code, err := Apply(h, Envelope{Token: "sk_live", TOTP: seed}, now)
	if err != nil {
		t.Fatal(err)
	}
	want, err := totp.GenerateCode(seed, now)
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("Authorization") != "Bearer sk_live" {
		t.Fatalf("auth=%q", h.Get("Authorization"))
	}
	if h.Get(HeaderTOTP) != want || code != want {
		t.Fatalf("code=%q header=%q want %q", code, h.Get(HeaderTOTP), want)
	}
}

func TestAuthorizationValue(t *testing.T) {
	if got := AuthorizationValue("sk_live"); got != "Bearer sk_live" {
		t.Fatalf("stripe %q", got)
	}
	if got := AuthorizationValue("lin_api_abc"); got != "lin_api_abc" {
		t.Fatalf("linear %q", got)
	}
	if got := AuthorizationValue("Bearer already"); got != "Bearer already" {
		t.Fatalf("passthrough %q", got)
	}
}

func TestPackPasskeyNeverInScrubMiss(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\npk\n-----END PRIVATE KEY-----"
	raw, err := PackPasskey(pem, "cred", "github.com", "dXNlcg")
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	if env.PasskeyPEM != pem || env.CredID != "cred" || env.RpID != "github.com" {
		t.Fatalf("%+v", env)
	}
	hide := ScrubList(env)
	found := false
	for _, h := range hide {
		if string(h) == pem {
			found = true
		}
	}
	if !found {
		t.Fatal("scrub missed pem")
	}
}

func TestPackCardScrubsPAN(t *testing.T) {
	raw, err := PackCard("4111111111111111", "12", "2030", "123", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	env := Unpack(raw)
	if env.Number != "4111111111111111" || env.CVV != "123" {
		t.Fatalf("%+v", env)
	}
	hide := ScrubList(env)
	found := 0
	for _, h := range hide {
		if string(h) == "4111111111111111" || string(h) == "123" {
			found++
		}
	}
	if found < 2 {
		t.Fatal("scrub missed pan or cvv")
	}
	for _, want := range []string{"12", "2030"} {
		ok := false
		for _, h := range hide {
			if string(h) == want {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("scrub missed %s", want)
		}
	}
}

func FuzzUnpack(f *testing.F) {
	packed, err := PackCard("4111111111111111", "12", "2030", "123", "Ada")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(packed)
	f.Add([]byte("sk_live_plain"))
	f.Add([]byte(`{"v":1,"number":"4111"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_ = Unpack(raw)
	})
}
