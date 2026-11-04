package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
