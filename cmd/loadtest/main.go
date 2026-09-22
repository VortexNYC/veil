package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/broker"
	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/envcompat"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/publicapi"
)

type origin struct {
	app *app.App
	srv *http.Server
	ln  net.Listener
	url string
	cmd *exec.Cmd
}

func main() {
	envcompat.BridgeLegacy()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(),
	})))
	if err := run(); err != nil {
		slog.Error("loadtest failed", "err", err)
		os.Exit(1)
	}
}

func logLevel() slog.Leveler {
	switch strings.ToLower(os.Getenv("VEIL_LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		return errors.New("PG_TEST_DSN is required")
	}

	// pgBot and other Postgres tooling expect DATABASE_URL.
	if err := os.Setenv("DATABASE_URL", dsn); err != nil {
		return fmt.Errorf("set DATABASE_URL: %w", err)
	}

	mode := envOr("LOADTEST_ORIGIN_MODE", "goroutine")
	replicas := envOrInt("LOADTEST_REPLICAS", 1)
	if replicas < 1 {
		replicas = 1
	}

	// The upstream may be supplied externally so that processes on other hosts
	// can reach it. If not, start a local one and bind it to 127.0.0.1:0.
	upstreamURL := os.Getenv("LOADTEST_UPSTREAM_URL")
	if mode == "external" && upstreamURL == "" {
		return errors.New("LOADTEST_UPSTREAM_URL is required for external origin mode")
	}
	if upstreamURL == "" {
		upstreamLn, u, err := startUpstream()
		if err != nil {
			return fmt.Errorf("start upstream: %w", err)
		}
		upstreamURL = u
		upstreamSrv := &http.Server{Handler: upstreamHandler(), ReadHeaderTimeout: 2 * time.Second}
		defer func() { _ = upstreamSrv.Close() }()
		go func() { _ = upstreamSrv.Serve(upstreamLn) }()
	}

	kek := os.Getenv("VEIL_KEK")
	if kek == "" {
		if mode == "external" {
			return errors.New("VEIL_KEK is required for external origin mode (must match the external origins' KEK)")
		}
		key, err := crypto.NewKey()
		if err != nil {
			return fmt.Errorf("generate KEK: %w", err)
		}
		kek = hex.EncodeToString(key[:])
		if err := os.Setenv("VEIL_KEK", kek); err != nil {
			return fmt.Errorf("set VEIL_KEK: %w", err)
		}
	}
	masterKey := os.Getenv("VEIL_MASTER_KEY")
	if masterKey == "" {
		if mode == "external" {
			return errors.New("VEIL_MASTER_KEY is required for external origin mode (seeds the org master row; must match the external origins' key)")
		}
		key, err := crypto.NewKey()
		if err != nil {
			return fmt.Errorf("generate master key: %w", err)
		}
		masterKey = hex.EncodeToString(key[:])
		if err := os.Setenv("VEIL_MASTER_KEY", masterKey); err != nil {
			return fmt.Errorf("set VEIL_MASTER_KEY: %w", err)
		}
	}

	origins, err := startOrigins(dsn, masterKey, kek, replicas)
	if err != nil {
		return err
	}
	defer closeOrigins(origins)

	if envOr("LOADTEST_RESET_DB", "0") == "1" {
		if err := resetTestTables(ctx, dsn); err != nil {
			return fmt.Errorf("reset test tables: %w", err)
		}
		slog.Warn("reset test tables")
	}

	vus := envOrInt("VEIL_VUS", 50)
	var agent protocol.Principal
	var item protocol.Item
	var sessions []protocol.Session
	var tokens []string
	if mode == "goroutine" {
		agent, item, sessions, tokens, err = seed(origins[0].app, upstreamURL, vus)
	} else {
		var seedApp *app.App
		seedApp, err = app.OpenPostgres(dsn)
		if err != nil {
			return fmt.Errorf("open seed app: %w", err)
		}
		agent, item, sessions, tokens, err = seed(seedApp, upstreamURL, vus)
		_ = seedApp.Close()
	}
	if err != nil {
		return err
	}
	if dump := os.Getenv("LOADTEST_DUMP_TOKENS"); dump != "" {
		if err := os.WriteFile(dump, []byte(strings.Join(tokens, "\n")+"\n"), 0o600); err != nil {
			return fmt.Errorf("dump tokens: %w", err)
		}
	}

	useProxyDefault := "1"
	if mode == "process" || mode == "external" {
		useProxyDefault = "0"
	}
	useProxy := envOr("LOADTEST_PROXY", useProxyDefault) != "0"

	var k6Origins []string
	var proxyURL string
	if useProxy {
		proxyURL, err = startProxy(origins)
		if err != nil {
			return fmt.Errorf("start proxy: %w", err)
		}
		k6Origins = []string{proxyURL}
		slog.Warn("running load test", "replicas", len(origins), "mode", mode, "proxy", proxyURL, "upstream", upstreamURL)
	} else {
		proxyURL = origins[0].url
		k6Origins = make([]string, len(origins))
		for i, o := range origins {
			k6Origins[i] = o.url
		}
		slog.Warn("running load test", "replicas", len(origins), "mode", mode, "upstream", upstreamURL)
	}

	outDir := envOr("LOADTEST_OUT", "tests/load/k6/out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if err := resetPGSS(ctx, dsn); err != nil {
		slog.Warn("pg_stat_statements reset failed", "err", err)
	}

	if err := pgbotInspect(ctx, filepath.Join(outDir, "pgbot-before.json")); err != nil {
		slog.Warn("pgbot before failed", "err", err)
	}

	cpuPath := filepath.Join(outDir, "cpu.pprof")
	cpuF, err := os.Create(cpuPath)
	if err != nil {
		return fmt.Errorf("create cpu profile: %w", err)
	}
	if err := pprof.StartCPUProfile(cpuF); err != nil {
		_ = cpuF.Close()
		return fmt.Errorf("start cpu profile: %w", err)
	}

	k6Script := envOr("LOADTEST_K6_SCRIPT", "tests/load/k6/use.js")
	k6Out := filepath.Join(outDir, "k6-summary.json")
	k6Args := []string{"run", "--summary-export", k6Out, k6Script}
	k6Cmd := exec.CommandContext(ctx, "k6", k6Args...)
	k6Cmd.Env = append(os.Environ(),
		"VEIL_ORIGINS="+strings.Join(k6Origins, ","),
		"VEIL_ORIGIN="+proxyURL,
		"VEIL_TOKENS="+strings.Join(tokens, ","),
		"VEIL_ITEM_ID="+item.ID,
		"VEIL_UPSTREAM_URL="+upstreamURL,
	)
	k6Cmd.Stdout = os.Stdout
	k6Cmd.Stderr = os.Stderr
	k6Err := k6Cmd.Run()

	pprof.StopCPUProfile()
	_ = cpuF.Close()

	heapPath := filepath.Join(outDir, "heap.pprof")
	heapF, err := os.Create(heapPath)
	if err != nil {
		return fmt.Errorf("create heap profile: %w", err)
	}
	if err := pprof.WriteHeapProfile(heapF); err != nil {
		_ = heapF.Close()
		return fmt.Errorf("write heap profile: %w", err)
	}
	_ = heapF.Close()

	// Stop origins and flush any pending audit batches before taking the final
	// pgbot snapshot, so pg_stat_statements reflects the complete workload.
	closeOrigins(origins)

	if err := pgbotInspect(ctx, filepath.Join(outDir, "pgbot-after.json")); err != nil {
		slog.Warn("pgbot after failed", "err", err)
	}

	slog.Warn("load test complete", "replicas", len(origins), "mode", mode, "summary", k6Out)

	_ = agent
	_ = sessions
	if k6Err != nil {
		return fmt.Errorf("k6 run: %w", k6Err)
	}
	return nil
}

func startOrigins(dsn, masterKey, kek string, n int) ([]*origin, error) {
	mode := envOr("LOADTEST_ORIGIN_MODE", "goroutine")
	switch mode {
	case "goroutine":
		return startGoroutineOrigins(dsn, masterKey, n)
	case "process":
		return startProcessOrigins(dsn, masterKey, kek, n)
	case "external":
		raw := os.Getenv("LOADTEST_ORIGINS")
		if raw == "" {
			return nil, errors.New("LOADTEST_ORIGINS is required for external origin mode")
		}
		urls := strings.Split(raw, ",")
		origins := make([]*origin, 0, len(urls))
		for _, u := range urls {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			origins = append(origins, &origin{url: u})
		}
		if len(origins) == 0 {
			return nil, errors.New("LOADTEST_ORIGINS contains no valid URLs")
		}
		return origins, nil
	default:
		return nil, fmt.Errorf("unknown LOADTEST_ORIGIN_MODE=%q", mode)
	}
}

func startGoroutineOrigins(dsn, masterKey string, n int) ([]*origin, error) {
	_ = masterKey
	maxInFlight := 0
	if raw := os.Getenv("VEIL_MAX_IN_FLIGHT_USE"); raw != "" {
		if m, err := strconv.Atoi(raw); err == nil && m > 0 {
			maxInFlight = m
		} else if err != nil {
			slog.Warn("invalid VEIL_MAX_IN_FLIGHT_USE, ignored", "value", raw)
		}
	}

	origins := make([]*origin, n)
	for i := 0; i < n; i++ {
		a, err := app.OpenPostgres(dsn)
		if err != nil {
			closeOrigins(origins[:i])
			return nil, fmt.Errorf("open origin %d: %w", i, err)
		}
		if maxInFlight > 0 {
			a.Broker = broker.NewWithInFlight(a.Store, maxInFlight)
			a.Broker.Auditor = a.Auditor
		}

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			_ = a.Close()
			closeOrigins(origins[:i])
			return nil, fmt.Errorf("listen origin %d: %w", i, err)
		}

		mux := http.NewServeMux()
		srv := &publicapi.Server{App: a}
		srv.Mount(mux)
		originSrv := &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
		origins[i] = &origin{app: a, srv: originSrv, ln: ln, url: "http://" + ln.Addr().String()}
		go func(s *http.Server, l net.Listener) { _ = s.Serve(l) }(originSrv, ln)
	}
	return origins, nil
}

