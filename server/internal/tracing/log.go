package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// LogAttrs returns slog key/value pairs for the current span, or nil when
// the context has no valid span. Safe to call when tracing is disabled.
func LogAttrs(ctx context.Context) []any {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return nil
	}
	return []any{"trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()}
}
