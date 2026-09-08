package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

func TestTaskLifecycleSameTrace(t *testing.T) {
	exp := SetupTest(t)
	ctx := context.Background()
	attrs := TaskAttrs{TaskID: "task-1", AgentID: "agent-1", IssueID: "issue-1"}

	RecordTaskEnqueued(ctx, attrs)
	RecordTaskClaimed(ctx, attrs)
	RecordTaskStarted(ctx, attrs)
	RecordTaskCompleted(ctx, attrs)

	spans := exp.GetSpans()
	if len(spans) != 4 {
		t.Fatalf("spans = %d, want 4", len(spans))
	}
	traceID := spans[0].SpanContext.TraceID()
	if !traceID.IsValid() {
		t.Fatal("enqueue trace id is invalid")
	}
	want := map[string]bool{
		spanTaskEnqueue:  false,
		spanTaskClaim:    false,
		spanTaskStart:    false,
		spanTaskComplete: false,
	}
	for _, span := range spans {
		if span.SpanContext.TraceID() != traceID {
			t.Fatalf("span %q trace = %s, want %s", span.Name, span.SpanContext.TraceID(), traceID)
		}
		if _, ok := want[span.Name]; !ok {
			t.Fatalf("unexpected span %q", span.Name)
		}
		want[span.Name] = true
		foundTask := false
		for _, a := range span.Attributes {
			if string(a.Key) == attrTaskID && a.Value.AsString() == "task-1" {
				foundTask = true
			}
		}
		if !foundTask {
			t.Fatalf("span %q missing task id", span.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing span %q", name)
		}
	}

	exp.Reset()
	RecordTaskClaimed(ctx, attrs)
	late := exp.GetSpans()
	if len(late) != 1 {
		t.Fatalf("post-forget claim spans = %d, want 1", len(late))
	}
	if late[0].SpanContext.TraceID() == traceID {
		t.Fatal("claim after complete must start a new trace")
	}
}

func TestTaskFailedMarksErrorAndForgets(t *testing.T) {
	exp := SetupTest(t)
	attrs := TaskAttrs{TaskID: "task-fail", FailureReason: "timeout"}
	RecordTaskEnqueued(context.Background(), attrs)
	RecordTaskFailed(context.Background(), attrs)

	var fail *struct {
		code codes.Code
	}
	for i := range exp.GetSpans() {
		span := exp.GetSpans()[i]
		if span.Name == spanTaskFail {
			fail = &struct{ code codes.Code }{span.Status.Code}
			if span.Status.Description != "timeout" {
				t.Fatalf("fail description = %q", span.Status.Description)
			}
		}
	}
	if fail == nil {
		t.Fatal("missing fail span")
	}
	if fail.code != codes.Error {
		t.Fatalf("fail status = %v", fail.code)
	}
}

func TestDisabledTaskStagesEmitNothing(t *testing.T) {
	setProvider(nil, nil)
	RecordTaskEnqueued(context.Background(), TaskAttrs{TaskID: "task-off"})
}
