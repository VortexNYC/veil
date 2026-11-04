package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

func TestOriginResolveItemRefByName(t *testing.T) {
	t.Setenv("VEIL_ORIGIN", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/items" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(publicapi.ItemsResponse{
			Items: []protocol.Item{{ID: "iabc1234567890ab", Name: "Stripe"}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VEIL_ORIGIN", srv.URL)
	t.Setenv("VEIL_HUMAN_TOKEN", "human-tok")

	got, err := originResolveItemRef(context.Background(), "human-tok", "Stripe")
	if err != nil {
		t.Fatal(err)
	}
	if got != "iabc1234567890ab" {
		t.Fatalf("got %q", got)
	}
}

func TestOriginRequestWatchExitsWhenApproved(t *testing.T) {
	const reqID = "req-watchtest00001"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/requests/stream" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(": ok\n\n"))
		case r.URL.Path == "/v1/requests" && r.Method == http.MethodGet:
			st := r.URL.Query().Get("status")
			var reqs []publicapi.RequestView
			if st == string(protocol.RequestApproved) {
				reqs = []publicapi.RequestView{{ID: reqID, Status: protocol.RequestApproved, Action: protocol.ActionFill}}
			}
			_ = json.NewEncoder(w).Encode(publicapi.RequestsResponse{Requests: reqs})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VEIL_ORIGIN", srv.URL)
	t.Setenv("VEIL_HUMAN_TOKEN", "human-tok")

	cmd := &cobra.Command{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd.SetContext(ctx)

	if err := originRequestWatchCtx(cmd, ctx, reqID, true); err != nil {
		t.Fatal(err)
	}
}
