package tracing

import (
	"context"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceparentForTask returns the W3C traceparent for the in-process span
// bound to taskID, or empty when tracing is off or the task was never bound.
func TraceparentForTask(taskID string) string {
	sc, ok := lookupTask(taskID)
	if !ok {
		return ""
	}
	h := make(http.Header)
	otel.GetTextMapPropagator().Inject(
		trace.ContextWithRemoteSpanContext(context.Background(), sc),
		propagation.HeaderCarrier(h),
	)
	return h.Get("Traceparent")
}

// ContextFromTraceparent attaches a remote parent from a W3C traceparent.
func ContextFromTraceparent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	h := make(http.Header)
	h.Set("Traceparent", traceparent)
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(h))
}

// InjectHTTP writes W3C traceparent/tracestate onto outbound request headers.
func InjectHTTP(ctx context.Context, header http.Header) {
	if header == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
	if header.Get("Traceparent") == "" {
		if tp := os.Getenv("TRACEPARENT"); tp != "" {
			header.Set("Traceparent", tp)
		}
		if ts := os.Getenv("TRACESTATE"); ts != "" {
			header.Set("Tracestate", ts)
		}
	}
}

// InjectEnv writes W3C context into a child-process env map so OpenCode can
// continue the same trace. Also maps MULTICA_OTEL_ENDPOINT onto the standard
// OTLP env if the child does not already have one.
func InjectEnv(ctx context.Context, env map[string]string) {
	if env == nil {
		return
	}
	h := make(http.Header)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(h))
	if tp := h.Get("Traceparent"); tp != "" {
		env["TRACEPARENT"] = tp
	}
	if ts := h.Get("Tracestate"); ts != "" {
		env["TRACESTATE"] = ts
	}
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] == "" && os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		if ep := os.Getenv("MULTICA_OTEL_ENDPOINT"); ep != "" {
			env["OTEL_EXPORTER_OTLP_ENDPOINT"] = ep
		}
	}
}

// StartDaemonTask starts the daemon-side run span. When ctx already carries a
// remote parent (claim traceparent), this is a child of that agent-run trace.
func StartDaemonTask(ctx context.Context, attrs TaskAttrs) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(taskAttributes(attrs)...),
	}
	if !trace.SpanFromContext(ctx).SpanContext().IsValid() {
		opts = append(opts, trace.WithNewRoot())
	}
	return tracer().Start(ctx, spanDaemonTask, opts...)
}
