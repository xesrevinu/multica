package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/tracing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	webhookWorkerPollInterval = time.Second
	webhookWorkerMaxAttempts  = 5
	webhookWorkerConcurrency  = 4
)

// WebhookDeliveryWorker owns queued webhook delivery dispatch. Its queue and
// lease both live in Postgres, so a process restart or replica failover simply
// reclaims expired rows; the in-memory notification is only a latency hint.
type WebhookDeliveryWorker struct {
	h      *Handler
	notify chan struct{}
	done   chan struct{}
}

func NewWebhookDeliveryWorker(h *Handler) *WebhookDeliveryWorker {
	return &WebhookDeliveryWorker{
		h:      h,
		notify: make(chan struct{}, webhookWorkerConcurrency),
		done:   make(chan struct{}),
	}
}

func (w *WebhookDeliveryWorker) Notify() {
	if w == nil {
		return
	}
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// Run owns a bounded per-process worker pool. SKIP LOCKED distributes queued
// deliveries across both these workers and other server replicas. ProcessNext
// is public to the package so handler tests can drive the durable queue
// synchronously without starting a goroutine.
func (w *WebhookDeliveryWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	defer close(w.done)
	if w.h == nil || w.h.Queries == nil {
		return
	}

	var workers sync.WaitGroup
	workers.Add(webhookWorkerConcurrency)
	for range webhookWorkerConcurrency {
		go func() {
			defer workers.Done()
			w.runLoop(ctx)
		}()
	}
	workers.Wait()
}

// runLoop polls as a recovery sweeper and also wakes immediately after local
// ingress. Each loop has its own ticker so an idle pool can fan back out even
// when burst notifications coalesce.
func (w *WebhookDeliveryWorker) runLoop(ctx context.Context) {
	ticker := time.NewTicker(webhookWorkerPollInterval)
	defer ticker.Stop()
	for {
		worked, err := w.ProcessNext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("webhook worker: process delivery", "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-w.notify:
		case <-ticker.C:
		}
	}
}

func (w *WebhookDeliveryWorker) WaitWithTimeout(timeout time.Duration) bool {
	if w == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return true
	case <-timer.C:
		return false
	}
}

func (w *WebhookDeliveryWorker) ProcessNext(ctx context.Context) (bool, error) {
	delivery, err := w.h.Queries.ClaimQueuedWebhookDelivery(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim queued delivery: %w", err)
	}

	ctx, triggerSpan := tracing.StartTrigger(ctx, "webhook", uuidToString(delivery.ID), "", "")
	defer triggerSpan.End()

	if w.h.WebhookRateLimiter != nil {
		key := uuidToString(delivery.TriggerID)
		if !w.h.WebhookRateLimiter.Allow(ctx, key) {
			w.h.Metrics.RecordWebhookRateLimited("worker_trigger")
			retryAfter := slidingWindowLimiterRetryAfter(ctx, w.h.WebhookRateLimiter, key)
			if retryAfter <= 0 {
				retryAfter = time.Second
			}
			_, err := w.h.Queries.DeferClaimedWebhookDelivery(ctx, db.DeferClaimedWebhookDeliveryParams{
				ID:          delivery.ID,
				LeaseToken:  delivery.LeaseToken,
				AvailableAt: time.Now().Add(retryAfter).UTC().Format(time.RFC3339Nano),
			})
			_, err = handleWebhookLeaseMutation("defer", delivery, err)
			return true, err
		}
	}

	trigger, err := w.h.Queries.GetAutopilotTrigger(ctx, delivery.TriggerID)
	if err != nil {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("load trigger: %w", err))
	}
	autopilot, err := w.h.Queries.GetAutopilot(ctx, delivery.AutopilotID)
	if err != nil {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("load autopilot: %w", err))
	}
	if uuidToString(trigger.AutopilotID) != uuidToString(delivery.AutopilotID) ||
		uuidToString(autopilot.WorkspaceID) != uuidToString(delivery.WorkspaceID) {
		return true, w.complete(ctx, delivery, deliveryStatusFailed, "", "delivery ownership mismatch")
	}

	headers := headersFromSelected([]byte(delivery.SelectedHeaders))
	if delivery.ContentType.Valid {
		headers.Set("Content-Type", delivery.ContentType.String)
	}
	envelope, err := normalizeWebhookPayload(delivery.RawBody, headers)
	if err != nil {
		return true, w.complete(ctx, delivery, deliveryStatusFailed, "", "normalize stored body: "+err.Error())
	}
	if delivery.ReceivedAt != "" {
		envelope.Request.ReceivedAt = parseDBTime(delivery.ReceivedAt).UTC().Format(time.RFC3339)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("encode envelope: %w", err))
	}

	// Once ingress has synchronously admitted a run and returned accepted or
	// skipped, that decision is durable. Re-check mutable trigger/autopilot
	// state only for deliveries recovered from the pre-admission crash window;
	// otherwise a pause immediately after the response could strand the run.
	_, admissionErr := w.h.Queries.GetAutopilotRunByWebhookDelivery(ctx, nullString(delivery.ID))
	hasAdmittedRun := admissionErr == nil
	if admissionErr != nil && !errors.Is(admissionErr, sql.ErrNoRows) {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("load admitted run: %w", admissionErr))
	}
	if !hasAdmittedRun {
		switch {
		case trigger.Enabled == 0:
			return true, w.complete(ctx, delivery, deliveryStatusIgnored, "", "trigger_disabled")
		case autopilot.Status == "archived":
			return true, w.complete(ctx, delivery, deliveryStatusIgnored, "", "autopilot_archived")
		case autopilot.Status != "active":
			return true, w.complete(ctx, delivery, deliveryStatusIgnored, "", "autopilot_paused")
		case !webhookEventAllowedByTriggerScope([]byte(trigger.EventFilters.String), envelope):
			return true, w.complete(ctx, delivery, deliveryStatusIgnored, "", "event_filtered")
		}
	}

	run, dispatchErr := w.h.AutopilotService.DispatchAutopilotForWebhookDelivery(
		ctx,
		autopilot, trigger.ID, payload, delivery.ID,
	)
	if dispatchErr != nil {
		var quotaErr *service.AutopilotQuotaExceededError
		if errors.As(dispatchErr, &quotaErr) {
			return true, w.complete(ctx, delivery, deliveryStatusIgnored, "", quotaErr.Error(), "quota_exceeded")
		}
		if run != nil {
			return true, w.complete(ctx, delivery, deliveryStatusFailed, run.ID, dispatchErr.Error())
		}
		return true, w.retryOrFail(ctx, delivery, dispatchErr)
	}
	if run.Status == "failed" {
		reason := "autopilot run failed"
		if run.FailureReason.Valid {
			reason = run.FailureReason.String
		}
		return true, w.complete(ctx, delivery, deliveryStatusFailed, run.ID, reason)
	}

	if err := w.h.Queries.TouchAutopilotTriggerFiredAt(ctx, trigger.ID); err != nil {
		slog.Warn("webhook worker: touch last_fired_at",
			"delivery_id", uuidToString(delivery.ID),
			"trigger_id", uuidToString(trigger.ID),
			"error", err,
		)
	}
	return true, w.complete(ctx, delivery, deliveryStatusDispatched, run.ID, "")
}

