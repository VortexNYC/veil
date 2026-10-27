package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/identity/glue"
	"github.com/VortexNYC/veil/internal/confirm"
	"github.com/VortexNYC/veil/internal/human"
)

func originHumanToken() (string, error) {
	if f := strings.TrimSpace(os.Getenv("VEIL_HUMAN_TOKEN_FILE")); f != "" {
		b, err := readFileMaterial(f)
		if err != nil {
			return "", err
		}
		if len(b) == 0 {
			return "", fmt.Errorf("origin: empty VEIL_HUMAN_TOKEN_FILE")
		}
		return string(b), nil
	}
	if t := strings.TrimSpace(os.Getenv("VEIL_HUMAN_TOKEN")); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("origin: VEIL_HUMAN_TOKEN_FILE or VEIL_HUMAN_TOKEN is required")
}

func originHumanTokenLive(ctx context.Context) (string, error) {
	tok, err := originHumanToken()
	if err == nil && !jwtNeedsRefresh(tok) {
		return tok, nil
	}
	out := strings.TrimSpace(os.Getenv("VEIL_HUMAN_TOKEN_FILE"))
	minted, merr := remintHumanHTTP(ctx, out)
	if merr == nil {
		return minted, nil
	}
	if err != nil {
		return "", err
	}
	if jwtNeedsRefresh(tok) {
		return "", fmt.Errorf("origin: human JWT expired (%w); veil human login --out-file", merr)
	}
	return tok, nil
}

func originHumanCLI(ctx context.Context) (string, error) {
	tok, err := originHumanTokenLive(ctx)
	if err != nil {
		return "", err
	}
	if err := confirm.CLIAccess(); err != nil {
		return "", err
	}
	return tok, nil
}

func originOwnerToken(ctx context.Context) (string, error) {
	tok, err := originHumanTokenLive(ctx)
	if err == nil {
		if err := confirm.CLIAccess(); err != nil {
			return "", err
		}
		return tok, nil
	}
	return originTokenLive(ctx, "")
}

func humanLogin(cmd *cobra.Command, home, outFile string, visit func(string) error) error {
	if outFile == "" {
		return fmt.Errorf("human login: --out-file is required")
	}
	if httpLoginReady() {
		raw, err := remintHumanHTTP(cmd.Context(), outFile)
		if err != nil {
			return err
		}
		_ = raw
		return encode(cmd, map[string]string{"out_file": outFile})
	}
	iss := envOr("VEIL_HYDRA_ISSUER", "https://id.veil.nyc")
	v, err := human.New(human.Config{
		Issuer:      iss,
		Audience:    envOr("VEIL_HYDRA_CLIENT_ID", glue.DefaultClientID),
		RedirectURL: envOr("VEIL_HYDRA_REDIRECT", human.DefaultRedirect),
	})
	if err != nil {
		return err
	}
	verifier, state, err := human.PKCE()
	if err != nil {
		return err
	}
	authURL, err := v.AuthCodeURL(cmd.Context(), state, verifier)
	if err != nil {
		return err
	}
	u, err := url.Parse(v.Redirect())
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", u.Host)
	if err != nil {
		return err
	}
	defer ln.Close()
	got := make(chan string, 1)
	errc := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		if e := r.URL.Query().Get("error"); e != "" {
			http.Error(w, e, http.StatusBadRequest)
			errc <- fmt.Errorf("human login: %s", e)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state", http.StatusBadRequest)
			errc <- fmt.Errorf("human login: state")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errc <- fmt.Errorf("human login: missing code")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
		got <- code
	})
	srv := &http.Server{Handler: mux}
	defer func() { _ = srv.Shutdown(context.Background()) }()
	go func() { _ = srv.Serve(ln) }()
	fmt.Fprintln(cmd.ErrOrStderr(), authURL)
	if visit == nil {
		visit = openBrowser
	}
	if err := visit(authURL); err != nil {
		return err
	}
	var code string
	select {
	case <-cmd.Context().Done():
		return cmd.Context().Err()
	case err := <-errc:
		return err
	case code = <-got:
	case <-time.After(10 * time.Minute):
		return fmt.Errorf("human login: timeout")
	}
	raw, err := v.Exchange(cmd.Context(), code, verifier)
	if err != nil {
		return err
	}
	if err := human.RequireMFA(raw); err != nil {
		return err
	}
	if err := os.WriteFile(outFile, []byte(raw+"\n"), 0o600); err != nil {
		return err
	}
	if originBase() != "" {
		// VEIL-78 lazy backfill: enroll the recovery share on login when the
		// member has neither a local key nor an enrolled share. Best-effort.
		maybeAutoEnrollRecovery(cmd.Context(), home, raw)
	}
	return encode(cmd, map[string]string{"out_file": outFile})
}

func httpLoginReady() bool {
	return strings.TrimSpace(os.Getenv("VEIL_LOGIN_EMAIL")) != "" &&
		strings.TrimSpace(os.Getenv("VEIL_KRATOS_PASSWORD_FILE")) != "" &&
		strings.TrimSpace(os.Getenv("VEIL_KRATOS_TOTP_FILE")) != ""
}

func remintHumanHTTP(ctx context.Context, outFile string) (string, error) {
	if !httpLoginReady() {
		return "", fmt.Errorf("origin: VEIL_LOGIN_EMAIL, VEIL_KRATOS_PASSWORD_FILE, and VEIL_KRATOS_TOTP_FILE remint a human JWT")
	}
	password, err := readFileMaterial(os.Getenv("VEIL_KRATOS_PASSWORD_FILE"))
	if err != nil {
		return "", err
	}
	seed, err := readFileMaterial(os.Getenv("VEIL_KRATOS_TOTP_FILE"))
	if err != nil {
		return "", err
	}
	iss := envOr("VEIL_HYDRA_ISSUER", "https://id.veil.nyc")
	kratos := envOr("VEIL_KRATOS_PUBLIC", "https://accounts.veil.nyc")
	v, err := human.New(human.Config{
		Issuer:      iss,
		Audience:    envOr("VEIL_HYDRA_CLIENT_ID", glue.DefaultClientID),
		RedirectURL: envOr("VEIL_HYDRA_REDIRECT", human.DefaultRedirect),
	})
	if err != nil {
		return "", err
	}
	raw, err := v.LoginHTTP(ctx, human.HTTPLogin{
		KratosPublic: kratos,
		Email:        strings.TrimSpace(os.Getenv("VEIL_LOGIN_EMAIL")),
		Password:     string(password),
		TOTPSeed:     string(seed),
	})
	if err != nil {
		return "", err
	}
	if outFile == "" {
		outFile = strings.TrimSpace(os.Getenv("VEIL_HUMAN_TOKEN_FILE"))
	}
	if outFile != "" {
		if err := os.WriteFile(outFile, []byte(raw+"\n"), 0o600); err != nil {
			return "", err
		}
	}
	return raw, nil
}

func openBrowser(rawURL string) error {
	if os.Getenv("VEIL_LOGIN_NO_OPEN") == "1" {
		return nil
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-a", "Google Chrome", rawURL).Start()
	}
	return exec.Command("open", rawURL).Start()
}
