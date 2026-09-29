package fill

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/material"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
	"github.com/VortexNYC/veil/internal/replica"
	"github.com/VortexNYC/veil/internal/scrub"
)

func jsonHandle(t *testing.T, h *Host, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return h.Handle(raw)
}

func TestJSONProtocolAgainstFakeOrigin(t *testing.T) {
	const login = "stripe@example.com"
	const secretB = secret + "-work"
	const seed = "JBSWY3DPEHPK3PXP"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe-a", URI: "https://dashboard.stripe.com", Secret: secret, Login: login, TOTPSeed: seed,
	})
	if code != http.StatusOK {
		t.Fatalf("a %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe-b", URI: "https://dashboard.stripe.com", Secret: secretB, Login: "work@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("b %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "github", URI: "https://github.com", Secret: secret + "-gh",
	})
	if code != http.StatusOK {
		t.Fatalf("github %d %s", code, raw)
	}

	var gets, fills atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/items" {
			gets.Add(1)
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/logins" {
			fills.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var in publicapi.FillLoginsRequest
			if json.Unmarshal(body, &in) != nil || strings.TrimSpace(in.UUID) == "" {
				t.Errorf("json fill must POST uuid, got %s", body)
			}
		}
		inner.ServeHTTP(w, r)
	})

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	nConfirm := 0
	h.Confirm = func(string) error { nConfirm++; return nil }

	ping := jsonHandle(t, h, map[string]string{"action": "ping"})
	var pong struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(ping, &pong); err != nil || pong.Version != JSONVersion {
		t.Fatalf("ping %s", ping)
	}
	if gets.Load() != 1 {
		t.Fatalf("ping GET /v1/items %d", gets.Load())
	}

	matchRaw := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://dashboard.stripe.com/login"})
	if scrub.Contains(matchRaw, []byte(secret)) || scrub.Contains(matchRaw, []byte(secretB)) || scrub.Contains(matchRaw, []byte(seed)) {
		t.Fatalf("match leaked secret: %s", matchRaw)
	}
	var matched struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(matchRaw, &matched); err != nil {
		t.Fatal(err)
	}
	if len(matched.Entries) != 2 {
		t.Fatalf("match %+v", matched)
	}
	byUUID := map[string]jsonMatchEntry{}
	for _, e := range matched.Entries {
		byUUID[e.UUID] = e
		if e.Kind != "login" || e.Affiliated {
			t.Fatalf("entry %+v", e)
		}
	}
	if byUUID["stripe-a"].Login != login || !byUUID["stripe-a"].HasTOTP || byUUID["stripe-a"].SavedFor != "dashboard.stripe.com" {
		t.Fatalf("stripe-a %+v", byUUID["stripe-a"])
	}
	if jsonHandle(t, h, map[string]string{"action": "match", "url": "https://dashboard.stripe.com/login"}); gets.Load() != 1 {
		t.Fatalf("match hit origin again GET=%d", gets.Load())
	}

	miss := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://evil.example/login"})
	var empty struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(miss, &empty); err != nil || len(empty.Entries) != 0 {
		t.Fatalf("wrong host %s", miss)
	}

	ambiguous := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://dashboard.stripe.com/login"})
	var none struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(ambiguous, &none); err != nil || len(none.Entries) != 0 {
		t.Fatalf("ambiguous fill %+v", none)
	}
	if fills.Load() != 0 || nConfirm != 0 {
		t.Fatalf("ambiguous decrypted fills=%d confirm=%d", fills.Load(), nConfirm)
	}

	one := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://dashboard.stripe.com/login", "uuid": "stripe-a"})
	if scrub.Contains(one, []byte(secretB)) || scrub.Contains(one, []byte(seed)) {
		t.Fatalf("uuid fill leaked the other secret: %s", one)
	}
	var filled struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(one, &filled); err != nil {
		t.Fatal(err)
	}
	if len(filled.Entries) != 1 || filled.Entries[0].UUID != "stripe-a" || filled.Entries[0].Login != login || filled.Entries[0].Password != secret {
		t.Fatalf("fill %+v", filled)
	}
	if filled.Entries[0].Kind != "login" || len(filled.Entries[0].TOTP) != 6 {
		t.Fatalf("mint totp %+v", filled.Entries[0])
	}
	if fills.Load() != 1 || nConfirm != 1 {
		t.Fatalf("fill origin=%d confirm=%d", fills.Load(), nConfirm)
	}

	again := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://dashboard.stripe.com/login", "uuid": "stripe-a"})
	if err := json.Unmarshal(again, &filled); err != nil || len(filled.Entries) != 1 || filled.Entries[0].Password != secret {
		t.Fatalf("reuse fill %s", again)
	}
	if nConfirm != 1 {
		t.Fatalf("30s reuse confirmed %d times", nConfirm)
	}

	cross := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "stripe-a"})
	if err := json.Unmarshal(cross, &none); err != nil || len(none.Entries) != 0 {
		t.Fatalf("cross-host fill %s", cross)
	}

	gen := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://dashboard.stripe.com"})
	var genOut struct {
		Error    string `json:"error"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(gen, &genOut); err != nil || genOut.Error != "choose" || genOut.Password != "" {
		t.Fatalf("generate existing %s", gen)
	}
}

func TestJSONFillConfirmDeniedDoesNotCallOrigin(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe", URI: "https://dashboard.stripe.com", Secret: secret, Login: "a@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	var fills atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/logins" {
			fills.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return errors.New("denied") }
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://dashboard.stripe.com/login", "uuid": "stripe"})
	if scrub.Contains(got, []byte(secret)) {
		t.Fatal("password left after cancel")
	}
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 0 {
		t.Fatalf("%s", got)
	}
	if fills.Load() != 0 {
		t.Fatalf("origin fill after cancel %d", fills.Load())
	}
}

func TestJSONReplicaFillDoesNotCallOriginAndHidesDisk(t *testing.T) {
	const login = "ada@example.com"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "github", URI: "https://github.com", Secret: secret, Login: login,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	var fills atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/logins" {
			fills.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(NewOrigin(dir, srv.URL, "human"))
	h.Replica = box
	if err := h.PullReplica(); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(replica.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte(secret)) || bytes.Contains(onDisk, []byte("github.com")) || bytes.Contains(onDisk, []byte(login)) {
		t.Fatal("replica.box is not fully encrypted")
	}
	srv.Close()
	h.Origin = "http://127.0.0.1:1"
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 || out.Entries[0].Password != secret || out.Entries[0].Login != login {
		t.Fatalf("airplane fill %s", got)
	}
	if fills.Load() != 0 {
		t.Fatalf("fill used origin %d", fills.Load())
	}
	deny := NewOrigin(dir, "http://127.0.0.1:1", "human")
	deny.Replica = box
	deny.Confirm = func(string) error { return errors.New("denied") }
	denied := jsonHandle(t, deny, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	if scrub.Contains(denied, []byte(secret)) {
		t.Fatal("cancel returned password")
	}
}

func TestPullReplicaSkipsEmptyMaterial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/fill/sync" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(publicapi.FillSyncResponse{Items: []publicapi.FillSyncItem{
			{Item: protocol.Item{ID: "dead", Name: "dead", Kind: protocol.ItemAPIKey}, Material: ""},
			{Item: protocol.Item{ID: "live", Name: "live", Kind: protocol.ItemAPIKey}, Material: `{"v":1,"token":"x"}`},
		}})
	}))
	t.Cleanup(srv.Close)
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(t.TempDir()), key)
	if err != nil {
		t.Fatal(err)
	}
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Replica = box
	if err := h.PullReplica(); err != nil {
		t.Fatal(err)
	}
	if box.Material("live") == "" {
		t.Fatal("valid row did not land")
	}
	if box.Material("dead") != "" || box.Len() != 1 {
		t.Fatalf("empty material landed: %d", box.Len())
	}
}

func TestJSONFillUnambiguousUUIDOmitted(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "github", URI: "https://github.com", Secret: secret, Login: "ada",
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].UUID != "github" || out.Entries[0].Password != secret || out.Entries[0].Login != "ada" {
		t.Fatalf("%+v", out)
	}
}

func TestJSONNeedLoginOnDeadJWT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	h := NewOrigin(t.TempDir(), srv.URL, "stale")
	ping := jsonHandle(t, h, map[string]string{"action": "ping"})
	var pong struct {
		Version string `json:"version"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(ping, &pong); err != nil {
		t.Fatal(err)
	}
	if pong.Version != JSONVersion || pong.Error != "need_login" {
		t.Fatalf("ping %s", ping)
	}
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://example.com/login"})
	var out struct {
		Error   string          `json:"error"`
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "need_login" || len(out.Entries) != 0 {
		t.Fatalf("fill %s", got)
	}
}

func TestJSONPasskeysAgainstFakeOrigin(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	var registers, gets atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/passkeys/register" {
			registers.Add(1)
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/passkeys/get" {
			gets.Add(1)
		}
		inner.ServeHTTP(w, r)
	})

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	nConfirm := 0
	h.Confirm = func(string) error { nConfirm++; return nil }

	empty := jsonHandle(t, h, map[string]string{"action": "passkeyCreate"})
	var fail struct {
		Error    string          `json:"error"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(empty, &fail); err != nil || fail.Error != "failed" || len(fail.Response) != 0 {
		t.Fatalf("empty create %s", empty)
	}
	if registers.Load() != 0 || nConfirm != 0 {
		t.Fatalf("empty create hit origin registers=%d confirm=%d", registers.Load(), nConfirm)
	}

	h.Confirm = func(string) error { return errors.New("no") }
	denied := jsonHandle(t, h, map[string]any{
		"action": "passkeyCreate",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "dGVzdGNoYWxsZW5nZQ",
			"rp":        map[string]string{"id": "github.com", "name": "GitHub"},
			"user":      map[string]string{"id": "dXNlcg", "name": "ada", "displayName": "Ada"},
			"pubKeyCredParams": []map[string]any{
				{"type": "public-key", "alg": -7},
			},
		},
	})
	if err := json.Unmarshal(denied, &fail); err != nil || fail.Error != "canceled" {
		t.Fatalf("denied %s", denied)
	}
	if registers.Load() != 0 {
		t.Fatalf("denied called origin")
	}

	h.Confirm = func(string) error { nConfirm++; return nil }
	created := jsonHandle(t, h, map[string]any{
		"action": "passkeyCreate",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "dGVzdGNoYWxsZW5nZQ",
			"rp":        map[string]string{"id": "github.com", "name": "GitHub"},
			"user":      map[string]string{"id": "dXNlcg", "name": "ada", "displayName": "Ada"},
			"pubKeyCredParams": []map[string]any{
				{"type": "public-key", "alg": -7},
			},
		},
	})
	if scrub.Contains(created, []byte("BEGIN")) {
		t.Fatalf("create leaked pem: %s", created)
	}
	var out struct {
		Error    string          `json:"error"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(created, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" {
		t.Fatalf("create %s", created)
	}
	var cred struct {
		ID       string `json:"id"`
		Response struct {
			AttestationObject string `json:"attestationObject"`
			Signature         string `json:"signature"`
		} `json:"response"`
	}
	if err := json.Unmarshal(out.Response, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.ID == "" || cred.Response.AttestationObject == "" {
		t.Fatalf("create response %s", out.Response)
	}
	if registers.Load() != 1 || nConfirm != 1 {
		t.Fatalf("create origin=%d confirm=%d", registers.Load(), nConfirm)
	}

	got := jsonHandle(t, h, map[string]any{
		"action": "passkeyGet",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "Z2V0Y2hhbGxlbmdlMTIz",
			"rpId":      "github.com",
			"allowCredentials": []map[string]string{
				{"id": cred.ID, "type": "public-key"},
			},
		},
	})
	if scrub.Contains(got, []byte("BEGIN")) {
		t.Fatalf("get leaked pem: %s", got)
	}
	if err := json.Unmarshal(got, &out); err != nil || out.Error != "" {
		t.Fatalf("get %s", got)
	}
	if err := json.Unmarshal(out.Response, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.Response.Signature == "" {
		t.Fatalf("get response %s", out.Response)
	}
	if gets.Load() != 1 {
		t.Fatalf("get origin=%d", gets.Load())
	}
	if nConfirm != 1 {
		t.Fatalf("30s reuse confirmed %d times", nConfirm)
	}
}

func TestJSONPasskeyGetConfirmDeniedDoesNotCallOrigin(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	var gets atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/passkeys/get" {
			gets.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return errors.New("no") }
	got := jsonHandle(t, h, map[string]any{
		"action": "passkeyGet",
		"origin": "https://github.com",
		"publicKey": map[string]any{
			"challenge": "Z2V0Y2hhbGxlbmdlMTIz",
			"rpId":      "github.com",
		},
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "canceled" {
		t.Fatalf("denied get %s", got)
	}
	if gets.Load() != 0 {
		t.Fatalf("denied get called origin %d", gets.Load())
	}
}

func TestJSONGenerateSignupSavesThenReturnsPassword(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	var creates atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/items" {
			creates.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var in publicapi.CreateItemRequest
			if json.Unmarshal(body, &in) != nil || in.Secret == "" || in.URI != "https://signup.example.com" || in.Login != "ada@example.com" || in.Name != "signup.example.com" {
				t.Errorf("create %s", body)
			}
			if scrub.Contains(body, []byte("BEGIN")) {
				t.Errorf("create leaked pem")
			}
		}
		inner.ServeHTTP(w, r)
	})

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	nConfirm := 0
	h.Confirm = func(string) error { nConfirm++; return nil }

	empty := jsonHandle(t, h, map[string]string{"action": "generate"})
	var fail struct {
		Error    string `json:"error"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(empty, &fail); err != nil || fail.Error != "failed" || fail.Password != "" {
		t.Fatalf("empty %s", empty)
	}
	if creates.Load() != 0 || nConfirm != 0 {
		t.Fatalf("empty hit origin creates=%d confirm=%d", creates.Load(), nConfirm)
	}

	h.Confirm = func(string) error { return errors.New("no") }
	denied := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://signup.example.com/join"})
	if err := json.Unmarshal(denied, &fail); err != nil || fail.Error != "canceled" || fail.Password != "" {
		t.Fatalf("denied %s", denied)
	}
	if creates.Load() != 0 {
		t.Fatal("denied called origin")
	}

	h.Confirm = func(string) error { nConfirm++; return nil }
	got := jsonHandle(t, h, map[string]any{
		"action":        "generate",
		"url":           "https://signup.example.com/join?src=ad",
		"login":         "ada@example.com",
		"passwordRules": "minlength: 24; maxlength: 40;",
	})
	if scrub.Contains(got, []byte("BEGIN")) {
		t.Fatalf("generate leaked pem: %s", got)
	}
	var out struct {
		Error    string `json:"error"`
		UUID     string `json:"uuid"`
		Name     string `json:"name"`
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.UUID == "" || out.Password == "" || out.Login != "ada@example.com" {
		t.Fatalf("generate %s", got)
	}
	if out.Name != "signup.example.com" {
		t.Fatalf("chooser name %q", out.Name)
	}
	if out.UUID == out.Name || !id.Valid(out.UUID) {
		t.Fatalf("id %q name %q", out.UUID, out.Name)
	}
	if len(out.Password) != 24 {
		t.Fatalf("rules length %d %q", len(out.Password), out.Password)
	}
	if creates.Load() != 1 || nConfirm != 1 {
		t.Fatalf("create origin=%d confirm=%d", creates.Load(), nConfirm)
	}

	listed := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://signup.example.com/login"})
	var match struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(listed, &match); err != nil || len(match.Entries) != 1 {
		t.Fatalf("match after generate %s", listed)
	}
	if match.Entries[0].UUID != out.UUID || match.Entries[0].Kind != "login" || match.Entries[0].Name != "signup.example.com" {
		t.Fatalf("match %+v", match.Entries[0])
	}
	if scrub.Contains(listed, []byte(out.Password)) {
		t.Fatalf("match leaked password %s", listed)
	}

	again := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://signup.example.com"})
	if err := json.Unmarshal(again, &fail); err != nil || fail.Error != "choose" || fail.Password != "" {
		t.Fatalf("second generate %s", again)
	}
	if creates.Load() != 1 {
		t.Fatalf("change-password created again %d", creates.Load())
	}
}

func TestJSONGenerateWithUUIDRotatesInsteadOfCreates(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe", URI: "https://dashboard.stripe.com", Secret: secret, Login: "ada@example.com", TOTPSeed: seed,
	})
	if code != http.StatusOK {
		t.Fatalf("create %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "other", URI: "https://other.example.com", Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("other %d %s", code, raw)
	}
	var creates, patches atomic.Int32
	var patchBody atomic.Value
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/items" {
			creates.Add(1)
		}
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/v1/items/") {
			patches.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			patchBody.Store(string(body))
			r.Body = io.NopCloser(bytes.NewReader(body))
			if r.URL.Path != "/v1/items/stripe" {
				t.Errorf("rotate must hit the item id, got %s", r.URL.Path)
			}
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	nConfirm := 0
	h.Confirm = func(string) error { nConfirm++; return nil }

	bogus := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://dashboard.stripe.com/settings/security", "uuid": "ghost"})
	var fail struct {
		Error    string `json:"error"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(bogus, &fail); err != nil || fail.Error != "choose" || fail.Password != "" {
		t.Fatalf("unknown uuid %s", bogus)
	}
	offSite := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://dashboard.stripe.com/settings/security", "uuid": "other"})
	if err := json.Unmarshal(offSite, &fail); err != nil || fail.Error != "choose" || fail.Password != "" {
		t.Fatalf("cross-site uuid %s", offSite)
	}
	if creates.Load() != 0 || patches.Load() != 0 || nConfirm != 0 {
		t.Fatalf("bad uuid hit origin/confirm creates=%d patches=%d confirm=%d", creates.Load(), patches.Load(), nConfirm)
	}

	h.Confirm = func(string) error { return errors.New("no") }
	denied := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://dashboard.stripe.com/settings/security", "uuid": "stripe"})
	if err := json.Unmarshal(denied, &fail); err != nil || fail.Error != "canceled" || fail.Password != "" {
		t.Fatalf("denied %s", denied)
	}
	if patches.Load() != 0 {
		t.Fatal("denied still patched")
	}

	h.Confirm = func(string) error { nConfirm++; return nil }
	got := jsonHandle(t, h, map[string]any{
		"action":        "generate",
		"url":           "https://dashboard.stripe.com/settings/security",
		"uuid":          "stripe",
		"login":         "ada@example.com",
		"passwordRules": "minlength: 20;",
	})
	var out struct {
		Error    string `json:"error"`
		UUID     string `json:"uuid"`
		Name     string `json:"name"`
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.UUID != "stripe" || len(out.Password) != 20 {
		t.Fatalf("rotate %s", got)
	}
	if creates.Load() != 0 || patches.Load() != 1 {
		t.Fatalf("rotate created=%d patched=%d", creates.Load(), patches.Load())
	}
	var patched publicapi.UpdateItemRequest
	if err := json.Unmarshal([]byte(patchBody.Load().(string)), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Secret != out.Password || patched.Login != "ada@example.com" {
		t.Fatalf("patch body %+v", patched)
	}

	fills, err := a.FillLogins(protocol.Principal{Kind: protocol.PrincipalHuman, ID: app.DefaultHuman, OrgID: a.OrgID}, "https://dashboard.stripe.com/login")
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].Password != out.Password || fills[0].Login != "ada@example.com" {
		t.Fatalf("rotate did not land: %+v", fills)
	}
	if !strings.Contains(patchBody.Load().(string), "secret") {
		t.Fatal("rotate never sent the secret")
	}
}

func TestJSONGenerateNeedLoginDoesNotMintOnDeadJWT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	h := NewOrigin(t.TempDir(), srv.URL, "stale")
	h.Confirm = func(string) error {
		t.Fatal("confirm before login")
		return nil
	}
	got := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://signup.example.com"})
	var fail struct {
		Error    string `json:"error"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "need_login" || fail.Password != "" {
		t.Fatalf("dead origin %s", got)
	}
}

func TestJSONCardMatchFillAndCVVNeverReuses(t *testing.T) {
	const pan = "4111111111111111"
	const cvv = "123"
	blob, err := material.PackCard(pan, "12", "2030", cvv, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	card := protocol.Item{ID: "amex", Name: "amex", Kind: protocol.ItemCard}
	if err := box.Put(card, blob); err != nil {
		t.Fatal(err)
	}
	loginBlob := []byte(`{"v":1,"token":"pw-github"}`)
	if err := box.Put(protocol.Item{ID: "github", Name: "github", Kind: protocol.ItemAPIKey, URIs: []string{"https://github.com"}}, loginBlob); err != nil {
		t.Fatal(err)
	}
	var nConfirm atomic.Int32
	h := NewOrigin(dir, "http://127.0.0.1:1", "human")
	h.Replica = box
	h.Confirm = func(string) error {
		nConfirm.Add(1)
		return nil
	}

	match := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://www.amazon.com/checkout"})
	var listed struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(match, &listed); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range listed.Entries {
		kinds = append(kinds, e.Kind)
		if e.Kind == "card" && (e.Login != "" || strings.Contains(string(match), pan) || strings.Contains(string(match), cvv)) {
			t.Fatalf("match leaked card secret %s", match)
		}
	}
	if !strings.Contains(strings.Join(kinds, ","), "card") {
		t.Fatalf("unbound card missing from match %s", match)
	}

	noUUID := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://www.amazon.com/checkout"})
	var none struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(noUUID, &none); err != nil || len(none.Entries) != 0 {
		t.Fatalf("card without uuid %+v", none)
	}
	if nConfirm.Load() != 0 {
		t.Fatal("choose prompted")
	}

	gh := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://github.com/login", "uuid": "github"})
	var ghOut struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(gh, &ghOut); err != nil || len(ghOut.Entries) != 1 || ghOut.Entries[0].Password != "pw-github" {
		t.Fatalf("github fill %s", gh)
	}
	if nConfirm.Load() != 1 {
		t.Fatalf("github confirm %d", nConfirm.Load())
	}

	first := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://www.amazon.com/checkout", "uuid": "amex"})
	var cardOut struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(first, &cardOut); err != nil || len(cardOut.Entries) != 1 {
		t.Fatalf("card fill %s", first)
	}
	e := cardOut.Entries[0]
	if e.Kind != "card" || e.Number != pan || e.CVV != cvv || e.ExpMonth != "12" || e.Password != "" {
		t.Fatalf("card entry %+v", e)
	}
	if nConfirm.Load() != 2 {
		t.Fatalf("amazon cvv reused github confirm %d", nConfirm.Load())
	}

	again := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://www.amazon.com/checkout", "uuid": "amex"})
	if err := json.Unmarshal(again, &none); err != nil || len(none.Entries) != 1 || none.Entries[0].CVV != cvv {
		t.Fatalf("second cvv %s", again)
	}
	if nConfirm.Load() != 3 {
		t.Fatalf("cvv reused %d", nConfirm.Load())
	}
}

func TestJSONIdentityFill(t *testing.T) {
	blob, err := material.PackIdentity("Ada", "Lovelace", "1 Street", "London", "", "SW1", "GB", "+44", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Put(protocol.Item{ID: "home", Name: "home", Kind: protocol.ItemIdentity}, blob); err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(NewOrigin(dir, "http://127.0.0.1:1", "human"))
	h.Replica = box
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://store.example/checkout", "uuid": "home"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 {
		t.Fatalf("%s", got)
	}
	e := out.Entries[0]
	if e.Kind != "identity" || e.GivenName != "Ada" || e.Address != "1 Street" || e.Phone != "+44" || e.Number != "" {
		t.Fatalf("%+v", e)
	}
}

func TestReplicaFillRejectsIdentityKind(t *testing.T) {
	blob, err := material.PackIdentity("", "Lovelace", "", "", "", "", "", "+44", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Put(protocol.Item{ID: "home", Name: "home", Kind: protocol.ItemIdentity}, blob); err != nil {
		t.Fatal(err)
	}
	h := NewOrigin(dir, "http://127.0.0.1:1", "human")
	h.Replica = box
	if _, ok := h.replicaFill("home", false); ok {
		t.Fatal("identity as password")
	}
}

func TestJSONCardFillFromOriginWithoutReplica(t *testing.T) {
	const pan = "4111111111111111"
	const cvv = "123"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	blob, err := material.PackCard(pan, "12", "2030", cvv, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItem(app.ItemOpts{Name: "amex", Kind: protocol.ItemCard, Token: blob}); err != nil {
		t.Fatal(err)
	}
	srv := originAPI(t, a)
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	if h.Replica != nil {
		t.Fatal("fixture must leave replica nil")
	}
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://www.amazon.com/checkout", "uuid": "amex"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 {
		t.Fatalf("origin card fill %s", got)
	}
	e := out.Entries[0]
	if e.Kind != "card" || e.Number != pan || e.CVV != cvv || e.ExpMonth != "12" {
		t.Fatalf("origin card entry %+v", e)
	}
}

func TestJSONCardFillReplicaEmptyFallsBackToOrigin(t *testing.T) {
	const pan = "4111111111111111"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	blob, err := material.PackCard(pan, "12", "2030", "123", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItem(app.ItemOpts{Name: "amex", Kind: protocol.ItemCard, Token: blob}); err != nil {
		t.Fatal(err)
	}
	srv := originAPI(t, a)
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Put(protocol.Item{ID: "amex", Name: "amex", Kind: protocol.ItemCard}, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	h := allowConfirm(NewOrigin(dir, srv.URL, "human"))
	h.Replica = box
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://www.amazon.com/checkout", "uuid": "amex"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 {
		t.Fatalf("fallback %s", got)
	}
	if out.Entries[0].Kind != "card" || out.Entries[0].Number != pan {
		t.Fatalf("fallback entry %+v", out.Entries[0])
	}
}

func TestJSONFillEmptyURLDoesNotConfirm(t *testing.T) {
	var n atomic.Int32
	h := NewOrigin(t.TempDir(), "http://127.0.0.1:1", "human")
	h.Confirm = func(string) error {
		n.Add(1)
		return nil
	}
	got := jsonHandle(t, h, map[string]string{"action": "fill", "uuid": "amex"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 0 {
		t.Fatalf("empty url fill %s", got)
	}
	if n.Load() != 0 {
		t.Fatal("empty url prompted")
	}
}

func TestJSONCardFillReplicaEmptyOriginDownDoesNotConfirm(t *testing.T) {
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Put(protocol.Item{ID: "amex", Name: "amex", Kind: protocol.ItemCard}, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	h := NewOrigin(dir, "http://127.0.0.1:1", "human")
	h.Replica = box
	h.Confirm = func(string) error {
		n.Add(1)
		return nil
	}
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://www.amazon.com/checkout", "uuid": "amex"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 0 {
		t.Fatalf("empty card fill %s", got)
	}
	if n.Load() != 0 {
		t.Fatal("empty card prompted")
	}
}

func TestJSONIdentityFillFromOriginWithoutReplica(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	blob, err := material.PackIdentity("Ada", "Lovelace", "1 Street", "London", "", "SW1", "GB", "+44", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutItem(app.ItemOpts{Name: "home", Kind: protocol.ItemIdentity, Token: blob}); err != nil {
		t.Fatal(err)
	}
	srv := originAPI(t, a)
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	got := jsonHandle(t, h, map[string]string{"action": "fill", "url": "https://store.example/checkout", "uuid": "home"})
	var out struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Entries) != 1 {
		t.Fatalf("origin identity fill %s", got)
	}
	e := out.Entries[0]
	if e.Kind != "identity" || e.GivenName != "Ada" || e.Address != "1 Street" || e.Phone != "+44" || e.Number != "" {
		t.Fatalf("origin identity entry %+v", e)
	}
}

func TestJSONMatchRanksExactHostFirst(t *testing.T) {
	// A card with no URIs matches every page — the login saved for this exact
	// host must still lead the suggestion list.
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	// the unbound item exists first — vault order cannot leak through
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "amex", Kind: "card", Secret: secret + "-card",
	})
	if code != http.StatusOK {
		t.Fatalf("card %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe-dash", URI: "https://dashboard.stripe.com", Secret: secret + "-dash", Login: "dash@example.com",
	})
	if code != http.StatusOK {
		t.Fatalf("dash %d %s", code, raw)
	}

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return nil }
	matchRaw := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://dashboard.stripe.com/login"})
	var matched struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(matchRaw, &matched); err != nil {
		t.Fatal(err)
	}
	if len(matched.Entries) != 2 {
		t.Fatalf("match %+v", matched)
	}
	if matched.Entries[0].Name != "stripe-dash" || matched.Entries[1].Name != "amex" {
		t.Fatalf("order %+v", matched.Entries)
	}
}

func TestJSONMatchIndexTTLRefreshes(t *testing.T) {
	// The bridge keeps a long-lived host — an item created on origin after
	// the index first loaded must appear once the TTL lapses, without a
	// bridge restart.
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "before", URI: "https://a.example.com", Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("before %d %s", code, raw)
	}

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return nil }
	matchNames := func() []string {
		var matched struct {
			Entries []jsonMatchEntry `json:"entries"`
		}
		if err := json.Unmarshal(jsonHandle(t, h, map[string]string{
			"action": "match", "url": "https://a.example.com",
		}), &matched); err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, e := range matched.Entries {
			names = append(names, e.Name)
		}
		return names
	}
	if got := matchNames(); len(got) != 1 || got[0] != "before" {
		t.Fatalf("first match %v", got)
	}

	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "after", URI: "https://a.example.com", Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("after %d %s", code, raw)
	}
	if got := matchNames(); len(got) != 1 {
		t.Fatalf("inside TTL must still serve the cached index, got %v", got)
	}

	h.mu.Lock()
	h.indexAt = time.Now().Add(-2 * indexTTL)
	h.mu.Unlock()
	if got := matchNames(); len(got) != 2 {
		t.Fatalf("after TTL lapse %v", got)
	}
}

func TestJSONMatchAppURI(t *testing.T) {
	// Native fill binds items to `app://<bundleID>` — the same host rule as
	// web URIs, canonicalized case-insensitively.
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "textedit-login", URI: "app://com.apple.TextEdit", Secret: secret, Login: "axuser",
	})
	if code != http.StatusOK {
		t.Fatalf("item %d %s", code, raw)
	}

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return nil }
	var matched struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(jsonHandle(t, h, map[string]string{
		"action": "match", "url": "app://com.apple.TEXTEDIT",
	}), &matched); err != nil {
		t.Fatal(err)
	}
	if len(matched.Entries) != 1 || matched.Entries[0].Name != "textedit-login" {
		t.Fatalf("app match %+v", matched.Entries)
	}
	if err := json.Unmarshal(jsonHandle(t, h, map[string]string{
		"action": "match", "url": "app://com.apple.Safari",
	}), &matched); err != nil {
		t.Fatal(err)
	}
	if len(matched.Entries) != 0 {
		t.Fatalf("other app must not match %+v", matched.Entries)
	}
}

