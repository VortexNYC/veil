package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/ssh"

	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/mcpserver"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

const secret = "sk_live_CLI_SECRET"

func run(t *testing.T, home string, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := New("test")
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"--home", home}, args...))
	err := cmd.Execute()
	if err != nil && errb.Len() > 0 {
		return out.String(), err
	}
	return out.String(), err
}

func TestCLILevel2FetchDoesNotPrintSecret(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok:"+r.Header.Get("Authorization"))
	}))
	t.Cleanup(upstream.Close)

	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", upstream.URL, "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatalf("cli printed secret: %s", out)
	}
	var got useDTO
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Body, "ok:") {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestCLILevel1NeedsApprove(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	t.Cleanup(upstream.Close)
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", upstream.URL, "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level1"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	var got useDTO
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionNeedApproval {
		t.Fatalf("%+v", got)
	}
	if _, err := run(t, home, "", "approve", "claude:stripe"); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision != protocol.DecisionAllow {
		t.Fatalf("%+v %s", got, out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatal(out)
	}
}

func TestCLIApproveOIDCNeedsIssuer(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", "https://example.com", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level1"); err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(home, "tok")
	if err := os.WriteFile(tok, []byte("not-a-jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "approve", "claude:stripe", "--oidc-token-file", tok); err == nil {
		t.Fatal("accepted a token with no hydra issuer")
	}
}

func TestCLIAgentHydraNeedsAgent(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	sec := filepath.Join(home, "hydra-secret")
	if _, err := run(t, home, "", "agent", "hydra", "flue", "--secret-file", sec); err == nil {
		t.Fatal("created a hydra client for a missing agent")
	}
}

func TestCLIAgentHydraNeedsSecretFile(t *testing.T) {
	home := t.TempDir()
	// Pin HOME so the canonical default path resolves inside the tempdir —
	// never the operator's real ~/.config/vortex secrets.
	t.Setenv("HOME", home)
	t.Setenv("PWM_HYDRA_SECRET_FILE", "")
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "flue"); err != nil {
		t.Fatal(err)
	}
	// No secret file anywhere: the command must try to provision, and fails
	// only because no admin endpoint is reachable from the test.
	if _, err := run(t, home, "", "agent", "hydra", "flue"); err == nil {
		t.Fatal("expected provisioning failure with no admin endpoint")
	}
}

func TestCLIAgentTokenWritesFileNotStdout(t *testing.T) {
	const hydraSecret = "hydra-agent-secret"
	const jwt = "aaa.bbb.ccc"
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "agent-flue" || pass != hydraSecret {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Fatalf("grant %q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": jwt,
			"token_type":   "bearer",
		})
	}))
	t.Cleanup(issuer.Close)
	t.Setenv("VEIL_HYDRA_ISSUER", issuer.URL)

	home := t.TempDir()
	secFile := filepath.Join(home, "hydra-secret")
	outFile := filepath.Join(home, "jwt")
	if err := os.WriteFile(secFile, []byte(hydraSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "agent", "token", "flue", "--secret-file", secFile, "--out-file", outFile)
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(hydraSecret)) || scrub.Contains([]byte(out), []byte(jwt)) {
		t.Fatalf("token printed secret: %s", out)
	}
	var got struct {
		AgentID  string `json:"agent_id"`
		ClientID string `json:"client_id"`
		OutFile  string `json:"out_file"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	if got.AgentID != "flue" || got.ClientID != "agent-flue" || got.OutFile != outFile {
		t.Fatalf("%+v", got)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != jwt {
		t.Fatalf("out-file %q", raw)
	}
	st, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}

func TestCLIAgentTokenNeedsFiles(t *testing.T) {
	home := t.TempDir()
	// Pin HOME + clear the env override so the default secret path resolves
	// inside the tempdir — never the operator's real secrets.
	t.Setenv("HOME", home)
	t.Setenv("PWM_HYDRA_SECRET_FILE", "")
	if _, err := run(t, home, "", "agent", "token", "flue"); err == nil {
		t.Fatal("accepted token without files")
	}
}

func TestCLIAgentTokenRejectsOpaque(t *testing.T) {
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "ory_at_opaque",
			"token_type":   "bearer",
		})
	}))
	t.Cleanup(issuer.Close)
	t.Setenv("VEIL_HYDRA_ISSUER", issuer.URL)
	home := t.TempDir()
	secFile := filepath.Join(home, "hydra-secret")
	outFile := filepath.Join(home, "jwt")
	if err := os.WriteFile(secFile, []byte("s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "token", "flue", "--secret-file", secFile, "--out-file", outFile); err == nil {
		t.Fatal("accepted opaque")
	}
	if _, err := os.Stat(outFile); err == nil {
		t.Fatal("wrote out-file on opaque token")
	}
}

func TestCLIFillNeedsVault(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "fill"); err == nil {
		t.Fatal("filled without a vault")
	}
}

func TestCLIFillInstallRequiresOrigin(t *testing.T) {
	t.Setenv("VEIL_ORIGIN", "")
	home := t.TempDir()
	if _, err := run(t, home, "", "fill", "install"); err == nil {
		t.Fatal("installed native host without VEIL_ORIGIN")
	}
}

func TestCLIServeNeedsVault(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "serve"); err == nil {
		t.Fatal("served without a vault")
	}
}

func TestCLIItemAddTOTPNeverPrintsSeedOrCode(t *testing.T) {
	const seed = "JBSWY3DPEHPK3PXP"
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	var sawTOTP string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawTOTP = r.Header.Get("X-TOTP")
		_, _ = io.WriteString(w, "otp:"+sawTOTP+" seed:"+seed)
	}))
	t.Cleanup(upstream.Close)

	secFile := filepath.Join(home, "sec")
	totpFile := filepath.Join(home, "totp")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(totpFile, []byte(seed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addOut, err := run(t, home, "", "item", "add", "stripe", "--uri", upstream.URL, "--secret-file", secFile, "--totp-file", totpFile)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(addOut), []byte(seed)) || scrub.Contains([]byte(addOut), []byte(secret)) {
		t.Fatalf("item add printed material: %s", addOut)
	}
	if !strings.Contains(addOut, `"has_totp": true`) {
		t.Fatalf("missing has_totp: %s", addOut)
	}
	listOut, err := run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(listOut), []byte(seed)) {
		t.Fatalf("list printed seed: %s", listOut)
	}

	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}
	// use path uses wall clock; assert the CLI result never contains seed or token.
	out, err := run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(out), []byte(secret)) || scrub.Contains([]byte(out), []byte(seed)) {
		t.Fatalf("cli printed material: %s", out)
	}
	if sawTOTP == "" || len(sawTOTP) != 6 {
		t.Fatalf("upstream did not see a minted code: %q", sawTOTP)
	}
	if scrub.Contains([]byte(out), []byte(sawTOTP)) {
		t.Fatalf("cli printed minted code: %s", out)
	}
}

func TestCLIItemLoginIsMetadataNotSecret(t *testing.T) {
	const login = "stripe@example.com"
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addOut, err := run(t, home, "", "item", "add", "stripe", "--uri", "https://dashboard.stripe.com", "--secret-file", secFile, "--login", login)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(addOut), []byte(secret)) {
		t.Fatalf("item add printed secret: %s", addOut)
	}
	if !scrub.Contains([]byte(addOut), []byte(login)) {
		t.Fatalf("item add omitted login: %s", addOut)
	}
	listOut, err := run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(listOut), []byte(secret)) {
		t.Fatalf("list printed secret: %s", listOut)
	}
	if !scrub.Contains([]byte(listOut), []byte(login)) {
		t.Fatalf("list omitted login: %s", listOut)
	}
	updOut, err := run(t, home, "", "item", "update", "stripe", "--login", "other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(updOut), []byte(secret)) {
		t.Fatalf("update printed secret: %s", updOut)
	}
	if !scrub.Contains([]byte(updOut), []byte("other@example.com")) {
		t.Fatalf("update omitted login: %s", updOut)
	}
}

func TestCLIItemImportCSVNoSecretInOutput(t *testing.T) {
	t.Setenv("VEIL_ORIGIN", "")
	const pass = "s3cret"
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(home, "dump.csv")
	if err := os.WriteFile(csv, []byte("name,url,username,password\nGitHub,https://github.com,ada,"+pass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "item", "import", csv)
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(pass)) {
		t.Fatalf("import printed password: %s", out)
	}
	if !strings.Contains(out, "GitHub") {
		t.Fatalf("import omitted name: %s", out)
	}
	listOut, err := run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(listOut), []byte(pass)) {
		t.Fatalf("list printed password: %s", listOut)
	}
}

func TestCLIItemAddCardNoPANInOutput(t *testing.T) {
	t.Setenv("VEIL_ORIGIN", "")
	const pan = "4111111111111111"
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	num := filepath.Join(home, "pan")
	cvv := filepath.Join(home, "cvv")
	if err := os.WriteFile(num, []byte(pan+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cvv, []byte("123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "item", "add", "amex", "--number-file", num, "--cvv-file", cvv, "--exp-month", "12", "--exp-year", "2030", "--holder", "Ada")
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(pan)) || scrub.Contains([]byte(out), []byte("123")) {
		t.Fatalf("add printed card: %s", out)
	}
	listOut, err := run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(listOut), []byte(pan)) {
		t.Fatalf("list printed pan: %s", listOut)
	}
}

func TestCLIOriginItemImportNoSecretInOutput(t *testing.T) {
	const pass = "s3cret"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-not-a-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/import" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if !scrub.Contains(raw, []byte(pass)) {
			t.Fatal("origin import missing password")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"names":["GitHub"],"count":1}`)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-not-a-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", tok)
	csv := filepath.Join(t.TempDir(), "dump.csv")
	if err := os.WriteFile(csv, []byte("name,url,username,password\nGitHub,https://github.com,ada,"+pass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, t.TempDir(), "", "item", "import", csv)
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(pass)) {
		t.Fatalf("cli printed password: %s", out)
	}
}

