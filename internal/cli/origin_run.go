package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/proxy"
	"github.com/VortexNYC/veil/internal/publicapi"
)

func originRun(cmd *cobra.Command, home, agent, tokenFile string, injects, args []string) error {
	if len(injects) > 0 {
		return fmt.Errorf("run: --inject is local vault; origin injects via HTTPS_PROXY")
	}
	s, pairs, err := startOriginProxy(cmd.Context(), home, agent, tokenFile, "")
	if err != nil {
		return err
	}
	defer s.Close()
	proc := exec.CommandContext(cmd.Context(), args[0], args[1:]...)
	proc.Stdin = cmd.InOrStdin()
	proc.Stdout = cmd.OutOrStdout()
	proc.Stderr = cmd.ErrOrStderr()
	proc.Env = append(os.Environ(), s.Env()...)
	proc.Env = append(proc.Env, pairs...)
	return proc.Run()
}

func originProxyWait(cmd *cobra.Command, home, agent, tokenFile, listen string) error {
	s, pairs, err := startOriginProxy(cmd.Context(), home, agent, tokenFile, listen)
	if err != nil {
		return err
	}
	defer s.Close()
	for _, e := range s.Env() {
		fmt.Fprintln(cmd.OutOrStdout(), e)
	}
	for _, e := range pairs {
		fmt.Fprintln(cmd.OutOrStdout(), e)
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

func startOriginProxy(ctx context.Context, home, agent, tokenFile, listen string) (*proxy.Server, []string, error) {
	tok, err := originTokenLive(ctx, tokenFile)
	if err != nil {
		return nil, nil, err
	}
	items, err := originAgentItems(ctx, tok)
	if err != nil {
		return nil, nil, err
	}
	dir, err := resolveHome(home)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	s, err := proxy.NewOrigin(agent, dir, items, func(ctx context.Context, item, method, rawURL string, header http.Header, body []byte) (proxy.OriginResult, error) {
		return originUseCall(ctx, tok, item, method, rawURL, header, body)
	})
	if err != nil {
		return nil, nil, err
	}
	if listen != "" {
		s.ListenAddr = listen
	}
	if err := s.Start(); err != nil {
		return nil, nil, err
	}
	return s, proxy.DummyEnv(items), nil
}

func originAgentItems(ctx context.Context, tok string) ([]protocol.Item, error) {
	raw, err := originDo(ctx, http.MethodGet, "/v1/items", tok, nil)
	if err != nil {
		return nil, err
	}
	var out publicapi.ItemsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []protocol.Item{}
	}
	return out.Items, nil
}

func originUseCall(ctx context.Context, tok, item, method, rawURL string, header http.Header, body []byte) (proxy.OriginResult, error) {
	in := publicapi.UseRequest{
		Item:    item,
		URL:     rawURL,
		Method:  method,
		Headers: map[string]string{},
	}
	if utf8.Valid(body) {
		in.Body = string(body)
	} else {
		in.BodyB64 = base64.StdEncoding.EncodeToString(body)
	}
	for k, vs := range header {
		if len(vs) > 0 {
			in.Headers[k] = vs[0]
		}
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return proxy.OriginResult{}, err
	}
	raw, err := originDo(ctx, http.MethodPost, "/v1/use", tok, payload)
	if err != nil {
		return proxy.OriginResult{}, err
	}
	var out publicapi.UseResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return proxy.OriginResult{}, err
	}
	resBody := []byte(out.Body)
	if out.BodyB64 != "" {
		resBody, err = base64.StdEncoding.DecodeString(out.BodyB64)
		if err != nil {
			return proxy.OriginResult{}, fmt.Errorf("use: bad body_b64: %w", err)
		}
	}
	return proxy.OriginResult{
		Decision: out.Decision,
		Reason:   out.Reason,
		Status:   out.Status,
		Header:   out.Headers,
		Body:     resBody,
	}, nil
}
