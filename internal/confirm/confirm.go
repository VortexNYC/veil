// Package confirm is Touch ID at fill time. The native host prompts
// before a secret goes to the extension. Host.Confirm nil fails closed.
package confirm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Enabled is Mac + VEIL_FILL_TOUCHID not "0". Linux/Windows wait for 35–36.
func Enabled() bool {
	if os.Getenv("VEIL_FILL_TOUCHID") == "0" {
		return false
	}
	return touchIDAvailable
}

// Action is the allow-line verb. "Veil wants to fill a card" → "fill a card".
func Action(reason string) string {
	s := strings.TrimSpace(reason)
	s = strings.TrimPrefix(s, "Veil wants to ")
	if s == "" {
		return "use Veil"
	}
	return s
}

func accountLabel() string {
	e := strings.TrimSpace(os.Getenv("VEIL_LOGIN_EMAIL"))
	if e != "" {
		return e
	}
	return "Veil"
}

func interactive() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// callerKey is the stable consent key: "b:<bundleID>" for a real app,
// "p:<exe path>" for an unbundled parent (agent CLIs, `go run`, CI) —
// the full path, so a same-named binary elsewhere does not inherit it.
func callerKey() string {
	if b := callerBundle(); b != "" {
		return "b:" + b
	}
	if p := callerLabel(); p != "" {
		return "p:" + p
	}
	return ""
}

// commandReason names what the human is authorizing — "Allow {app} to
// run `veil grant add --agent claude`" — instead of a context-free
// "get CLI access". Secrets never travel argv (files only), so the
// full verb line is safe to show; it truncates at 72 chars.
func commandReason() string {
	verb := strings.Join(os.Args[1:], " ")
	if strings.TrimSpace(verb) == "" {
		return "run veil commands"
	}
	if len(verb) > 72 {
		verb = verb[:72] + "…"
	}
	return "run `veil " + verb + "`"
}

// TouchIDHelper runs the access sheet + eval in a short-lived child
// (`veil fill --confirm-prompt`). The bridge daemon must never initialize
// AppKit in-process: once it does, launchd/RBS manage it as an app and
// the OS kills it for efficiency mid-session — the socket then refuses
// connections. A transient helper carries the app lifecycle instead and
// exits cleanly per confirm.
//
// helperDeadline bounds the child: the in-process eval times out at 60s and
// the browser request at 90s, so a wedged child must die between the two —
// otherwise its ultra-late reply desyncs request/reply order on the socket.
const helperDeadline = 75 * time.Second

func TouchIDHelper(reason string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helperDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "fill", "--confirm-prompt")
	cmd.Env = append(os.Environ(), "VEIL_CONFIRM_REASON="+reason)
	// The child's veil-confirm logs land in the daemon's fill-bridge.log.
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fill: touch id declined")
	}
	return nil
}

// ServeStdio is the --confirm-server child loop: one JSON line in
// {"reason":...} → a sheet + LA eval → one JSON line out {"ok":bool}.
// It must run on the main thread: veil_access drives AppKit and pumps the
// runloop itself while an eval is in flight; between requests the main
// thread just blocks on stdin. Stdin EOF (daemon exit) ends the process.
func ServeStdio() {
	// Pay the AppKit boot once, at spawn — the daemon prewarms this child
	// at bridge start so even the first prompt is warm.
	warmAppKit()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var req struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			_ = enc.Encode(map[string]bool{"ok": false})
			continue
		}
		err := TouchID(req.Reason)
		_ = enc.Encode(map[string]bool{"ok": err == nil})
	}
}

// TouchIDServer keeps one --confirm-server child warm so a prompt costs a
// pipe round trip + the LA eval (~1.5s + the human) instead of a full
// process boot — measured 6-7s of spawn/AppKit/LS per prompt otherwise.
// Requests serialize through the mutex; a wedged or dead child is killed
// and the next call respawns. Same 75s deadline as TouchIDHelper.
func TouchIDServer(reason string) error {
	return server.ask(reason)
}

// PrewarmServer spawns the child in the background so the first real
// prompt is already warm. Called once at bridge boot.
func PrewarmServer() {
	go func() {
		server.mu.Lock()
		defer server.mu.Unlock()
		_ = server.ensure()
	}()
}

var server = &confirmServer{}

type confirmServer struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.Reader
	dead bool
}

func (s *confirmServer) ensure() error {
	if s.cmd != nil && !s.dead {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "fill", "--confirm-server")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd, s.in, s.out, s.dead = cmd, in, out, false
	go func() {
		_ = cmd.Wait()
		server.mu.Lock()
		server.dead = true
		server.mu.Unlock()
	}()
	return nil
}

func (s *confirmServer) kill() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.dead = true
}

func (s *confirmServer) ask(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return TouchIDHelper(reason)
	}
	req, _ := json.Marshal(struct {
		Reason string `json:"reason"`
	}{Reason: reason})
	if f, ok := s.in.(*os.File); ok {
		_ = f.SetWriteDeadline(time.Now().Add(helperDeadline))
	}
	if _, err := s.in.Write(append(req, '\n')); err != nil {
		s.kill()
		return TouchIDHelper(reason)
	}
	if f, ok := s.out.(*os.File); ok {
		_ = f.SetReadDeadline(time.Now().Add(helperDeadline))
	}
	var reply struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(s.out).Decode(&reply); err != nil {
		// Timeout or dead child: its late reply would desync the next
		// request, so the process must die. Fall back to a fresh
		// one-shot child for this prompt.
		s.kill()
		return TouchIDHelper(reason)
	}
	if !reply.OK {
		return fmt.Errorf("fill: touch id declined")
	}
	return nil
}

// CLIAccess is the consent sheet: Allow {app} to run `veil <verb>`.
// Fill uses TouchID. Agents, tests, and VEIL_FILL_TOUCHID=0 skip.
func CLIAccess() error {
	if !Enabled() || !interactive() || testing.Testing() {
		return nil
	}
	key := callerKey()
	if key != "" && cliAllowed(key) {
		return nil
	}
	if err := TouchID(commandReason()); err != nil {
		return err
	}
	if key != "" {
		cliRemember(key)
	}
	return nil
}
