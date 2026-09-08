package tracing

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

func TestStartTriggerQueryableByKindAndRunID(t *testing.T) {
	exp := SetupTest(t)
	ctx, span := StartTrigger(context.Background(), "scheduler", "exec-1", "autopilot_schedule_dispatch", "global/global")
	EndTrigger(span, nil)
	_ = ctx

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Name != spanTrigger {
		t.Fatalf("name = %q", spans[0].Name)
	}
	got := map[string]string{}
	for _, a := range spans[0].Attributes {
		got[string(a.Key)] = a.Value.AsString()
	}
	if got[attrTriggerKind] != "scheduler" || got[attrTriggerRunID] != "exec-1" || got[attrTriggerJob] != "autopilot_schedule_dispatch" {
		t.Fatalf("attrs = %#v", got)
	}
}

func TestEndTriggerRecordsError(t *testing.T) {
	exp := SetupTest(t)
	_, span := StartTrigger(context.Background(), "webhook", "delivery-1", "", "")
	EndTrigger(span, errors.New("boom"))
	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatalf("spans=%d status=%v", len(spans), spans[0].Status)
	}
}

func TestWrapRPCEmitsWebsocketTransport(t *testing.T) {
	exp := SetupTest(t)
	ctx, span := WrapRPC(context.Background(), "tasks.claim")
	EndRPC(span, 200)
	_ = ctx
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Name != spanRPCClaim {
		t.Fatalf("name = %q", spans[0].Name)
	}
	got := map[string]string{}
	for _, a := range spans[0].Attributes {
		got[string(a.Key)] = a.Value.Emit()
	}
	if got[attrRPCMethod] != "tasks.claim" || got[attrRPCTransport] != "websocket" {
		t.Fatalf("attrs = %#v", got)
	}
}
