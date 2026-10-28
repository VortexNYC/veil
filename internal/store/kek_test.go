package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mock key-service: AES-256-GCM, envelope 0x02||iv||ct, aad-bound.
func mockKeysvc(t *testing.T, key []byte) (*httptest.Server, string) {
	tok := "test-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+tok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		aad, _ := base64.StdEncoding.DecodeString(in["aad_b64"])
		block, _ := aes.NewCipher(key)
		gcm, _ := cipher.NewGCM(block)
		switch r.URL.Path {
		case "/seal":
			pt, _ := base64.StdEncoding.DecodeString(in["plaintext_b64"])
			iv := make([]byte, 12)
			crand.Read(iv)
			out := append([]byte{0x02}, append(iv, gcm.Seal(nil, iv, pt, aad)...)...)
			json.NewEncoder(w).Encode(map[string]string{"ciphertext_b64": base64.StdEncoding.EncodeToString(out)})
		case "/open":
			blob, _ := base64.StdEncoding.DecodeString(in["ciphertext_b64"])
			pt, err := gcm.Open(nil, blob[1:13], blob[13:], aad)
			if err != nil {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "open failed"})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"plaintext_b64": base64.StdEncoding.EncodeToString(pt)})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, tok
}

func TestRemoteKEKRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	crand.Read(key)
	srv, tok := mockKeysvc(t, key)
	r := newRemoteKEK(srv.URL, tok)
	pt := []byte("org-master-key-material-here-32!")
	aad := []byte("org-1234")
	blob, err := r.seal(context.Background(), pt, aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if blob[0] != remoteBlobVersion {
		t.Fatalf("blob version = %#x, want %#x", blob[0], remoteBlobVersion)
	}
	got, err := r.open(context.Background(), blob, aad)
	if err != nil || string(got) != string(pt) {
		t.Fatalf("open: %v got %q", err, got)
	}
	if _, err := r.open(context.Background(), blob, []byte("other-org")); err == nil {
		t.Fatal("wrong aad opened — binding broken")
	}
}
