package workload

import "testing"

func TestResolvePreset(t *testing.T) {
	iss, sub, aud, err := ResolvePreset(PresetOpts{
		Preset: "github-actions",
		Repo:   "VortexNYC/veil",
	})
	if err != nil {
		t.Fatal(err)
	}
	if iss != "https://token.actions.githubusercontent.com" || sub != "repo:VortexNYC/veil:ref:refs/heads/main" || aud != "veil" {
		t.Fatalf("gh preset: %q %q %q", iss, sub, aud)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset: "vercel",
		Org:    "acme",
		App:    "acme_website",
	})
	if err != nil {
		t.Fatal(err)
	}
	if iss != "https://oidc.vercel.com/acme" || sub != "owner:acme:project:acme_website:environment:production" || aud != "https://vercel.com/acme" {
		t.Fatalf("vercel preset: %q %q %q", iss, sub, aud)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset: "fly",
		Org:    "vortex",
		App:    "worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if iss != "https://oidc.fly.io/vortex" || sub != "vortex:worker" || aud != "veil" {
		t.Fatalf("fly preset: %q %q %q", iss, sub, aud)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset:  "better-auth",
		Issuer:  "https://auth.example.com",
		Subject: "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if iss != "https://auth.example.com" || sub != "user_123" || aud != "veil" {
		t.Fatalf("better-auth preset: %q %q %q", iss, sub, aud)
	}

	if _, _, _, err := ResolvePreset(PresetOpts{Preset: "github-actions"}); err == nil {
		t.Fatal("missing repo should fail")
	}
	if _, _, _, err := ResolvePreset(PresetOpts{Preset: "bogus"}); err == nil {
		t.Fatal("unknown preset should fail")
	}
}
