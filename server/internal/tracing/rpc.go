package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// WrapRPC starts a server span for an in-process daemon WS RPC that never
// passes through Chi middleware (tasks.claim).
func WrapRPC(ctx context.Context, method string) (context.Context, trace.Span) {
	return tracer().Start(ctx, spanRPCClaim, trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String(attrRPCMethod, method),
			attribute.String(attrRPCTransport, "websocket"),
		),
	)
}

func EndRPC(span trace.Span, status int) {
	if span == nil {
		return
	}
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= 500 {
		span.SetStatus(codes.Error, fmt.Sprintf("rpc %d", status))
	}
	span.End()
}
