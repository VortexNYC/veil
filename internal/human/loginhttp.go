package human

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/material"
)

// HTTPLogin is Kratos frontend HTTP. Not Ory Elements. Not CDP.
type HTTPLogin struct {
	KratosPublic string
	Email        string
	Password     string
	TOTPSeed     string
}

type flowJSON struct {
	ID  string `json:"id"`
	UI  uiJSON `json:"ui"`
	AAL string `json:"requested_aal"`
}

type uiJSON struct {
	Action   string      `json:"action"`
	Nodes    []uiNode    `json:"nodes"`
	Messages []uiMessage `json:"messages"`
}

type uiNode struct {
	Attributes uiAttrs `json:"attributes"`
}

type uiAttrs struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
	Type  string `json:"type"`
}

type uiMessage struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

type loginClient struct {
	hc   *http.Client
	base string
}

func newLoginClient() (*loginClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &loginClient{
		hc: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// LoginHTTP mints an ID token with password then TOTP. prompt=login is already
// on AuthCodeURL. The id token is rejected unless amr includes totp.
func (v *Verifier) LoginHTTP(ctx context.Context, in HTTPLogin) (string, error) {
	email := strings.TrimSpace(in.Email)
	if email == "" || in.Password == "" || strings.TrimSpace(in.TOTPSeed) == "" {
		return "", fmt.Errorf("human: email, password, and totp seed are required")
	}
	kratos := strings.TrimRight(strings.TrimSpace(in.KratosPublic), "/")
	if kratos == "" {
		return "", fmt.Errorf("human: kratos public URL is required")
	}
	verifier, state, err := PKCE()
	if err != nil {
		return "", err
	}
	authURL, err := v.AuthCodeURL(ctx, state, verifier)
	if err != nil {
		return "", err
	}
	lc, err := newLoginClient()
	if err != nil {
		return "", err
	}
	lc.base = kratos
	challenge, err := lc.loginChallenge(ctx, authURL)
	if err != nil {
		return "", err
	}
	flow, err := lc.loadFlow(ctx, kratos+"/self-service/login/browser?login_challenge="+url.QueryEscape(challenge))
	if err != nil {
		return "", err
	}
	if !flow.has("password") && !flow.has("identifier") {
		return "", fmt.Errorf("human: login flow has no password node")
	}
	next, loc, err := lc.submit(ctx, flow, map[string]string{
		"method":     "password",
		"identifier": email,
		"password":   in.Password,
	})
	if err != nil {
		return "", err
	}
	if isCallback(loc, v.redirect) {
		return "", fmt.Errorf("human: hydra accepted aal1")
	}
	switch {
	case next.has("totp_code"):
		flow = next
	case loc != "":
		flow, err = lc.loadFlow(ctx, loc)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("human: no aal2 after password")
	}
	if !flow.has("totp_code") {
		return "", fmt.Errorf("human: login flow has no totp_code")
	}
	var callback string
	for _, off := range []int{0, -1, 1} {
		code, err := totpAt(in.TOTPSeed, off)
		if err != nil {
			return "", err
		}
		_, loc, err = lc.submit(ctx, flow, map[string]string{
			"method":    "totp",
			"totp_code": code,
		})
		if err != nil {
			return "", err
		}
		if loc != "" {
			callback, err = lc.followCallback(ctx, loc, v.redirect)
			if err == nil && callback != "" {
				break
			}
		}
	}
	if callback == "" {
		return "", fmt.Errorf("human: totp did not reach callback")
	}
	u, err := url.Parse(callback)
	if err != nil {
		return "", err
	}
	if u.Query().Get("error") != "" {
		return "", fmt.Errorf("human: callback %s", u.Query().Get("error"))
	}
	if u.Query().Get("state") != state {
		return "", fmt.Errorf("human: login state")
	}
	code := u.Query().Get("code")
	raw, err := v.Exchange(ctx, code, verifier)
	if err != nil {
		return "", err
	}
	if err := RequireMFA(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func totpAt(seed string, offset int) (string, error) {
	now := time.Now().Add(time.Duration(offset) * 30 * time.Second)
	return material.Mint(seed, now)
}

func (f flowJSON) has(name string) bool {
	for _, n := range f.UI.Nodes {
		if n.Attributes.Name == name {
			return true
		}
	}
	return false
}

func (f flowJSON) csrf() string {
	for _, n := range f.UI.Nodes {
		if n.Attributes.Name != "csrf_token" {
			continue
		}
		if s, ok := n.Attributes.Value.(string); ok {
			return s
		}
	}
	return ""
}

func (lc *loginClient) abs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.IsAbs() {
		return raw
	}
	base, err := url.Parse(lc.base + "/")
	if err != nil {
		return raw
	}
	return base.ResolveReference(u).String()
}

func (lc *loginClient) loginChallenge(ctx context.Context, authURL string) (string, error) {
	nxt := authURL
	for i := 0; i < 12; i++ {
		if nxt == "" {
			break
		}
		u, err := url.Parse(nxt)
		if err != nil {
			return "", err
		}
		if u.Query().Get("login_challenge") != "" {
			return u.Query().Get("login_challenge"), nil
		}
		_, hdrs, _, loc, err := lc.call(ctx, nxt, nil)
		if err != nil {
			return "", err
		}
		_ = hdrs
		nxt = loc
	}
	return "", fmt.Errorf("human: no login_challenge")
}

func (lc *loginClient) loadFlow(ctx context.Context, rawURL string) (flowJSON, error) {
	status, _, payload, loc, err := lc.call(ctx, rawURL, nil)
	if err != nil {
		return flowJSON{}, err
	}
	var flow flowJSON
	if json.Unmarshal(payload, &flow) == nil && flow.UI.Action != "" {
		return flow, nil
	}
	if loc != "" {
		u, err := url.Parse(loc)
		if err != nil {
			return flowJSON{}, err
		}
		fid := u.Query().Get("flow")
		if fid != "" {
			base := strings.TrimRight(kratosBase(rawURL, loc), "/")
			return lc.loadFlow(ctx, base+"/self-service/login/flows?id="+url.QueryEscape(fid))
		}
		if status == http.StatusFound || status == http.StatusSeeOther || status == http.StatusTemporaryRedirect {
			return lc.loadFlow(ctx, loc)
		}
	}
	return flowJSON{}, fmt.Errorf("human: no login flow")
}

func kratosBase(raw, loc string) string {
	u, err := url.Parse(raw)
	if err != nil {
		u, _ = url.Parse(loc)
	}
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (lc *loginClient) submit(ctx context.Context, flow flowJSON, body map[string]string) (flowJSON, string, error) {
	if flow.UI.Action == "" {
		return flowJSON{}, "", fmt.Errorf("human: flow has no action")
	}
	if token := flow.csrf(); token != "" {
		body["csrf_token"] = token
	}
	status, _, payload, loc, err := lc.call(ctx, flow.UI.Action, body)
	if err != nil {
		return flowJSON{}, "", err
	}
	_ = status
	var next flowJSON
	if json.Unmarshal(payload, &next) == nil && next.has("totp_code") {
		return next, loc, nil
	}
	return next, loc, nil
}

func (lc *loginClient) followCallback(ctx context.Context, nxt, redirect string) (string, error) {
	for i := 0; i < 12; i++ {
		if nxt == "" {
			return "", fmt.Errorf("human: no redirect after totp")
		}
		if isCallback(nxt, redirect) {
			return nxt, nil
		}
		_, _, _, loc, err := lc.call(ctx, nxt, nil)
		if err != nil {
			return "", err
		}
		if loc == "" {
			return "", fmt.Errorf("human: no redirect after totp")
		}
		nxt = loc
	}
	return "", fmt.Errorf("human: no callback")
}

func isCallback(raw, redirect string) bool {
	if raw == "" || redirect == "" {
		return false
	}
	return strings.HasPrefix(raw, strings.TrimRight(redirect, "/"))
}

func (lc *loginClient) call(ctx context.Context, rawURL string, form map[string]string) (int, http.Header, []byte, string, error) {
	rawURL = lc.abs(rawURL)
	var body io.Reader
	method := http.MethodGet
	if form != nil {
		method = http.MethodPost
		raw, err := json.Marshal(form)
		if err != nil {
			return 0, nil, nil, "", err
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return 0, nil, nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := lc.hc.Do(req)
	if err != nil {
		return 0, nil, nil, "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, nil, nil, "", err
	}
	loc := res.Header.Get("Location")
	if loc == "" {
		var env struct {
			RedirectBrowserTo string `json:"redirect_browser_to"`
			Error             struct {
				RedirectBrowserTo string `json:"redirect_browser_to"`
			} `json:"error"`
			ContinueWith []struct {
				RedirectBrowserTo string `json:"redirect_browser_to"`
			} `json:"continue_with"`
		}
		if json.Unmarshal(raw, &env) == nil {
			loc = env.RedirectBrowserTo
			if loc == "" {
				loc = env.Error.RedirectBrowserTo
			}
			if loc == "" {
				for _, c := range env.ContinueWith {
					if c.RedirectBrowserTo != "" {
						loc = c.RedirectBrowserTo
						break
					}
				}
			}
		}
	}
	if loc != "" {
		base, err := url.Parse(rawURL)
		if err == nil {
			if ref, err := url.Parse(loc); err == nil {
				loc = base.ResolveReference(ref).String()
			}
		}
	}
	return res.StatusCode, res.Header, raw, loc, nil
}
