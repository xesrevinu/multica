package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// SetupTest installs an in-memory tracer for the current test and restores
// the process no-op provider on cleanup.
func SetupTest(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	setProvider(tp, func(ctx context.Context) error { return tp.Shutdown(ctx) })
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		setProvider(nil, nil)
		taskContexts.Range(func(key, _ any) bool {
			taskContexts.Delete(key)
			return true
		})
	})
	return exp
}
