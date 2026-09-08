package tracing

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

func TestPrepareLaunchDrainNestUnderParent(t *testing.T) {
	exp := SetupTest(t)
	ctx, parent := StartDaemonTask(context.Background(), TaskAttrs{TaskID: "t-phase"})
	ctx, prepare := StartPhase(ctx, PhasePrepare)
	EndPhase(prepare, nil)
	ctx, launch := StartPhase(ctx, PhaseLaunch)
	EndPhase(launch, nil)
	_, drain := StartPhase(ctx, PhaseDrain)
	EndDrain(drain, "completed", nil, 3)
	parent.End()

	root := ""
	toolCount := int64(0)
	names := map[string]string{}
	for _, s := range exp.GetSpans() {
		id := s.SpanContext.TraceID().String()
		names[s.Name] = id
		if s.Name == spanDaemonTask {
			root = id
		}
		if s.Name == PhaseDrain {
			for _, a := range s.Attributes {
				if string(a.Key) == attrDaemonToolCount {
					toolCount = a.Value.AsInt64()
				}
			}
		}
	}
	if root == "" {
		t.Fatal("missing parent span")
	}
	for _, name := range []string{PhasePrepare, PhaseLaunch, PhaseDrain} {
		if names[name] != root {
			t.Fatalf("%s trace = %s, want %s", name, names[name], root)
		}
	}
	if toolCount != 3 {
		t.Fatalf("tool_count = %d", toolCount)
	}
}

func TestEndDrainMarksNonCompleted(t *testing.T) {
	exp := SetupTest(t)
	_, span := StartPhase(context.Background(), PhaseDrain)
	EndDrain(span, "idle_watchdog", nil, 0)
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error || spans[0].Status.Description != "idle_watchdog" {
		t.Fatalf("status = %+v", spans[0].Status)
	}
}

func TestEndPhaseRecordsError(t *testing.T) {
	exp := SetupTest(t)
	_, span := StartPhase(context.Background(), PhasePrepare)
	EndPhase(span, errors.New("disk full"))
	if exp.GetSpans()[0].Status.Description != "disk full" {
		t.Fatalf("status = %q", exp.GetSpans()[0].Status.Description)
	}
}
