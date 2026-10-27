package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/identity/glue"
	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/health"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

const jwtRefreshSkew = 2 * time.Minute

func originBase() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("VEIL_ORIGIN")), "/")
}

func originToken(tokenFile string) (string, error) {
	if tokenFile != "" {
		b, err := readFileMaterial(tokenFile)
		if err != nil {
			return "", err
		}
		if len(b) == 0 {
			return "", fmt.Errorf("origin: empty token file")
		}
		return string(b), nil
	}
	if f := strings.TrimSpace(os.Getenv("VEIL_OIDC_TOKEN_FILE")); f != "" {
		b, err := readFileMaterial(f)
		if err != nil {
			return "", err
		}
		if len(b) == 0 {
			return "", fmt.Errorf("origin: empty VEIL_OIDC_TOKEN_FILE")
		}
		return string(b), nil
	}
	if t := strings.TrimSpace(os.Getenv("VEIL_OIDC_TOKEN")); t != "" {
		return t, nil
	}
	// @vercel/oidc 4.0.0 compatibility: check VERCEL_OIDC_TOKEN_FILE first
	// (strict: an unreadable or empty configured file errors rather than
	// falling back to VERCEL_OIDC_TOKEN), then VERCEL_OIDC_TOKEN.
	if f := strings.TrimSpace(os.Getenv("VERCEL_OIDC_TOKEN_FILE")); f != "" {
		b, err := readFileMaterial(f)
		if err != nil {
			return "", err
		}
		if len(b) == 0 {
			return "", fmt.Errorf("origin: empty VERCEL_OIDC_TOKEN_FILE")
		}
		return string(b), nil
	}
	if t := strings.TrimSpace(os.Getenv("VERCEL_OIDC_TOKEN")); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("origin: --oidc-token-file, VEIL_OIDC_TOKEN_FILE, or VEIL_OIDC_TOKEN is required")
}

func originTokenLive(ctx context.Context, tokenFile string) (string, error) {
	tok, err := originToken(tokenFile)
	if err == nil && !jwtNeedsRefresh(tok) {
		return tok, nil
	}
	cred, ok, credErr := originHydraCred()
	if !ok {
		if err != nil {
			return "", err
		}
		return tok, nil
	}
	if credErr != nil {
		return "", credErr
	}
	minted, err := originRemint(ctx, tokenFile, cred)
	if err != nil {
		return "", err
	}
	return minted, nil
}

// originHydraCred resolves the bootstrap secret for headless/sandbox runs.
// The file wins: VEIL_HYDRA_SECRET_FILE may carry issuer and audience
// metadata alongside the secret, VEIL_HYDRA_SECRET is the bare env-var
// fallback for sandboxes that cannot materialize files. Either source may
// hold a `ses_` session lease instead of a Hydra client secret — a session
// token is already the bearer and is used as-is. Stdin is deliberately not
// a source: `veil mcp stdio` owns stdin for JSON-RPC, and a lazily-read
// remint cannot pause the protocol stream.
func originHydraCred() (hydraCred, bool, error) {
	if f := strings.TrimSpace(os.Getenv("VEIL_HYDRA_SECRET_FILE")); f != "" {
		cred, err := readHydraCred(f)
		return cred, true, err
	}
	if s := strings.TrimSpace(os.Getenv("VEIL_HYDRA_SECRET")); s != "" {
		return hydraCred{Secret: s}, true, nil
	}
	return hydraCred{}, false, nil
}

func jwtNeedsRefresh(tok string) bool {
	if app.IsSessionToken(tok) {
		return false
	}
	exp, ok := jwtExpUnix(tok)
	if !ok {
		return false
	}
	return time.Now().Add(jwtRefreshSkew).Unix() >= exp
}

