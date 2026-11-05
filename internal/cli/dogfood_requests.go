package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

var errRequestWatchApproved = errors.New("request watch: approved")

// originResolveItemRef maps a vault item name or id to the canonical item id.
// Origin grant APIs require id.Valid item ids; humans think in names.
func originResolveItemRef(ctx context.Context, tok, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("item is required")
	}
	if id.Valid(ref) {
		return ref, nil
	}
	raw, err := originDo(ctx, http.MethodGet, "/v1/items", tok, nil)
	if err != nil {
		return "", err
	}
	var out publicapi.ItemsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	for _, it := range out.Items {
		if it.ID == ref || strings.EqualFold(it.Name, ref) {
			return it.ID, nil
		}
	}
	return "", fmt.Errorf("unknown item %q", ref)
}

func resolveLocalItemRef(store interface {
	ListItems() ([]protocol.Item, error)
}, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if id.Valid(ref) {
		return ref, nil
	}
	items, err := store.ListItems()
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if it.ID == ref || strings.EqualFold(it.Name, ref) {
			return it.ID, nil
		}
	}
	return "", fmt.Errorf("unknown item %q", ref)
}

func originAgentFillRequest(cmd *cobra.Command, agentName, itemRef, pageURL string, waitApproved bool, approvalTimeout time.Duration) error {
	out, err := originAgentFillRequestJSON(cmd, agentName, itemRef, pageURL)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Approve: %s\nHuman CLI: veil request confirm %s\n             veil request approve %s\n             veil request watch %s\n",
		out.ApprovalURL, out.RequestID, out.RequestID, out.RequestID,
	)
	if waitApproved {
		if err := originAgentFillWaitApproved(cmd, agentName, out.RequestID, approvalTimeout); err != nil {
			return err
		}
		return encode(cmd, map[string]any{
			"request_id": out.RequestID,
			"status":     protocol.RequestApproved,
			"item_id":    out.ItemID,
			"expires_at": out.ExpiresAt,
		})
	}
	return encode(cmd, out)
}

func originAgentFillRequestJSON(cmd *cobra.Command, agentName, itemRef, pageURL string) (publicapi.AgentFillRequestOut, error) {
	tok, err := originAgentCLI(cmd.Context(), agentName)
	if err != nil {
		return publicapi.AgentFillRequestOut{}, err
	}
	itemRef = strings.TrimSpace(itemRef)
	pageURL = strings.TrimSpace(pageURL)
	if itemRef == "" || pageURL == "" {
		return publicapi.AgentFillRequestOut{}, fmt.Errorf("agent fill request: --item and --url are required")
	}
	itemID, err := originResolveItemRef(cmd.Context(), tok, itemRef)
	if err != nil {
		return publicapi.AgentFillRequestOut{}, err
	}
	payload, err := json.Marshal(publicapi.AgentFillRequestIn{
		ItemID: itemID,
		URL:    pageURL,
	})
	if err != nil {
		return publicapi.AgentFillRequestOut{}, err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/agent/fill/request", tok, payload)
	if err != nil {
		return publicapi.AgentFillRequestOut{}, err
	}
	var out publicapi.AgentFillRequestOut
	if err := json.Unmarshal(raw, &out); err != nil {
		return publicapi.AgentFillRequestOut{}, err
	}
	return out, nil
}

func originAgentFillWaitApproved(cmd *cobra.Command, agentName, requestID string, timeout time.Duration) error {
	tok, err := originAgentCLI(cmd.Context(), agentName)
	if err != nil {
		return err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("agent fill wait: request id is required")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		raw, err := originDo(ctx, http.MethodGet, "/v1/agent/fill/request/"+url.PathEscape(requestID), tok, nil)
		if err != nil {
			return err
		}
		var out publicapi.AgentFillRequestOut
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		switch out.Status {
		case protocol.RequestApproved:
			return nil
		case protocol.RequestDenied, protocol.RequestExpired, protocol.RequestCancelled:
			return fmt.Errorf("agent fill request %s: %s", requestID, out.Status)
		case protocol.RequestOpen:
		default:
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("agent fill wait: timeout waiting for %s", requestID)
		case <-tick.C:
		}
	}
}

func originGrantResolve(cmd *cobra.Command, agentName, itemRef string, requireGrant bool) error {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	}
	if agentName == "" {
		return fmt.Errorf("grant resolve: set --agent or VEIL_AGENT")
	}
	id, err := originGrantResolveItemID(cmd, agentName, itemRef, requireGrant)
	if err != nil {
		return err
	}
	return encode(cmd, map[string]string{"item_id": id, "item": itemRef})
}

