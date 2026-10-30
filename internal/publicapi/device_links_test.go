package publicapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

func TestDeviceLinkCreateClaimUnauthenticated(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	pub := make([]byte, 32)
	for i := range pub {
		pub[i] = byte(i + 1)
	}

	code, raw := doJSON(t, srv, "POST", "/v1/device-links", "", map[string]any{
		"label": "new-phone", "pubkey": pub,
	})
	if code != 200 {
		t.Fatalf("create: %d %s", code, raw)
	}
	var created protocol.DeviceLink
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || len(created.Pubkey) != 32 {
		t.Fatalf("bad create body: %+v", created)
	}
	stored, err := a.Store.DeviceLink(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("store after create: %v", err)
	}
	if stored.ConsumedAt != nil {
		t.Fatalf("unexpected consumed: %+v", stored)
	}

	code, _ = doJSON(t, srv, "GET", "/v1/device-links/"+created.Token, "", nil)
	if code != 401 {
		t.Fatalf("get without session: want 401, got %d", code)
	}

	code, _ = doJSON(t, srv, "POST", "/v1/device-links/"+created.Token+"/claim", "", nil)
	if code != 400 {
		t.Fatalf("claim before approve: want 400, got %d", code)
	}

	code, raw = doJSON(t, srv, "GET", "/v1/device-links/"+created.Token, "human", nil)
	if code != 200 {
		t.Fatalf("get as approver: want 200, got %d %s", code, raw)
	}

	sealed := []byte("ciphertext-only-blob")
	code, raw = doJSON(t, srv, "POST", "/v1/device-links/"+created.Token+"/approve", "human", map[string]any{
		"sealed": sealed,
	})
	if code != 204 {
		t.Fatalf("approve: want 204, got %d %s", code, raw)
	}

	code, raw = doJSON(t, srv, "POST", "/v1/device-links/"+created.Token+"/claim", "", nil)
	if code != 200 {
		t.Fatalf("claim: %d %s", code, raw)
	}
	var claimed protocol.DeviceLink
	if err := json.Unmarshal(raw, &claimed); err != nil {
		t.Fatal(err)
	}
	if string(claimed.Sealed) != string(sealed) {
		t.Fatalf("claim lost sealed blob")
	}

	code, _ = doJSON(t, srv, "POST", "/v1/device-links/"+created.Token+"/claim", "", nil)
	if code != 400 {
		t.Fatalf("second claim: want 400, got %d", code)
	}
}
