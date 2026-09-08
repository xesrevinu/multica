package tracing

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TaskAttrs is the allowlisted identity of an agent-task span. Callers pass
// IDs they already have; this package never loads a task row.
type TaskAttrs struct {
	TaskID         string
	AgentID        string
	IssueID        string
	RuntimeID      string
	AutopilotRunID string
	EvidenceKind   string
	FailureReason  string
}

var taskContexts sync.Map // taskID -> trace.SpanContext

func bindTask(taskID string, sc trace.SpanContext) {
	if taskID == "" || !sc.IsValid() {
		return
	}
	taskContexts.Store(taskID, sc)
}

func lookupTask(taskID string) (trace.SpanContext, bool) {
	if taskID == "" {
		return trace.SpanContext{}, false
	}
	v, ok := taskContexts.Load(taskID)
	if !ok {
		return trace.SpanContext{}, false
	}
	sc, ok := v.(trace.SpanContext)
	return sc, ok && sc.IsValid()
}

// ForgetTask drops the in-process span context for a terminal task.
func ForgetTask(taskID string) {
	if taskID == "" {
		return
	}
	taskContexts.Delete(taskID)
}

func RecordTaskEnqueued(ctx context.Context, attrs TaskAttrs) {
	recordTaskStage(ctx, spanTaskEnqueue, attrs, false)
}

func RecordTaskClaimed(ctx context.Context, attrs TaskAttrs) {
	recordTaskStage(ctx, spanTaskClaim, attrs, false)
}

func RecordTaskStarted(ctx context.Context, attrs TaskAttrs) {
	recordTaskStage(ctx, spanTaskStart, attrs, false)
}

func RecordTaskCompleted(ctx context.Context, attrs TaskAttrs) {
	recordTaskStage(ctx, spanTaskComplete, attrs, false)
	ForgetTask(attrs.TaskID)
}

func RecordTaskFailed(ctx context.Context, attrs TaskAttrs) {
	recordTaskStage(ctx, spanTaskFail, attrs, true)
	ForgetTask(attrs.TaskID)
}

func RecordTaskCancelled(ctx context.Context, attrs TaskAttrs) {
	recordTaskStage(ctx, spanTaskCancel, attrs, false)
	ForgetTask(attrs.TaskID)
}

func recordTaskStage(ctx context.Context, name string, attrs TaskAttrs, failed bool) {
	parent, bound := lookupTask(attrs.TaskID)
	startCtx := ctx
	opts := []trace.SpanStartOption{
		trace.WithAttributes(taskAttributes(attrs)...),
	}
	if current := trace.SpanFromContext(ctx); current.SpanContext().IsValid() {
		opts = append(opts, trace.WithLinks(trace.LinkFromContext(ctx)))
	}
	if bound {
		startCtx = trace.ContextWithRemoteSpanContext(ctx, parent)
	} else {
		opts = append(opts, trace.WithNewRoot())
	}

	_, span := tracer().Start(startCtx, name, opts...)
	if failed {
		reason := attrs.FailureReason
		if reason == "" {
			reason = "failed"
		}
		span.SetStatus(codes.Error, reason)
	}
	bindTask(attrs.TaskID, span.SpanContext())
	span.End()
}

func taskAttributes(attrs TaskAttrs) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 6)
	if attrs.TaskID != "" {
		out = append(out, attribute.String(attrTaskID, attrs.TaskID))
	}
	if attrs.AgentID != "" {
		out = append(out, attribute.String(attrTaskAgentID, attrs.AgentID))
	}
	if attrs.IssueID != "" {
		out = append(out, attribute.String(attrTaskIssueID, attrs.IssueID))
	}
	if attrs.RuntimeID != "" {
		out = append(out, attribute.String(attrTaskRuntimeID, attrs.RuntimeID))
	}
	if attrs.AutopilotRunID != "" {
		out = append(out, attribute.String(attrTaskAutopilot, attrs.AutopilotRunID))
	}
	if attrs.EvidenceKind != "" {
		out = append(out, attribute.String(attrTaskEvidence, attrs.EvidenceKind))
	}
	return out
}