func originAgentFillRequireItemLogin(cmd *cobra.Command, agentName, itemRef string) error {
	tok, err := originAgentCLI(cmd.Context(), agentName)
	if err != nil {
		return err
	}
	itemRef = strings.TrimSpace(itemRef)
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/items", tok, nil)
	if err != nil {
		return err
	}
	var out publicapi.ItemsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, it := range out.Items {
		if it.ID == itemRef || strings.EqualFold(it.Name, itemRef) {
			if strings.TrimSpace(it.Login) == "" {
				return fmt.Errorf(
					"agent fill: item %q has no username/email in the vault — edit it at app.veil.nyc (GitHub sign-in needs your username or email, not password alone)",
					itemRef,
				)
			}
			return nil
		}
	}
	return fmt.Errorf("agent fill: unknown item %q", itemRef)
}

func originGrantResolveItemID(cmd *cobra.Command, agentName, itemRef string, requireGrant bool) (string, error) {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return "", err
	}
	itemRef = strings.TrimSpace(itemRef)
	if itemRef == "" {
		return "", fmt.Errorf("grant resolve: --item is required")
	}
	if id.Valid(itemRef) {
		if requireGrant {
			if err := originAgentHasGrant(cmd.Context(), tok, agentName, itemRef); err != nil {
				return "", err
			}
		}
		return itemRef, nil
	}
	rawItems, err := originDo(cmd.Context(), http.MethodGet, "/v1/items", tok, nil)
	if err != nil {
		return "", err
	}
	var itemsOut publicapi.ItemsResponse
	if err := json.Unmarshal(rawItems, &itemsOut); err != nil {
		return "", err
	}
	rawGrants, err := originDo(cmd.Context(), http.MethodGet, "/v1/grants", tok, nil)
	if err != nil {
		return "", err
	}
	var grantsOut publicapi.GrantsResponse
	if err := json.Unmarshal(rawGrants, &grantsOut); err != nil {
		return "", err
	}
	granted := map[string]struct{}{}
	for _, g := range grantsOut.Grants {
		if g.AgentID != agentName || g.Level == string(protocol.LevelDeny) {
			continue
		}
		granted[g.ItemID] = struct{}{}
	}
	var candidates []protocol.Item
	for _, it := range itemsOut.Items {
		if strings.EqualFold(it.Name, itemRef) {
			candidates = append(candidates, it)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("unknown item %q", itemRef)
	}
	for _, it := range candidates {
		if _, ok := granted[it.ID]; ok {
			return it.ID, nil
		}
	}
	if requireGrant {
		return "", fmt.Errorf("item %q is not granted to agent %q", itemRef, agentName)
	}
	return candidates[0].ID, nil
}

func originAgentHasGrant(ctx context.Context, humanTok, agentName, itemID string) error {
	raw, err := originDo(ctx, http.MethodGet, "/v1/grants", humanTok, nil)
	if err != nil {
		return err
	}
	var out publicapi.GrantsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, g := range out.Grants {
		if g.AgentID == agentName && g.ItemID == itemID && g.Level != string(protocol.LevelDeny) {
			return nil
		}
	}
	return fmt.Errorf("item %q is not granted to agent %q", itemID, agentName)
}

func originAgentCLI(ctx context.Context, agentName string) (string, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = strings.TrimSpace(os.Getenv("VEIL_AGENT"))
	}
	if agentName == "" {
		return "", fmt.Errorf("agent: set --agent or VEIL_AGENT")
	}
	if f := strings.TrimSpace(os.Getenv("VEIL_AGENT_TOKEN_FILE")); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	path := strings.TrimSpace(os.Getenv("VEIL_AGENT_JWT_FILE"))
	if path == "" {
		path = defaultAgentJWTPath(agentName)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("agent token: read %s: %w", path, err)
	}
	return strings.TrimSpace(string(b)), nil
}