func TestCLIRunSetsProxyEnvWithoutVaultSecret(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "run", "--agent", "claude", "--", "env")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "HTTPS_PROXY=http://claude:") {
		t.Fatalf("missing proxy env: %s", out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatal("vault secret in env")
	}
}

func TestCLIRunInjectsGrantedSecretWithoutPrinting(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", "https://api.stripe.com", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}
	wantFile := filepath.Join(home, "want")
	if err := os.WriteFile(wantFile, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "run", "--agent", "claude", "--", "sh", "-c", `test "$STRIPE" = "$(cat "`+wantFile+`")" && printf injected`)
	if err != nil {
		t.Fatal(err, out)
	}
	if strings.TrimSpace(out) != "injected" {
		t.Fatalf("child=%q", out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatalf("broker printed secret: %s", out)
	}
	list, err := run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(list), []byte(secret)) {
		t.Fatal("item list leaked")
	}
}

func TestCLIRunSkipsLevel1WithoutPrompt(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "bank", "--uri", "https://bank.example", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "bank", "--level", "level1"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "run", "--agent", "claude", "--", "sh", "-c", `test -z "$BANK" && printf skipped`)
	if err != nil {
		t.Fatal(err, out)
	}
	if strings.TrimSpace(out) != "skipped" {
		t.Fatalf("child=%q", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("printed secret: %s", out)
	}
}

