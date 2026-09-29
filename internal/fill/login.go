// Browser sign-in for the extension. need_login used to strand the human on
// login.veil.nyc with no way to hand the token back to the host. The host runs
// the same PKCE flow `veil human login` runs: authorize URL out to the
// extension, code caught on the localhost redirect, exchanged for an ID token,
// written to the token file the CLI's TokenFn already re-reads.
package fill

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/human"
)

// loginFlow is an in-flight browser sign-in. Issuer, ClientID, Redirect, and
// TokenPath live on Host; the CLI sets them from VEIL_HYDRA_* / the token file.
type loginFlow struct {
	authURL  string
	listener net.Listener
	state    string
	verifier string
}

func authURLReply(authURL, errMsg string) []byte {
	return jsonBytes(struct {
		AuthURL string `json:"auth_url,omitempty"`
		Error   string `json:"error,omitempty"`
	}{AuthURL: authURL, Error: errMsg})
}

// jsonLogin starts (or returns the in-flight) browser sign-in. Replies
// {auth_url} immediately; the code exchange lands asynchronously.
func (h *Host) jsonLogin() []byte {
	if h.Issuer == "" || h.TokenPath == "" {
		return authURLReply("", "no issuer")
	}
	h.mu.Lock()
	if h.login != nil {
		url := h.login.authURL
		h.mu.Unlock()
		return authURLReply(url, "")
	}
	h.mu.Unlock()

	verifier, state, err := human.PKCE()
	if err != nil {
		return authURLReply("", "pkce")
	}
	v, err := human.New(human.Config{Issuer: h.Issuer, Audience: h.ClientID, RedirectURL: h.Redirect})
	if err != nil {
		return authURLReply("", "issuer")
	}
	authURL, err := v.AuthCodeURL(context.Background(), state, verifier)
	if err != nil {
		return authURLReply("", "issuer")
	}
	u, err := url.Parse(v.Redirect())
	if err != nil {
		return authURLReply("", "redirect")
	}
	ln, err := net.Listen("tcp", u.Host)
	if err != nil {
		return authURLReply("", "login listener busy")
	}
	flow := &loginFlow{authURL: authURL, listener: ln, state: state, verifier: verifier}
	h.mu.Lock()
	if h.login != nil {
		other := h.login.authURL
		h.mu.Unlock()
		_ = ln.Close()
		return authURLReply(other, "")
	}
	h.login = flow
	h.mu.Unlock()
	go h.finishLogin(v, flow, u.Path)
	return authURLReply(authURL, "")
}

// finishLogin waits for the localhost callback, exchanges the code, writes the
// token file, and clears needLogin. Failure leaves the host in need_login; the
// next login action starts a fresh flow.
func (h *Host) finishLogin(v *human.Verifier, flow *loginFlow, path string) {
	defer func() {
		_ = flow.listener.Close()
		h.mu.Lock()
		if h.login == flow {
			h.login = nil
		}
		h.mu.Unlock()
	}()
	codes := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if e := r.URL.Query().Get("error"); e != "" {
			http.Error(w, e, http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != flow.state {
			http.Error(w, "state", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Signed in to Veil. You can close this tab.\n"))
		select {
		case codes <- code:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(flow.listener) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var code string
	select {
	case code = <-codes:
	case <-ctx.Done():
		return
	}
	raw, err := v.Exchange(ctx, code, flow.verifier)
	if err != nil {
		fillDebug("login exchange: " + err.Error())
		return
	}
	if err := human.RequireTOTP(raw); err != nil {
		fillDebug("login aal2: " + err.Error())
		return
	}
	if err := os.WriteFile(h.TokenPath, []byte(strings.TrimSpace(raw)+"\n"), 0o600); err != nil {
		fillDebug("login write: " + err.Error())
		return
	}
	h.mu.Lock()
	h.indexOK = false
	h.mu.Unlock()
	h.setNeedLogin(false)
}