// originAgentEnsureFresh remints the agent JWT when origin rejects the on-disk bearer.
func originAgentEnsureFresh(cmd *cobra.Command, agentName string) error {
	tok, err := originAgentCLI(cmd.Context(), agentName)
	if err != nil {
		return err
	}
	if _, err := originDo(cmd.Context(), http.MethodGet, "/v1/items", tok, nil); err == nil {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	hydra := filepath.Join(home, ".config/vortex/veil", agentName+".hydra")
	out := defaultAgentJWTPath(agentName)
	if _, err := os.Stat(hydra); err != nil {
		return fmt.Errorf("agent bearer rejected by origin — remint: veil agent token %s --secret-file %s --out-file %s", agentName, hydra, out)
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.CommandContext(cmd.Context(), bin, "agent", "token", agentName, "--secret-file", hydra, "--out-file", out)
	c.Env = os.Environ()
	c.Stderr = cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return fmt.Errorf("agent remint: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "reminted agent JWT for %q\n", agentName)
	return nil
}

func defaultAgentJWTPath(agentName string) string {
	if dir := strings.TrimSpace(os.Getenv("VEIL_CONFIG_DIR")); dir != "" {
		return dir + "/" + agentName + ".jwt"
	}
	home, _ := os.UserHomeDir()
	return home + "/.config/vortex/veil/" + agentName + ".jwt"
}

func originRequestFind(ctx context.Context, tok, requestID string) (publicapi.RequestView, protocol.RequestStatus, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return publicapi.RequestView{}, "", fmt.Errorf("request id is required")
	}
	for _, st := range []protocol.RequestStatus{
		protocol.RequestOpen,
		protocol.RequestApproved,
		protocol.RequestDenied,
		protocol.RequestExpired,
		protocol.RequestCancelled,
	} {
		path := "/v1/requests?status=" + url.QueryEscape(string(st))
		raw, err := originDo(ctx, http.MethodGet, path, tok, nil)
		if err != nil {
			return publicapi.RequestView{}, "", err
		}
		var out publicapi.RequestsResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return publicapi.RequestView{}, "", err
		}
		for _, req := range out.Requests {
			if req.ID == requestID {
				return req, st, nil
			}
		}
	}
	return publicapi.RequestView{}, "", fmt.Errorf("request %q not found", requestID)
}

func originRequestWatch(cmd *cobra.Command, requestID string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = time.Hour
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	return originRequestWatchCtx(cmd, ctx, requestID, false)
}

func originRequestWatchCtx(cmd *cobra.Command, ctx context.Context, requestID string, quiet bool) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request watch: REQUEST_ID is required")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Hour)
	}
	if !quiet {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"Watching %s until approved, denied, or expired\n  approve: veil request approve %s\n  deny:    veil request deny %s\n",
			requestID, requestID, requestID,
		)
	}

	emit := func(st protocol.RequestStatus, view publicapi.RequestView) error {
		switch st {
		case protocol.RequestApproved:
			if !quiet {
				if err := encode(cmd, map[string]any{"request_id": requestID, "status": st, "request": view}); err != nil {
					return err
				}
			}
			return errRequestWatchApproved
		case protocol.RequestDenied, protocol.RequestExpired, protocol.RequestCancelled:
			return fmt.Errorf("request %s: %s", requestID, st)
		case protocol.RequestOpen:
			return nil
		default:
			return nil
		}
	}

	check := func() error {
		view, st, err := originRequestFind(ctx, tok, requestID)
		if err != nil {
			return err
		}
		if st != protocol.RequestOpen {
			return emit(st, view)
		}
		return nil
	}

	if err := check(); err != nil {
		if errors.Is(err, errRequestWatchApproved) {
			return nil
		}
		return err
	}

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	events := make(chan struct{}, 8)
	go originRequestStream(ctx, tok, events)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("request watch: timeout waiting for %s", requestID)
		case <-events:
			if err := check(); err != nil {
				if errors.Is(err, errRequestWatchApproved) {
					return nil
				}
				return err
			}
		case <-tick.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("request watch: timeout waiting for %s", requestID)
			}
			if err := check(); err != nil {
				if errors.Is(err, errRequestWatchApproved) {
					return nil
				}
				return err
			}
		}
	}
}

// originRequestAwaitApproval blocks until a fill request is approved — Veil.app,
// iOS, or this terminal ([y/N/deny] when interactiveCLI). Then fill run continues.
func originRequestAwaitApproval(cmd *cobra.Command, agentName, requestID string, timeout time.Duration, interactiveCLI bool) error {
	requestID = strings.TrimSpace(requestID)
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if st, err := originAgentFillRequestStatus(cmd, agentName, requestID); err == nil && st == protocol.RequestApproved {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Request %s already approved — running fill…\n", requestID)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"\n── Fill approval (Veil.app / iOS / CLI — same request) ──\n  %s\n",
		requestID,
	)
	if _, err := originHumanCLI(cmd.Context()); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"  Set VEIL_OIDC_TOKEN_FILE (human JWT) to approve here, or approve in the app.\n  Waiting on agent poll…\n",
		)
		if err := originAgentFillWaitApproved(cmd, agentName, requestID, timeout); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Approved — running agentic fill in the browser…\n")
		return nil
	}
	if interactiveCLI && stdinIsTTY() {
		if err := originRequestConfirmOrWatch(cmd, requestID, timeout); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Approved — running agentic fill in the browser…\n")
		return nil
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"  Approve in Veil.app / iOS, or in your terminal: veil request confirm %s\n  (watching until approved…)\n",
		requestID,
	)
	if err := originRequestWatch(cmd, requestID, timeout); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Approved — running agentic fill in the browser…\n")
	return nil
}

