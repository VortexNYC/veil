package publicapi

import (
	"encoding/json"
	"testing"
)

// VTX-788: /v1/org/travel-mode — owner toggles, everyone reads, and the
// item list honors the flag plus per-item travel_safe.

func TestTravelModeAPI(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	// Members read the flag but cannot toggle it.
	code, raw := doJSON(t, srv, "GET", "/v1/org/travel-mode", "member", nil)
	if code != 200 {
		t.Fatalf("member GET: %d %s", code, raw)
	}
	var view TravelModeView
	if err := json.Unmarshal(raw, &view); err != nil || view.Enabled {
		t.Fatalf("default flag: %s", raw)
	}
	code, _ = doJSON(t, srv, "POST", "/v1/org/travel-mode", "member", TravelModeRequest{Enabled: true})
	if code != 403 {
		t.Fatalf("member POST: %d", code)
	}
	// Agents are not the human toggle surface either.
	code, _ = doJSON(t, srv, "POST", "/v1/org/travel-mode", "agent", TravelModeRequest{Enabled: true})
	if code != 403 {
		t.Fatalf("agent POST: %d", code)
	}

	code, raw = doJSON(t, srv, "POST", "/v1/org/travel-mode", "human", TravelModeRequest{Enabled: true})
	if code != 200 {
		t.Fatalf("owner POST: %d %s", code, raw)
	}
	if err := json.Unmarshal(raw, &view); err != nil || !view.Enabled {
		t.Fatalf("toggle response: %s", raw)
	}

	// Items list hides non-safe rows; the create flag carries through.
	code, raw = doJSON(t, srv, "POST", "/v1/items", "human", CreateItemRequest{
		Name: "stripe", Secret: secret, TravelSafe: false,
	})
	if code != 200 {
		t.Fatalf("create: %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, "POST", "/v1/items", "human", CreateItemRequest{
		Name: "gmail", Secret: "gm_secret", TravelSafe: true,
	})
	if code != 200 {
		t.Fatalf("create safe: %d %s", code, raw)
	}
	var item struct {
		TravelSafe bool `json:"travel_safe"`
	}
	if err := json.Unmarshal(raw, &item); err != nil || !item.TravelSafe {
		t.Fatalf("create response: %s", raw)
	}

	code, raw = doJSON(t, srv, "GET", "/v1/items", "human", nil)
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	var listed ItemsResponse
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Name != "gmail" || !listed.Items[0].TravelSafe {
		t.Fatalf("filtered list: %s", raw)
	}

	// PATCH can flip the flag off an item.
	on := true
	code, raw = doJSON(t, srv, "PATCH", "/v1/items/stripe", "human", UpdateItemRequest{TravelSafe: &on})
	if code != 200 {
		t.Fatalf("patch: %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, "GET", "/v1/items", "human", nil)
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("after mark-safe: %s", raw)
	}

	// Toggle off — everything resurfaces.
	code, _ = doJSON(t, srv, "POST", "/v1/org/travel-mode", "human", TravelModeRequest{Enabled: false})
	if code != 200 {
		t.Fatalf("off POST: %d", code)
	}
	code, raw = doJSON(t, srv, "GET", "/v1/org/travel-mode", "human", nil)
	if err := json.Unmarshal(raw, &view); err != nil || view.Enabled {
		t.Fatalf("off read: %s", raw)
	}
}
