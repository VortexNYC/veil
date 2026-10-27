package cli

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

func encodeHex(b []byte) string { return hex.EncodeToString(b) }

func reqFixture(id string) protocol.RecoveryRequest {
	return protocol.RecoveryRequest{
		ID: id, OrgID: "o", HumanID: "h", InitiatedBy: "h",
		Status:    protocol.RecoveryOpen,
		CreatedAt: time.Now().UTC(), UnlockAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
	}
}

// TestRecoveryKeygen writes a 0600 private key and prints only pubkeys.
func TestRecoveryKeygen(t *testing.T) {
	home := t.TempDir()
	out := filepath.Join(home, "recovery.key.pending")
	stdout, err := run(t, home, "", "recovery", "keygen", "--out-file", out)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		PubkeyHex    string `json:"pubkey_hex"`
		PubkeyBase64 string `json:"pubkey_base64"`
		OutFile      string `json:"out_file"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("keygen output %q: %v", stdout, err)
	}
	if len(got.PubkeyHex) != 64 {
		t.Fatalf("pubkey_hex %q", got.PubkeyHex)
	}
	priv, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != device.PublicSize {
		t.Fatalf("priv len %d", len(priv))
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %o", info.Mode().Perm())
	}
	// The private key must never appear in the printed output.
	if strings.Contains(stdout, base64.StdEncoding.EncodeToString(priv)) {
		t.Fatal("keygen printed the private key")
	}
	derived, err := device.Public(priv)
	if err != nil {
		t.Fatal(err)
	}
	if got.PubkeyHex != encodeHex(derived) {
		t.Fatal("printed pubkey does not match the written key")
	}
}

// TestRecoveryEnrollPostsPubkey: enroll generates recovery.key (0600) and
// POSTs the base64 pubkey to /v1/recovery/enroll.
func TestRecoveryEnrollPostsPubkey(t *testing.T) {
	home := t.TempDir()
	var posted publicapi.EnrollRecoveryRequest
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recovery/enroll":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"org_id":"o","human_id":"h","wrap_pubkey_epoch":1,"org_key_version":1}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	t.Setenv("VEIL_HUMAN_TOKEN", "tok-human")

	if _, err := run(t, home, "", "recovery", "enroll"); err != nil {
		t.Fatal(err)
	}
	pub, err := base64.StdEncoding.DecodeString(posted.Pubkey)
	if err != nil || len(pub) != device.PublicSize {
		t.Fatalf("posted pubkey %q", posted.Pubkey)
	}
	keyPath := filepath.Join(home, "recovery.key")
	priv, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != device.PublicSize {
		t.Fatalf("recovery.key len %d", len(priv))
	}
	info, _ := os.Stat(keyPath)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("recovery.key mode %o", info.Mode().Perm())
	}
	derived, _ := device.Public(priv)
	if base64.StdEncoding.EncodeToString(derived) != posted.Pubkey {
		t.Fatal("posted pubkey does not match the escrowed key")
	}
}

// TestRecoveryAutoEnrollOnLoginBackfill: when the share reports
// enrolled:false and no recovery.key exists, auto-enroll writes the key
// file and posts the pubkey. A share that is already enrolled is a no-op.
func TestRecoveryAutoEnrollOnLoginBackfill(t *testing.T) {
	home := t.TempDir()
	var posted publicapi.EnrollRecoveryRequest
	enrolled := false
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/recovery/share":
			_ = json.NewEncoder(w).Encode(app.RecoveryShareView{Enrolled: enrolled})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recovery/enroll":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"wrap_pubkey_epoch":1}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)

	maybeAutoEnrollRecovery(t.Context(), home, "tok-human")
	if posted.Pubkey == "" {
		t.Fatal("auto-enroll did not post a pubkey")
	}
	if _, err := os.Stat(filepath.Join(home, "recovery.key")); err != nil {
		t.Fatalf("auto-enroll wrote no key: %v", err)
	}

	// Already enrolled: no call, no file.
	home2 := t.TempDir()
	enrolled = true
	posted.Pubkey = ""
	maybeAutoEnrollRecovery(t.Context(), home2, "tok-human")
	if posted.Pubkey != "" {
		t.Fatal("auto-enroll posted despite enrolled share")
	}
	if _, err := os.Stat(filepath.Join(home2, "recovery.key")); !os.IsNotExist(err) {
		t.Fatal("auto-enroll wrote a key for an enrolled member")
	}
}

// TestRecoveryStartStagesPendingAndDenyCancels: self-start stages
// recovery.key.pending and posts new_pubkey; deny posts to the deny route.
func TestRecoveryStartStagesPendingAndDenyCancels(t *testing.T) {
	home := t.TempDir()
	var postedStart publicapi.StartRecoveryRequest
	var denied string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/recovery/start":
			if err := json.NewDecoder(r.Body).Decode(&postedStart); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(publicapi.StartRecoveryResponse{
				Request: app.RecoveryView{RecoveryRequest: reqFixture("rec-1")},
				Created: true,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/deny"):
			denied = r.URL.Path
			_ = json.NewEncoder(w).Encode(app.RecoveryView{RecoveryRequest: reqFixture("rec-1")})
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	t.Setenv("VEIL_HUMAN_TOKEN", "tok-human")

	out, err := run(t, home, "", "recovery", "start", "--reset-mfa")
	if err != nil {
		t.Fatal(err)
	}
	var startOut publicapi.StartRecoveryResponse
	if err := json.Unmarshal([]byte(out), &startOut); err != nil {
		t.Fatalf("start output %q: %v", out, err)
	}
	if postedStart.NewPubkey == "" || !postedStart.ResetMFA {
		t.Fatalf("posted start %+v", postedStart)
	}
	pending := filepath.Join(home, "recovery.key.pending")
	priv, err := os.ReadFile(pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != device.PublicSize {
		t.Fatalf("pending key len %d", len(priv))
	}
	derived, _ := device.Public(priv)
	if base64.StdEncoding.EncodeToString(derived) != postedStart.NewPubkey {
		t.Fatal("posted new_pubkey does not match the staged pending key")
	}

	if _, err := run(t, home, "", "recovery", "deny", "rec-1"); err != nil {
		t.Fatal(err)
	}
	if denied != "/v1/recovery/rec-1/deny" {
		t.Fatalf("deny hit %q", denied)
	}
}
