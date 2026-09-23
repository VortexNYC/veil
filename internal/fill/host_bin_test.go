package fill

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/scrub"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func buildPWM(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "veil")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/veil")
	cmd.Dir = repoRoot(t)
	cmd.Env = envBin()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func envClean() []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "VEIL_ORIGIN=") ||
			strings.HasPrefix(e, "VEIL_FILL_TOUCHID=") ||
			strings.HasPrefix(e, "VEIL_HOME=") ||
			strings.HasPrefix(e, "VEIL_OIDC_") ||
			strings.HasPrefix(e, "VEIL_HUMAN_") ||
			strings.HasPrefix(e, "VEIL_HYDRA_") ||
			strings.HasPrefix(e, "VEIL_") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// envBin is compiled-host isolation for make test. It leaves Confirm
// unattached so those tests fail closed without a Touch ID prompt.
// Headed CFT must use envProve. Forcing VEIL_FILL_TOUCHID=0 on Chrome is a fake.
func envBin() []string {
	return append(envClean(), "VEIL_FILL_TOUCHID=0")
}

func envProve() []string {
	return envClean()
}

func TestEnvProveDoesNotDisableTouchID(t *testing.T) {
	for _, e := range envProve() {
		if strings.HasPrefix(e, "VEIL_FILL_TOUCHID=") {
			t.Fatalf("prove env still sets %s", e)
		}
	}
}

func veil(t *testing.T, bin, home string, args ...string) []byte {
	t.Helper()
	return veilEnv(t, bin, home, nil, args...)
}

