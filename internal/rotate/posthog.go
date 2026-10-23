package rotate

// PostHog personal API keys: self-rotating via /api/personal_api_keys/ across
// PostHog Cloud US, EU, and default hosts. Registering the spec both enables
// zero-touch rotation and blocks agent fetches to /api/personal_api_keys.
func init() {
	for _, host := range []string{"us.posthog.com", "eu.posthog.com", "app.posthog.com"} {
		_ = RegisterSpec(Spec{
			Host:            host,
			CreatePath:      "/api/personal_api_keys/",
			TokenField:      "value",
			KeyIDField:      "id",
			VerifyPath:      "/api/users/@me/",
			RetirePath:      "/api/personal_api_keys/{id}/",
			ManagementPaths: []string{"/api/personal_api_keys"},
		})
	}
}