func (w *WebhookDeliveryWorker) complete(
	ctx context.Context,
	delivery db.WebhookDelivery,
	status string,
	runID string,
	reason string,
	reasonCode ...string,
) error {
	params := db.CompleteClaimedWebhookDeliveryParams{
		ID:             delivery.ID,
		LeaseToken:     delivery.LeaseToken,
		Status:         status,
		AutopilotRunID: nullString(runID),
	}
	if reason != "" {
		params.Error = sql.NullString{String: reason, Valid: true}
	}
	if len(reasonCode) > 0 && reasonCode[0] != "" {
		params.ReasonCode = sql.NullString{String: reasonCode[0], Valid: true}
	}
	_, err := w.h.Queries.CompleteClaimedWebhookDelivery(ctx, params)
	lost, err := handleWebhookLeaseMutation("complete", delivery, err)
	if err != nil {
		return err
	}
	if lost {
		return nil
	}
	w.h.Metrics.RecordWebhookDelivery(delivery.Provider, status)
	return nil
}

func (w *WebhookDeliveryWorker) retryOrFail(ctx context.Context, delivery db.WebhookDelivery, cause error) error {
	if delivery.DispatchAttempts+1 >= webhookWorkerMaxAttempts {
		return w.complete(ctx, delivery, deliveryStatusFailed, "", cause.Error())
	}
	backoff := time.Second * time.Duration(1<<min(delivery.DispatchAttempts, 6))
	_, err := w.h.Queries.RetryClaimedWebhookDelivery(ctx, db.RetryClaimedWebhookDeliveryParams{
		ID:          delivery.ID,
		LeaseToken:  delivery.LeaseToken,
		AvailableAt: time.Now().Add(backoff).UTC().Format(time.RFC3339Nano),
		Error:       sql.NullString{String: cause.Error(), Valid: true},
	})
	lost, mutationErr := handleWebhookLeaseMutation("retry", delivery, err)
	if mutationErr != nil {
		return fmt.Errorf("retry claimed delivery after %v: %w", cause, mutationErr)
	}
	if lost {
		return nil
	}
	slog.Warn("webhook worker: delivery deferred",
		"delivery_id", uuidToString(delivery.ID),
		"attempt", delivery.DispatchAttempts+1,
		"backoff", backoff,
		"error", cause,
	)
	return nil
}

// handleWebhookLeaseMutation normalizes the expected race where a slow worker
// outlives its lease and a new owner has already claimed or completed the row.
// The new owner is responsible for the terminal transition, so the stale
// worker must neither emit an error nor record completion metrics.
func handleWebhookLeaseMutation(operation string, delivery db.WebhookDelivery, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		slog.Debug("webhook worker: lease ownership changed",
			"operation", operation,
			"delivery_id", uuidToString(delivery.ID),
		)
		return true, nil
	}
	return false, fmt.Errorf("%s claimed delivery: %w", operation, err)
}