func veilEnv(t *testing.T, bin, home string, extra []string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--home", home}, args...)...)
	cmd.Env = append(envBin(), extra...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	if scrub.Contains(out, []byte(secret)) {
		t.Fatalf("binary leaked secret running %v: %s", args, out)
	}
	return out
}

func TestCompiledBinaryAddListMatchFill(t *testing.T) {
	const login = "stripe@example.com"
	bin := buildPWM(t)
	home := t.TempDir()
	veil(t, bin, home, "init")
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	veil(t, bin, home, "item", "add", "stripe", "--uri", "https://dashboard.stripe.com", "--secret-file", secFile, "--login", login)
	veil(t, bin, home, "item", "add", "github", "--uri", "https://github.com", "--secret-file", secFile)

	listOut := veil(t, bin, home, "item", "list")
	var listed []protocol.Item
	if err := json.Unmarshal(listOut, &listed); err != nil {
		t.Fatalf("list json: %v\n%s", err, listOut)
	}
	byName := map[string]protocol.Item{}
	for _, item := range listed {
		byName[item.Name] = item
	}
	if byName["stripe"].Login != login {
		t.Fatalf("list login: %+v", byName["stripe"])
	}
	if byName["github"].Login != "" {
		t.Fatalf("empty login must be honest: %+v", byName["github"])
	}

	a, err := app.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: app.DefaultHuman, OrgID: a.OrgID}
	got, err := a.Match(human, "https://dashboard.stripe.com/login")
	if err != nil {
		t.Fatal(err)
	}
	matchJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(matchJSON, []byte(secret)) {
		t.Fatal("match leaked secret")
	}
	if len(got) != 1 || got[0].Login != login || got[0].Name != "stripe" {
		t.Fatalf("match %+v", got)
	}
	empty, err := a.Match(human, "https://github.com/login")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 1 || empty[0].Login != "" {
		t.Fatalf("github match %+v", empty)
	}
	miss, err := a.Match(human, "https://evil.example/login")
	if err != nil {
		t.Fatal(err)
	}
	if len(miss) != 0 {
		t.Fatalf("wrong host %+v", miss)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "fill", "--home", home)
	cmd.Env = envBin()
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
	var filled loginReply
	if err := json.Unmarshal(c.sendProc(t, stdin, stdout, req), &filled); err != nil {
		t.Fatal(err)
	}
	if filled.Success != "false" || len(filled.Entries) != 0 {
		t.Fatalf("host with no Confirm must not fill %+v", filled)
	}
	wrong, err := json.Marshal(struct {
		Action string     `json:"action"`
		URL    string     `json:"url"`
		Keys   []assocKey `json:"keys"`
	}{
		Action: "get-logins",
		URL:    "https://evil.example/login",
		Keys:   []assocKey{{ID: assocID, Key: c.idKey}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var missFill loginReply
	if err := json.Unmarshal(c.sendProc(t, stdin, stdout, wrong), &missFill); err != nil {
		t.Fatal(err)
	}
	if missFill.Count != "0" || len(missFill.Entries) != 0 {
		t.Fatalf("wrong host fill %+v", missFill)
	}
}

func TestCompiledBinaryAgainstOriginHTTP(t *testing.T) {
	t.Setenv("VEIL_REPLICA_KEYSTORE", "mem")
	const login = "stripe@example.com"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	bin := buildPWM(t)
	home := t.TempDir()
	tok := filepath.Join(home, "human.jwt")
	if err := os.WriteFile(tok, []byte("human\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extra := []string{"VEIL_ORIGIN=" + srv.URL, "VEIL_HUMAN_TOKEN_FILE=" + tok}
	veilEnv(t, bin, home, extra, "item", "add", "stripe", "--uri", "https://dashboard.stripe.com", "--secret-file", secFile, "--login", login)
	listOut := veilEnv(t, bin, home, extra, "item", "list")
	var listed []protocol.Item
	if err := json.Unmarshal(listOut, &listed); err != nil {
		t.Fatalf("origin list json: %v\n%s", err, listOut)
	}
	if len(listed) != 1 || listed[0].Login != login {
		t.Fatalf("origin list %+v", listed)
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: app.DefaultHuman, OrgID: a.OrgID}
	got, err := a.Match(human, "https://dashboard.stripe.com/login")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Contains(raw, []byte(secret)) {
		t.Fatal("origin match leaked secret")
	}
	if len(got) != 1 || got[0].Login != login {
		t.Fatalf("origin match %+v", got)
	}

	cmd := exec.Command(bin, "fill", "--home", home)
	cmd.Env = append(envBin(), extra...)
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
	var filled loginReply
	if err := json.Unmarshal(c.sendProc(t, stdin, stdout, req), &filled); err != nil {
		t.Fatal(err)
	}
	if filled.Success == "true" && len(filled.Entries) > 0 && filled.Entries[0].Password != "" {
		t.Fatalf("host with no Confirm filled %+v", filled)
	}
}

func TestCompiledBinaryJSONPingMatchFill(t *testing.T) {
	t.Setenv("VEIL_REPLICA_KEYSTORE", "mem")
	const login = "stripe@example.com"
	a, err := app.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := originAPI(t, a)
	bin := buildPWM(t)
	home := t.TempDir()
	tok := filepath.Join(home, "human.jwt")
	if err := os.WriteFile(tok, []byte("human\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secFile := filepath.Join(home, "sec")
	if err := os.WriteFile(secFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extra := []string{"VEIL_ORIGIN=" + srv.URL, "VEIL_HUMAN_TOKEN_FILE=" + tok}
	veilEnv(t, bin, home, extra, "item", "add", "stripe", "--uri", "https://dashboard.stripe.com", "--secret-file", secFile, "--login", login)

	cmd := exec.Command(bin, "fill", "--home", home)
	cmd.Env = append(envBin(), extra...)
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

	ping := jsonFrame(t, stdin, stdout, map[string]string{"action": "ping"})
	var pong struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(ping, &pong); err != nil || pong.Version != JSONVersion {
		t.Fatalf("ping %s", ping)
	}

	matchRaw := jsonFrame(t, stdin, stdout, map[string]string{"action": "match", "url": "https://dashboard.stripe.com/login"})
	if scrub.Contains(matchRaw, []byte(secret)) {
		t.Fatal("match leaked secret")
	}
	var matched struct {
		Entries []jsonMatchEntry `json:"entries"`
	}
	if err := json.Unmarshal(matchRaw, &matched); err != nil {
		t.Fatal(err)
	}
	if len(matched.Entries) != 1 || matched.Entries[0].Login != login || matched.Entries[0].UUID != "stripe" {
		t.Fatalf("match %+v", matched)
	}

	filledRaw := jsonFrame(t, stdin, stdout, map[string]string{"action": "fill", "url": "https://dashboard.stripe.com/login", "uuid": "stripe"})
	var filled struct {
		Entries []jsonFillEntry `json:"entries"`
	}
	if err := json.Unmarshal(filledRaw, &filled); err != nil {
		t.Fatal(err)
	}
	if len(filled.Entries) != 0 {
		t.Fatalf("host with no Confirm filled %+v", filled)
	}
}

func jsonFrame(t *testing.T, in io.Writer, out io.Reader, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(in, raw); err != nil {
		t.Fatal(err)
	}
	got, err := Read(out)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