func TestCLIUseBodyFileDoesNotPrintSecret(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	var saw string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		saw = string(raw)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", upstream.URL, "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}
	bodyFile := filepath.Join(home, "body.json")
	if err := os.WriteFile(bodyFile, []byte(`{"email":"a@b.c"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL+"/v1", "--method", "POST", "--header", "Content-Type: application/json", "--body-file", bodyFile)
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatalf("cli printed secret: %s", out)
	}
	if saw != `{"email":"a@b.c"}` {
		t.Fatalf("upstream %q", saw)
	}
}

func TestCLIRunInjectFileDoesNotPrintSecret(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", "https://api.stripe.com", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}
	tmpl := filepath.Join(home, "tmpl")
	if err := os.WriteFile(tmpl, []byte("token=${STRIPE}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, "out.env")
	out, err := run(t, home, "", "run", "--agent", "claude", "--inject", tmpl+":"+dest, "--", "true")
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatalf("broker printed secret: %s", out)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "token="+secret+"\n" {
		t.Fatalf("%q", got)
	}
	bad := filepath.Join(home, "bad")
	if err := os.WriteFile(bad, []byte("${MISSING}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "run", "--agent", "claude", "--inject", bad+":"+filepath.Join(home, "nope"), "--", "true"); err == nil {
		t.Fatal("unknown ref")
	}
}

func TestCLIAuditHasNoSecret(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", "https://api.stripe.com", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "run", "--agent", "claude", "--", "true"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "audit")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatalf("audit leaked: %s", out)
	}
}

func TestCLIGenOutFileNotJSONSecret(t *testing.T) {
	home := t.TempDir()
	dest := filepath.Join(home, "pw")
	out, err := run(t, home, "", "gen", "--length", "12", "--out-file", dest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	pw := strings.TrimSpace(string(got))
	if len(pw) != 12 {
		t.Fatalf("%q", got)
	}
	if strings.Contains(out, pw) {
		t.Fatalf("cli printed password: %s", out)
	}
}

func TestCLITotpEnrollWritesFilesNotStdout(t *testing.T) {
	home := t.TempDir()
	seedFile := filepath.Join(home, "seed")
	qrFile := filepath.Join(home, "qr.png")
	out, err := run(t, home, "", "totp", "enroll", "--account", "stripe", "--out-file", seedFile, "--qr-file", qrFile)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile(seedFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(seed))) < 16 {
		t.Fatalf("seed %q", seed)
	}
	if scrub.Contains([]byte(out), seed) || strings.Contains(out, "otpauth") {
		t.Fatalf("enroll printed seed: %s", out)
	}
	st, err := os.Stat(seedFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("seed mode %o", st.Mode().Perm())
	}
	png, err := os.ReadFile(qrFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatal("qr is not png")
	}
	if _, err := run(t, home, "", "totp", "enroll", "--account", "stripe"); err == nil {
		t.Fatal("enroll without files")
	}
}

func TestCLITotpEnrollHasNoSeedArgv(t *testing.T) {
	cmd := New("test")
	for _, c := range cmd.Commands() {
		if c.Name() != "totp" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() != "enroll" {
				continue
			}
			if sub.Flags().Lookup("seed") != nil || sub.Flags().Lookup("totp") != nil || sub.Flags().Lookup("otpauth") != nil {
				t.Fatal("seed must not be argv")
			}
			if sub.Flags().Lookup("out-file") == nil || sub.Flags().Lookup("qr-file") == nil {
				t.Fatal("missing file flags")
			}
			return
		}
	}
	t.Fatal("missing totp enroll")
}

func TestCLIRejectsSecretOnArgvPattern(t *testing.T) {
	cmd := New("test")
	itemAdd := cmd.Commands()
	_ = itemAdd
	// --secret must not exist. --secret-file is the only path.
	for _, c := range cmd.Commands() {
		if c.Name() != "item" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() != "add" {
				continue
			}
			if sub.Flags().Lookup("secret") != nil {
				t.Fatal("do not accept --secret on argv")
			}
			if sub.Flags().Lookup("totp") != nil {
				t.Fatal("do not accept --totp on argv")
			}
			if sub.Flags().Lookup("secret-file") == nil {
				t.Fatal("missing --secret-file")
			}
			if sub.Flags().Lookup("ssh") != nil {
				t.Fatal("do not accept --ssh on argv")
			}
			if sub.Flags().Lookup("ssh-file") == nil {
				t.Fatal("missing --ssh-file")
			}
			if sub.Flags().Lookup("totp-file") == nil {
				t.Fatal("missing --totp-file")
			}
			if sub.Flags().Lookup("refresh") != nil || sub.Flags().Lookup("client-secret") != nil {
				t.Fatal("do not accept oauth secrets on argv")
			}
			if sub.Flags().Lookup("refresh-file") == nil {
				t.Fatal("missing --refresh-file")
			}
			if sub.Flags().Lookup("file") == nil {
				t.Fatal("missing --file")
			}
		}
	}
	for _, c := range cmd.Commands() {
		if c.Name() != "gen" {
			continue
		}
		if c.Flags().Lookup("secret") != nil {
			t.Fatal("do not accept --secret on gen")
		}
		if c.Flags().Lookup("out-file") == nil {
			t.Fatal("missing --out-file")
		}
	}
	for _, c := range cmd.Commands() {
		if c.Name() != "agent" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() != "token" {
				continue
			}
			if sub.Flags().Lookup("secret") != nil || sub.Flags().Lookup("token") != nil {
				t.Fatal("do not accept --secret or --token on argv")
			}
			if sub.Flags().Lookup("secret-file") == nil || sub.Flags().Lookup("out-file") == nil {
				t.Fatal("missing --secret-file or --out-file")
			}
		}
	}
	for _, c := range cmd.Commands() {
		if c.Name() != "session" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() != "create" {
				continue
			}
			if sub.Flags().Lookup("token") != nil || sub.Flags().Lookup("secret") != nil {
				t.Fatal("do not accept --token or --secret on session create")
			}
			if sub.Flags().Lookup("out-file") == nil {
				t.Fatal("missing --out-file on session create")
			}
		}
	}
	for _, c := range cmd.Commands() {
		if c.Name() != "use" {
			continue
		}
		if c.Flags().Lookup("body") != nil {
			t.Fatal("do not accept --body on argv")
		}
		if c.Flags().Lookup("body-file") == nil {
			t.Fatal("missing --body-file")
		}
	}
	for _, c := range cmd.Commands() {
		if c.Name() != "human" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() != "invite" {
				continue
			}
			if sub.Flags().Lookup("code") != nil {
				t.Fatal("do not accept --code on argv")
			}
			if sub.Flags().Lookup("code-file") == nil {
				t.Fatal("missing --code-file")
			}
		}
	}
}

func TestCLISSHItemListHasNoPrivateKey(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(block)
	keyFile := filepath.Join(home, "id_ed25519")
	if err := os.WriteFile(keyFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "item", "add", "github", "--ssh-file", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(out), pemBytes) || strings.Contains(out, "PRIVATE KEY") {
		t.Fatalf("cli printed private key: %s", out)
	}
	var item protocol.Item
	if err := json.Unmarshal([]byte(out), &item); err != nil {
		t.Fatal(err, out)
	}
	if item.Kind != protocol.ItemSSH {
		t.Fatalf("kind %q", item.Kind)
	}
	list, err := run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(list), pemBytes) || strings.Contains(list, "PRIVATE KEY") {
		t.Fatalf("item list leaked pem: %s", list)
	}
}

func TestCLIHumanListHasNoEmail(t *testing.T) {
	t.Setenv("VEIL_KRATOS_ADMIN", "http://127.0.0.1:1")
	t.Setenv("VEIL_KRATOS_PUBLIC", "http://127.0.0.1:1")
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "human", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "self") {
		t.Fatalf("missing planted human: %s", out)
	}
	if strings.Contains(out, "@") {
		t.Fatalf("email in vault list: %s", out)
	}
}

func TestCLIDevicePairOpensSecondHomeAndDoesNotPrintMaster(t *testing.T) {
	src := t.TempDir()
	if _, err := run(t, src, "", "init"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok:"+r.Header.Get("Authorization"))
	}))
	t.Cleanup(upstream.Close)
	secFile := filepath.Join(src, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, src, "", "item", "add", "stripe", "--uri", upstream.URL, "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, src, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, src, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	keyFile := filepath.Join(work, "device.key")
	pubFile := filepath.Join(work, "device.pub")
	wrapFile := filepath.Join(work, "wrap.bin")
	newOut, err := run(t, src, "", "device", "new", "--key-file", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	master := mustMaster(t, src)
	if scrub.Contains([]byte(newOut), priv) || scrub.Contains([]byte(newOut), master) {
		t.Fatalf("device new leaked: %s", newOut)
	}
	if _, err := run(t, src, "", "device", "pubkey", "--key-file", keyFile, "--out-file", pubFile); err != nil {
		t.Fatal(err)
	}
	offerOut, err := run(t, src, "", "device", "offer", "--pubkey-file", pubFile, "--to-file", wrapFile)
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := os.ReadFile(wrapFile)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(wrap, master) || scrub.Contains([]byte(offerOut), master) {
		t.Fatalf("offer leaked master: %s", offerOut)
	}

	dst := t.TempDir()
	for _, name := range []string{"vault.db", "config.json"} {
		raw, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	acceptOut, err := run(t, dst, "", "device", "accept", "--key-file", keyFile, "--from-file", wrapFile)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(acceptOut), master) || scrub.Contains([]byte(acceptOut), priv) {
		t.Fatalf("accept leaked: %s", acceptOut)
	}
	if _, err := os.Stat(filepath.Join(dst, "master.key")); err == nil {
		t.Fatal("accept wrote plaintext master.key")
	}
	out, err := run(t, dst, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains([]byte(out), []byte(secret)) || scrub.Contains([]byte(out), master) {
		t.Fatalf("cli printed secret: %s", out)
	}
}

func TestCLIMCPConfigNoSecret(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("VEIL_MCP_URL", "")
	home := t.TempDir()
	out, err := run(t, home, "", "mcp", "config")
	if err != nil {
		t.Fatal(err, out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatalf("mcp config leaked secret: %s", out)
	}
	var got struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	if got.URL != "http://127.0.0.1:4461/mcp" {
		t.Fatalf("url %q", got.URL)
	}
	if got.Headers["Authorization"] != "Bearer ${VEIL_OIDC_TOKEN}" {
		t.Fatalf("headers %v", got.Headers)
	}
}

func TestCLIMCPConfigRailwayPORT(t *testing.T) {
	t.Setenv("PORT", "4461")
	t.Setenv("VEIL_MCP_URL", "https://pwm-production.up.railway.app/mcp")
	home := t.TempDir()
	out, err := run(t, home, "", "mcp", "config")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "https://pwm-production.up.railway.app/mcp") {
		t.Fatalf("url %s", out)
	}
}

func TestCLIMCPConfigPublicURL(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("VEIL_MCP_URL", "https://veil.nyc/mcp")
	home := t.TempDir()
	out, err := run(t, home, "", "mcp", "config")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "https://veil.nyc/mcp") {
		t.Fatalf("url %s", out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatal("mcp config leaked secret")
	}
}

func TestCLIOriginItemGrantNoLocalVault(t *testing.T) {
	var sawCreate, sawList, sawGrant bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer jwt-not-a-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/items":
			sawCreate = true
			raw, _ := io.ReadAll(r.Body)
			if !scrub.Contains(raw, []byte(secret)) {
				t.Fatal("origin create missing secret")
			}
			if !bytes.Contains(raw, []byte("user@example.com")) {
				t.Fatal("origin create missing login")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"github","org_id":"org","name":"github","kind":"api_key","owner":{"kind":"org","id":"org"},"uris":["https://api.github.com"]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/items":
			sawList = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[{"id":"github","org_id":"org","name":"github","kind":"api_key","owner":{"kind":"org","id":"org"},"uris":["https://api.github.com"]}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/grants":
			sawGrant = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"claude:github","org_id":"org","agent_id":"claude","item_id":"github","level":"level2"}`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-not-a-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", tok)
	t.Setenv("VEIL_OIDC_TOKEN_FILE", "")
	t.Setenv("VEIL_OIDC_TOKEN", "")
	home := t.TempDir()
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "item", "add", "github", "--uri", "https://api.github.com", "--secret-file", secFile, "--login", "user@example.com")
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawCreate {
		t.Fatal("did not hit origin POST /v1/items")
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatal("cli origin item add printed secret")
	}
	if strings.Contains(out, "user@example.com") {
		t.Fatal("cli origin item add printed login")
	}
	out, err = run(t, home, "", "item", "list")
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawList {
		t.Fatal("did not hit origin GET /v1/items")
	}
	if !strings.Contains(out, `"name": "github"`) {
		t.Fatalf("list %s", out)
	}
	out, err = run(t, home, "", "grant", "add", "--agent", "claude", "--item", "github", "--level", "level2")
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawGrant {
		t.Fatal("did not hit origin POST /v1/grants")
	}
}

