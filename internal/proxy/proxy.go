// Package proxy is the Infisical/OneCLI inject path: HTTP(S)_PROXY with
// goproxy MITM. CONNECT is goproxy. Credentials attach at the edge.
// Unknown hosts fail closed. The agent never receives a secret.
//
// CA wiring follows the official customca example:
// https://github.com/elazarl/goproxy/blob/master/examples/customca/main.go
package proxy

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/elazarl/goproxy/ext/auth"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/broker"
	"github.com/VortexNYC/veil/internal/grant"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

type Server struct {
	App         *app.App
	Agent       protocol.Principal
	Token       string
	Dir         string
	CA          tls.Certificate
	CAPEM       []byte
	ListenAddr  string
	OutboundTLS *tls.Config
	Now         func() time.Time
	OriginItems []protocol.Item
	OriginUse   OriginUse
	httpProxy   *goproxy.ProxyHttpServer
	srv         *http.Server
	ln          net.Listener
}

type ctxData struct {
	authed  bool
	secrets [][]byte
}

func New(a *app.App, agentID, dir string) (*Server, error) {
	agent, err := a.Store.Agent(agentID)
	if err != nil {
		return nil, fmt.Errorf("proxy: unknown agent %q: %w", agentID, err)
	}
	ca, pemBytes, err := LoadOrCreateCA(dir)
	if err != nil {
		return nil, err
	}
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	s := &Server{
		App:        a,
		Agent:      agent,
		Token:      tok,
		Dir:        dir,
		CA:         ca,
		CAPEM:      pemBytes,
		ListenAddr: "127.0.0.1:0",
		Now:        time.Now,
	}
	return s, nil
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) authorized(user, pass string) bool {
	return subtle.ConstantTimeCompare([]byte(user), []byte(s.Agent.ID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(s.Token)) == 1
}

func (s *Server) build() *goproxy.ProxyHttpServer {
	px := goproxy.NewProxyHttpServer()
	px.Verbose = false
	if s.OutboundTLS != nil {
		if px.Tr == nil {
			px.Tr = &http.Transport{}
		}
		px.Tr.TLSClientConfig = s.OutboundTLS
	}

	mitm := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&s.CA),
	}
	px.OnRequest().HandleConnect(goproxy.FuncHttpsHandler(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if !s.checkAuth(ctx.Req) {
			ctx.Resp = auth.BasicUnauthorized(ctx.Req, "veil")
			return goproxy.RejectConnect, host
		}
		ctx.UserData = ctxData{authed: true}
		if !s.agentMayHost("https://" + host) {
			ctx.Resp = jsonResp(ctx.Req, http.StatusForbidden, protocol.UseResult{
				Decision: protocol.DecisionDeny,
				Reason:   "host_not_allowed",
			})
			return goproxy.RejectConnect, host
		}
		return mitm, host
	}))

	px.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if req.Method == http.MethodConnect {
			return req, nil
		}
		data, _ := ctx.UserData.(ctxData)
		// HTTPS MITM: Proxy-Authorization was on CONNECT only.
		if !data.authed && req.TLS == nil && !s.checkAuth(req) {
			return nil, auth.BasicUnauthorized(req, "veil")
		}
		return s.inject(req, ctx)
	})
	px.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil {
			return resp
		}
		data, _ := ctx.UserData.(ctxData)
		if len(data.secrets) == 0 {
			return resp
		}
		return scrubResp(resp, data.secrets)
	})
	return px
}

func (s *Server) checkAuth(req *http.Request) bool {
	if req == nil {
		return false
	}
	ok := false
	handler := auth.Basic("veil", func(user, pass string) bool {
		ok = s.authorized(user, pass)
		return ok
	})
	_, resp := handler.Handle(req, nil)
	return resp == nil && ok
}

func (s *Server) currentAgent() (protocol.Principal, error) {
	if s.App == nil {
		return s.Agent, nil
	}
	return s.App.Store.Agent(s.Agent.ID)
}

func (s *Server) agentMayHost(raw string) bool {
	if s.OriginUse != nil {
		_, dec := s.originItem(raw)
		return dec.Decision == protocol.DecisionAllow
	}
	if s.App == nil {
		return false
	}
	items, err := s.App.ItemsForAgent(s.Agent.ID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if grant.HostAllowed(item, raw) {
			return true
		}
	}
	return false
}

