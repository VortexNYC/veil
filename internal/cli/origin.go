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
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/identity/glue"
	"github.com/VortexNYC/veil/internal/app"
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
	return "", fmt.Errorf("origin: --oidc-token-file, VEIL_OIDC_TOKEN_FILE, or VEIL_OIDC_TOKEN is required")
}

func originTokenLive(ctx context.Context, tokenFile string) (string, error) {
	tok, err := originToken(tokenFile)
	if err == nil && !jwtNeedsRefresh(tok) {
		return tok, nil
	}
	secretFile := strings.TrimSpace(os.Getenv("VEIL_HYDRA_SECRET_FILE"))
	if secretFile == "" {
		if err != nil {
			return "", err
		}
		return tok, nil
	}
	minted, err := originRemint(ctx, tokenFile, secretFile)
	if err != nil {
		return "", err
	}
	return minted, nil
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

func originRemint(ctx context.Context, tokenFile, secretFile string) (string, error) {
	cred, err := readHydraCred(secretFile)
	if err != nil {
		return "", err
	}
	if cred.Secret == "" {
		return "", fmt.Errorf("origin: empty VEIL_HYDRA_SECRET_FILE")
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
	// Mint under the audience the binding recorded — bare secret files
	// predate the rename and can only produce glue.LegacyAudience.
	audience := cred.Audience
	if audience == "" {
		audience = glue.LegacyAudience
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

func originItemAdd(cmd *cobra.Command, name, uri string, tags []string, kind protocol.ItemKind, token, totpSeed []byte, login string) error {
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

func originItemImport(cmd *cobra.Command, path string) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	q := url.QueryEscape(filepath.Base(path))
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

func originGrantAdd(cmd *cobra.Command, grantee, item, level string, expires time.Duration, asHuman bool) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	in := publicapi.CreateGrantRequest{Item: item, Level: level}
	if asHuman {
		in.Human = grantee
	} else {
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