func TestCLIItemUpdateURIAddsWithoutDropping(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "github", "--uri", "https://api.github.com", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, home, "", "item", "update", "github", "--uri", "https://github.com")
	if err != nil {
		t.Fatal(err, out)
	}
	var item protocol.Item
	if err := json.Unmarshal([]byte(out), &item); err != nil {
		t.Fatal(err, out)
	}
	if len(item.URIs) != 2 || item.URIs[0] != "https://api.github.com" || item.URIs[1] != "https://github.com" {
		t.Fatalf("update dropped a host: %+v", item.URIs)
	}
}

func TestCLIOriginItemUpdateURIAdds(t *testing.T) {
	var patches int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-not-a-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/items/github" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		patches++
		raw, _ := io.ReadAll(r.Body)
		if !bytes.Contains(raw, []byte(`"uri":"https://github.com"`)) {
			t.Fatalf("patch body %s", raw)
		}
		if bytes.Contains(raw, []byte(`"uris"`)) {
			t.Fatal("cli sent replace uris")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"github","org_id":"org","name":"github","kind":"api_key","owner":{"kind":"org","id":"org"},"uris":["https://api.github.com","https://github.com"]}`)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-not-a-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", tok)
	t.Setenv("VEIL_OIDC_TOKEN_FILE", "")
	t.Setenv("VEIL_OIDC_TOKEN", "")
	home := t.TempDir()
	out, err := run(t, home, "", "item", "update", "github", "--uri", "https://github.com")
	if err != nil {
		t.Fatal(err, out)
	}
	if patches != 1 {
		t.Fatalf("patches %d", patches)
	}
	if !strings.Contains(out, `"https://api.github.com"`) || !strings.Contains(out, `"https://github.com"`) {
		t.Fatalf("out %s", out)
	}
}

func TestCLIGrantHumanXORAgent(t *testing.T) {
	cmd := New("test")
	for _, c := range cmd.Commands() {
		if c.Name() != "grant" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() != "add" {
				continue
			}
			if sub.Flags().Lookup("human") == nil {
				t.Fatal("missing --human")
			}
			if sub.Flags().Lookup("agent") == nil {
				t.Fatal("missing --agent")
			}
			home := t.TempDir()
			out, err := run(t, home, "", "init")
			if err != nil {
				t.Fatal(err, out)
			}
			out, err = run(t, home, "", "grant", "add", "--item", "github", "--level", "level2")
			if err == nil {
				t.Fatalf("neither agent nor human: %s", out)
			}
			out, err = run(t, home, "", "grant", "add", "--agent", "claude", "--human", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "--item", "github", "--level", "level2")
			if err == nil {
				t.Fatalf("both: %s", out)
			}
			return
		}
	}
	t.Fatal("missing grant add")
}

func TestCLIOriginHumanGrant(t *testing.T) {
	const member = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	var sawHuman bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-not-a-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/grants" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte(`"agent"`)) && bytes.Contains(raw, []byte("claude")) {
			t.Fatal("human grant posted agent")
		}
		if !bytes.Contains(raw, []byte(member)) || !bytes.Contains(raw, []byte(`"human"`)) {
			t.Fatalf("origin grant body %s", raw)
		}
		sawHuman = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"`+member+`:github","org_id":"org","agent_id":"`+member+`","item_id":"github","level":"level2"}`)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-not-a-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", tok)
	home := t.TempDir()
	out, err := run(t, home, "", "grant", "add", "--human", member, "--item", "github", "--level", "level2")
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawHuman {
		t.Fatal("did not hit origin POST /v1/grants")
	}
	if !strings.Contains(out, member) {
		t.Fatalf("cli %s", out)
	}
}

