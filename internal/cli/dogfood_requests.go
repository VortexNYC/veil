package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

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

func originAgentFillRequest(cmd *cobra.Command, agentName, itemRef, pageURL string) error {
	tok, err := originAgentCLI(cmd.Context(), agentName)
	if err != nil {
		return err
	}
	itemRef = strings.TrimSpace(itemRef)
	pageURL = strings.TrimSpace(pageURL)
	if itemRef == "" || pageURL == "" {
		return fmt.Errorf("agent fill request: --item and --url are required")
	}
	itemID, err := originResolveItemRef(cmd.Context(), tok, itemRef)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(publicapi.AgentFillRequestIn{
		ItemID: itemID,
		URL:    pageURL,
	})
	if err != nil {
		return err
	}
	raw, err := originDo(cmd.Context(), http.MethodPost, "/v1/agent/fill/request", tok, payload)
	if err != nil {
		return err
	}
	var out publicapi.AgentFillRequestOut
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Approve: %s\nHuman CLI: veil request confirm %s\n             veil request approve %s\n             veil request watch %s\n",
		out.ApprovalURL, out.RequestID, out.RequestID, out.RequestID,
	)
	return encode(cmd, out)
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
	tok, err := originHumanCLI(cmd.Context())
	if err != nil {
		return err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request watch: REQUEST_ID is required")
	}
	if timeout <= 0 {
		timeout = time.Hour
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	deadline := time.Now().Add(timeout)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Watching %s until approved, denied, or expired (timeout %s)\n  approve: veil request approve %s\n  deny:    veil request deny %s\n",
		requestID, timeout.Round(time.Second), requestID, requestID,
	)

	emit := func(st protocol.RequestStatus, view publicapi.RequestView) error {
		switch st {
		case protocol.RequestApproved:
			return encode(cmd, map[string]any{"request_id": requestID, "status": st, "request": view})
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
				return err
			}
		case <-tick.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("request watch: timeout waiting for %s", requestID)
			}
			if err := check(); err != nil {
				return err
			}
		}
	}
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
