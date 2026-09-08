package tracing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPMiddlewareSkipsHealth(t *testing.T) {
	exp := SetupTest(t)
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	for _, path := range []string{"/health", "/readyz", "/healthz"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
	}
	if spans := exp.GetSpans(); len(spans) != 0 {
		t.Fatalf("health probes emitted %d spans", len(spans))
	}
}

func TestHTTPMiddlewareUsesRoutePatternNotRawPath(t *testing.T) {
	exp := SetupTest(t)
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Post("/api/webhooks/autopilots/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/autopilots/awt_SECRET?token=secret-token", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	r.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "POST /api/webhooks/autopilots/{token}" {
		t.Fatalf("span name = %q", span.Name)
	}
	dump := spanDump(span)
	for _, leaked := range []string{"awt_SECRET", "secret-token", "Bearer"} {
		if strings.Contains(dump, leaked) {
			t.Fatalf("span leaked %q: %s", leaked, dump)
		}
	}
}

func TestHTTPMiddlewareExtractsTraceparent(t *testing.T) {
	exp := SetupTest(t)

	traceID := mustTraceID("0123456789abcdef0123456789abcdef")
	spanID := mustSpanID("0123456789abcdef")
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	header := make(http.Header)
	otel.GetTextMapPropagator().Inject(
		trace.ContextWithRemoteSpanContext(t.Context(), parent),
		propagation.HeaderCarrier(header),
	)

	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/api/issues/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/issues/abc", nil)
	req.Header = header
	r.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].SpanContext.TraceID() != traceID {
		t.Fatalf("trace id = %s, want parent %s", spans[0].SpanContext.TraceID(), traceID)
	}
	if spans[0].Parent.SpanID() != spanID {
		t.Fatalf("parent span = %s, want %s", spans[0].Parent.SpanID(), spanID)
	}
}

func TestHTTPMiddlewareMarks5xxAsError(t *testing.T) {
	exp := SetupTest(t)
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Fatalf("status = %v, want error", spans[0].Status.Code)
	}
}

func spanDump(span tracetest.SpanStub) string {
	var b strings.Builder
	b.WriteString(span.Name)
	for _, a := range span.Attributes {
		b.WriteString(string(a.Key))
		b.WriteString("=")
		b.WriteString(a.Value.Emit())
		b.WriteString(";")
	}
	return b.String()
}

func mustTraceID(hex string) trace.TraceID {
	id, err := trace.TraceIDFromHex(hex)
	if err != nil {
		panic(err)
	}
	return id
}

func mustSpanID(hex string) trace.SpanID {
	id, err := trace.SpanIDFromHex(hex)
	if err != nil {
		panic(err)
	}
	return id
}