func TestCLIOriginRefusesSecondVault(t *testing.T) {
	t.Setenv("VEIL_ORIGIN", "https://veil.nyc")
	home := t.TempDir()
	// init against an origin provisions — it needs a human token, not a vault.
	out, err := run(t, home, "", "init")
	if err == nil {
		t.Fatalf("init with origin: %s", out)
	}
	if !strings.Contains(err.Error(), "TOKEN") {
		t.Fatal(err)
	}
	out, err = run(t, home, "", "serve")
	if err == nil {
		t.Fatalf("serve with origin: %s", out)
	}
	if !strings.Contains(err.Error(), "origin is the vault") {
		t.Fatal(err)
	}
}

func TestCLIInitProvisionsOnOrigin(t *testing.T) {
	var sawProvision bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/provision" {
			sawProvision = true
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-human") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"subject":"sub-1","org_id":"org-1"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer origin.Close()
	t.Setenv("VEIL_ORIGIN", origin.URL)
	t.Setenv("VEIL_HUMAN_TOKEN", "tok-human")
	out, err := run(t, t.TempDir(), "", "init")
	if err != nil {
		t.Fatalf("init provision: %v %s", err, out)
	}
	if !sawProvision {
		t.Fatal("init never called /v1/provision")
	}
	if !strings.Contains(out, "org-1") {
		t.Fatalf("init output: %s", out)
	}
}

func TestCLIOriginUseAndAudit(t *testing.T) {
	var sawUse, sawEvents bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/use":
			sawUse = true
			raw, _ := io.ReadAll(r.Body)
			if scrub.Contains(raw, []byte(secret)) {
				t.Fatal("origin use request leaked secret")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"decision":"allow","status":200,"body":"ok"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/events":
			sawEvents = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"events":[{"time":"2026-09-10T00:00:00Z","org_id":"org","agent_id":"cursor","item_id":"github","action":"fetch","decision":"allow"}]}`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-not-a-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	out, err := run(t, home, "", "use", "--oidc-token-file", tok, "--item", "github", "--url", "https://api.github.com/user")
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawUse {
		t.Fatal("did not hit origin /v1/use")
	}
	if !strings.Contains(out, `"decision": "allow"`) {
		t.Fatalf("use %s", out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatal("cli origin use leaked secret")
	}
	out, err = run(t, home, "", "audit", "--oidc-token-file", tok)
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawEvents {
		t.Fatal("did not hit origin /v1/events")
	}
	if !strings.Contains(out, `"item_id": "github"`) {
		t.Fatalf("audit %s", out)
	}
}

func TestMCPStdioOriginListAndFetch(t *testing.T) {
	var sawList, sawUse bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-not-a-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/items":
			sawList = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[{"id":"github","name":"github","uris":["https://api.github.com"]}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/use":
			sawUse = true
			raw, _ := io.ReadAll(r.Body)
			if scrub.Contains(raw, []byte(secret)) {
				t.Fatal("stdio fetch request leaked secret")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"decision":"allow","status":200,"body":"{\"login\":\"veil\"}"}`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	t.Setenv("VEIL_OIDC_TOKEN", "jwt-not-a-secret")
	t.Setenv("VEIL_OIDC_TOKEN_FILE", "")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	pr, pw := io.Pipe()
	cr, cw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close(); _ = cr.Close(); _ = cw.Close() })
	errc := make(chan error, 1)
	go func() {
		errc <- originMCPServer().Run(ctx, &mcp.IOTransport{Reader: pr, Writer: cw})
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.IOTransport{Reader: cr, Writer: pw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	list, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_items"})
	if err != nil {
		t.Fatal(err)
	}
	if list.IsError {
		t.Fatalf("list_items error: %+v", list)
	}
	listRaw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(listRaw, []byte(secret)) {
		t.Fatal("stdio list leaked secret")
	}
	if !bytes.Contains(listRaw, []byte("github")) {
		t.Fatalf("list missing github: %s", listRaw)
	}

	fetch, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "fetch",
		Arguments: mcpserver.FetchIn{Item: "github", URL: "https://api.github.com/user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetch.IsError {
		t.Fatalf("fetch error: %+v", fetch)
	}
	fetchRaw, err := json.Marshal(fetch)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(fetchRaw, []byte(secret)) {
		t.Fatal("stdio fetch leaked secret")
	}
	if !bytes.Contains(fetchRaw, []byte(`"decision":"allow"`)) {
		t.Fatalf("fetch %s", fetchRaw)
	}
	if !sawList || !sawUse {
		t.Fatalf("origin hits list=%t use=%t", sawList, sawUse)
	}
}

func TestCLIMCPLaptopNoSecret(t *testing.T) {
	home := t.TempDir()
	out, err := run(t, home, "", "mcp", "laptop")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, `"mcp"`) || !strings.Contains(out, `"stdio"`) {
		t.Fatalf("laptop %s", out)
	}
	if scrub.Contains([]byte(out), []byte(secret)) {
		t.Fatal("mcp laptop leaked secret")
	}
}

