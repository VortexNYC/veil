// Package otelsetup is OpenTelemetry for the origin.
//
// HTTP: otelhttp. Grant line: a Use span. Sink is OTLP HTTP when
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is set (PostHog /i/v1/traces).
// No endpoint means a no-op provider. Health and ready are not traced.
package otelsetup

import (
	"context"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Start installs a tracer provider. No endpoint: no-op, not an error.
func Start(ctx context.Context) (func(context.Context) error, error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")) == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if name == "" {
		name = "veil"
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(semconv.ServiceName(name)),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Handler traces the mux. GET /health and /ready are skipped. After the
// route serves, url.path is overwritten with the matched mux pattern and
// client.address is blanked — raw paths carry item/agent/member names and
// client IPs, neither of which belongs in the trace sink.
func Handler(h http.Handler) http.Handler {
	scrub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		span := trace.SpanFromContext(r.Context())
		if p := r.Pattern; p != "" {
			span.SetAttributes(
				semconv.URLPath(p),
				semconv.HTTPRoute(p),
			)
		} else {
			span.SetAttributes(semconv.URLPath(""))
		}
		span.SetAttributes(semconv.ClientAddress(""))
	})
	return otelhttp.NewHandler(scrub, "veil", otelhttp.WithFilter(func(r *http.Request) bool {
		switch r.URL.Path {
		case "/health", "/ready":
			return false
		default:
			return true
		}
	}))
}