func TestJSONListMetadataAndURIsOnly(t *testing.T) {
	// `list` feeds the AutoFill identity store: every fillable item with its
	// URIs, metadata only — never a secret, never archived.
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "stripe", URI: "https://dashboard.stripe.com", Secret: secret, Login: "me@x.com",
	})
	if code != http.StatusOK {
		t.Fatalf("login %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "arch", URI: "https://old.example.com", Secret: secret + "-old",
	})
	if code != http.StatusOK {
		t.Fatalf("arch %d %s", code, raw)
	}
	code, raw = originJSON(t, srv, http.MethodPost, "/v1/items/arch/archive", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("archive %d %s", code, raw)
	}

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	raw = jsonHandle(t, h, map[string]string{"action": "list"})
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatalf("list leaked secret: %s", raw)
	}
	var listed struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 1 || listed.Entries[0].Name != "stripe" {
		t.Fatalf("list must return live fillable items only: %+v", listed.Entries)
	}
	if len(listed.Entries[0].URIs) != 1 || listed.Entries[0].URIs[0] != "https://dashboard.stripe.com" {
		t.Fatalf("list must carry URIs for identity sync: %+v", listed.Entries[0])
	}
	if listed.Entries[0].UUID == "" || listed.Entries[0].Login != "me@x.com" {
		t.Fatalf("identity needs uuid+user: %+v", listed.Entries[0])
	}
}

