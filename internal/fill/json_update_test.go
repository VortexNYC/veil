package fill

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
)

// The update action PATCHes metadata through the same write path as save —
// and the wire must never carry a secret field.
func TestJSONUpdatePatchesMetadata(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{
		Name: "netflix", URI: "https://www.netflix.com", Token: []byte("pw123"), Login: "ada",
	}); err != nil {
		t.Fatal(err)
	}
	srv := originAPI(t, a)
	var patches atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var in map[string]any
			if json.Unmarshal(body, &in) != nil {
				t.Error("bad patch json")
			}
			if _, has := in["secret"]; has {
				t.Error("update carried a secret field")
			}
			if in["login"] != "grace@example.com" {
				t.Errorf("login = %v", in["login"])
			}
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return nil }
	// Resolve the item id through the index — update names by uuid.
	var id string
	for _, e := range h.jsonList() {
		if e.Name == "netflix" {
			id = e.UUID
		}
	}
	if id == "" {
		t.Fatal("netflix not in index")
	}
	got := jsonHandle(t, h, map[string]any{
		"action": "update", "uuid": id,
		"login": "grace@example.com",
		"uris":  []string{"https://www.netflix.com", "https://netflix.com"},
		"tags":  []string{"home"},
	})
	var ok struct {
		OK   bool   `json:"ok"`
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(got, &ok); err != nil || !ok.OK || ok.UUID != id {
		t.Fatalf("update %s", got)
	}
	if patches.Load() != 1 {
		t.Fatalf("patches = %d", patches.Load())
	}
}

func TestJSONUpdateRequiresConfirm(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{
		Name: "netflix", URI: "https://www.netflix.com", Token: []byte("pw123"),
	}); err != nil {
		t.Fatal(err)
	}
	srv := originAPI(t, a)
	var patches atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return errors.New("no") }
	var id string
	for _, e := range h.jsonList() {
		if e.Name == "netflix" {
			id = e.UUID
		}
	}
	got := jsonHandle(t, h, map[string]any{
		"action": "update", "uuid": id, "login": "x",
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "canceled" {
		t.Fatalf("denied %s", got)
	}
	if patches.Load() != 0 {
		t.Fatal("denied called origin")
	}
}

// An unknown uuid fails before confirm — no prompt for nothing.
func TestJSONUpdateUnknownUUIDDoesNotConfirm(t *testing.T) {
	var n atomic.Int32
	h := NewOrigin(t.TempDir(), "http://127.0.0.1:1", "human")
	h.Confirm = func(string) error {
		n.Add(1)
		return nil
	}
	got := jsonHandle(t, h, map[string]any{
		"action": "update", "uuid": "no-such-item", "login": "x",
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "failed" {
		t.Fatalf("%s", got)
	}
	if n.Load() != 0 {
		t.Fatal("unknown uuid prompted")
	}
}