var builtOriginBinary string

func startProcessOrigins(dsn, masterKey, kek string, n int) ([]*origin, error) {
	bin, err := originBinary()
	if err != nil {
		return nil, err
	}

	origins := make([]*origin, n)
	for i := 0; i < n; i++ {
		port, err := freePort()
		if err != nil {
			closeOrigins(origins[:i])
			return nil, fmt.Errorf("allocate port for origin %d: %w", i, err)
		}
		url := "http://127.0.0.1:" + port
		cmd := exec.Command(bin, "mcp", "--listen", "127.0.0.1:"+port)
		cmd.Env = append(os.Environ(),
			"VEIL_POSTGRES_DSN="+dsn,
			"VEIL_KEK="+kek,
			"VEIL_MASTER_KEY="+masterKey,
			"VEIL_LOG_LEVEL=warn",
		)
		cmd.Stdout = io.Discard
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			closeOrigins(origins[:i])
			return nil, fmt.Errorf("start origin %d: %w", i, err)
		}
		origins[i] = &origin{url: url, cmd: cmd}
		if err := waitReady(url); err != nil {
			closeOrigins(origins[:i+1])
			return nil, fmt.Errorf("origin %d not ready: %w", i, err)
		}
	}
	return origins, nil
}

func originBinary() (string, error) {
	if builtOriginBinary != "" {
		return builtOriginBinary, nil
	}
	if p := os.Getenv("LOADTEST_ORIGIN_BINARY"); p != "" {
		return p, nil
	}
	// Build from the working tree, never exec.LookPath: a stale installed
	// binary silently skews the measurement (observed: 100% 401s from a
	// day-old build with a different session path).
	root, err := repoRoot()
	if err != nil {
		return "", fmt.Errorf("find repo root: %w", err)
	}
	f, err := os.CreateTemp("", "veil-loadtest-*")
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}
	_ = f.Close()
	path := f.Name()
	cmd := exec.Command("go", "build", "-o", path, "./cmd/veil")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("build origin binary: %w\n%s", err, out)
	}
	builtOriginBinary = path
	return path, nil
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return strconv.Itoa(port), nil
}

