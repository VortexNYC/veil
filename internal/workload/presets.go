package workload

import (
	"fmt"
	"strings"
)

// DefaultAudience is the expected OIDC aud claim when a preset is used and
// no explicit --audience is provided.
const DefaultAudience = "veil"

// PresetOpts carries the inputs for ResolvePreset. Explicit Issuer/Subject/
// Audience win when set; presets fill the canonical issuer URL and subject
// claim format for common workload & auth identity providers.
type PresetOpts struct {
	Preset   string
	Issuer   string
	Subject  string
	Audience string
	Repo     string // github-actions: owner/repo
	Ref      string // github-actions: git ref (default refs/heads/main)
	Org      string // fly / vercel: org or team slug
	App      string // fly / vercel: app or project name
	Env      string // vercel: production, preview, development, or * (default production)
}

// ResolvePreset expands a named OIDC federation preset (github-actions, vercel,
// fly, better-auth, clerk, workos) or validates raw (issuer, subject, audience).
func ResolvePreset(opts PresetOpts) (issuer, subject, audience string, err error) {
	p := strings.ToLower(strings.TrimSpace(opts.Preset))
	issuer = strings.TrimSpace(opts.Issuer)
	subject = strings.TrimSpace(opts.Subject)
	audience = strings.TrimSpace(opts.Audience)

	switch p {
	case "":
		// Raw flags: caller must provide all three explicitly.
	case "vercel":
		team := strings.TrimSpace(opts.Org)
		if issuer == "" {
			if team == "" {
				return "", "", "", fmt.Errorf("workload: vercel preset requires --org <team-slug> (or --issuer)")
			}
			issuer = "https://oidc.vercel.com/" + team
		}
		if subject == "" {
			proj := strings.TrimSpace(opts.App)
			if team == "" || proj == "" {
				return "", "", "", fmt.Errorf("workload: vercel preset requires --org <team-slug> and --app <project-name> (or --subject)")
			}
			env := strings.TrimSpace(opts.Env)
			if env == "" {
				env = "production"
			}
			subject = "owner:" + team + ":project:" + proj + ":environment:" + env
		}
		if audience == "" {
			if team != "" {
				audience = "https://vercel.com/" + team
			} else {
				audience = DefaultAudience
			}
		}
	case "github-actions":
		if issuer == "" {
			issuer = "https://token.actions.githubusercontent.com"
		}
		if subject == "" {
			repo := strings.TrimSpace(opts.Repo)
			if repo == "" || !strings.Contains(repo, "/") {
				return "", "", "", fmt.Errorf("workload: github-actions preset requires --repo owner/name (or --subject)")
			}
			ref := strings.TrimSpace(opts.Ref)
			if ref == "" {
				ref = "refs/heads/main"
			}
			subject = "repo:" + repo + ":ref:" + ref
		}
		if audience == "" {
			audience = DefaultAudience
		}
	case "fly":
		org := strings.TrimSpace(opts.Org)
		if issuer == "" {
			if org == "" {
				return "", "", "", fmt.Errorf("workload: fly preset requires --org <fly-org-slug> (or --issuer)")
			}
			issuer = "https://oidc.fly.io/" + org
		}
		if subject == "" {
			app := strings.TrimSpace(opts.App)
			if org == "" || app == "" {
				return "", "", "", fmt.Errorf("workload: fly preset requires --org and --app (or --subject)")
			}
			subject = org + ":" + app
		}
		if audience == "" {
			audience = DefaultAudience
		}
	case "better-auth", "clerk", "workos":
		if issuer == "" || subject == "" {
			return "", "", "", fmt.Errorf("workload: %s preset requires --issuer and --subject", p)
		}
		if audience == "" {
			audience = DefaultAudience
		}
	default:
		return "", "", "", fmt.Errorf("workload: unknown preset %q (want vercel, github-actions, fly, better-auth, clerk, or workos)", p)
	}

	if issuer == "" || subject == "" || audience == "" {
		return "", "", "", fmt.Errorf("workload: issuer, subject, and audience are required")
	}
	return issuer, subject, audience, nil
}