func jwtExpUnix(tok string) (int64, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[1] == "" {
		return 0, false
	}
	p := parts[1]
	if n := len(p) % 4; n != 0 {
		p += strings.Repeat("=", 4-n)
	}
	raw, err := base64.URLEncoding.DecodeString(p)
	if err != nil {
		return 0, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Exp == 0 {
		return 0, false
	}
	return claims.Exp, true
}

func originRemint(ctx context.Context, tokenFile string, cred hydraCred) (string, error) {
	if cred.Secret == "" {
		return "", fmt.Errorf("origin: empty VEIL_HYDRA_SECRET_FILE")
	}
	// A session lease is already the bearer — sandboxes hand it over via
	// VEIL_HYDRA_SECRET_FILE/VEIL_HYDRA_SECRET exactly like a client secret.
	// It cannot remint; it must not be sent to Hydra as a client_secret.
	if app.IsSessionToken(cred.Secret) {
		return cred.Secret, nil
	}
	issuer := strings.TrimSpace(os.Getenv("VEIL_HYDRA_ISSUER"))
	if issuer == "" {
		issuer = cred.Issuer
	}
	if issuer == "" {
		return "", fmt.Errorf("origin: VEIL_HYDRA_ISSUER is required to remint")
	}
	agentName := strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	if agentName == "" {
		if tok, err := originToken(tokenFile); err == nil {
			agentName = agentNameFromJWT(tok)
		}
	}
	if !id.Valid(agentName) {
		return "", fmt.Errorf("origin: VEIL_AGENT is required to remint")
	}
	// Mint under the audience the binding recorded; a bare secret file
	// carries none and mints under the default audience.
	audience := cred.Audience
	if audience == "" {
		audience = glue.DefaultClientID
	}
	if env := os.Getenv("VEIL_HYDRA_CLIENT_ID"); env != "" {
		audience = env
	}
	raw, err := glue.ClientCredentials(ctx, issuer, glue.AgentClientID(agentName), cred.Secret, audience)
	if err != nil {
		return "", err
	}
	out := tokenFile
	if out == "" {
		out = strings.TrimSpace(os.Getenv("VEIL_OIDC_TOKEN_FILE"))
	}
	if out != "" {
		if err := os.WriteFile(out, []byte(raw+"\n"), 0o600); err != nil {
			return "", err
		}
	}
	return raw, nil
}

func agentNameFromJWT(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return ""
	}
	p := parts[1]
	if n := len(p) % 4; n != 0 {
		p += strings.Repeat("=", 4-n)
	}
	raw, err := base64.URLEncoding.DecodeString(p)
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	sub := strings.TrimSpace(claims.Sub)
	if strings.HasPrefix(sub, glue.AgentClientPrefix) {
		return strings.TrimPrefix(sub, glue.AgentClientPrefix)
	}
	return ""
}

func originDo(ctx context.Context, method, path, token string, body []byte) ([]byte, error) {
	base := originBase()
	if base == "" {
		return nil, fmt.Errorf("origin: VEIL_ORIGIN is empty")
	}
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg != "" {
			return raw, fmt.Errorf("origin %s %s: http %d: %s", method, path, res.StatusCode, msg)
		}
		return raw, fmt.Errorf("origin %s %s: http %d", method, path, res.StatusCode)
	}
	return raw, nil
}

func originUse(cmd *cobra.Command, tokenFile, item, rawURL, method string, headers []string, bodyFile string) error {
	tok, err := originTokenLive(cmd.Context(), tokenFile)
	if err != nil {
		return err
	}
	in := publicapi.UseRequest{Item: item, URL: rawURL, Method: method, Headers: map[string]string{}}
	for _, h := range headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("use: --header is Name: value")
		}
		in.Headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if bodyFile != "" {
		b, err := os.ReadFile(bodyFile)
		if err != nil {
			return err
		}
		if utf8.Valid(b) {
			in.Body = string(b)
		} else {
			in.BodyB64 = base64.StdEncoding.EncodeToString(b)
		}
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/use", tok, payload)
	if err != nil {
		return err
	}
	var out useDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originEvents(cmd *cobra.Command, tokenFile string) error {
	tok, err := originTokenLive(cmd.Context(), tokenFile)
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/events", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.EventsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Events == nil {
		out.Events = []protocol.AuditEvent{}
	}
	return encode(cmd, out.Events)
}

