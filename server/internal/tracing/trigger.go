package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StartTrigger opens a new-root span for an internal job (scheduler, webhook
// worker, sweeper). The caller must End the span.
func StartTrigger(ctx context.Context, kind, runID, job, scope string) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{attribute.String(attrTriggerKind, kind)}
	if runID != "" {
		attrs = append(attrs, attribute.String(attrTriggerRunID, runID))
	}
	if job != "" {
		attrs = append(attrs, attribute.String(attrTriggerJob, job))
	}
	if scope != "" {
		attrs = append(attrs, attribute.String(attrTriggerScope, scope))
	}
	return tracer().Start(ctx, spanTrigger, trace.WithNewRoot(), trace.WithAttributes(attrs...))
}

func EndTrigger(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