func testJWT(sub string, exp time.Time) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q,"exp":%d}`, sub, exp.Unix())))
	return hdr + "." + payload + ".sig"
}

func TestJWTNeedsRefresh(t *testing.T) {
	if jwtNeedsRefresh("jwt-not-a-secret") {
		t.Fatal("opaque token must not remint")
	}
	if jwtNeedsRefresh(testJWT("agent-cursor", time.Now().Add(time.Hour))) {
		t.Fatal("fresh jwt remint")
	}
	if !jwtNeedsRefresh(testJWT("agent-cursor", time.Now().Add(-time.Minute))) {
		t.Fatal("expired jwt skipped")
	}
	if jwtNeedsRefresh("ses_" + strings.Repeat("ab", 32)) {
		t.Fatal("session token must not remint as a JWT")
	}
}

func TestCLISessionCreateWritesFileNotStdout(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(home, "session.jwt")
	out, err := run(t, home, "", "session", "create", "claude", "--out-file", outFile)
	if err != nil {
		t.Fatal(err, out)
	}
	if strings.Contains(out, "ses_") {
		t.Fatalf("cli printed session token: %s", out)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(tok, "ses_") {
		t.Fatalf("out-file %q", tok)
	}
	listed, err := run(t, home, "", "session", "list")
	if err != nil {
		t.Fatal(err, listed)
	}
	if strings.Contains(listed, tok) {
		t.Fatal("session list printed token")
	}
}

func TestOriginRemintsExpiredJWT(t *testing.T) {
	const hydraSecret = "hydra-agent-secret"
	fresh := testJWT("agent-cursor", time.Now().Add(time.Hour))
	var minted int
	hydra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			http.NotFound(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "agent-cursor" || pass != hydraSecret {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		minted++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fresh,
			"token_type":   "bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(hydra.Close)
	var sawAuth string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/use" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"decision":"allow","status":200,"body":"ok"}`)
	}))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	tok := filepath.Join(dir, "cursor.jwt")
	sec := filepath.Join(dir, "cursor.hydra")
	if err := os.WriteFile(tok, []byte(testJWT("agent-cursor", time.Now().Add(-time.Hour))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sec, []byte(hydraSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ORIGIN", origin.URL)
	t.Setenv("VEIL_HYDRA_ISSUER", hydra.URL)
	t.Setenv("VEIL_HYDRA_SECRET_FILE", sec)
	t.Setenv("VEIL_AGENT", "cursor")
	t.Setenv("VEIL_OIDC_TOKEN_FILE", tok)
	t.Setenv("VEIL_OIDC_TOKEN", "")

	out, err := run(t, t.TempDir(), "", "use", "--item", "github", "--url", "https://api.github.com/user")
	if err != nil {
		t.Fatal(err, out)
	}
	if minted != 1 {
		t.Fatalf("minted %d", minted)
	}
	if sawAuth != "Bearer "+fresh {
		t.Fatalf("auth %q", sawAuth)
	}
	got, err := os.ReadFile(tok)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != fresh {
		t.Fatal("did not persist reminted jwt")
	}
	if scrub.Contains([]byte(out), []byte(hydraSecret)) {
		t.Fatal("cli printed hydra secret")
	}
}

func TestOriginDoesNotRemintFreshJWT(t *testing.T) {
	fresh := testJWT("agent-cursor", time.Now().Add(time.Hour))
	hydra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("hydra token endpoint must not be called")
	}))
	t.Cleanup(hydra.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fresh {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"decision":"allow","status":200,"body":"ok"}`)
	}))
	t.Cleanup(origin.Close)
	dir := t.TempDir()
	tok := filepath.Join(dir, "cursor.jwt")
	if err := os.WriteFile(tok, []byte(fresh+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ORIGIN", origin.URL)
	t.Setenv("VEIL_HYDRA_ISSUER", hydra.URL)
	t.Setenv("VEIL_HYDRA_SECRET_FILE", filepath.Join(dir, "missing.hydra"))
	t.Setenv("VEIL_OIDC_TOKEN_FILE", tok)
	t.Setenv("VEIL_OIDC_TOKEN", "")
	out, err := run(t, t.TempDir(), "", "use", "--item", "github", "--url", "https://api.github.com/user")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, `"decision": "allow"`) {
		t.Fatalf("use %s", out)
	}
}

func TestCLIHumanLoginWritesTokenNotStdout(t *testing.T) {
	const idTok = "jwt-human-id-token"
	var hydraURL string
	hydra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 hydraURL,
				"authorization_endpoint": hydraURL + "/oauth2/auth",
				"token_endpoint":         hydraURL + "/oauth2/token",
				"jwks_uri":               hydraURL + "/keys",
			})
		case "/oauth2/auth":
			redir := r.URL.Query().Get("redirect_uri")
			state := r.URL.Query().Get("state")
			http.Redirect(w, r, redir+"?code=ok&state="+state, http.StatusFound)
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
				http.Error(w, "pkce", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "not-an-id-token",
				"token_type":   "bearer",
				"id_token":     idTok,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	hydraURL = hydra.URL
	t.Cleanup(hydra.Close)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	redir := "http://" + ln.Addr().String() + "/oidc/callback"
	_ = ln.Close()
	t.Setenv("VEIL_HYDRA_ISSUER", hydra.URL)
	t.Setenv("VEIL_HYDRA_REDIRECT", redir)
	outFile := filepath.Join(t.TempDir(), "human.jwt")
	cmd := New("test")
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetContext(context.Background())
	if err := humanLogin(cmd, outFile, func(authURL string) error {
		res, err := http.Get(authURL)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}); err != nil {
		t.Fatal(err, errb.String())
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != idTok {
		t.Fatalf("file %q", got)
	}
	if strings.Contains(out.String(), idTok) {
		t.Fatal("cli printed the id token")
	}
}

func TestCLIOriginRunDummyEnvNotSecret(t *testing.T) {
	var listed bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-agent" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/items" {
			listed = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[{"id":"stripe","org_id":"org","name":"stripe","kind":"api_key","owner":{"kind":"org","id":"org"},"uris":["https://api.stripe.com"]}]}`)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_OIDC_TOKEN_FILE", tok)
	t.Setenv("VEIL_OIDC_TOKEN", "")
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", "")
	t.Setenv("VEIL_HUMAN_TOKEN", "")
	home := t.TempDir()
	out, err := run(t, home, "", "run", "--agent", "cursor", "--", "sh", "-c", `printf %s "$STRIPE"`)
	if err != nil {
		t.Fatal(err, out)
	}
	if !listed {
		t.Fatal("did not hit origin GET /v1/items")
	}
	if strings.TrimSpace(out) != "veil-inject" {
		t.Fatalf("child env %q", out)
	}
	if strings.Contains(out, secret) {
		t.Fatal("secret in child")
	}
}

