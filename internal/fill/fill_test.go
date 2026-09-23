package fill

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

const secret = "sk_live_FILL_SECRET"

type client struct {
	pub, priv *[32]byte
	host      [32]byte
	id        string
	idKey     string
}

func newClient(t *testing.T) *client {
	t.Helper()
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	idPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &client{
		pub:   pub,
		priv:  priv,
		id:    "client-1",
		idKey: base64.StdEncoding.EncodeToString(idPub[:]),
	}
}

func (c *client) handshake(t *testing.T, h *Host) {
	t.Helper()
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope{
		Action:    "change-public-keys",
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		ClientID:  c.id,
		PublicKey: base64.StdEncoding.EncodeToString(c.pub[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got envelope
	if err := json.Unmarshal(h.Handle(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.PublicKey == "" {
		t.Fatalf("%+v", got)
	}
	var n [24]byte
	copy(n[:], nonce)
	want := bumpNonce(n)
	if got.Nonce != base64.StdEncoding.EncodeToString(want[:]) {
		t.Fatalf("nonce %q", got.Nonce)
	}
	k, err := b64key(got.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	c.host = k
}

func (c *client) send(t *testing.T, h *Host, inner []byte) []byte {
	t.Helper()
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	boxed := box.Seal(nil, inner, &nonce, &c.host, c.priv)
	raw, err := json.Marshal(envelope{
		Action:   "get-logins",
		Message:  base64.StdEncoding.EncodeToString(boxed),
		Nonce:    base64.StdEncoding.EncodeToString(nonce[:]),
		ClientID: c.id,
	})
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(h.Handle(raw), &env); err != nil {
		t.Fatal(err)
	}
	n, err := b64nonce(env.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	plain, ok := box.Open(nil, mustB64(env.Message), &n, &c.host, c.priv)
	if !ok {
		t.Fatal("decrypt failed")
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(plain, &body); err != nil {
		t.Fatal(err)
	}
	if string(body["nonce"]) != `"`+env.Nonce+`"` {
		t.Fatalf("inner nonce %s want %s", body["nonce"], env.Nonce)
	}
	return plain
}

func vault(t *testing.T) *app.App {
	t.Helper()
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{
		Name:  "stripe",
		URI:   "https://dashboard.stripe.com",
		Token: []byte(secret),
		Login: "stripe@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGetLoginsFillsMatchingURI(t *testing.T) {
	a := vault(t)
	h := allowConfirm(New(a))
	c := newClient(t)
	c.handshake(t, h)
	assoc, err := json.Marshal(map[string]string{"action": "associate", "key": c.idKey, "idKey": c.idKey})
	if err != nil {
		t.Fatal(err)
	}
	var assocGot map[string]string
	if err := json.Unmarshal(c.send(t, h, assoc), &assocGot); err != nil {
		t.Fatal(err)
	}
	if assocGot["success"] != "true" || assocGot["id"] != assocID {
		t.Fatalf("%v", assocGot)
	}

	req, err := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{
		Action: "get-logins",
		URL:    "https://dashboard.stripe.com/login",
		Keys:   []assocKey{{ID: assocID, Key: c.idKey}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got.Success != "true" || got.Count != "1" || len(got.Entries) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Entries[0].Password != secret {
		t.Fatal("fill did not write the password to the extension")
	}
	if got.Entries[0].Login != "stripe@example.com" || got.Entries[0].Name != "stripe" {
		t.Fatalf("login must be username, not item name: %+v", got.Entries[0])
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("item list leaked the secret")
	}
	if !scrub.Contains(raw, []byte("stripe@example.com")) {
		t.Fatal("item list omitted login")
	}
}

func TestGetLoginsWrongHostEmpty(t *testing.T) {
	a := vault(t)
	h := New(a)
	c := newClient(t)
	c.handshake(t, h)
	inner, _ := json.Marshal(map[string]string{"action": "associate", "key": c.idKey, "idKey": c.idKey})
	_ = c.send(t, h, inner)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://evil.example", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != "0" || len(got.Entries) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestGetLoginsNeedsAssociate(t *testing.T) {
	a := vault(t)
	h := New(a)
	c := newClient(t)
	c.handshake(t, h)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://dashboard.stripe.com", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got map[string]string
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got["success"] != "false" {
		t.Fatalf("%v", got)
	}
}

func TestNativeFramingRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := []byte(`{"action":"change-public-keys"}`)
	if err := Write(&buf, msg); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(msg) {
		t.Fatalf("%s", got)
	}
}

func TestWriteToPipeDoesNotSync(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	msg := []byte(`{"success":"true"}`)
	if err := Write(w, msg); err != nil {
		t.Fatal(err)
	}
	got, err := Read(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(msg) {
		t.Fatalf("%s", got)
	}
}

func TestInstallWritesManifestsNotExtension(t *testing.T) {
	user := t.TempDir()
	vaultDir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "veil")
	payload := []byte("veil-host-binary\n")
	if err := os.WriteFile(bin, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	chromeDir := filepath.Join(user, "Library/Application Support/Google/Chrome/NativeMessagingHosts")
	if err := os.MkdirAll(chromeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chromeDir, NativeHostName+".json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(bin, vaultDir, user); err != nil {
		t.Fatal(err)
	}
	jsonChrome := filepath.Join(user, "Library/Application Support/Google/Chrome/NativeMessagingHosts", JSONHostName+".json")
	jsonRaw, err := os.ReadFile(jsonChrome)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(jsonRaw, []byte(secret)) {
		t.Fatal("secret in manifest")
	}
	host, err := os.ReadFile(filepath.Join(vaultDir, HostFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(host, payload) {
		t.Fatalf("host is not the binary: %q", host)
	}
	if bytes.HasPrefix(host, []byte("#!")) {
		t.Fatal("host is a script")
	}
	if !bytes.Contains(jsonRaw, []byte(filepath.Join(vaultDir, HostFile))) {
		t.Fatalf("manifest path %s", jsonRaw)
	}
	if !bytes.Contains(jsonRaw, []byte(JSONHostName)) || !bytes.Contains(jsonRaw, []byte(JSONChromeOrigin())) {
		t.Fatalf("json host %s", jsonRaw)
	}
	if bytes.Contains(jsonRaw, []byte("nacl")) || bytes.Contains(jsonRaw, []byte(NativeHostName)) {
		t.Fatalf("json manifest mixed with kpxc: %s", jsonRaw)
	}
	kpxc := filepath.Join(user, "Library/Application Support/Google/Chrome/NativeMessagingHosts", NativeHostName+".json")
	if _, err := os.Stat(kpxc); !os.IsNotExist(err) {
		t.Fatal("kpxc listing still installed")
	}
}

func TestGetTOTPMintsCodeNotSeed(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{Name: "stripe", URI: "https://dashboard.stripe.com", Token: []byte(secret), TOTPSeed: []byte(seed)}); err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(New(a))
	c := newClient(t)
	c.handshake(t, h)
	inner, _ := json.Marshal(map[string]string{"action": "associate", "key": c.idKey, "idKey": c.idKey})
	_ = c.send(t, h, inner)
	req, _ := json.Marshal(map[string]string{"action": "get-totp", "uuid": "stripe"})
	var got map[string]string
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got["success"] != "true" || len(got["totp"]) != 6 {
		t.Fatalf("%v", got)
	}
	if got["totp"] == seed {
		t.Fatal("returned the seed")
	}
}

func TestGetLoginsSignalsTOTPWithoutSeed(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if _, err := a.PutItem(app.ItemOpts{Name: "stripe", URI: "https://dashboard.stripe.com", Token: []byte(secret), TOTPSeed: []byte(seed)}); err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(New(a))
	c := associated(t, h)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://dashboard.stripe.com/login", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Totp != totpPresent {
		t.Fatalf("%+v", got.Entries)
	}
	if got.Entries[0].Totp == seed || len(got.Entries[0].Totp) == 6 {
		t.Fatal("get-logins returned a code or seed")
	}
}

func TestOriginLoginsProbesTOTPWithoutPuttingCode(t *testing.T) {
	const code = "123456"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer human" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/fill/logins":
			_, _ = io.WriteString(w, `{"entries":[{"login":"stripe","name":"stripe","password":"sk_live_FILL_SECRET","uuid":"stripe"}]}`)
		case "/v1/fill/totp":
			_, _ = io.WriteString(w, `{"totp":"`+code+`"}`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	h := allowConfirm(NewOrigin(t.TempDir(), origin.URL, "human"))
	c := associated(t, h)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://dashboard.stripe.com", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Totp != totpPresent {
		t.Fatalf("%+v", got.Entries)
	}
	if got.Entries[0].Totp == code {
		t.Fatal("put the minted code in get-logins")
	}
}

func TestInstallOriginBakesOriginNotToken(t *testing.T) {
	user := t.TempDir()
	vaultDir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "veil")
	payload := []byte("veil-host-binary\n")
	if err := os.WriteFile(bin, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallOrigin(InstallEnv{Bin: bin, VaultHome: vaultDir, UserHome: user, Origin: "https://veil.nyc"}); err != nil {
		t.Fatal(err)
	}
	host, err := os.ReadFile(filepath.Join(vaultDir, HostFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(host, payload) {
		t.Fatalf("host is not the binary")
	}
	cfg, err := ReadHostConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != "https://veil.nyc" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.TokenFile == "" || !strings.Contains(cfg.TokenFile, "human.jwt") {
		t.Fatalf("token file %q", cfg.TokenFile)
	}
	raw, err := os.ReadFile(ConfigPath(vaultDir))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("token in fill.json")
	}
}

func TestGetLoginsFromOrigin(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer human" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/fill/logins" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"entries":[{"login":"stripe","name":"stripe","password":"sk_live_FILL_SECRET","uuid":"stripe"}]}`)
	}))
	t.Cleanup(origin.Close)
	h := allowConfirm(NewOrigin(t.TempDir(), origin.URL, "human"))
	c := newClient(t)
	c.handshake(t, h)
	inner, _ := json.Marshal(map[string]string{"action": "associate", "key": c.idKey, "idKey": c.idKey})
	_ = c.send(t, h, inner)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://dashboard.stripe.com", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got.Success != "true" || len(got.Entries) != 1 || got.Entries[0].Password != secret {
		t.Fatalf("%+v", got)
	}
}

func TestGetLoginsRemintsAfter401(t *testing.T) {
	var saw []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Authorization")
		saw = append(saw, tok)
		if tok != "Bearer fresh" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"entries":[{"login":"stripe","name":"stripe","password":"sk_live_FILL_SECRET","uuid":"stripe"}]}`)
	}))
	t.Cleanup(origin.Close)
	tok := "stale"
	h := allowConfirm(NewOrigin(t.TempDir(), origin.URL, ""))
	h.TokenFn = func() (string, error) { return tok, nil }
	h.Refresh = func() (string, error) {
		tok = "fresh"
		return tok, nil
	}
	c := associated(t, h)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://dashboard.stripe.com", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got.Success != "true" || len(got.Entries) != 1 || got.Entries[0].Password != secret {
		t.Fatalf("%+v saw %v", got, saw)
	}
	if len(saw) < 2 || saw[0] != "Bearer stale" || saw[len(saw)-1] != "Bearer fresh" {
		t.Fatalf("refresh path %v", saw)
	}
}

func TestConfirmDeniedDoesNotReturnPassword(t *testing.T) {
	a := vault(t)
	h := New(a)
	h.Confirm = func(string) error { return errors.New("denied") }
	c := associated(t, h)
	req, _ := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{Action: "get-logins", URL: "https://dashboard.stripe.com", Keys: []assocKey{{ID: assocID, Key: c.idKey}}})
	var got loginReply
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got.Success == "true" {
		t.Fatalf("canceled fill succeeded %+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("password left the host after cancel")
	}
}

func TestConfirmDeniedDoesNotRegisterPasskey(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	h := New(a)
	h.Confirm = func(string) error { return errors.New("denied") }
	c := associated(t, h)
	create, err := json.Marshal(map[string]any{
		"action": "passkeys-register",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "dGVzdGNoYWxsZW5nZQ",
			"rp":        map[string]string{"id": "github.com", "name": "GitHub"},
			"user":      map[string]string{"id": "dXNlcg", "name": "ada", "displayName": "Ada"},
			"pubKeyCredParams": []map[string]any{
				{"type": "public-key", "alg": -7},
			},
		},
		"keys": []assocKey{{ID: assocID, Key: c.idKey}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(c.send(t, h, create), &got); err != nil {
		t.Fatal(err)
	}
	var inner struct {
		ErrorCode int    `json:"errorCode"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(got.Response, &inner); err != nil {
		t.Fatal(err)
	}
	if inner.ErrorCode != passkeysCanceled || inner.ID != "" {
		t.Fatalf("%s", got.Response)
	}
	items, err := a.ItemsForPrincipal(protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("canceled register wrote %d items", len(items))
	}
}

func TestInstallOriginBakesRemintPathsNotPassword(t *testing.T) {
	user := t.TempDir()
	vaultDir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "veil")
	if err := os.WriteFile(bin, []byte("veil-host-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	passFile := filepath.Join(t.TempDir(), "kratos-pass")
	if err := os.WriteFile(passFile, []byte("not-the-jwt-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InstallOrigin(InstallEnv{
		Bin:          bin,
		VaultHome:    vaultDir,
		UserHome:     user,
		Origin:       "https://veil.nyc",
		LoginEmail:   "ada@veil.nyc",
		PasswordFile: passFile,
		TOTPFile:     "/tmp/totp-seed",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigPath(vaultDir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"login_email": "ada@veil.nyc"`)) {
		t.Fatalf("%s", raw)
	}
	if !bytes.Contains(raw, []byte(passFile)) {
		t.Fatalf("%s", raw)
	}
	if bytes.Contains(raw, []byte("not-the-jwt-secret")) {
		t.Fatal("password in fill.json")
	}
	host, err := os.ReadFile(filepath.Join(vaultDir, HostFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(host, []byte("#!")) {
		t.Fatal("host is a script")
	}
}

func TestFillDoesNotAddAgentSurface(t *testing.T) {
	a := vault(t)
	if _, err := a.AddAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddGrant("claude", "stripe", protocol.Level2); err != nil {
		t.Fatal(err)
	}
	items, err := a.ItemsForAgent("claude")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal(string(raw))
	}
}

func associated(t *testing.T, h *Host) *client {
	t.Helper()
	c := newClient(t)
	c.handshake(t, h)
	inner, err := json.Marshal(map[string]string{"action": "associate", "key": c.idKey, "idKey": c.idKey})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.send(t, h, inner)
	return c
}

func TestPasskeysRegisterThenGet(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	h := allowConfirm(New(a))
	c := associated(t, h)
	keys := []assocKey{{ID: assocID, Key: c.idKey}}
	create, err := json.Marshal(map[string]any{
		"action": "passkeys-register",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "dGVzdGNoYWxsZW5nZQ",
			"rp":        map[string]string{"id": "github.com", "name": "GitHub"},
			"user":      map[string]string{"id": "dXNlcg", "name": "ada", "displayName": "Ada"},
			"pubKeyCredParams": []map[string]any{
				{"type": "public-key", "alg": -7},
			},
		},
		"keys": keys,
	})
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Success  string          `json:"success"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(c.send(t, h, create), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Success != "true" {
		t.Fatalf("%s", reg.Response)
	}
	var cred struct {
		ID       string `json:"id"`
		Response struct {
			AttestationObject string `json:"attestationObject"`
			Signature         string `json:"signature"`
		} `json:"response"`
		ErrorCode int `json:"errorCode"`
	}
	if err := json.Unmarshal(reg.Response, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.ErrorCode != 0 || cred.ID == "" || cred.Response.AttestationObject == "" {
		t.Fatalf("%s", reg.Response)
	}
	if bytes.Contains(reg.Response, []byte("BEGIN")) {
		t.Fatal("wire leaked pem")
	}
	get, err := json.Marshal(map[string]any{
		"action": "passkeys-get",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "Z2V0Y2hhbGxlbmdlMTIz",
			"rpId":      "github.com",
			"allowCredentials": []map[string]string{
				{"id": cred.ID, "type": "public-key"},
			},
		},
		"keys": keys,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(c.send(t, h, get), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got.Response, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.ErrorCode != 0 || cred.Response.Signature == "" {
		t.Fatalf("%s", got.Response)
	}
	if bytes.Contains(got.Response, []byte("BEGIN")) {
		t.Fatal("get leaked pem")
	}
	items, err := a.Store.ListItems()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(listed, []byte("BEGIN")) || bytes.Contains(listed, []byte("passkey_pem")) {
		t.Fatal("item list leaked passkey material")
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
	logins, err := a.FillLogins(human, "https://github.com/login")
	if err != nil {
		t.Fatal(err)
	}
	if len(logins) != 0 {
		t.Fatalf("get-logins returned passkey %+v", logins)
	}
}

func TestPasskeysNeedsAssociate(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	h := New(a)
	c := newClient(t)
	c.handshake(t, h)
	req, err := json.Marshal(map[string]any{
		"action":    "passkeys-get",
		"origin":    "https://github.com",
		"publicKey": map[string]any{"challenge": "Z2V0Y2hhbGxlbmdlMTIz", "rpId": "github.com"},
		"keys":      []assocKey{{ID: assocID, Key: c.idKey}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if got["success"] != "false" {
		t.Fatalf("%v", got)
	}
}

func TestPasskeysFromOrigin(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer human" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/fill/passkeys/get" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"response":{"id":"abc","type":"public-key","authenticatorAttachment":"platform","response":{"signature":"c2ln"}}}`)
	}))
	t.Cleanup(origin.Close)
	h := allowConfirm(NewOrigin(t.TempDir(), origin.URL, "human"))
	c := associated(t, h)
	req, err := json.Marshal(map[string]any{
		"action":    "passkeys-get",
		"origin":    "https://github.com",
		"publicKey": map[string]any{"challenge": "Z2V0Y2hhbGxlbmdlMTIz", "rpId": "github.com"},
		"keys":      []assocKey{{ID: assocID, Key: c.idKey}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(c.send(t, h, req), &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got.Response, []byte(`"id":"abc"`)) {
		t.Fatalf("%s", got.Response)
	}
}

func envNoOrigin() []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "VEIL_ORIGIN=") || strings.HasPrefix(e, "VEIL_FILL_TOUCHID=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "VEIL_FILL_TOUCHID=0")
}

func (c *client) handshakeProc(t *testing.T, in io.Writer, out io.Reader) {
	t.Helper()
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope{
		Action:    "change-public-keys",
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		ClientID:  c.id,
		PublicKey: base64.StdEncoding.EncodeToString(c.pub[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(in, raw); err != nil {
		t.Fatal(err)
	}
	gotRaw, err := Read(out)
	if err != nil {
		t.Fatal(err)
	}
	var got envelope
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatal(err)
	}
	k, err := b64key(got.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	c.host = k
}

func (c *client) sendProc(t *testing.T, in io.Writer, out io.Reader, inner []byte) []byte {
	t.Helper()
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	boxed := box.Seal(nil, inner, &nonce, &c.host, c.priv)
	raw, err := json.Marshal(envelope{
		Action:   "get-logins",
		Message:  base64.StdEncoding.EncodeToString(boxed),
		Nonce:    base64.StdEncoding.EncodeToString(nonce[:]),
		ClientID: c.id,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(in, raw); err != nil {
		t.Fatal(err)
	}
	gotRaw, err := Read(out)
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(gotRaw, &env); err != nil {
		t.Fatal(err)
	}
	n, err := b64nonce(env.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	plain, ok := box.Open(nil, mustB64(env.Message), &n, &c.host, c.priv)
	if !ok {
		t.Fatal("decrypt failed")
	}
	return plain
}

func TestPasskeysLiveCLI(t *testing.T) {
	bin := os.Getenv("VEIL_BIN")
	if bin == "" {
		t.Skip("VEIL_BIN")
	}
	home := t.TempDir()
	a, err := app.Init(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "fill", "--home", home)
	cmd.Env = envNoOrigin()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	c := newClient(t)
	c.handshakeProc(t, stdin, stdout)
	assoc, err := json.Marshal(map[string]string{"action": "associate", "key": c.idKey, "idKey": c.idKey})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.sendProc(t, stdin, stdout, assoc)
	keys := []assocKey{{ID: assocID, Key: c.idKey}}
	create, err := json.Marshal(map[string]any{
		"action": "passkeys-register",
		"origin": "https://webauthn.io",
		"publicKey": map[string]any{
			"challenge": "dGVzdGNoYWxsZW5nZQ",
			"rp":        map[string]string{"id": "webauthn.io", "name": "webauthn.io"},
			"user":      map[string]string{"id": "dXNlcg", "name": "ada", "displayName": "Ada"},
			"pubKeyCredParams": []map[string]any{
				{"type": "public-key", "alg": -7},
			},
		},
		"keys": keys,
	})
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Success  string          `json:"success"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(c.sendProc(t, stdin, stdout, create), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Success != "true" {
		t.Fatalf("%s", reg.Response)
	}
	if bytes.Contains(reg.Response, []byte("BEGIN")) {
		t.Fatal("cli leaked pem")
	}
	var cred struct {
		ID        string `json:"id"`
		ErrorCode int    `json:"errorCode"`
	}
	if err := json.Unmarshal(reg.Response, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.ErrorCode != 0 || cred.ID == "" {
		t.Fatalf("%s", reg.Response)
	}
	get, err := json.Marshal(map[string]any{
		"action": "passkeys-get",
		"origin": "https://webauthn.io",
		"publicKey": map[string]any{
			"challenge":        "Z2V0Y2hhbGxlbmdlMTIz",
			"rpId":             "webauthn.io",
			"allowCredentials": []map[string]string{{"id": cred.ID, "type": "public-key"}},
		},
		"keys": keys,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(c.sendProc(t, stdin, stdout, get), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got.Response, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.ErrorCode != 0 || cred.ID == "" {
		t.Fatalf("%s", got.Response)
	}
	if bytes.Contains(got.Response, []byte("BEGIN")) {
		t.Fatal("get leaked pem")
	}
}
