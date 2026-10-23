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
	case "gitlab":
		if issuer == "" {
			issuer = "https://gitlab.com"
		}
		if subject == "" {
			repo := strings.TrimSpace(opts.Repo)
			if repo == "" || !strings.Contains(repo, "/") {
				return "", "", "", fmt.Errorf("workload: gitlab preset requires --repo group/project (or --subject)")
			}
			ref := strings.TrimSpace(opts.Ref)
			if ref == "" {
				ref = "main"
			}
			subject = "project_path:" + repo + ":ref_type:branch:ref:" + ref
		}
		if audience == "" {
			audience = DefaultAudience
		}
	case "google":
		if issuer == "" {
			issuer = "https://accounts.google.com"
		}
		if subject == "" {
			return "", "", "", fmt.Errorf("workload: google preset requires --subject <unique-id-or-*>")
		}
		if audience == "" {
			audience = DefaultAudience
		}
	case "bitbucket":
		ws := strings.TrimSpace(opts.Org)
		if issuer == "" {
			if ws == "" {
				return "", "", "", fmt.Errorf("workload: bitbucket preset requires --org <workspace> (or --issuer)")
			}
			issuer = "https://api.bitbucket.org/2.0/workspaces/" + ws + "/pipelines-config/identity/oidc"
		}
		if subject == "" {
			return "", "", "", fmt.Errorf("workload: bitbucket preset requires --subject")
		}
		if audience == "" {
			if ws != "" {
				audience = "ari:cloud:bitbucket::workspace/" + ws
			} else {
				audience = DefaultAudience
			}
		}
	case "aws-eks", "aws":
		if issuer == "" {
			return "", "", "", fmt.Errorf("workload: aws-eks preset requires --issuer https://oidc.eks.<region>.amazonaws.com/id/<id>")
		}
		if subject == "" {
			ns := strings.TrimSpace(opts.Org)
			sa := strings.TrimSpace(opts.App)
			if ns == "" || sa == "" {
				return "", "", "", fmt.Errorf("workload: aws-eks preset requires --org <namespace> and --app <service-account> (or --subject)")
			}
			subject = "system:serviceaccount:" + ns + ":" + sa
		}
		if audience == "" {
			audience = DefaultAudience
		}
	case "azure":
		tenant := strings.ToLower(strings.TrimSpace(opts.Org))
		if issuer == "" {
			if tenant == "" {
				return "", "", "", fmt.Errorf("workload: azure preset requires --org <tenant-id> (or --issuer)")
			}
			issuer = "https://login.microsoftonline.com/" + tenant + "/v2.0"
		}
		if subject == "" {
			return "", "", "", fmt.Errorf("workload: azure preset requires --subject")
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
		return "", "", "", fmt.Errorf("workload: unknown preset %q (want vercel, github-actions, gitlab, google, bitbucket, aws-eks, azure, fly, better-auth, clerk, or workos)", p)
	}

	if issuer == "" || subject == "" || audience == "" {
		return "", "", "", fmt.Errorf("workload: issuer, subject, and audience are required")
	}
	return issuer, subject, audience, nil
}
