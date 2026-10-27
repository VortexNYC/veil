package publicapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VortexNYC/veil/internal/scrub"
)

var shareBlob = base64.StdEncoding.EncodeToString([]byte("sealed-envelope-bytes"))

func createTestShare(t *testing.T, srv *httptest.Server, token string) ShareView {
	t.Helper()
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/shares", token, CreateShareRequest{
		Name:       "stripe-key",
		BlobB64:    shareBlob,
		Meta:       json.RawMessage(`{"v":1,"alg":"nacl.secretbox"}`),
		TTLSeconds: 3600,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	var sh ShareView
	if err := json.Unmarshal(raw, &sh); err != nil {
		t.Fatal(err)
	}
	if sh.ID == "" {
		t.Fatal("no share id")
	}
	// The create response is metadata — the envelope must not echo back.
	if scrub.Contains(raw, []byte(shareBlob)) {
		t.Fatal("create echoed the blob")
	}
	return sh
}

func TestShareCreateRedeemOnce(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	sh := createTestShare(t, srv, "human")

	// Public redeem — deliberately no Authorization header.
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/shares/"+sh.ID+"/redeem", "", nil)
	if code != http.StatusOK {
		t.Fatalf("redeem %d %s", code, raw)
	}
	var out RedeemShareResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.BlobB64 != shareBlob {
		t.Fatalf("blob %q", out.BlobB64)
	}
	// One-time: the second redeem is the generic 404.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/shares/"+sh.ID+"/redeem", "", nil)
	if code != http.StatusNotFound {
		t.Fatalf("second redeem: %d", code)
	}
}

func TestShareCreateOwnerOnly(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	in := CreateShareRequest{BlobB64: shareBlob}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares", "member", in); code != http.StatusForbidden {
		t.Fatalf("member create: %d", code)
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares", "agent", in); code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("agent create: %d", code)
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares", "", in); code != http.StatusUnauthorized {
		t.Fatalf("no-token create: %d", code)
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares", "human", CreateShareRequest{Name: "x"}); code != http.StatusBadRequest {
		t.Fatalf("empty blob: %d", code)
	}
}

func TestShareRevokeAndList(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	sh := createTestShare(t, srv, "human")

	// Owner list is metadata — the sealed blob never leaves the row.
	code, raw := doJSON(t, srv, http.MethodGet, "/v1/shares", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list %d", code)
	}
	if scrub.Contains(raw, []byte(shareBlob)) {
		t.Fatal("list leaked the blob")
	}
	var list SharesResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Shares) != 1 || list.Shares[0].ID != sh.ID {
		t.Fatalf("list %+v", list)
	}
	if code, _ := doJSON(t, srv, http.MethodGet, "/v1/shares", "member", nil); code != http.StatusForbidden {
		t.Fatalf("member list: %d", code)
	}

	code, _ = doJSON(t, srv, http.MethodDelete, "/v1/shares/"+sh.ID, "human", nil)
	if code != http.StatusOK {
		t.Fatalf("revoke %d", code)
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares/"+sh.ID+"/redeem", "", nil); code != http.StatusNotFound {
		t.Fatalf("redeem after revoke: %d", code)
	}
	// Cross-org owner deletes nothing.
	sh2 := createTestShare(t, srv, "human")
	if code, _ := doJSON(t, srv, http.MethodDelete, "/v1/shares/"+sh2.ID, "otherorg", nil); code != http.StatusForbidden {
		t.Fatalf("other-org revoke: %d", code)
	}
}

func TestShareRedeemUnknownIs404(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares/shr_missing/redeem", "", nil); code != http.StatusNotFound {
		t.Fatalf("missing: %d", code)
	}
}

func TestShareRedeemRateLimited(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	for i := 0; i < 10; i++ {
		if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares/shr_probe_http/redeem", "", nil); code != http.StatusNotFound {
			t.Fatalf("probe %d: %d", i, code)
		}
	}
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/shares/shr_probe_http/redeem", "", nil); code != http.StatusTooManyRequests {
		t.Fatalf("11th probe: %d", code)
	}
}
