package otelsetup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHandlerSkipsHealth(t *testing.T) {
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health=%d", rec.Code)
	}
}

// TestHandlerScrubsSpan proves server spans carry the route pattern, not
// the raw path or the client IP — url.path with a named item would leak
// vault metadata into the trace sink.
func TestHandlerScrubsSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/items/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/items/github-prod", nil)
	req.RemoteAddr = "203.0.113.7:5555"
	Handler(mux).ServeHTTP(httptest.NewRecorder(), req)

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("spans=%d", len(ended))
	}
	attrs := map[string]string{}
	for _, a := range ended[0].Attributes() {
		attrs[string(a.Key)] = a.Value.AsString()
	}
	if got := attrs["url.path"]; got != "GET /v1/items/{name}" {
		t.Fatalf("url.path=%q — raw path leaked", got)
	}
	if got := attrs["http.route"]; got != "GET /v1/items/{name}" {
		t.Fatalf("http.route=%q", got)
	}
	if got := attrs["client.address"]; got != "" {
		t.Fatalf("client.address=%q — IP leaked", got)
	}
}