func TestCLIOriginRunInjectRefused(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_OIDC_TOKEN_FILE", tok)
	home := t.TempDir()
	tmpl := filepath.Join(home, "tmpl")
	if err := os.WriteFile(tmpl, []byte("${STRIPE}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, home, "", "run", "--agent", "cursor", "--inject", tmpl+":"+filepath.Join(home, "out"), "--", "true")
	if err == nil {
		t.Fatal("origin --inject")
	}
	if !strings.Contains(err.Error(), "HTTPS_PROXY") {
		t.Fatalf("%v", err)
	}
}

func TestCLIOriginHumanExpiredNeedsLogin(t *testing.T) {
	expired := "eyJhbGciOiJub25lIn0.eyJleHAiOjF9."
	t.Setenv("VEIL_ORIGIN", "https://veil.nyc")
	t.Setenv("VEIL_HUMAN_TOKEN", expired)
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", "")
	t.Setenv("VEIL_OIDC_TOKEN", "")
	t.Setenv("VEIL_OIDC_TOKEN_FILE", "")
	t.Setenv("VEIL_LOGIN_EMAIL", "")
	t.Setenv("VEIL_KRATOS_PASSWORD_FILE", "")
	t.Setenv("VEIL_KRATOS_TOTP_FILE", "")
	_, err := originHumanTokenLive(context.Background())
	if err == nil {
		t.Fatal("expired human token")
	}
	if !strings.Contains(err.Error(), "human login --out-file") {
		t.Fatalf("%v", err)
	}
}

func mustMaster(t *testing.T, dir string) []byte {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "master.key")); err == nil {
		t.Fatal("plaintext master.key")
	}
	priv, err := os.ReadFile(filepath.Join(dir, "device.key"))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := device.Public(priv)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(filepath.Join(dir, "wraps", hex.EncodeToString(pub)))
	if err != nil {
		t.Fatal(err)
	}
	master, err := device.Accept(blob, priv)
	if err != nil {
		t.Fatal(err)
	}
	return master
}

func TestCLIAgentRevokeKillsUse(t *testing.T) {
	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "item", "add", "stripe", "--uri", upstream.URL, "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "grant", "add", "--agent", "claude", "--item", "stripe", "--level", "level2"); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	var before useDTO
	if err := json.Unmarshal([]byte(out), &before); err != nil {
		t.Fatal(err)
	}
	if before.Decision != protocol.DecisionAllow {
		t.Fatalf("before: %+v", before)
	}

	out, err = run(t, home, "", "agent", "revoke", "--id", "claude")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "revoked_at") {
		t.Fatalf("revoke: %s", out)
	}

	out, err = run(t, home, "", "use", "--agent", "claude", "--item", "stripe", "--url", upstream.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	var after useDTO
	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatal(err)
	}
	if after.Decision != protocol.DecisionDeny || after.Reason != "agent_revoked" {
		t.Fatalf("after: %+v", after)
	}
}

