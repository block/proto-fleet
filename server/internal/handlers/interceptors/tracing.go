package interceptors

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

// TracingMiddleware creates a span for every HTTP request and records the response status code.
func TracingMiddleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.request",
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return httpSpanName(r)
		}),
	)
}

func httpSpanName(r *http.Request) string {
	if strings.HasPrefix(r.Pattern, r.Method+" ") {
		return r.Pattern
	}
	if r.Pattern == "" {
		return r.Method
	}
	return r.Method + " " + r.Pattern
}