func TestJSONMatchNeverCarriesURIs(t *testing.T) {
	// `match` replies reach page-adjacent extension contexts — sibling URIs
	// of a matched item would leak the vault's other bindings.
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	code, raw := originJSON(t, srv, http.MethodPost, "/v1/items", "human", publicapi.CreateItemRequest{
		Name: "multi", URI: "https://a.example.com", Secret: secret,
	})
	if code != http.StatusOK {
		t.Fatalf("item %d %s", code, raw)
	}
	var item protocol.Item
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	code, raw = originJSON(t, srv, http.MethodPatch, "/v1/items/"+item.ID, "human", map[string]any{
		"uris": []string{"https://a.example.com", "https://b.example.com"},
	})
	if code != http.StatusOK {
		t.Fatalf("uris %d %s", code, raw)
	}

	h := NewOrigin(t.TempDir(), srv.URL, "human")
	raw = jsonHandle(t, h, map[string]string{"action": "match", "url": "https://a.example.com"})
	if strings.Contains(string(raw), "b.example.com") || strings.Contains(string(raw), `"uris"`) {
		t.Fatalf("match must not carry uris: %s", raw)
	}
}

// The AutoFill appex builds ASPasskeyCredentialIdentity from `list`: it needs
// credId/rpId/userHandle metadata. The private key never leaves the replica.
func TestJSONListPasskeyIdentityFields(t *testing.T) {
	dir := t.TempDir()
	key, err := replica.Unlock(replica.Mem())
	if err != nil {
		t.Fatal(err)
	}
	box, err := replica.Open(replica.Path(dir), key)
	if err != nil {
		t.Fatal(err)
	}
	const pem = "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----"
	blob, err := material.PackPasskey(pem, "cred-abc", "github.com", "u-handle")
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.Item{ID: "pk1", Name: "github.com", Kind: protocol.ItemPasskey, URIs: []string{"https://github.com"}, Login: "ada"}
	if err := box.Put(item, blob); err != nil {
		t.Fatal(err)
	}
	if err := box.Put(protocol.Item{ID: "gh", Name: "gh", Kind: protocol.ItemAPIKey, URIs: []string{"https://github.com"}}, []byte(`{"v":1,"token":"x"}`)); err != nil {
		t.Fatal(err)
	}
	h := NewOrigin(dir, "http://127.0.0.1:1", "human")
	h.Replica = box

	raw := jsonHandle(t, h, map[string]string{"action": "list"})
	if bytes.Contains(raw, []byte("PRIVATE KEY")) || strings.Contains(string(raw), pem[:30]) {
		t.Fatalf("list leaked passkey material: %s", raw)
	}
	var out struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	var pk *jsonMatchEntry
	for i := range out.Entries {
		if out.Entries[i].UUID == "pk1" {
			pk = &out.Entries[i]
		}
	}
	if pk == nil || pk.CredID != "cred-abc" || pk.RpID != "github.com" || pk.UserHandle != "u-handle" {
		t.Fatalf("passkey identity fields %s", raw)
	}
	for _, e := range out.Entries {
		if e.Kind != "passkey" && (e.CredID != "" || e.RpID != "") {
			t.Fatalf("non-passkey entry carried passkey fields: %+v", e)
		}
	}
}
