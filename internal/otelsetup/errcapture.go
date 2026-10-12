// errcapture.go — $exception events to PostHog on panic and 5xx.
// Traces already land via OTLP; error tracking needs the $exception
// event shape. One POST to the public capture host, async, fire-and-
// forget — a failed report must never fail a request. No request body,
// no item names, no agent ids: the event carries method, route-shaped
// path, and status only.
package otelsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const captureHost = "https://us.i.posthog.com/capture/"

var captureKey = strings.TrimSpace(os.Getenv("POSTHOG_PROJECT_KEY"))

type recWriter struct {
	http.ResponseWriter
	status int
}

func (w *recWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// A 5xx bleed must not become a capture egress storm: at most one report
// per path per minute. The event still lands; the volume collapses.
var (
	reportMu   sync.Mutex
	reportLast = map[string]time.Time{}
)

func reportException(surface, path string, code int, errText string) {
	if captureKey == "" {
		return
	}
	reportMu.Lock()
	if now := time.Now(); now.Sub(reportLast[path]) < time.Minute {
		reportMu.Unlock()
		return
	} else {
		reportLast[path] = now
	}
	reportMu.Unlock()
	body, _ := json.Marshal(map[string]any{
		"api_key": captureKey,
		"event":   "$exception",
		"properties": map[string]any{
			"distinct_id":        "veil-origin",
			"$lib":               "veil-origin",
			"$exception_type":    "5xx",
			"$exception_message": errText,
			"surface":            surface,
			"path":               path,
			"status":             code,
		},
	})
	// Async: a reporting hiccup must never add latency or fail a request.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, captureHost, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
		}
	}()
}

// ExceptionCapture wraps a handler: recovered panics and 5xx statuses
// become $exception events in PostHog error tracking — the same place
// the client surfaces report.
func ExceptionCapture(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				reportException("panic", r.URL.Path, 500, fmt.Sprintf("%v", rec))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		rw := &recWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if rw.status >= 500 {
			reportException("5xx", r.URL.Path, rw.status, fmt.Sprintf("%s %s -> %d", r.Method, r.URL.Path, rw.status))
		}
	})
}
