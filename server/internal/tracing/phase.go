package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	PhasePrepare = spanDaemonPrepare
	PhaseLaunch  = spanDaemonLaunch
	PhaseDrain   = spanDaemonDrain
)

func StartPhase(ctx context.Context, name string) (context.Context, trace.Span) {
	return tracer().Start(ctx, name)
}

func EndPhase(span trace.Span, err error) {
	EndTrigger(span, err)
}

func SetSpanString(span trace.Span, key, value string) {
	if span == nil || value == "" {
		return
	}
	span.SetAttributes(attribute.String(key, value))
}

func EndDrain(span trace.Span, status string, err error, toolCount int32) {
	if span == nil {
		return
	}
	if toolCount > 0 {
		span.SetAttributes(attribute.Int64(attrDaemonToolCount, int64(toolCount)))
	}
	if err != nil {
		EndTrigger(span, err)
		return
	}
	if status != "" && status != "completed" {
		span.SetStatus(codes.Error, status)
	}
	span.End()
}

func ProviderAttrKey() string { return attrDaemonProvider }