// originAuditFeed pages the owner-gated org audit feed — the SIEM pull
// endpoint. Prints the raw page (events + next_after) so callers can loop
// on the cursor.
func originAuditFeed(cmd *cobra.Command, after int64, limit int) error {
	tok, err := originOwnerToken(cmd.Context())
	if err != nil {
		return err
	}
	q := url.Values{}
	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/v1/audit/events"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, path, tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.AuditFeedResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Events == nil {
		out.Events = []protocol.AuditFeedEvent{}
	}
	return encode(cmd, out)
}

func originItemList(cmd *cobra.Command) error {
	tok, err := originOwnerToken(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/items", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.ItemsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Items == nil {
		out.Items = []protocol.Item{}
	}
	return encode(cmd, out.Items)
}

func originItemAdd(cmd *cobra.Command, name, uri string, tags []string, kind protocol.ItemKind, token, totpSeed []byte, login string, wif *publicapi.WIFItemFields) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	in := publicapi.CreateItemRequest{
		Name:     name,
		URI:      uri,
		Tags:     tags,
		Kind:     string(kind),
		Secret:   string(token),
		TOTPSeed: string(totpSeed),
		Login:    login,
		WIF:      wif,
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/items", tok, payload)
	if err != nil {
		return err
	}
	var item protocol.Item
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	return encode(cmd, item)
}

func originItemImportBytes(cmd *cobra.Command, name string, raw []byte) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	q := url.QueryEscape(name)
	body, err := originDoFile(cmd.Context(), "/v1/import?filename="+q, tok, raw)
	if err != nil {
		return err
	}
	var got publicapi.ImportResponse
	if err := json.Unmarshal(body, &got); err != nil {
		return err
	}
	return encode(cmd, got)
}

func originDoFile(ctx context.Context, path, token string, body []byte) ([]byte, error) {
	base := originBase()
	if base == "" {
		return nil, fmt.Errorf("origin: VEIL_ORIGIN is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return raw, fmt.Errorf("origin POST %s: http %d", path, res.StatusCode)
	}
	return raw, nil
}

func originItemUpdate(cmd *cobra.Command, name string, addURIs, tags []string, login string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	patches := addURIs
	if len(patches) == 0 {
		patches = []string{""}
	}
	var last []byte
	for i, u := range patches {
		in := publicapi.UpdateItemRequest{URI: u}
		if i == 0 {
			in.Tags = tags
			in.Login = login
		}
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		last, err = originDo(cmd.Context(), http.MethodPatch, "/v1/items/"+name, tok, payload)
		if err != nil {
			return err
		}
	}
	var item protocol.Item
	if err := json.Unmarshal(last, &item); err != nil {
		return err
	}
	return encode(cmd, item)
}

func originItemArchive(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/items/"+name+"/archive", tok, nil)
	if err != nil {
		return err
	}
	var out map[string]bool
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originItemDelete(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodDelete, "/v1/items/"+name, tok, nil)
	if err != nil {
		return err
	}
	var out map[string]bool
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originGrantAdd(cmd *cobra.Command, grantee, item, level string, expires time.Duration, asHuman bool, group string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	in := publicapi.CreateGrantRequest{Item: item, Level: level}
	switch {
	case group != "":
		in.Group = group
	case asHuman:
		in.Human = grantee
	default:
		in.Agent = grantee
	}
	if expires > 0 {
		in.Expires = expires.String()
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/grants", tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.GrantView
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originGroupAdd(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.CreateGroupRequest{Name: name})
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/groups", tok, payload)
	if err != nil {
		return err
	}
	var out protocol.Group
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originGroupList(cmd *cobra.Command) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/groups", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.GroupsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out.Groups)
}

func originGroupMemberList(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/groups/"+url.PathEscape(name)+"/members", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.GroupMembersResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out.Members)
}

func originGroupMemberAdd(cmd *cobra.Command, name string, kind protocol.PrincipalKind, memberID string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.AddGroupMemberRequest{MemberKind: string(kind), MemberID: memberID})
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/groups/"+url.PathEscape(name)+"/members", tok, payload)
	if err != nil {
		return err
	}
	var out map[string]bool
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originGroupMemberRemove(cmd *cobra.Command, name string, kind protocol.PrincipalKind, memberID string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	path := "/v1/groups/" + url.PathEscape(name) + "/members/" + url.PathEscape(string(kind)) + "/" + url.PathEscape(memberID)
	raw, err := originDo(cmd.Context(), http.MethodDelete, path, tok, nil)
	if err != nil {
		return err
	}
	var out map[string]bool
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originGrantList(cmd *cobra.Command) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/grants", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.GrantsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Grants == nil {
		out.Grants = []publicapi.GrantView{}
	}
	return encode(cmd, out.Grants)
}

func originSessionCreate(cmd *cobra.Command, agent string, ttl time.Duration, maxUses int, outFile string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	in := publicapi.CreateSessionRequest{Agent: agent, MaxUses: maxUses}
	if ttl > 0 {
		in.TTL = ttl.String()
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/sessions", tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.CreateSessionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if strings.TrimSpace(out.Token) == "" {
		return fmt.Errorf("origin session: empty token")
	}
	if err := os.WriteFile(outFile, []byte(out.Token+"\n"), 0o600); err != nil {
		return err
	}
	return encode(cmd, sessionCreateDTO{
		ID:        out.ID,
		AgentID:   out.AgentID,
		ExpiresAt: out.ExpiresAt,
		OutFile:   outFile,
	})
}

func originSessionList(cmd *cobra.Command) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/sessions", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.SessionsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Sessions == nil {
		out.Sessions = []protocol.Session{}
	}
	return encode(cmd, out.Sessions)
}

