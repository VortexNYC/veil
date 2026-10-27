package publicapi

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
)

// The create→redeem round-trip against real Postgres — the redeem audit
// event lands in the same tx as the claim, and the partitioned audit table
// constrains org_id non-empty, which only PG catches.
func TestShareRedeemPostgresHTTP(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	t.Setenv("VEIL_KEK", strings.Repeat("ab", 32))
	a, err := app.OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := apiServer(t, a)

	sh := createTestShare(t, srv, "human")
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/shares/"+sh.ID+"/redeem", "", nil)
	if code != http.StatusOK {
		t.Fatalf("redeem %d %s", code, raw)
	}
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/shares/"+sh.ID+"/redeem", "", nil)
	if code != http.StatusNotFound {
		t.Fatalf("second redeem: %d", code)
	}
	code, raw = doJSON(t, srv, http.MethodDelete, "/v1/shares/"+sh.ID, "human", nil)
	if code != http.StatusOK && code != http.StatusNotFound {
		t.Fatalf("revoke %d %s", code, raw)
	}
}