func (s *Server) inject(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if s.OriginUse != nil {
		return s.injectOrigin(req, ctx)
	}
	raw := destURL(req)
	item, g, dec, err := s.lookup(raw)
	if err != nil {
		return nil, jsonResp(req, http.StatusInternalServerError, protocol.UseResult{
			Decision: protocol.DecisionDeny,
			Reason:   "lookup_failed",
		})
	}
	event := protocol.AuditEvent{
		Time:       s.now(),
		OrgID:      s.Agent.OrgID,
		AgentID:    s.Agent.ID,
		ItemID:     item.ID,
		Action:     protocol.ActionFetch,
		Decision:   dec.Decision,
		Reason:     dec.Reason,
		ApprovalID: dec.ApprovalID,
	}
	if err := s.App.Store.AppendAudit(event); err != nil {
		broker.LogEvent(event, item.Name, destURL(req), 0)
		return nil, jsonResp(req, http.StatusInternalServerError, protocol.UseResult{
			Decision: protocol.DecisionDeny,
			Reason:   "audit_unavailable",
		})
	}
	broker.LogEvent(event, item.Name, destURL(req), 0)
	if dec.Decision != protocol.DecisionAllow {
		if dec.Decision == protocol.DecisionNeedApproval && g != nil {
			if filed, err := s.App.Broker.FileRequest(req.Context(), s.Agent, item, g, protocol.ActionFetch); err == nil {
				exp := filed.ExpiresAt
				dec.RequestID, dec.RequestExpiresAt = filed.ID, &exp
			}
		}
		status := http.StatusForbidden
		return nil, jsonResp(req, status, protocol.UseResult{
			Decision: dec.Decision, Reason: dec.Reason,
			RequestID: dec.RequestID, RequestExpiresAt: dec.RequestExpiresAt,
		})
	}
	secret, err := s.App.Store.Secret(item.ID)
	if err != nil {
		return nil, jsonResp(req, http.StatusInternalServerError, protocol.UseResult{
			Decision: protocol.DecisionDeny,
			Reason:   "lookup_failed",
		})
	}
	env := material.Unpack(secret)
	access, err := material.AccessToken(req.Context(), env, http.DefaultClient)
	if err != nil {
		return nil, jsonResp(req, http.StatusInternalServerError, protocol.UseResult{
			Decision: protocol.DecisionDeny,
			Reason:   "oauth_failed",
		})
	}
	if access != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", material.AuthorizationValue(access))
	}
	code, err := material.Apply(req.Header, env, s.now())
	if err != nil {
		return nil, jsonResp(req, http.StatusInternalServerError, protocol.UseResult{
			Decision: protocol.DecisionDeny,
			Reason:   "totp_failed",
		})
	}
	data, _ := ctx.UserData.(ctxData)
	data.authed = true
	data.secrets = append(s.agentSecrets(), material.ScrubList(env, secret, []byte(code), []byte(access))...)
	ctx.UserData = data
	return req, nil
}

func destURL(req *http.Request) string {
	if req.URL != nil && req.URL.Host != "" {
		return req.URL.String()
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + req.Host + req.URL.RequestURI()
}

func (s *Server) lookup(rawURL string) (protocol.Item, *protocol.Grant, protocol.UseResult, error) {
	agent, err := s.currentAgent()
	if err != nil {
		return protocol.Item{}, nil, protocol.UseResult{}, err
	}
	grants, err := s.App.Store.ListGrants()
	if err != nil {
		return protocol.Item{}, nil, protocol.UseResult{}, err
	}
	var hitItem protocol.Item
	var hitGrant *protocol.Grant
	n := 0
	for i := range grants {
		g := grants[i]
		if g.AgentID != s.Agent.ID {
			continue
		}
		item, err := s.App.Store.Item(g.ItemID)
		if err != nil {
			return protocol.Item{}, nil, protocol.UseResult{}, err
		}
		if !grant.HostAllowed(item, rawURL) {
			continue
		}
		n++
		hitItem = item
		cp := g
		hitGrant = &cp
	}
	if n == 0 {
		return protocol.Item{}, nil, protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "host_not_allowed"}, nil
	}
	if n > 1 {
		return protocol.Item{}, nil, protocol.UseResult{Decision: protocol.DecisionDeny, Reason: "ambiguous_item"}, nil
	}
	appr, err := s.App.Store.LiveApproval(hitGrant.ID, s.now())
	if err != nil {
		return protocol.Item{}, nil, protocol.UseResult{}, err
	}
	dec := grant.Evaluate(grant.Input{
		Principal: agent,
		Item:      hitItem,
		Grant:     hitGrant,
		Action:    protocol.ActionFetch,
		TargetURL: rawURL,
		Approval:  appr,
		Now:       s.now(),
	})
	return hitItem, hitGrant, dec, nil
}

func (s *Server) agentSecrets() [][]byte {
	items, err := s.App.ItemsForAgent(s.Agent.ID)
	if err != nil {
		return nil
	}
	var out [][]byte
	for _, item := range items {
		sec, err := s.App.Store.Secret(item.ID)
		if err != nil {
			continue
		}
		env := material.Unpack(sec)
		out = append(out, material.ScrubList(env, sec)...)
	}
	return out
}

func (s *Server) Start() error {
	s.httpProxy = s.build()
	addr := s.ListenAddr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.srv = &http.Server{
		Handler:           s.httpProxy,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

func (s *Server) Close() error {
	if s.srv != nil {
		_ = s.srv.Close()
	}
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) ProxyURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		User:   url.UserPassword(s.Agent.ID, s.Token),
		Host:   s.Addr(),
	}
}

func (s *Server) Env() []string {
	u := s.ProxyURL().String()
	ca := CACertPath(s.Dir)
	return []string{
		"HTTP_PROXY=" + u,
		"HTTPS_PROXY=" + u,
		"http_proxy=" + u,
		"https_proxy=" + u,
		"SSL_CERT_FILE=" + ca,
		"NODE_EXTRA_CA_CERTS=" + ca,
		"REQUESTS_CA_BUNDLE=" + ca,
		"CURL_CA_BUNDLE=" + ca,
		"GIT_SSL_CAINFO=" + ca,
	}
}

func jsonResp(req *http.Request, status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{
		StatusCode:    status,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
		Request:       req,
	}
}

func scrubResp(resp *http.Response, secrets [][]byte) *http.Response {
	if resp.Body == nil {
		return resp
	}
	var r io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err == nil {
			r = gz
			resp.Header.Del("Content-Encoding")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		return resp
	}
	raw = scrub.Bytes(raw, secrets...)
	for k, vs := range resp.Header {
		cleaned := make([]string, len(vs))
		for i, v := range vs {
			cleaned[i] = string(scrub.Bytes([]byte(v), secrets...))
		}
		resp.Header[k] = cleaned
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	resp.ContentLength = int64(len(raw))
	resp.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	return resp
}
