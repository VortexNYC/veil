//go:build liveorigin

package livetest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/device"
)

// TestDeviceLinkHandoffLive exercises approve+claim on prod when a live
// human JWT is available (VEIL_HUMAN_TOKEN_FILE). Ciphertext is device.Offer
// smoke material — not the org master from recovery share.
func TestDeviceLinkHandoffLive(t *testing.T) {
	humanPath := strings.TrimSpace(os.Getenv("VEIL_HUMAN_TOKEN_FILE"))
	if humanPath == "" {
		t.Skip("VEIL_HUMAN_TOKEN_FILE")
	}
	raw, err := os.ReadFile(humanPath)
	if err != nil {
		t.Fatal(err)
	}
	humanJWT := strings.TrimSpace(string(raw))

	origin := strings.TrimRight(envOr("VEIL_ORIGIN", "https://veil.nyc"), "/")
	client := &http.Client{Timeout: 25 * time.Second}

	master, err := crypto.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	newPub, newPriv, err := device.Generate()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := device.Offer(master, newPub)
	if err != nil {
		t.Fatal(err)
	}

	code, body := postJSON(t, client, origin+"/v1/device-links", "", map[string]any{
		"label": "livetest-handoff", "pubkey": newPub,
	})
	assertNoLeak(t, body)
	if code != 200 {
		t.Fatalf("create http=%d body=%q", code, clip(body))
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Token == "" {
		t.Fatal("create json")
	}

	code, body = postJSON(t, client, origin+"/v1/device-links/"+created.Token+"/approve", humanJWT, map[string]any{
		"sealed": sealed,
	})
	assertNoLeak(t, body)
	if code != 204 {
		t.Fatalf("approve http=%d body=%q", code, clip(body))
	}

	code, body = postJSON(t, client, origin+"/v1/device-links/"+created.Token+"/claim", "", nil)
	assertNoLeak(t, body)
	if code != 200 {
		t.Fatalf("claim http=%d body=%q", code, clip(body))
	}
	var claimed struct {
		Sealed []byte `json:"sealed"`
	}
	if err := json.Unmarshal(body, &claimed); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(claimed.Sealed, sealed) {
		t.Fatal("claim lost sealed blob")
	}
	got, err := device.Accept(claimed.Sealed, newPriv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, master) {
		t.Fatal("handoff master mismatch")
	}
}
