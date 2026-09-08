package tracing

import (
	"context"
	"net/http"
	"testing"
)

func TestTraceparentRoundTripSameTrace(t *testing.T) {
	exp := SetupTest(t)
	attrs := TaskAttrs{TaskID: "task-prop"}
	RecordTaskEnqueued(context.Background(), attrs)
	tp := TraceparentForTask("task-prop")
	if tp == "" {
		t.Fatal("expected traceparent after enqueue")
	}

	ctx := ContextFromTraceparent(context.Background(), tp)
	ctx, span := StartDaemonTask(ctx, attrs)
	span.End()

	var enqueue, daemon *struct{ trace, name string }
	for _, s := range exp.GetSpans() {
		id := s.SpanContext.TraceID().String()
		switch s.Name {
		case spanTaskEnqueue:
			enqueue = &struct{ trace, name string }{id, s.Name}
		case spanDaemonTask:
			daemon = &struct{ trace, name string }{id, s.Name}
		}
	}
	if enqueue == nil || daemon == nil {
		t.Fatalf("missing spans enqueue=%v daemon=%v", enqueue, daemon)
	}
	if enqueue.trace != daemon.trace {
		t.Fatalf("trace mismatch enqueue=%s daemon=%s", enqueue.trace, daemon.trace)
	}
}

func TestInjectHTTPWritesTraceparent(t *testing.T) {
	SetupTest(t)
	ctx, span := tracer().Start(context.Background(), "client")
	defer span.End()
	h := make(http.Header)
	InjectHTTP(ctx, h)
	if h.Get("Traceparent") == "" {
		t.Fatal("expected Traceparent header")
	}
}

func TestInjectEnvWritesTraceparent(t *testing.T) {
	SetupTest(t)
	ctx, span := StartDaemonTask(context.Background(), TaskAttrs{TaskID: "t-env"})
	defer span.End()
	env := map[string]string{}
	InjectEnv(ctx, env)
	if env["TRACEPARENT"] == "" {
		t.Fatal("expected TRACEPARENT")
	}
	if got, want := env["TRACEPARENT"][:3], "00-"; got != want {
		t.Fatalf("TRACEPARENT = %q", env["TRACEPARENT"])
	}
}

func TestTraceparentForUnknownTaskIsEmpty(t *testing.T) {
	SetupTest(t)
	if got := TraceparentForTask("missing"); got != "" {
		t.Fatalf("got %q", got)
	}
}
