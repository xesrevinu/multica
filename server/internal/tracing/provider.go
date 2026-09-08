package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

var (
	providerMu sync.RWMutex
	provider   trace.TracerProvider
	shutdown   func(context.Context) error
)

func tracer() trace.Tracer {
	return currentProvider().Tracer("multica")
}

func currentProvider() trace.TracerProvider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	if provider != nil {
		return provider
	}
	return noop.NewTracerProvider()
}

func setProvider(tp trace.TracerProvider, stop func(context.Context) error) {
	providerMu.Lock()
	defer providerMu.Unlock()
	provider = tp
	shutdown = stop
}

// Setup installs the OTLP/HTTP tracer when cfg.Endpoint is set. Compression
// is forced off so the payload is uncompressed protobuf.
func Setup(ctx context.Context, cfg Config, version string) (func(context.Context) error, error) {
	if !cfg.Enabled() {
		setProvider(nil, nil)
		return func(context.Context) error { return nil }, nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(tracesURL(cfg.Endpoint)),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp http exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		_ = exp.Shutdown(ctx)
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Warn("otel exporter error", "error", err)
	}))

	stop := func(ctx context.Context) error {
		flushErr := tp.ForceFlush(ctx)
		shutErr := tp.Shutdown(ctx)
		setProvider(nil, nil)
		if flushErr != nil {
			return flushErr
		}
		return shutErr
	}
	setProvider(tp, stop)
	slog.Info("tracing enabled", "endpoint", tracesURL(cfg.Endpoint), "service", cfg.ServiceName)
	return stop, nil
}

// Shutdown flushes and tears down the provider installed by Setup.
func Shutdown(ctx context.Context) error {
	providerMu.RLock()
	stop := shutdown
	providerMu.RUnlock()
	if stop == nil {
		return nil
	}
	return stop(ctx)
}

func tracesURL(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" {
		return endpoint
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/v1/traces"
	}
	return u.String()
}
