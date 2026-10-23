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

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset: "gitlab",
		Repo:   "acme/platform/web",
	})
	if err != nil || iss != "https://gitlab.com" || sub != "project_path:acme/platform/web:ref_type:branch:ref:main" || aud != "veil" {
		t.Fatalf("gitlab preset: %q %q %q %v", iss, sub, aud, err)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset:  "google",
		Subject: "117000111222",
	})
	if err != nil || iss != "https://accounts.google.com" || sub != "117000111222" || aud != "veil" {
		t.Fatalf("google preset: %q %q %q %v", iss, sub, aud, err)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset:  "bitbucket",
		Org:     "acme",
		Subject: "{repo-uuid}",
	})
	if err != nil || iss != "https://api.bitbucket.org/2.0/workspaces/acme/pipelines-config/identity/oidc" || aud != "ari:cloud:bitbucket::workspace/acme" {
		t.Fatalf("bitbucket preset: %q %q %q %v", iss, sub, aud, err)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset: "aws-eks",
		Issuer: "https://oidc.eks.us-east-1.amazonaws.com/id/EX",
		Org:    "default",
		App:    "deploy-bot",
	})
	if err != nil || sub != "system:serviceaccount:default:deploy-bot" || aud != "veil" {
		t.Fatalf("aws-eks preset: %q %q %q %v", iss, sub, aud, err)
	}

	iss, sub, aud, err = ResolvePreset(PresetOpts{
		Preset:  "azure",
		Org:     "aaaabbbb-0000-cccc-1111-dddd2222eeee",
		Subject: "*",
	})
	if err != nil || iss != "https://login.microsoftonline.com/aaaabbbb-0000-cccc-1111-dddd2222eeee/v2.0" || sub != "*" || aud != "veil" {
		t.Fatalf("azure preset: %q %q %q %v", iss, sub, aud, err)
	}

	if _, _, _, err := ResolvePreset(PresetOpts{Preset: "github-actions"}); err == nil {
		t.Fatal("missing repo should fail")
	}
	for _, bad := range []PresetOpts{
		{Preset: "gitlab"},
		{Preset: "google"},
		{Preset: "bitbucket"},
		{Preset: "aws-eks"},
		{Preset: "azure"},
	} {
		if _, _, _, err := ResolvePreset(bad); err == nil {
			t.Fatalf("missing required fields for preset %q should fail", bad.Preset)
		}
	}
	if _, _, _, err := ResolvePreset(PresetOpts{Preset: "bogus"}); err == nil {
		t.Fatal("unknown preset should fail")
	}
}