func waitReady(url string) error {
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("origin %s not ready", url)
}

func closeOrigins(origins []*origin) {
	var wg sync.WaitGroup
	for _, o := range origins {
		if o == nil {
			continue
		}
		wg.Add(1)
		go func(o *origin) {
			defer wg.Done()
			if o.cmd != nil && o.cmd.Process != nil {
				_ = o.cmd.Process.Signal(os.Interrupt)
				done := make(chan error, 1)
				go func() { done <- o.cmd.Wait() }()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					_ = o.cmd.Process.Kill()
					_ = o.cmd.Wait()
				}
			}
			if o.srv != nil {
				_ = o.srv.Close()
			}
			if o.app != nil {
				_ = o.app.Close()
			}
			o.srv = nil
			o.app = nil
		}(o)
	}
	wg.Wait()
}

func seed(a *app.App, upstreamURL string, n int) (protocol.Principal, protocol.Item, []protocol.Session, []string, error) {
	agentName := envOr("LOADTEST_AGENT", "loadtest-agent")
	itemName := envOr("LOADTEST_ITEM", "loadtest-item")
	secret := []byte(envOr("LOADTEST_SECRET", "sk_live_loadtest_secret"))
	numAgents := envOrInt("LOADTEST_AGENTS", 1)
	if numAgents < 1 {
		numAgents = 1
	}

	agents := make([]protocol.Principal, 0, numAgents)
	for i := 0; i < numAgents; i++ {
		name := agentName
		if numAgents > 1 {
			name = fmt.Sprintf("%s-%d", agentName, i)
		}
		agent, err := a.AddAgent(name)
		if err != nil {
			return protocol.Principal{}, protocol.Item{}, nil, nil, fmt.Errorf("add agent %d: %w", i, err)
		}
		agents = append(agents, agent)
	}
	item, err := a.AddItem(itemName, upstreamURL, secret)
	if err != nil {
		return protocol.Principal{}, protocol.Item{}, nil, nil, fmt.Errorf("add item: %w", err)
	}
	for _, agent := range agents {
		if _, err := a.AddGrant(agent.ID, item.ID, protocol.Level2); err != nil {
			return protocol.Principal{}, protocol.Item{}, nil, nil, fmt.Errorf("add grant: %w", err)
		}
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
	sessions := make([]protocol.Session, 0, n)
	tokens := make([]string, 0, n)
	for i := 0; i < n; i++ {
		agent := agents[i%len(agents)]
		session, token, err := a.CreateSession(human, agent.ID, time.Hour, 0)
		if err != nil {
			return protocol.Principal{}, protocol.Item{}, nil, nil, fmt.Errorf("create session %d: %w", i, err)
		}
		sessions = append(sessions, session)
		tokens = append(tokens, token)
	}
	slog.Warn("seeded", "agents", len(agents), "item", item.ID, "sessions", len(sessions))
	return agents[0], item, sessions, tokens, nil
}

func startProxy(origins []*origin) (string, error) {
	urls := make([]*url.URL, 0, len(origins))
	for _, o := range origins {
		u, err := url.Parse(o.url)
		if err != nil {
			return "", fmt.Errorf("parse origin url %q: %w", o.url, err)
		}
		urls = append(urls, u)
	}
	if len(urls) == 0 {
		return "", errors.New("no origins for proxy")
	}

	var counter uint64
	proxy := httputil.NewSingleHostReverseProxy(urls[0])
	proxy.Director = nil
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		idx := atomic.AddUint64(&counter, 1) % uint64(len(urls))
		pr.SetURL(urls[idx])
		pr.Out.Host = urls[idx].Host
	}
	// The default transport only keeps two idle connections per host, which
	// causes connection churn and port exhaustion under high request rates.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	proxy.Transport = transport

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen proxy: %w", err)
	}
	srv := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

func startUpstream() (net.Listener, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	return ln, "http://" + ln.Addr().String() + "/ok", nil
}

func upstreamHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
}

func pgbotInspect(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "pgbot", "inspect", "--format", "json", "--fail-on", "none")
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create pgbot output file: %w", err)
	}
	defer func() { _ = out.Close() }()
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pgbot inspect: %w", err)
	}
	return nil
}

func resetPGSS(ctx context.Context, dsn string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to reset pg_stat_statements: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		return fmt.Errorf("pg_stat_statements_reset: %w", err)
	}
	return nil
}

func resetTestTables(ctx context.Context, dsn string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to reset test tables: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, `TRUNCATE TABLE agents, items, grants, approvals, audit, workloads, owner_keys, item_versions, sessions`)
	if err != nil {
		return fmt.Errorf("truncate test tables: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("invalid env var, using fallback", "key", key, "value", raw, "fallback", fallback)
		return fallback
	}
	return n
}