func originRequestList(cmd *cobra.Command, status string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	path := "/v1/requests"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, path, tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.RequestsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Requests == nil {
		out.Requests = []publicapi.RequestView{}
	}
	return encode(cmd, out.Requests)
}

func originRequestResolve(cmd *cobra.Command, id, verb string, ttl time.Duration) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	var payload []byte
	if verb == "approve" {
		payload, err = json.Marshal(map[string]string{"ttl": ttl.String()})
		if err != nil {
			return err
		}
	}
	path := "/v1/requests/" + url.PathEscape(id) + "/" + verb
	raw, err := originDo(cmd.Context(), http.MethodPost, path, tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.RequestView
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originAgentAdd(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.CreateAgentRequest{Name: name})
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/agents", tok, payload)
	if err != nil {
		return err
	}
	var agent protocol.Principal
	if err := json.Unmarshal(raw, &agent); err != nil {
		return err
	}
	return encode(cmd, agent)
}

func originAgentList(cmd *cobra.Command) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/agents", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.AgentsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Agents == nil {
		out.Agents = []protocol.Principal{}
	}
	return encode(cmd, out.Agents)
}

func originAgentRevoke(cmd *cobra.Command, id string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	path := "/v1/agents/" + id + "/revoke"
	raw, err := originDo(cmd.Context(), http.MethodPost, path, tok, nil)
	if err != nil {
		return err
	}
	var agent protocol.Principal
	if err := json.Unmarshal(raw, &agent); err != nil {
		return err
	}
	return encode(cmd, agent)
}

// originReport fetches the vault health report from origin. Human mint; the
// response is metadata only.
func originReport(cmd *cobra.Command, hibp bool) error {
	tok, err := originOwnerToken(cmd.Context())
	if err != nil {
		return err
	}
	path := "/v1/report"
	if hibp {
		path += "?hibp=1"
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, path, tok, nil)
	if err != nil {
		return err
	}
	var rep health.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return err
	}
	return encode(cmd, rep)
}

