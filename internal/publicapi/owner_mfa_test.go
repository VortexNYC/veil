package publicapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/human"
	"github.com/VortexNYC/veil/internal/protocol"
)

func jwtWithAMR(t *testing.T, amr ...string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"amr": amr})
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".e30"
}

func TestOwnerGrantRequiresAAL2AMR(t *testing.T) {
	a := testApp(t)
	owner := protocol.Principal{Kind: protocol.PrincipalHuman, ID: app.DefaultHuman, OrgID: protocol.LocalOrgID}
	if _, err := a.Store.PlantHuman(owner); err != nil {
		t.Fatal(err)
	}
	baseIdent := identity(a)
	jwtIdent := func(ctx context.Context, raw string) (protocol.Principal, error) {
		if _, ok := human.AMR(raw); ok {
			return owner, nil
		}
		return baseIdent(ctx, raw)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(CORS(mux))
	t.Cleanup(srv.Close)
	(&Server{App: a, Identity: jwtIdent}).Mount(mux)
	aal1 := jwtWithAMR(t, "password")
	code, _ := doJSON(t, srv, http.MethodPost, "/v1/grants", aal1, map[string]any{
		"agent": "claude", "item": "x", "level": "2",
	})
	if code != http.StatusForbidden {
		t.Fatalf("aal1 grant: want 403, got %d", code)
	}
	aal2 := jwtWithAMR(t, "password", "totp")
	_, _ = doJSON(t, srv, http.MethodPost, "/v1/items", aal2, map[string]any{
		"name": "x", "uri": "https://example.com", "secret": "s",
	})
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/grants", aal2, map[string]any{
		"agent": "claude", "item": "x", "level": "2",
	})
	if code != http.StatusOK && code != http.StatusBadRequest {
		t.Fatalf("aal2 grant: got %d", code)
	}
}
