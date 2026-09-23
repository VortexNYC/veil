// Package testutil holds helpers shared across package tests. Nothing in
// here ships in a binary — it is imported only from _test.go files.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

// HydraEnsureClient registers a public PKCE client on a dev Hydra admin
// API; tolerates an existing registration (409).
func HydraEnsureClient(ctx context.Context, adm, clientID, redirectURI string) error {
	body := fmt.Sprintf(`{"client_id":%q,"grant_types":["authorization_code"],"response_types":["code"],"redirect_uris":[%q],"token_endpoint_auth_method":"none","scope":"openid"}`,
		clientID, redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(adm, "/")+"/admin/clients", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusConflict {
		return fmt.Errorf("hydra create client: %s", res.Status)
	}
	return nil
}

// HydraAuthorize drives a real Hydra authorize flow: opens authURL in a
// cookie-carrying client, accepts the login request on the admin API as
// subject, accepts consent for the openid scope, and returns the
// authorization code. The caller exchanges it (Verifier.Exchange). The
// only human step faked is "who clicked accept" — every token is minted
// by real Hydra.
//
// Expects a dev Hydra (`serve all --dev`, DSN=memory) with
// URLS_SELF_ISSUER matching the public URL, e.g.:
//
//	docker run -d --name veil-hydra-test -p 5555:4444 -p 5556:4445 \
//	  -e DSN=memory -e SECRETS_SYSTEM=test-system-secret-0123456789abcdef \
//	  -e OIDC_SUBJECT_IDENTIFIERS_PAIRWISE_SALT=test-salt-0123456789abcdef \
//	  -e URLS_SELF_ISSUER=http://127.0.0.1:5555 \
//	  oryd/hydra:v26.2.0 serve all --dev
func HydraAuthorize(ctx context.Context, adm, authURL, subject string) (string, error) {
	adm = strings.TrimRight(adm, "/")
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	// The authorize → login-accept → consent-accept chain needs the session
	// cookie the authorize call sets; without it Hydra answers
	// request_forbidden (no CSRF value).
	browser := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := browser.Get(authURL)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	loginCh, err := redirectParam(res, "login_challenge")
	if err != nil {
		return "", fmt.Errorf("authorize: %w", err)
	}
	next, err := hydraAccept(ctx, adm, "login", "login_challenge", loginCh,
		fmt.Sprintf(`{"subject":%q,"remember":false,"acr":"aal2","amr":["pwd","otp"]}`, subject))
	if err != nil {
		return "", err
	}
	res, err = browser.Get(next)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	consentCh, err := redirectParam(res, "consent_challenge")
	if err != nil {
		return "", fmt.Errorf("login redirect: %w", err)
	}
	next, err = hydraAccept(ctx, adm, "consent", "consent_challenge", consentCh,
		`{"grant_scope":["openid"],"remember":false}`)
	if err != nil {
		return "", err
	}
	// Consent accept returns the authorize URL with consent_verifier —
	// following it is what emits the client redirect carrying the code.
	res, err = browser.Get(next)
	if err != nil {
		return "", err
	}
	res.Body.Close()
	code, err := redirectParam(res, "code")
	if err != nil {
		return "", fmt.Errorf("consent redirect: %w", err)
	}
	return code, nil
}

// hydraAccept accepts a Hydra login/consent request via the admin API and
// returns the URL the browser must be sent to next.
func hydraAccept(ctx context.Context, adm, kind, param, challenge, body string) (string, error) {
	u := fmt.Sprintf("%s/admin/oauth2/auth/requests/%s/accept?%s=%s",
		adm, kind, param, url.QueryEscape(challenge))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		RedirectTo string `json:"redirect_to"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("%s accept: %w", kind, err)
	}
	if res.StatusCode != http.StatusOK || out.RedirectTo == "" {
		return "", fmt.Errorf("%s accept: %s (%s)", kind, res.Status, out.Error)
	}
	return out.RedirectTo, nil
}

func redirectParam(res *http.Response, name string) (string, error) {
	u, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		return "", err
	}
	v := u.Query().Get(name)
	if v == "" {
		return "", fmt.Errorf("no %s in redirect %q", name, u.Redacted())
	}
	return v, nil
}