// originRotateItem POSTs the rotate route — the origin owns the secret and
// the provider call; the CLI never sees either key.
func originRotateItem(cmd *cobra.Command, name, mgmtItem string, interval time.Duration, replacement []byte) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	var payload []byte
	if mgmtItem != "" || interval > 0 || len(replacement) > 0 {
		in := publicapi.RotateItemRequest{
			ManagementItem: mgmtItem,
			Secret:         string(replacement),
		}
		if interval > 0 {
			in.Interval = interval.String()
		}
		payload, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/items/"+name+"/rotate", tok, payload)
	if err != nil {
		return err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

// originItemHookMutate GETs the hook list, applies fn, and PUTs the result —
// add/remove converge on the same read-modify-write against the sealed list.
func originItemHookMutate(cmd *cobra.Command, name string, fn func([]protocol.RotateHook) ([]protocol.RotateHook, error)) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	base := "/v1/items/" + url.PathEscape(name) + "/rotate-hooks"
	raw, err := originDo(cmd.Context(), http.MethodGet, base, tok, nil)
	if err != nil {
		return err
	}
	var got publicapi.RotateHooksResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		return err
	}
	next, err := fn(got.Hooks)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.RotateHooksRequest{Hooks: next})
	if err != nil {
		return err
	}
	raw, err = originDo(cmd.Context(), http.MethodPut, base, tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.RotateHooksResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originItemListHooks(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/items/"+url.PathEscape(name)+"/rotate-hooks", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.RotateHooksResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out.Hooks)
}

func originItemFireHooks(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/items/"+url.PathEscape(name)+"/rotate-hooks/fire", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.RotateHooksFireResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originRotateProviders(cmd *cobra.Command) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/rotate/providers", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.RotateProvidersResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originItemVerify(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/items/"+url.PathEscape(name)+"/verify", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.VerifyItemResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originAgentBind(cmd *cobra.Command, name, issuer, subject, audience string, claims map[string][]string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.BindWorkloadRequest{
		Issuer:   issuer,
		Subject:  subject,
		Audience: audience,
		Claims:   claims,
	})
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/agents/"+url.PathEscape(name)+"/bind", tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.WorkloadView
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, protocol.Workload{
		AgentID:  out.AgentID,
		Issuer:   out.Issuer,
		Subject:  out.Subject,
		Audience: out.Audience,
		Claims:   out.Claims,
	})
}

func originVerifyIssuer(cmd *cobra.Command, issuer string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.VerifyIssuerRequest{Issuer: issuer})
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/workloads/verify-issuer", tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.VerifyIssuerResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out)
}

func originAgentWorkloads(cmd *cobra.Command, name string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/agents/"+url.PathEscape(name)+"/workloads", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.WorkloadsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	return encode(cmd, out.Workloads)
}

func originAgentUnbind(cmd *cobra.Command, name, issuer, subject string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	q := "/v1/agents/" + url.PathEscape(name) + "/workloads?issuer=" + url.QueryEscape(issuer) + "&subject=" + url.QueryEscape(subject)
	if _, err := originDo(cmd.Context(), http.MethodDelete, q, tok, nil); err != nil {
		return err
	}
	return encode(cmd, map[string]bool{"unbound": true})
}

func originAgentHydra(cmd *cobra.Command, name, issuer, clientID, audience, secretFile string, existing hydraCred, verified bool) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	if verified {
		payload, err := json.Marshal(publicapi.BindWorkloadRequest{
			Issuer:   issuer,
			Subject:  clientID,
			Audience: audience,
		})
		if err != nil {
			return err
		}
		raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/agents/"+url.PathEscape(name)+"/bind", tok, payload)
		if err != nil {
			return err
		}
		var out publicapi.WorkloadView
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		existing.Audience = out.Audience
		existing.Issuer = out.Issuer
		if err := writeHydraCred(secretFile, existing); err != nil {
			return err
		}
		return encode(cmd, hydraAgentDTO{
			AgentID:  name,
			ClientID: clientID,
			Issuer:   out.Issuer,
			Audience: out.Audience,
		})
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/agents/"+url.PathEscape(name)+"/hydra", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.HydraAgentResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Secret != "" {
		existing.Secret = out.Secret
	}
	if existing.Secret == "" {
		return fmt.Errorf("agent hydra: no client secret to record")
	}
	existing.Audience = out.Audience
	existing.Issuer = out.Issuer
	if err := writeHydraCred(secretFile, existing); err != nil {
		return err
	}
	return encode(cmd, hydraAgentDTO{
		AgentID:  name,
		ClientID: out.ClientID,
		Issuer:   out.Issuer,
		Audience: out.Audience,
	})
}