func TestCLIOriginAgentRevoke(t *testing.T) {
	var sawRevoke bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/agents/flue/revoke" {
			sawRevoke = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"flue","org_id":"org","revoked_at":"2026-09-10T00:00:00Z"}`)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("VEIL_ORIGIN", origin.URL)
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("jwt-not-a-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_HUMAN_TOKEN_FILE", tok)
	home := t.TempDir()
	out, err := run(t, home, "", "agent", "revoke", "--id", "flue")
	if err != nil {
		t.Fatal(err, out)
	}
	if !sawRevoke {
		t.Fatal("did not hit origin POST /v1/agents/flue/revoke")
	}
	if !strings.Contains(out, "revoked_at") {
		t.Fatalf("cli: %s", out)
	}
}

func TestResolveHomeMigratesLegacyDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("VEIL_HOME", "")
	legacy := filepath.Join(dir, ".password-manager")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := resolveHome("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".veil"); got != want {
		t.Fatalf("home = %q, want %q", got, want)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy dir still present after migration")
	}
}

func TestResolveHomeKeepsVeilWhenBothExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("VEIL_HOME", "")
	for _, d := range []string{".veil", ".password-manager"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err := resolveHome("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".veil"); got != want {
		t.Fatalf("home = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, ".password-manager")); err != nil {
		t.Fatal("legacy dir must not be touched when .veil exists")
	}
}

// fakeHydra serves both the public token endpoint and the admin client
// endpoints. liveSecret mints; any other secret gets invalid_client.
// putCalls counts client rotations — the whole point of these tests.
func fakeHydra(t *testing.T, liveSecret, rotatedSecret string, putCalls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/oauth2/token":
			user, pass, ok := r.BasicAuth()
			if !ok || pass != liveSecret || !strings.HasPrefix(user, "agent-") {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"access_token": "aaa.bbb.ccc", "token_type": "bearer",
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/clients/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": strings.TrimPrefix(r.URL.Path, "/admin/clients/")})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/admin/clients/"):
			*putCalls++
			id := strings.TrimPrefix(r.URL.Path, "/admin/clients/")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"client_id":                  id,
				"client_secret":              rotatedSecret,
				"grant_types":                []string{"client_credentials"},
				"audience":                   []string{"password-manager"},
				"access_token_strategy":      "jwt",
				"token_endpoint_auth_method": "client_secret_basic",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCLIAgentHydraSkipsRotationWhenSecretVerifies(t *testing.T) {
	var puts int
	srv := fakeHydra(t, "live-secret", "rotated-secret", &puts)
	t.Setenv("VEIL_HYDRA_ISSUER", srv.URL)
	t.Setenv("VEIL_HYDRA_ADMIN", srv.URL)
	t.Setenv("PWM_HYDRA_SECRET_FILE", "")

	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "codex"); err != nil {
		t.Fatal(err)
	}
	// Canonical default path under HOME — no --secret-file flag.
	secFile := filepath.Join(home, ".config", "vortex", "pwm-railway", "codex.hydra")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Dir(secFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secFile, []byte("live-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, home, "", "agent", "hydra", "codex"); err != nil {
		t.Fatal(err)
	}
	if puts != 0 {
		t.Fatalf("verified secret still rotated: puts=%d", puts)
	}
	raw, _ := os.ReadFile(secFile)
	if strings.TrimSpace(string(raw)) != "live-secret" {
		t.Fatal("secret file rewritten during a verify-only run")
	}
}

func TestCLIAgentHydraRotatesWhenSecretStale(t *testing.T) {
	var puts int
	srv := fakeHydra(t, "live-secret", "rotated-secret", &puts)
	t.Setenv("VEIL_HYDRA_ISSUER", srv.URL)
	t.Setenv("VEIL_HYDRA_ADMIN", srv.URL)

	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "codex"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "stale.hydra")
	if err := os.WriteFile(secFile, []byte("stale-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "hydra", "codex", "--secret-file", secFile); err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("stale secret did not rotate: puts=%d", puts)
	}
	raw, _ := os.ReadFile(secFile)
	if strings.TrimSpace(string(raw)) != "rotated-secret" {
		t.Fatal("rotated secret not written back to file")
	}
}

func TestCLIAgentHydraRefusesRotateWhenIssuerUnreachable(t *testing.T) {
	var puts int
	srv := fakeHydra(t, "live-secret", "rotated-secret", &puts)
	admin := srv.URL
	srv.Close() // issuer unreachable; admin URL kept so the fake still counts PUTs if hit
	t.Setenv("VEIL_HYDRA_ISSUER", srv.URL)
	t.Setenv("VEIL_HYDRA_ADMIN", admin)

	home := t.TempDir()
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "codex"); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "maybe-live.hydra")
	if err := os.WriteFile(secFile, []byte("maybe-live\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "hydra", "codex", "--secret-file", secFile); err == nil {
		t.Fatal("rotated despite unverifiable secret")
	}
	if puts != 0 {
		t.Fatalf("network error triggered rotation: puts=%d", puts)
	}
	raw, _ := os.ReadFile(secFile)
	if strings.TrimSpace(string(raw)) != "maybe-live" {
		t.Fatal("secret file clobbered on failed verify")
	}
}

// PWM_HYDRA_SECRET_FILE describes the ambient agent (PWM_AGENT). Running
// `agent hydra OTHER` must not read — and on rotation, must not overwrite —
// that agent's file.
func TestCLIAgentHydraIgnoresForeignSecretEnv(t *testing.T) {
	var puts int
	srv := fakeHydra(t, "live-secret", "rotated-secret", &puts)
	t.Setenv("VEIL_HYDRA_ISSUER", srv.URL)
	t.Setenv("VEIL_HYDRA_ADMIN", srv.URL)

	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "codex"); err != nil {
		t.Fatal(err)
	}

	// Ambient agent is cursor; its secret file must not be consulted or
	// written when provisioning codex.
	foreign := filepath.Join(home, "cursor.hydra")
	if err := os.WriteFile(foreign, []byte("cursors-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWM_AGENT", "cursor")
	t.Setenv("PWM_HYDRA_SECRET_FILE", foreign)

	if _, err := run(t, home, "", "agent", "hydra", "codex"); err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("expected provisioning rotate: puts=%d", puts)
	}
	raw, _ := os.ReadFile(foreign)
	if strings.TrimSpace(string(raw)) != "cursors-secret" {
		t.Fatal("foreign agent's secret file was overwritten")
	}
	canon := filepath.Join(home, ".config", "vortex", "pwm-railway", "codex.hydra")
	raw, err := os.ReadFile(canon)
	if err != nil {
		t.Fatalf("canonical secret file not written: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "rotated-secret" {
		t.Fatal("canonical file has wrong secret")
	}
}

func TestCLIAgentHydraUsesEnvForMatchingAgent(t *testing.T) {
	var puts int
	srv := fakeHydra(t, "live-secret", "rotated-secret", &puts)
	t.Setenv("VEIL_HYDRA_ISSUER", srv.URL)
	t.Setenv("VEIL_HYDRA_ADMIN", srv.URL)

	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := run(t, home, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, home, "", "agent", "add", "cursor"); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(home, "env.hydra")
	if err := os.WriteFile(envFile, []byte("live-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWM_AGENT", "cursor")
	t.Setenv("PWM_HYDRA_SECRET_FILE", envFile)

	if _, err := run(t, home, "", "agent", "hydra", "cursor"); err != nil {
		t.Fatal(err)
	}
	if puts != 0 {
		t.Fatalf("verified env-path secret still rotated: puts=%d", puts)
	}
}