func originRequestConfirmOrWatch(cmd *cobra.Command, requestID string, timeout time.Duration) error {
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	requestID = strings.TrimSpace(requestID)
	view, st, err := originRequestFind(cmd.Context(), tok, requestID)
	if err != nil {
		return err
	}
	if st == protocol.RequestApproved {
		return nil
	}
	if st != protocol.RequestOpen {
		return fmt.Errorf("request %s is already %s", requestID, st)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Agent %q → %s item %q (expires %s)\nApprove? [y/N/deny] — or use Veil.app / iOS now\n",
		view.AgentID, view.Action, view.ItemID, view.ExpiresAt.UTC().Format(time.RFC3339),
	)
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	done := make(chan error, 2)
	go func() {
		done <- originRequestWatchCtx(cmd, ctx, requestID, true)
	}()
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			done <- err
			return
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			done <- originRequestResolveQuiet(cmd, requestID, "approve", 15*time.Minute)
		case "d", "deny", "n", "no":
			done <- originRequestResolveQuiet(cmd, requestID, "deny", 0)
		default:
			done <- fmt.Errorf("request confirm: declined (not approved)")
		}
	}()
	select {
	case err := <-done:
		cancel()
		return err
	case <-ctx.Done():
		cancel()
		return fmt.Errorf("timeout waiting for approval of %s", requestID)
	}
}

func originAgentFillRequestStatus(cmd *cobra.Command, agentName, requestID string) (protocol.RequestStatus, error) {
	tok, err := originAgentCLI(cmd.Context(), agentName)
	if err != nil {
		return "", err
	}
	raw, err := originDo(cmd.Context(), http.MethodGet, "/v1/agent/fill/request/"+url.PathEscape(requestID), tok, nil)
	if err != nil {
		return "", err
	}
	var out publicapi.AgentFillRequestOut
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

func originRequestStream(ctx context.Context, tok string, wake chan<- struct{}) {
	base := originBase()
	if base == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/requests/stream", nil)
	if err != nil {
		return
	}
	if err := originAttachAuth(req, tok); err != nil {
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "data:") {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
}

func originRequestStreamUntil(ctx context.Context, tok string, max int) error {
	base := originBase()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/requests/stream", nil)
	if err != nil {
		return err
	}
	if err := originAttachAuth(req, tok); err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("stream: http %d %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() && max > 0 {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), "data:") {
			max--
		}
	}
	return sc.Err()
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func originRequestConfirm(cmd *cobra.Command, requestID string, autoYes, autoDeny bool, ttl time.Duration) error {
	if autoYes && autoDeny {
		return fmt.Errorf("request confirm: use only one of --yes or --deny")
	}
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request confirm: REQUEST_ID is required")
	}
	view, st, err := originRequestFind(cmd.Context(), tok, requestID)
	if err != nil {
		return err
	}
	if st != protocol.RequestOpen {
		return fmt.Errorf("request %s is already %s", requestID, st)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Agent %q → %s item %q (expires %s)\nSame ask as Veil.app / iOS / app.veil.nyc/requests.\n",
		view.AgentID, view.Action, view.ItemID, view.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if autoDeny {
		return originRequestResolve(cmd, requestID, "deny", 0)
	}
	if autoYes {
		return originRequestResolve(cmd, requestID, "approve", ttl)
	}
	if !stdinIsTTY() {
		return fmt.Errorf("request confirm: stdin is not a terminal — use veil request approve %s, --yes, or run watch in another shell", requestID)
	}
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Approve? [y/N/deny] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return originRequestResolve(cmd, requestID, "approve", ttl)
	case "d", "deny", "n", "no":
		return originRequestResolve(cmd, requestID, "deny", 0)
	default:
		return fmt.Errorf("request confirm: declined (not approved)")
	}
}
