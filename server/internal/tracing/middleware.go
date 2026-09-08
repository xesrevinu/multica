package tracing

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Middleware starts a server span for each request. Health probes are skipped.
// Span names use the Chi route pattern so webhook tokens in the path never
// become span names or attributes.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthProbePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer().Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(semconv.HTTPRequestMethodKey.String(r.Method)),
		)
		defer span.End()

		if rid := chimw.GetReqID(ctx); rid != "" {
			span.SetAttributes(attribute.String(attrRequestID, rid))
		}

		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		route := routePattern(r)
		span.SetName(r.Method + " " + route)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		span.SetAttributes(
			semconv.HTTPRoute(route),
			semconv.HTTPResponseStatusCode(status),
		)
		if status >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("http %d", status))
		}
	})
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

func isHealthProbePath(path string) bool {
	switch path {
	case "/health", "/healthz", "/readyz":
		return true
	default:
		return false
	}
}
