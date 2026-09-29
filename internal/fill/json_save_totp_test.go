package fill

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/id"
	"github.com/VortexNYC/veil/internal/publicapi"
	"github.com/VortexNYC/veil/internal/scrub"
)

const typedPassword = "typed-netflix-pw"
const enrollSeed = "JBSWY3DPEHPK3PXP"
const enrollOTPAuth = "otpauth://totp/Netflix:ada@example.com?secret=" + enrollSeed + "&issuer=Netflix"

func TestParseOTPAuth(t *testing.T) {
	if got := parseOTPAuth(enrollOTPAuth); got != enrollSeed {
		t.Fatalf("seed %q", got)
	}
	if parseOTPAuth("otpauth://hotp/x?secret="+enrollSeed) != "" {
		t.Fatal("hotp")
	}
	if parseOTPAuth("otpauth://totp/x?secret=") != "" {
		t.Fatal("empty secret")
	}
	if parseOTPAuth(enrollSeed) != "" {
		t.Fatal("bare seed is harvest")
	}
	if parseOTPAuth("otpauth://totp/x?secret=not-base32!!") != "" {
		t.Fatal("garbage secret")
	}
}

func TestJSONSaveTypedPasswordCreatesLogin(t *testing.T) {
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
			if json.Unmarshal(body, &in) != nil || in.Secret != typedPassword || in.URI != "https://www.netflix.com" || in.Login != "ada@example.com" || in.Name != "www.netflix.com" {
				t.Errorf("create %s", body)
			}
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	nConfirm := 0
	h.Confirm = func(string) error { nConfirm++; return nil }

	got := jsonHandle(t, h, map[string]string{
		"action":   "save",
		"url":      "https://www.netflix.com/login",
		"login":    "ada@example.com",
		"password": typedPassword,
	})
	if scrub.Contains(got, []byte(typedPassword)) {
		t.Fatalf("save echoed password %s", got)
	}
	var out struct {
		Error string `json:"error"`
		UUID  string `json:"uuid"`
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.UUID == "" || out.Login != "ada@example.com" || out.Name != "www.netflix.com" {
		t.Fatalf("save %s", got)
	}
	if out.UUID == out.Name || !id.Valid(out.UUID) {
		t.Fatalf("id %q", out.UUID)
	}
	if creates.Load() != 1 || nConfirm != 1 {
		t.Fatalf("create origin=%d confirm=%d", creates.Load(), nConfirm)
	}

	listed := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://www.netflix.com/login"})
	var match struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(listed, &match); err != nil || len(match.Entries) != 1 {
		t.Fatalf("match after save %s", listed)
	}
	if match.Entries[0].UUID != out.UUID || match.Entries[0].Kind != "login" {
		t.Fatalf("match %+v", match.Entries[0])
	}
	if scrub.Contains(listed, []byte(typedPassword)) {
		t.Fatalf("match leaked password %s", listed)
	}
}

func TestJSONSaveRequiresConfirm(t *testing.T) {
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
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	h.Confirm = func(string) error { return errors.New("no") }
	got := jsonHandle(t, h, map[string]string{
		"action": "save", "url": "https://www.netflix.com/login", "login": "ada", "password": typedPassword,
	})
	var fail struct {
		Error string `json:"error"`
		UUID  string `json:"uuid"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "canceled" || fail.UUID != "" {
		t.Fatalf("denied %s", got)
	}
	if creates.Load() != 0 {
		t.Fatal("denied called origin")
	}
	nilHost := NewOrigin(t.TempDir(), srv.URL, "human")
	nilGot := jsonHandle(t, nilHost, map[string]string{
		"action": "save", "url": "https://www.netflix.com/login", "password": typedPassword,
	})
	if err := json.Unmarshal(nilGot, &fail); err != nil || fail.Error != "canceled" {
		t.Fatalf("nil confirm %s", nilGot)
	}
	if creates.Load() != 0 {
		t.Fatal("nil confirm created")
	}
}

func TestJSONSaveEmptyPasswordDoesNotConfirm(t *testing.T) {
	var n atomic.Int32
	h := NewOrigin(t.TempDir(), "http://127.0.0.1:1", "human")
	h.Confirm = func(string) error {
		n.Add(1)
		return nil
	}
	got := jsonHandle(t, h, map[string]string{"action": "save", "url": "https://www.netflix.com/login", "password": "  "})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "failed" {
		t.Fatalf("%s", got)
	}
	if n.Load() != 0 {
		t.Fatal("empty password prompted")
	}
}

func TestJSONSaveExistingLoginIsChoose(t *testing.T) {
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
	var creates atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/items" {
			creates.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	got := jsonHandle(t, h, map[string]string{
		"action": "save", "url": "https://github.com/login", "login": "ada", "password": typedPassword,
	})
	var fail struct {
		Error string `json:"error"`
		UUID  string `json:"uuid"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "choose" || fail.UUID != "" {
		t.Fatalf("%s", got)
	}
	if creates.Load() != 0 {
		t.Fatal("change-password created a second github")
	}
}

func TestJSONSaveTypedPasswordRotatesExisting(t *testing.T) {
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
	var creates, patches atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/items" {
			creates.Add(1)
		}
		if r.Method == http.MethodPatch {
			patches.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var in struct {
				Secret string `json:"secret"`
				Login  string `json:"login"`
			}
			if json.Unmarshal(body, &in) != nil || in.Secret != "rotated-typed-pw" || in.Login != "ada" {
				t.Errorf("patch %s", body)
			}
		}
		inner.ServeHTTP(w, r)
	})
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))

	var item struct {
		ID string `json:"id"`
	}
	matched := jsonHandle(t, h, map[string]string{"action": "match", "url": "https://github.com/login"})
	var m struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(matched, &m); err != nil || len(m.Entries) != 1 {
		t.Fatalf("match %s", matched)
	}
	item.ID = m.Entries[0].UUID

	got := jsonHandle(t, h, map[string]string{
		"action":   "save",
		"url":      "https://github.com/login",
		"login":    "ada",
		"password": "rotated-typed-pw",
		"uuid":     item.ID,
	})
	if scrub.Contains(got, []byte("rotated-typed-pw")) {
		t.Fatalf("save echoed password %s", got)
	}
	var out struct {
		Error string `json:"error"`
		UUID  string `json:"uuid"`
	}
	if err := json.Unmarshal(got, &out); err != nil || out.Error != "" || out.UUID != item.ID {
		t.Fatalf("save %s", got)
	}
	if patches.Load() != 1 || creates.Load() != 0 {
		t.Fatalf("patch=%d create=%d", patches.Load(), creates.Load())
	}

	filled := jsonHandle(t, h, map[string]string{
		"action": "fill", "url": "https://github.com/login", "uuid": item.ID,
	})
	var fo struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(filled, &fo); err != nil || len(fo.Entries) != 1 || fo.Entries[0].Password != "rotated-typed-pw" {
		t.Fatalf("fill after rotate %s", filled)
	}

	// A uuid that is not a login match on this URL is still "choose" — the
	// save prompt must never patch an unrelated item.
	bogus := jsonHandle(t, h, map[string]string{
		"action":   "save",
		"url":      "https://github.com/login",
		"login":    "ada",
		"password": "rotated-typed-pw",
		"uuid":     "it_bogusbogusbogusbogusbo",
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(bogus, &fail); err != nil || fail.Error != "choose" {
		t.Fatalf("bogus uuid %s", bogus)
	}
	if patches.Load() != 1 {
		t.Fatal("bogus uuid patched")
	}
}

func TestJSONSaveNeedLoginDoesNotCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	h := NewOrigin(t.TempDir(), srv.URL, "stale")
	h.Confirm = func(string) error {
		t.Fatal("confirm before login")
		return nil
	}
	got := jsonHandle(t, h, map[string]string{
		"action": "save", "url": "https://www.netflix.com/login", "password": typedPassword,
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "need_login" {
		t.Fatalf("%s", got)
	}
}

func TestJSONSaveThenEnrollTotp(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	var enrolls atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/totp/enroll" {
			enrolls.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var in publicapi.FillTOTPEnrollRequest
			if json.Unmarshal(body, &in) != nil || in.TOTPSeed != enrollSeed || in.UUID == "" {
				t.Errorf("enroll %s", body)
			}
		}
		inner.ServeHTTP(w, r)
	})
	h := NewOrigin(t.TempDir(), srv.URL, "human")
	nConfirm := 0
	h.Confirm = func(string) error { nConfirm++; return nil }

	saved := jsonHandle(t, h, map[string]string{
		"action": "save", "url": "https://www.netflix.com/login", "login": "ada", "password": typedPassword,
	})
	var created struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(saved, &created); err != nil || created.UUID == "" {
		t.Fatalf("save %s", saved)
	}

	hotp := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://www.netflix.com/2fa", "otpauth": "otpauth://hotp/x?secret=" + enrollSeed,
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(hotp, &fail); err != nil || fail.Error != "failed" {
		t.Fatalf("hotp %s", hotp)
	}
	if enrolls.Load() != 0 {
		t.Fatal("hotp hit origin")
	}

	cross := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://github.com/settings/security", "otpauth": enrollOTPAuth,
	})
	if err := json.Unmarshal(cross, &fail); err != nil || fail.Error != "choose" {
		t.Fatalf("cross host %s", cross)
	}
	if enrolls.Load() != 0 {
		t.Fatal("cross host enrolled")
	}

	got := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://www.netflix.com/2fa", "otpauth": enrollOTPAuth,
	})
	if scrub.Contains(got, []byte(enrollSeed)) {
		t.Fatalf("enroll leaked seed %s", got)
	}
	var out struct {
		Error   string `json:"error"`
		UUID    string `json:"uuid"`
		HasTOTP bool   `json:"hasTotp"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "" || out.UUID != created.UUID || !out.HasTOTP {
		t.Fatalf("enroll %s", got)
	}
	if enrolls.Load() != 1 || nConfirm != 2 {
		t.Fatalf("enroll origin=%d confirm=%d", enrolls.Load(), nConfirm)
	}

	again := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://www.netflix.com/2fa", "otpauth": enrollOTPAuth,
	})
	if err := json.Unmarshal(again, &fail); err != nil || fail.Error != "choose" {
		t.Fatalf("second enroll %s", again)
	}
	if enrolls.Load() != 1 {
		t.Fatalf("overwrite enroll %d", enrolls.Load())
	}

	filled := jsonHandle(t, h, map[string]string{
		"action": "fill", "url": "https://www.netflix.com/login", "uuid": created.UUID,
	})
	if scrub.Contains(filled, []byte(enrollSeed)) {
		t.Fatalf("fill leaked seed %s", filled)
	}
	var fillOut struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(filled, &fillOut); err != nil || len(fillOut.Entries) != 1 {
		t.Fatalf("fill %s", filled)
	}
	if fillOut.Entries[0].Password != typedPassword || len(fillOut.Entries[0].TOTP) != 6 {
		t.Fatalf("fill entry %+v", fillOut.Entries[0])
	}
}

func TestJSONGenerateThenEnrollTotp(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	gen := jsonHandle(t, h, map[string]string{"action": "generate", "url": "https://signup.example.com/join", "login": "ada"})
	var created struct {
		UUID     string `json:"uuid"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(gen, &created); err != nil || created.UUID == "" || created.Password == "" {
		t.Fatalf("generate %s", gen)
	}
	got := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://signup.example.com/2fa", "otpauth": enrollOTPAuth,
	})
	if scrub.Contains(got, []byte(enrollSeed)) {
		t.Fatalf("enroll leaked seed %s", got)
	}
	var out struct {
		UUID    string `json:"uuid"`
		HasTOTP bool   `json:"hasTotp"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(got, &out); err != nil || out.Error != "" || out.UUID != created.UUID || !out.HasTOTP {
		t.Fatalf("enroll %s", got)
	}
}

func TestJSONEnrollTotpRequiresConfirm(t *testing.T) {
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	var enrolls atomic.Int32
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fill/totp/enroll" {
			enrolls.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	h := allowConfirm(NewOrigin(t.TempDir(), srv.URL, "human"))
	saved := jsonHandle(t, h, map[string]string{
		"action": "save", "url": "https://www.netflix.com/login", "password": typedPassword,
	})
	var created struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(saved, &created); err != nil || created.UUID == "" {
		t.Fatalf("save %s", saved)
	}
	h.Confirm = func(string) error { return errors.New("no") }
	got := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://www.netflix.com/2fa", "otpauth": enrollOTPAuth,
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "canceled" {
		t.Fatalf("denied %s", got)
	}
	if enrolls.Load() != 0 {
		t.Fatal("denied enroll hit origin")
	}
	if scrub.Contains(got, []byte(enrollSeed)) {
		t.Fatal("denied leaked seed")
	}
}

func TestJSONEnrollTotpWithoutSaveIsChoose(t *testing.T) {
	var n atomic.Int32
	h := NewOrigin(t.TempDir(), "http://127.0.0.1:1", "human")
	h.Confirm = func(string) error {
		n.Add(1)
		return nil
	}
	got := jsonHandle(t, h, map[string]string{
		"action": "enrollTotp", "url": "https://www.netflix.com/2fa", "otpauth": enrollOTPAuth,
	})
	var fail struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(got, &fail); err != nil || fail.Error != "choose" {
		t.Fatalf("%s", got)
	}
	if n.Load() != 0 {
		t.Fatal("stray otpauth prompted")
	}
}
