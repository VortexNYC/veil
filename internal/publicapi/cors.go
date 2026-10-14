package publicapi

import "net/http"

// VaultOrigins are browser origins allowed to call origin with a human Bearer.
// Agents do not use CORS; they send Authorization from a process.
var VaultOrigins = map[string]struct{}{
	"https://app.veil.nyc":  {},
	"http://127.0.0.1:4470": {},
	"http://localhost:4470": {},
	// The Mac app's bundled vault — WKWebView on the veil-app scheme.
	"veil-app://vault": {},
}

// CORS allows the vault SPA to call origin. OPTIONS is answered here so
// preflight never hits Bearer. Unknown origins get no ACAO.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := VaultOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
